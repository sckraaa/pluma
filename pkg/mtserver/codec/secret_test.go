package codec

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// bufReader is a CodecReader over the bytes received so far.
type bufReader struct{ buf []byte }

func (r *bufReader) Peek(n int) ([]byte, error) {
	if n < 0 || n > len(r.buf) {
		return r.buf, nil
	}
	return r.buf[:n], nil
}

func (r *bufReader) Discard(n int) (int, error) {
	if n > len(r.buf) {
		n = len(r.buf)
	}
	r.buf = r.buf[n:]
	return n, nil
}

// obfClient is the client side of an obfuscated connection, written after
// tdesktop's TcpConnection::Protocol (connection_tcp.cpp:160-165, 458-491)
// with the standard library's AES-CTR, so that it shares no code with the
// server side under test.
type obfClient struct {
	send, recv cipher.Stream
}

// newObfClient builds the 64-byte header for transport tag and dc id; with
// secret (16 bytes) the keys are SHA256(key + secret).
func newObfClient(t *testing.T, secret []byte, tag uint32, dc int16) (*obfClient, []byte) {
	t.Helper()
	// As the clients do, a header that could be read as another transport
	// is drawn again (Android Connection.cpp:524-527).
	nonce := make([]byte, 64)
	for {
		if _, err := rand.Read(nonce); err != nil {
			t.Fatal(err)
		}
		first := binary.LittleEndian.Uint32(nonce)
		if nonce[0] != ABRIDGED_FLAG && binary.LittleEndian.Uint32(nonce[4:]) != 0 &&
			!slices.Contains([]uint32{HTTP_HEAD_FLAG, HTTP_POST_FLAG, HTTP_GET_FLAG, HTTP_OPTION_FLAG,
				INTERMEDIATE_FLAG, PADDED_INTERMEDIATE_FLAG, UNKNOWN_FLAG, PVRG_FLAG}, first) {
			break
		}
	}
	prepareKey := func(src []byte) []byte {
		if secret == nil {
			return slices.Clone(src)
		}
		h := sha256.Sum256(append(slices.Clone(src), secret...))
		return h[:]
	}
	stream := func(key, iv []byte) cipher.Stream {
		b, err := aes.NewCipher(key)
		if err != nil {
			t.Fatal(err)
		}
		return cipher.NewCTR(b, iv)
	}
	binary.LittleEndian.PutUint32(nonce[56:], tag)
	binary.LittleEndian.PutUint16(nonce[60:], uint16(dc))

	reversed := slices.Clone(nonce[8:56])
	slices.Reverse(reversed)
	c := &obfClient{
		send: stream(prepareKey(nonce[8:40]), nonce[40:56]),
		recv: stream(prepareKey(reversed[:32]), reversed[32:48]),
	}
	encrypted := make([]byte, 64)
	c.send.XORKeyStream(encrypted, nonce)
	header := append(slices.Clone(nonce[:56]), encrypted[56:]...)
	return c, header
}

// paddedPacket frames payload for the padded intermediate transport with
// pad bytes of padding counted in the length, as the clients send it.
func (c *obfClient) paddedPacket(payload []byte, pad int) []byte {
	p := make([]byte, 4+len(payload)+pad)
	binary.LittleEndian.PutUint32(p, uint32(len(payload)+pad))
	copy(p[4:], payload)
	_, _ = rand.Read(p[4+len(payload):])
	c.send.XORKeyStream(p, p)
	return p
}

// readPadded decrypts one padded intermediate packet the server sent.
func (c *obfClient) readPadded(t *testing.T, b []byte) []byte {
	t.Helper()
	plain := make([]byte, len(b))
	c.recv.XORKeyStream(plain, b)
	n := binary.LittleEndian.Uint32(plain)
	if int(n) != len(plain)-4 {
		t.Fatalf("packet length %d of %d bytes", n, len(plain)-4)
	}
	return plain[4:]
}

func testSecret(t *testing.T) Secret {
	t.Helper()
	var s Secret
	if _, err := rand.Read(s[:]); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestSecretRoundTrip: a client with the secret is understood both ways,
// whatever padding it adds, and the dc id it asked for is kept.
func TestSecretRoundTrip(t *testing.T) {
	secret := testSecret(t)
	for _, dc := range []int16{201, -201} {
		client, header := newObfClient(t, secret[:], PADDED_INTERMEDIATE_FLAG, dc)
		payload := bytes.Repeat([]byte{0x5a, 0x01, 0x02, 0x03}, 10)
		r := &bufReader{buf: append(header, client.paddedPacket(payload, 7)...)}

		c, err := CreateSecretCodec(r, secret)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.(*ObfuscatedCodec).DcId(); got != dc {
			t.Fatalf("dc %d, want %d", got, dc)
		}
		// The first packet arrived with the header; the others one by one.
		decode := func(pad int) {
			t.Helper()
			_, got, err := c.Decode(r)
			if err != nil {
				t.Fatalf("padding %d: %v", pad, err)
			}
			if len(got) != len(payload)+pad || !bytes.HasPrefix(got, payload) {
				t.Fatalf("padding %d: got %d bytes", pad, len(got))
			}
		}
		decode(7)
		for _, pad := range []int{0, 1, 2, 3, 15, 256} {
			r.buf = append(r.buf, client.paddedPacket(payload, pad)...)
			decode(pad)
		}
		if len(r.buf) != 0 {
			t.Fatalf("%d bytes left unread", len(r.buf))
		}

		answer := bytes.Repeat([]byte{0x11, 0x22, 0x33, 0x44}, 8)
		// The length counts the padding (readPadded checks it), many
		// answers in a row stay in step.
		for i := 0; i < 32; i++ {
			out, err := c.Encode(nil, &mtproto.MTPRawMessage{Payload: answer})
			if err != nil {
				t.Fatal(err)
			}
			if got := client.readPadded(t, out); !bytes.HasPrefix(got, answer) || len(got) > len(answer)+15 {
				t.Fatalf("answer decrypted to %x", got)
			}
		}

		// A transport error goes unpadded, as the clients expect.
		code := binary.LittleEndian.AppendUint32(nil, uint32(0xfffffe6c)) // -404
		out, err := c.Encode(nil, &mtproto.MTPRawMessage{Payload: code})
		if err != nil {
			t.Fatal(err)
		}
		if got := client.readPadded(t, out); !bytes.Equal(got, code) {
			t.Fatalf("transport error decrypted to %x", got)
		}
	}
}

// TestSecretRejects: with a secret, only an obfuscated header made with it
// and the padded intermediate transport is accepted; nothing is consumed
// before the whole header has arrived.
func TestSecretRejects(t *testing.T) {
	secret := testSecret(t)

	t.Run("incomplete header", func(t *testing.T) {
		_, header := newObfClient(t, secret[:], PADDED_INTERMEDIATE_FLAG, 201)
		r := &bufReader{buf: header[:63]}
		if _, err := CreateSecretCodec(r, secret); !errors.Is(err, ErrUnexpectedEOF) {
			t.Fatalf("err = %v", err)
		}
		if len(r.buf) != 63 || !bytes.Equal(r.buf, header[:63]) {
			t.Fatal("an incomplete header was consumed or changed")
		}
	})

	cases := []struct {
		name   string
		header func() []byte
	}{
		{"another secret", func() []byte {
			other := testSecret(t)
			_, h := newObfClient(t, other[:], PADDED_INTERMEDIATE_FLAG, 201)
			return h
		}},
		{"no secret", func() []byte {
			_, h := newObfClient(t, nil, PADDED_INTERMEDIATE_FLAG, 201)
			return h
		}},
		{"intermediate", func() []byte {
			_, h := newObfClient(t, secret[:], INTERMEDIATE_FLAG, 201)
			return h
		}},
		{"abridged", func() []byte {
			_, h := newObfClient(t, secret[:], ABRIDGED_INT32_FLAG, 201)
			return h
		}},
		{"dc 0", func() []byte {
			_, h := newObfClient(t, secret[:], PADDED_INTERMEDIATE_FLAG, 0)
			return h
		}},
		{"plain intermediate", func() []byte {
			h := make([]byte, 64)
			binary.LittleEndian.PutUint32(h, INTERMEDIATE_FLAG)
			return h
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A wrong key turns the tag into random bytes, which pass once
			// in 2^32.
			if _, err := CreateSecretCodec(&bufReader{buf: c.header()}, secret); !errors.Is(err, ErrProtoBadMagic) {
				t.Fatalf("err = %v, want ErrProtoBadMagic", err)
			}
		})
	}
}

// TestNoSecretUnchanged: without a secret the obfuscated transport takes
// the keys as they are and all three transports, as before; a header made
// with a secret does not pass there.
func TestNoSecretUnchanged(t *testing.T) {
	for _, tag := range []uint32{ABRIDGED_INT32_FLAG, INTERMEDIATE_FLAG, PADDED_INTERMEDIATE_FLAG} {
		client, header := newObfClient(t, nil, tag, 2)
		payload := bytes.Repeat([]byte{1, 2, 3, 4}, 4)
		var packet []byte
		if tag == ABRIDGED_INT32_FLAG {
			packet = append([]byte{byte(len(payload) / 4)}, payload...)
			client.send.XORKeyStream(packet, packet)
		} else {
			packet = client.paddedPacket(payload, 0)
		}
		r := &bufReader{buf: append(header, packet...)}
		c, err := CreateMTProtoCodec(r)
		if err != nil {
			t.Fatalf("tag %08x: %v", tag, err)
		}
		if _, got, err := c.Decode(r); err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("tag %08x: %x, %v", tag, got, err)
		}
	}

	// The gateway keeps the stricter length check of padded intermediate.
	client, header := newObfClient(t, nil, PADDED_INTERMEDIATE_FLAG, 2)
	r := &bufReader{buf: append(header, client.paddedPacket(make([]byte, 16), 3)...)}
	c, err := CreateMTProtoCodec(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.Decode(r); !errors.Is(err, ErrProtoBadLength) {
		t.Fatalf("err = %v, want ErrProtoBadLength", err)
	}

	secret := testSecret(t)
	_, header = newObfClient(t, secret[:], PADDED_INTERMEDIATE_FLAG, 2)
	if _, err = CreateMTProtoCodec(&bufReader{buf: header}); !errors.Is(err, ErrProtoBadMagic) {
		t.Fatalf("a header made with a secret, without it: %v", err)
	}
}

func TestParseSecret(t *testing.T) {
	s, err := ParseSecretHex("dd000102030405060708090a0b0c0d0e0f")
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range s {
		if b != byte(i) {
			t.Fatalf("secret %x", s)
		}
	}
	if got := s.Bytes(); len(got) != 17 || got[0] != 0xdd || !bytes.Equal(got[1:], s[:]) {
		t.Fatalf("Bytes %x", got)
	}
	if back, err := ParseSecret(s.Bytes()); err != nil || back != s {
		t.Fatalf("round trip %x %v", back, err)
	}
	for _, bad := range []string{
		"",
		"000102030405060708090a0b0c0d0e0f",   // 16 bytes, no dd
		"ee000102030405060708090a0b0c0d0e0f", // fake TLS
		"ee000102030405060708090a0b0c0d0e0f6578616d706c652e636f6d", // fake TLS with a domain
		"dd000102030405060708090a0b0c0d0e",                         // 15 bytes after dd
		"dd000102030405060708090a0b0c0d0e0f10",                     // 17 bytes after dd
		"dd000102030405060708090a0b0c0d0e0g",                       // not hex
	} {
		if _, err := ParseSecretHex(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

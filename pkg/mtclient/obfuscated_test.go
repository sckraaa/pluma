package mtclient_test

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/teamgram/proto/mtproto"

	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/mtserver/codec"
)

// memConn is a connection whose writes are collected and whose reads come
// from what the test put in.
type memConn struct {
	net.Conn
	out bytes.Buffer // written by the client
	in  bytes.Buffer // read by the client
}

func (c *memConn) Write(b []byte) (int, error) { return c.out.Write(b) }
func (c *memConn) Read(b []byte) (int, error) {
	if c.in.Len() == 0 {
		return 0, io.EOF
	}
	return c.in.Read(b)
}
func (c *memConn) Close() error { return nil }

// codecReader is the server's view of the bytes the client sent.
type codecReader struct{ buf []byte }

func (r *codecReader) Peek(n int) ([]byte, error) {
	if n < 0 || n > len(r.buf) {
		return r.buf, nil
	}
	return r.buf[:n], nil
}

// String names the connection in the codec's log lines, which print it.
func (r *codecReader) String() string { return "test connection" }

func (r *codecReader) Discard(n int) (int, error) {
	n = min(n, len(r.buf))
	r.buf = r.buf[n:]
	return n, nil
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// unencrypted is a message of the key exchange: auth_key_id 0.
func unencrypted(t *testing.T, n int) []byte {
	b := randomBytes(t, n)
	binary.LittleEndian.PutUint64(b, 0)
	return b
}

// encrypted is shaped as an encrypted message: a key id, msg_key and AES
// blocks.
func encrypted(t *testing.T, blocks int) []byte {
	b := randomBytes(t, 24+16*blocks)
	b[0] |= 1
	return b
}

func ddSecret(t *testing.T) codec.Secret {
	t.Helper()
	var s codec.Secret
	copy(s[:], randomBytes(t, codec.SecretSize))
	return s
}

// TestObfuscatedSecret: the server's codec of a connection made with a "dd"
// secret reads what the client sends, with the client's padding, and the
// client reads what the codec sends, with the server's padding.
func TestObfuscatedSecret(t *testing.T) {
	secret := ddSecret(t)
	conn := &memConn{}
	tr, err := mtclient.NewObfuscated(conn, secret.Bytes(), 201)
	if err != nil {
		t.Fatal(err)
	}

	type sent struct {
		p      []byte
		maxPad int
	}
	var all []sent
	for i := 0; i < 64; i++ {
		u, e := unencrypted(t, 40+4*(i%8)), encrypted(t, 1+i%5)
		for _, p := range [][]byte{u, e} {
			if err := tr.WritePacket(p); err != nil {
				t.Fatal(err)
			}
		}
		// Android pads a message of the key exchange by up to 256 bytes and
		// an encrypted one by up to 15.
		all = append(all, sent{u, 256}, sent{e, 15})
	}

	r := &codecReader{buf: conn.out.Bytes()}
	c, err := codec.CreateSecretCodec(r, secret)
	if err != nil {
		t.Fatalf("the codec refused the header: %v", err)
	}
	if dc := c.(*codec.ObfuscatedCodec).DcId(); dc != 201 {
		t.Fatalf("dc %d in the header, want 201", dc)
	}
	padded, unaligned := 0, 0
	for i, s := range all {
		_, got, err := c.Decode(r)
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		pad := len(got) - len(s.p)
		if !bytes.HasPrefix(got, s.p) || pad < 0 || pad > s.maxPad {
			t.Fatalf("packet %d: %d bytes for %d sent", i, len(got), len(s.p))
		}
		if pad > 0 {
			padded++
		}
		if len(got)%4 != 0 {
			unaligned++
		}
	}
	if len(r.buf) != 0 {
		t.Fatalf("%d bytes left after the packets", len(r.buf))
	}
	if padded == 0 || unaligned == 0 {
		t.Fatalf("of %d packets %d were padded, %d of a length not divisible by 4", len(all), padded, unaligned)
	}

	// The server's answers: the padding after an encrypted message is cut
	// to whole AES blocks, a message of the key exchange keeps it (its
	// length is inside), a transport error comes as such.
	var answers [][]byte
	for i := 0; i < 32; i++ {
		answers = append(answers, encrypted(t, 1+i%7), unencrypted(t, 84))
	}
	for _, a := range answers {
		out, err := c.Encode(nil, &mtproto.MTPRawMessage{Payload: a})
		if err != nil {
			t.Fatal(err)
		}
		conn.in.Write(out)
	}
	out, err := c.Encode(nil, &mtproto.MTPRawMessage{Payload: binary.LittleEndian.AppendUint32(nil, uint32(0xfffffe6c))})
	if err != nil {
		t.Fatal(err)
	}
	conn.in.Write(out)
	for i, a := range answers {
		got, err := tr.ReadPacket()
		if err != nil {
			t.Fatalf("answer %d: %v", i, err)
		}
		if binary.LittleEndian.Uint64(a) != 0 && !bytes.Equal(got, a) || !bytes.HasPrefix(got, a) {
			t.Fatalf("answer %d: %d bytes for %d", i, len(got), len(a))
		}
	}
	if _, err = tr.ReadPacket(); !mtclient.IsTransportError(err, -404) {
		t.Fatalf("the transport error: %v", err)
	}
}

// TestObfuscatedSecretRefused: a node with another secret, and the gateway,
// which takes no secret, refuse a connection made with one.
func TestObfuscatedSecretRefused(t *testing.T) {
	secret := ddSecret(t)
	conn := &memConn{}
	tr, err := mtclient.NewObfuscated(conn, secret.Bytes(), 201)
	if err != nil {
		t.Fatal(err)
	}
	if err = tr.WritePacket(unencrypted(t, 40)); err != nil {
		t.Fatal(err)
	}
	if _, err = codec.CreateSecretCodec(&codecReader{buf: bytes.Clone(conn.out.Bytes())}, ddSecret(t)); err == nil {
		t.Error("a node with another secret took the connection")
	}
	if _, err = codec.CreateMTProtoCodec(&codecReader{buf: bytes.Clone(conn.out.Bytes())}); err == nil {
		t.Error("the gateway took a connection made with a secret")
	}
}

// TestObfuscatedNoSecret: without a secret the gateway's codec takes the
// connection, and the padded packets keep a length divisible by 4, which
// it requires then.
func TestObfuscatedNoSecret(t *testing.T) {
	conn := &memConn{}
	tr, err := mtclient.NewObfuscated(conn, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	var all [][]byte
	for i := 0; i < 32; i++ {
		u, e := unencrypted(t, 40), encrypted(t, 2)
		for _, p := range [][]byte{u, e} {
			if err := tr.WritePacket(p); err != nil {
				t.Fatal(err)
			}
		}
		all = append(all, u, e)
	}
	r := &codecReader{buf: conn.out.Bytes()}
	c, err := codec.CreateMTProtoCodec(r)
	if err != nil {
		t.Fatalf("the gateway refused the header: %v", err)
	}
	if dc := c.(*codec.ObfuscatedCodec).DcId(); dc != 2 {
		t.Fatalf("dc %d in the header, want 2", dc)
	}
	for i, p := range all {
		_, got, err := c.Decode(r)
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		if !bytes.HasPrefix(got, p) || len(got)-len(p) > 12 {
			t.Fatalf("packet %d: %d bytes for %d", i, len(got), len(p))
		}
	}
	if _, err = codec.CreateSecretCodec(&codecReader{buf: bytes.Clone(conn.out.Bytes())}, ddSecret(t)); err == nil {
		t.Error("a node took a connection made without its secret")
	}
}

func TestObfuscatedArguments(t *testing.T) {
	good := ddSecret(t).Bytes()
	for name, c := range map[string]struct {
		secret []byte
		dc     int16
	}{
		"16 bytes, no prefix": {good[1:], 201},
		"the ee form":         {append([]byte{0xee}, good[1:]...), 201},
		"a byte too many":     {append(good, 0), 201},
		"no DC":               {good, 0},
	} {
		if _, err := mtclient.NewObfuscated(&memConn{}, c.secret, c.dc); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

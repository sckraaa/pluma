package mtclient

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"time"
)

// The obfuscated transport (https://core.telegram.org/mtproto/mtproto-transports#transport-obfuscation):
// a 64-byte random header carries the AES-256-CTR keys of both directions,
// the transport tag and the DC, and everything after it is encrypted. This
// client always speaks the padded intermediate transport inside it (tag
// 0xdddddddd), the one every official client speaks to a DC whose
// dcOption carries a "dd" secret:
//
//   - tdesktop: TcpConnection::Protocol::Create picks VersionD for a 17-byte
//     secret starting with 0xdd; prepareKey hashes each key with the 16
//     bytes after it (Telegram/SourceFiles/mtproto/connection_tcp.cpp:160-165,
//     235-248), and prepareConnectionStartPrefix builds the header
//     (:448-491);
//   - Android: Connection::sendData picks ProtocolTypeDD for such a secret,
//     draws the header again while it reads as another transport and writes
//     the tag and the DC (TMessagesProj/jni/tgnet/Connection.cpp:458-466,
//     520-552).
//
// The Go standard library's AES-CTR is used, not the server's codec, so
// that the tests drive the server with code it does not share.

const (
	// paddedIntermediateTag is the tag of the padded intermediate transport.
	paddedIntermediateTag = 0xdddddddd
	// obfuscatedHeaderSize is the length of the header.
	obfuscatedHeaderSize = 64
	// secretPrefix is the first byte of a "random padding" secret, and
	// secretKeySize the bytes after it that the keys are hashed with.
	secretPrefix  = 0xdd
	secretKeySize = 16
	// encryptedHeaderSize is auth_key_id and msg_key, before the AES-IGE
	// blocks of an encrypted message.
	encryptedHeaderSize = 24
)

// obfuscated is the obfuscated transport over a stream connection.
type obfuscated struct {
	conn       net.Conn
	r          *bufio.Reader
	withSecret bool

	wmu  sync.Mutex
	send cipher.Stream // guarded by wmu

	recv cipher.Stream // used by ReadPacket only
}

// DialObfuscated connects to addr and starts the obfuscated transport with
// secret, as dcOption.secret carries it (0xdd and 16 bytes), or without a
// secret when it is empty. dc is the DC number written in the header: the
// clients put the DC they mean to reach there, and the servers refuse 0.
func DialObfuscated(ctx context.Context, addr string, secret []byte, dc int16) (Transport, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	t, err := NewObfuscated(conn, secret, dc)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return t, nil
}

// NewObfuscated starts the obfuscated transport on conn: it sends the
// header. See DialObfuscated for secret and dc.
func NewObfuscated(conn net.Conn, secret []byte, dc int16) (Transport, error) {
	key, err := secretKey(secret)
	if err != nil {
		return nil, err
	}
	if dc == 0 {
		return nil, errors.New("mtclient: the obfuscated header needs a DC")
	}
	header, err := obfuscatedNonce()
	if err != nil {
		return nil, err
	}
	binary.LittleEndian.PutUint32(header[56:], paddedIntermediateTag)
	// Little-endian, as the clients write it (Android Connection.cpp:551-552,
	// tdesktop qToUnaligned in connection_tcp.cpp:487).
	binary.LittleEndian.PutUint16(header[60:], uint16(dc))

	reversed := slices.Clone(header[8:56])
	slices.Reverse(reversed)
	send, err := ctrStream(obfuscationKey(header[8:40], key), header[40:56])
	if err != nil {
		return nil, err
	}
	recv, err := ctrStream(obfuscationKey(reversed[:32], key), reversed[32:48])
	if err != nil {
		return nil, err
	}
	// The first 56 bytes go as they are; the tag and the DC encrypted, with
	// the stream positioned after the whole header.
	encrypted := make([]byte, obfuscatedHeaderSize)
	send.XORKeyStream(encrypted, header)
	copy(header[56:], encrypted[56:])
	if _, err = conn.Write(header); err != nil {
		return nil, err
	}
	return &obfuscated{
		conn:       conn,
		r:          bufio.NewReaderSize(conn, 64<<10),
		withSecret: key != nil,
		send:       send,
		recv:       recv,
	}, nil
}

// secretKey returns the 16 bytes the keys are hashed with, nil without a
// secret (nil or empty, as a dcOption without one decodes). Only the "dd" form is supported: the plain 16-byte form lets a
// client choose another transport, and the "ee" form needs fake TLS.
func secretKey(secret []byte) ([]byte, error) {
	if len(secret) == 0 {
		return nil, nil
	}
	if len(secret) != 1+secretKeySize || secret[0] != secretPrefix {
		return nil, fmt.Errorf("mtclient: a secret of %d bytes: want 0xdd and %d bytes", len(secret), secretKeySize)
	}
	return slices.Clone(secret[1:]), nil
}

// obfuscatedNonce draws the random header until it cannot be read as
// another transport or a plain one, as Android does
// (Connection.cpp:520-527): the abridged tag 0xef, HTTP, the intermediate
// and padded tags, the TLS record 0x02010316, and a second word of zero
// (the full transport).
func obfuscatedNonce() ([]byte, error) {
	b := make([]byte, obfuscatedHeaderSize)
	for {
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		first := binary.LittleEndian.Uint32(b)
		if b[0] == 0xef || binary.LittleEndian.Uint32(b[4:]) == 0 {
			continue
		}
		switch first {
		case 0x44414548, 0x54534f50, 0x20544547, 0x4954504f, // HEAD, POST, GET , OPTI
			intermediateTag, paddedIntermediateTag, 0x02010316:
			continue
		}
		return b, nil
	}
}

// obfuscationKey is the AES key of one direction: the header's bytes
// themselves without a secret, SHA256(bytes + secret) with one (tdesktop
// Version1::prepareKey, connection_tcp.cpp:159-165; Android
// Connection.cpp:634-655).
func obfuscationKey(src, secret []byte) []byte {
	if secret == nil {
		return slices.Clone(src)
	}
	h := sha256.New()
	h.Write(src)
	h.Write(secret)
	return h.Sum(nil)
}

func ctrStream(key, iv []byte) (cipher.Stream, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewCTR(b, iv), nil
}

// padding returns how many random bytes follow a packet of n bytes. With a
// secret, as Android: up to 256 after an unencrypted message (the key
// exchange), up to 15 after an encrypted one
// (Connection.cpp:484-491); tdesktop adds up to 15 to every packet
// (VersionD::finalizePacket, connection_tcp.cpp:195-208). Without a secret
// the length stays a multiple of 4, which the gateway requires of a
// padded packet then.
func (t *obfuscated) padding(p []byte) (int, error) {
	var r [2]byte
	if _, err := rand.Read(r[:]); err != nil {
		return 0, err
	}
	v := int(binary.LittleEndian.Uint16(r[:]))
	if !t.withSecret {
		return v % 4 * 4, nil
	}
	if binary.LittleEndian.Uint64(p) == 0 {
		return v % 257, nil
	}
	return v % 16, nil
}

func (t *obfuscated) WritePacket(p []byte) error {
	if len(p) < 8 || len(p) > maxPacketSize || len(p)%4 != 0 {
		return fmt.Errorf("mtclient: bad packet length %d", len(p))
	}
	pad, err := t.padding(p)
	if err != nil {
		return err
	}
	b := make([]byte, 4+len(p)+pad)
	binary.LittleEndian.PutUint32(b, uint32(len(p)+pad))
	copy(b[4:], p)
	if _, err = rand.Read(b[4+len(p):]); err != nil {
		return err
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	t.send.XORKeyStream(b, b)
	_, err = t.conn.Write(b)
	return err
}

func (t *obfuscated) ReadPacket() ([]byte, error) {
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(t.r, hdr[:]); err != nil {
			return nil, err
		}
		t.recv.XORKeyStream(hdr[:], hdr[:])
		n := binary.LittleEndian.Uint32(hdr[:])
		if n&quickAckBit != 0 {
			// A quick ACK (never requested by this client): no payload.
			continue
		}
		if n < 4 || n > maxPacketSize {
			return nil, fmt.Errorf("mtclient: bad packet length %d", n)
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(t.r, p); err != nil {
			return nil, err
		}
		t.recv.XORKeyStream(p, p)
		if n < 12 {
			// Fewer than three words is a transport error, 0 a no-op
			// (tdesktop TcpConnection::parsePacket, connection_tcp.cpp:394-410).
			code := int32(binary.LittleEndian.Uint32(p))
			if code == 0 {
				continue
			}
			return nil, &TransportError{Code: code}
		}
		if n >= encryptedHeaderSize && binary.LittleEndian.Uint64(p) != 0 {
			// The padding after an encrypted message: its AES blocks are
			// what is left in whole 16 bytes (tdesktop
			// session_private.cpp:1303-1305). An unencrypted message
			// carries its own length.
			p = p[:encryptedHeaderSize+(n-encryptedHeaderSize)/16*16]
		}
		return p, nil
	}
}

func (t *obfuscated) Close() error {
	return t.conn.Close()
}

// SetDeadline sets the read and write deadline of the connection (the key
// exchange uses its context's deadline).
func (t *obfuscated) SetDeadline(d time.Time) error {
	return t.conn.SetDeadline(d)
}

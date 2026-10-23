package mtclient

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// maxPacketSize bounds a transport packet: an MTProto message is at most
// 16 MiB (the gateway's limit as well).
const maxPacketSize = 16 << 20

// intermediateTag opens a connection with the intermediate transport
// (https://core.telegram.org/mtproto/mtproto-transports#intermediate).
const intermediateTag = 0xeeeeeeee

// quickAckBit marks a quick ACK in place of a packet length.
const quickAckBit = 0x80000000

// Transport sends and receives MTProto packets over a connection.
type Transport interface {
	// WritePacket sends one packet; it may be called concurrently.
	WritePacket(p []byte) error
	// ReadPacket returns the next packet. A transport error the server sends
	// in place of a packet is returned as *TransportError.
	ReadPacket() ([]byte, error)
	// Close closes the connection, which unblocks ReadPacket.
	Close() error
}

// intermediate is the intermediate transport over a stream connection: the
// tag 0xeeeeeeee once, then every packet as a little-endian int32 length and
// the payload.
type intermediate struct {
	conn net.Conn
	r    *bufio.Reader
	wmu  sync.Mutex
}

// DialIntermediate connects to addr (host:port of an MTProto TCP port) and
// starts the intermediate transport.
func DialIntermediate(ctx context.Context, addr string) (Transport, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	t, err := NewIntermediate(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return t, nil
}

// NewIntermediate starts the intermediate transport on conn: it sends the tag.
func NewIntermediate(conn net.Conn) (Transport, error) {
	var tag [4]byte
	binary.LittleEndian.PutUint32(tag[:], intermediateTag)
	if _, err := conn.Write(tag[:]); err != nil {
		return nil, err
	}
	return &intermediate{conn: conn, r: bufio.NewReaderSize(conn, 64<<10)}, nil
}

func (t *intermediate) WritePacket(p []byte) error {
	if len(p) == 0 || len(p) > maxPacketSize || len(p)%4 != 0 {
		return fmt.Errorf("mtclient: bad packet length %d", len(p))
	}
	b := make([]byte, 4+len(p))
	binary.LittleEndian.PutUint32(b, uint32(len(p)))
	copy(b[4:], p)
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_, err := t.conn.Write(b)
	return err
}

func (t *intermediate) ReadPacket() ([]byte, error) {
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(t.r, hdr[:]); err != nil {
			return nil, err
		}
		n := binary.LittleEndian.Uint32(hdr[:])
		if n&quickAckBit != 0 {
			// A quick ACK (never requested by this client): no payload.
			continue
		}
		if n == 0 || n > maxPacketSize {
			return nil, fmt.Errorf("mtclient: bad packet length %d", n)
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(t.r, p); err != nil {
			return nil, err
		}
		if n == 4 {
			// A transport error: a negative int32 instead of a message.
			return nil, &TransportError{Code: int32(binary.LittleEndian.Uint32(p))}
		}
		return p, nil
	}
}

func (t *intermediate) Close() error {
	return t.conn.Close()
}

// SetDeadline sets the read and write deadline of the connection (the key
// exchange uses its context's deadline).
func (t *intermediate) SetDeadline(d time.Time) error {
	return t.conn.SetDeadline(d)
}

// TransportError is a transport-level error the server sent instead of a
// message (https://core.telegram.org/mtproto/mtproto-transports#transport-errors),
// e.g. -404 for an unknown or expired auth key.
type TransportError struct {
	Code int32
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("mtclient: transport error %d", e.Code)
}

// IsTransportError reports whether err is the transport error code.
func IsTransportError(err error, code int32) bool {
	var te *TransportError
	return errors.As(err, &te) && te.Code == code
}

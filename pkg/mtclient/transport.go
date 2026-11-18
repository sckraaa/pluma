package mtclient

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gobwas/ws"
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

// DialWebSocket connects to a WebSocket entry of the gateway (a ws:// or
// wss:// URL such as wss://host/apiws) with the subprotocol "binary", as
// the web client does (tweb src/lib/mtproto/transports/websocket.ts:51),
// and header added to the upgrade request (the Edge entry takes the
// client's address from X-Pluma-Client-Addr). The intermediate transport
// then runs in its binary messages: the tag, then one message per packet.
// The connection answers the server's pings. A refused upgrade is an
// error wrapping ws.StatusError, the HTTP status.
func DialWebSocket(ctx context.Context, url string, header http.Header) (Transport, error) {
	return DialWebSocketVia(ctx, url, header, nil, nil)
}

// DialWebSocketVia is DialWebSocket with the TCP connection opened by dial
// (to any address, whatever the host of url) and, for a wss:// URL, TLS by
// tlsConfig: a test reaches a site of Caddy by its name on a loopback port,
// and may write what a proxy in front of it writes (PROXY v1) before the
// TLS. Nil dial and tlsConfig are the defaults of DialWebSocket.
func DialWebSocketVia(ctx context.Context, url string, header http.Header, dial func(ctx context.Context, network, addr string) (net.Conn, error), tlsConfig *tls.Config) (Transport, error) {
	d := ws.Dialer{Protocols: []string{"binary"}, NetDial: dial, TLSConfig: tlsConfig}
	if len(header) > 0 {
		d.Header = ws.HandshakeHeaderHTTP(header)
	}
	conn, br, hs, err := d.Dial(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("mtclient: websocket %s: %w", url, err)
	}
	if hs.Protocol != "binary" {
		_ = conn.Close()
		return nil, fmt.Errorf("mtclient: websocket %s: the server chose the subprotocol %q, not binary", url, hs.Protocol)
	}
	if br == nil {
		br = bufio.NewReaderSize(conn, 64<<10)
	}
	t, err := NewIntermediate(&wsStream{Conn: conn, r: br})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return t, nil
}

// wsStream is the byte stream a client WebSocket carries in its binary
// messages: Write sends one masked binary message, Read returns the data
// of the server's messages in order, whatever their boundaries. Pings are
// answered, pongs dropped; a close frame ends the stream.
type wsStream struct {
	net.Conn
	r *bufio.Reader
	// wmu orders the frames written by Write and the pongs of Read.
	wmu sync.Mutex
	// data is what Read has not returned yet of the last data frame.
	data []byte
}

// maxWSFramePayload bounds a frame from the server: a packet and the
// framing around it.
const maxWSFramePayload = maxPacketSize + 1024

func (s *wsStream) Read(p []byte) (int, error) {
	for len(s.data) == 0 {
		if err := s.readFrame(); err != nil {
			return 0, err
		}
	}
	n := copy(p, s.data)
	s.data = s.data[n:]
	return n, nil
}

// readFrame reads one frame: data goes to s.data, a ping is answered.
func (s *wsStream) readFrame() error {
	h, err := ws.ReadHeader(s.r)
	if err != nil {
		return err
	}
	// RFC 6455 §5.1: a server never masks its frames.
	if h.Masked {
		return errors.New("mtclient: websocket: a masked frame from the server")
	}
	if h.Length > maxWSFramePayload || (h.OpCode.IsControl() && (h.Length > 125 || !h.Fin)) {
		return fmt.Errorf("mtclient: websocket: a frame of opcode %d and %d bytes", h.OpCode, h.Length)
	}
	payload := make([]byte, h.Length)
	if _, err := io.ReadFull(s.r, payload); err != nil {
		return err
	}
	switch h.OpCode {
	case ws.OpBinary, ws.OpContinuation:
		s.data = payload
		return nil
	case ws.OpPing:
		return s.writeFrame(ws.NewPongFrame(payload))
	case ws.OpPong:
		return nil
	case ws.OpClose:
		_ = s.writeFrame(ws.NewCloseFrame(nil))
		return io.EOF
	default:
		return fmt.Errorf("mtclient: websocket: a frame of opcode %d", h.OpCode)
	}
}

func (s *wsStream) Write(p []byte) (int, error) {
	if err := s.writeFrame(ws.NewBinaryFrame(append([]byte(nil), p...))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// writeFrame masks f in place and writes it.
func (s *wsStream) writeFrame(f ws.Frame) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return ws.WriteFrame(s.Conn, ws.MaskFrameInPlace(f))
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

package ws

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/gobwas/ws"
	"github.com/panjf2000/gnet/v2"
)

// fakeConn is the part of gnet.Conn the codec uses.
type fakeConn struct {
	gnet.Conn
	in  []byte
	out bytes.Buffer
}

func (c *fakeConn) Next(n int) ([]byte, error) {
	if n < 0 || n > len(c.in) {
		n = len(c.in)
	}
	b := c.in[:n]
	c.in = c.in[n:]
	return b, nil
}
func (c *fakeConn) Write(b []byte) (int, error) { return c.out.Write(b) }
func (c *fakeConn) ConnId() int64               { return 1 }
func (c *fakeConn) RemoteAddr() net.Addr        { return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1} }

func frame(t *testing.T, f ws.Frame) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := ws.WriteFrame(&b, ws.MaskFrame(f)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// header is a masked frame header claiming length bytes of payload.
func header(t *testing.T, op ws.OpCode, fin bool, length int64) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := ws.WriteHeader(&b, ws.Header{Fin: fin, OpCode: op, Masked: true, Mask: ws.NewMask(), Length: length}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func upgraded() *WsCodec { return &WsCodec{upgraded: true} }

func feed(w *WsCodec, b []byte) ([][]byte, error) {
	w.Buf.Write(b)
	msgs, err := w.Decode(&fakeConn{})
	var out [][]byte
	for _, m := range msgs {
		out = append(out, m.Payload)
	}
	return out, err
}

func TestDecodeMessages(t *testing.T) {
	w := upgraded()
	in := append(frame(t, ws.NewBinaryFrame([]byte("abcd"))), frame(t, ws.NewBinaryFrame([]byte("ef")))...)
	msgs, err := feed(w, in)
	if err != nil || len(msgs) != 2 || string(msgs[0]) != "abcd" || string(msgs[1]) != "ef" {
		t.Fatalf("%q %v", msgs, err)
	}

	// A message in two fragments.
	first := ws.NewFrame(ws.OpBinary, false, []byte("12"))
	second := ws.NewFrame(ws.OpContinuation, true, []byte("34"))
	msgs, err = feed(w, append(frame(t, first), frame(t, second)...))
	if err != nil || len(msgs) != 1 || string(msgs[0]) != "1234" {
		t.Fatalf("%q %v", msgs, err)
	}
}

func TestDecodeLimits(t *testing.T) {
	cases := []struct {
		name string
		in   func(t *testing.T) []byte
		want error
	}{
		{"frame over the message limit", func(t *testing.T) []byte {
			return header(t, ws.OpBinary, true, MaxMessageSize+1)
		}, ErrMessageTooLarge},
		{"frame claiming 2^62 bytes", func(t *testing.T) []byte {
			return header(t, ws.OpBinary, true, 1<<62)
		}, ErrMessageTooLarge},
		{"control frame over 125 bytes", func(t *testing.T) []byte {
			return header(t, ws.OpPing, true, 126)
		}, nil}, // gobwas rejects it already, or we do
		{"fragmented control frame", func(t *testing.T) []byte {
			return header(t, ws.OpPing, false, 4)
		}, nil},
		{"fragments over the message limit", func(t *testing.T) []byte {
			b := frame(t, ws.NewFrame(ws.OpBinary, false, make([]byte, 1<<20)))
			for i := 0; i < 16; i++ {
				b = append(b, frame(t, ws.NewFrame(ws.OpContinuation, false, make([]byte, 1<<20)))...)
			}
			return b
		}, ErrMessageTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := feed(upgraded(), tc.in(t))
			if err == nil {
				t.Fatal("accepted")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestReadBufferBytesBounded(t *testing.T) {
	w := upgraded()
	chunk := make([]byte, 4<<20)
	for i := 0; ; i++ {
		if w.ReadBufferBytes(&fakeConn{in: chunk}) == gnet.Close {
			break
		}
		if w.Buf.Len() > maxBuffered {
			t.Fatalf("buffered %d bytes", w.Buf.Len())
		}
		if i > 20 {
			t.Fatal("never closed")
		}
	}
}

func TestUpgradeRequestBounded(t *testing.T) {
	w := &WsCodec{}
	c := &fakeConn{}
	w.Buf.WriteString("GET /apiws HTTP/1.1\r\nHost: x\r\nX: " + strings.Repeat("a", maxUpgradeRequestSize))
	ok, action := w.Upgrade(c)
	if ok || action != gnet.Close {
		t.Fatalf("ok %v action %v", ok, action)
	}

	// A normal request upgrades.
	w = &WsCodec{}
	w.Buf.WriteString("GET /apiws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: binary\r\n\r\n")
	if ok, action := w.Upgrade(c); !ok || action != gnet.None {
		t.Fatalf("ok %v action %v: %s", ok, action, c.out.String())
	}
}

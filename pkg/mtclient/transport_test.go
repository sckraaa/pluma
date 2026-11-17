package mtclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// wsServer accepts one WebSocket connection on a loopback port and runs
// serve on it after the upgrade; header receives the request's
// X-Pluma-Client-Addr. reject, if not zero, refuses the upgrade with it.
func wsServer(t *testing.T, reject int, serve func(conn net.Conn, header string)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var header string
		u := ws.Upgrader{
			Protocol: func(p []byte) bool { return string(p) == "binary" },
			OnHeader: func(key, value []byte) error {
				if string(key) == "X-Pluma-Client-Addr" {
					header = string(value)
				}
				return nil
			},
			OnBeforeUpgrade: func() (ws.HandshakeHeader, error) {
				if reject != 0 {
					return nil, ws.RejectConnectionError(ws.RejectionStatus(reject))
				}
				return nil, nil
			},
		}
		if _, err := u.Upgrade(conn); err != nil {
			return
		}
		serve(conn, header)
	}()
	return "ws://" + ln.Addr().String() + "/apiws"
}

// TestWebSocketTransport: the transport sends the header, the intermediate
// tag and packets in binary messages, reads a packet split over messages
// and answers the server's ping meanwhile.
func TestWebSocketTransport(t *testing.T) {
	type seen struct {
		header string
		tag    []byte
		packet []byte
		pong   []byte
		err    error
	}
	got := make(chan seen, 1)
	url := wsServer(t, 0, func(conn net.Conn, header string) {
		s := seen{header: header}
		defer func() { got <- s }()
		if s.err = wsutil.WriteServerMessage(conn, ws.OpPing, []byte("p1")); s.err != nil {
			return
		}
		r := wsutil.Reader{Source: conn, State: ws.StateServerSide}
		next := func() (ws.OpCode, []byte, error) {
			h, err := r.NextFrame()
			if err != nil {
				return 0, nil, err
			}
			b, err := io.ReadAll(&r)
			return h.OpCode, b, err
		}
		// The client may write its tag before it reads the ping.
		for s.tag == nil || s.packet == nil || s.pong == nil {
			op, b, err := next()
			if err != nil {
				s.err = err
				return
			}
			switch {
			case op == ws.OpPong:
				s.pong = b
			case op == ws.OpBinary && s.tag == nil:
				s.tag = b
			case op == ws.OpBinary:
				s.packet = b
			default:
				s.err = errors.New("an unexpected frame")
				return
			}
		}
		// A packet of 8 bytes in two messages, with a ping between.
		reply := binary.LittleEndian.AppendUint32(nil, 8)
		reply = append(reply, "abcdefgh"...)
		if s.err = wsutil.WriteServerBinary(conn, reply[:6]); s.err != nil {
			return
		}
		if s.err = wsutil.WriteServerMessage(conn, ws.OpPing, nil); s.err != nil {
			return
		}
		s.err = wsutil.WriteServerBinary(conn, reply[6:])
		// Wait for the client to close.
		_, _ = io.Copy(io.Discard, conn)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tr, err := DialWebSocket(ctx, url, http.Header{"X-Pluma-Client-Addr": {"203.0.113.9"}})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if err := tr.WritePacket([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	// The ping is answered as the client reads.
	go func() {
		p, err := tr.ReadPacket()
		if err != nil || string(p) != "abcdefgh" {
			t.Errorf("ReadPacket = %q, %v; want abcdefgh", p, err)
		}
		_ = tr.Close()
	}()

	select {
	case s := <-got:
		if s.err != nil {
			t.Fatal(s.err)
		}
		if s.header != "203.0.113.9" {
			t.Errorf("X-Pluma-Client-Addr = %q", s.header)
		}
		if !bytes.Equal(s.tag, []byte{0xee, 0xee, 0xee, 0xee}) {
			t.Errorf("tag message %x", s.tag)
		}
		if want := append(binary.LittleEndian.AppendUint32(nil, 4), "1234"...); !bytes.Equal(s.packet, want) {
			t.Errorf("packet message %x, want %x", s.packet, want)
		}
		if string(s.pong) != "p1" {
			t.Errorf("pong %q, want the ping's payload", s.pong)
		}
	case <-ctx.Done():
		t.Fatal("the server saw nothing")
	}
}

// A refused upgrade is an error with the HTTP status.
func TestWebSocketTransportRefused(t *testing.T) {
	url := wsServer(t, http.StatusBadRequest, func(net.Conn, string) {})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := DialWebSocket(ctx, url, nil)
	var status ws.StatusError
	if !errors.As(err, &status) || int(status) != http.StatusBadRequest {
		t.Fatalf("DialWebSocket = %v, want status 400", err)
	}
}

package freezeproxy

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// echoServer echoes every connection until it closes; closeAfter, when
// set, closes each connection after echoing that many bytes.
func echoServer(t *testing.T, closeAfter int) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				if closeAfter > 0 {
					_, _ = io.CopyN(c, c, int64(closeAfter))
					return
				}
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().String()
}

func start(t *testing.T, opts Options) *Proxy {
	t.Helper()
	p, err := Listen("127.0.0.1:0", opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func dial(t *testing.T, p *Proxy) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// expectSilence checks that nothing arrives on c for a while and that the
// connection stays open.
func expectSilence(t *testing.T, c net.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	n, err := c.Read(make([]byte, 1))
	if n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read %d bytes, %v; want silence", n, err)
	}
	_ = c.SetReadDeadline(time.Time{})
}

// The first Down bytes of a connection come back, then nothing, and the
// connection stays open, even after the target closed it; each connection
// counts anew.
func TestFreezeDown(t *testing.T) {
	p := start(t, Options{Target: echoServer(t, 0), Down: 100})
	for i := 0; i < 2; i++ {
		c := dial(t, p)
		data := bytes.Repeat([]byte{byte(i + 1)}, 300)
		if _, err := c.Write(data); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, 100)
		if _, err := io.ReadFull(c, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, data[:100]) {
			t.Fatal("other bytes than sent")
		}
		expectSilence(t, c)
	}
	if n := p.Frozen(); n != 2 {
		t.Fatalf("%d connections froze, want 2", n)
	}

	p2 := start(t, Options{Target: echoServer(t, 150), Down: 100})
	c := dial(t, p2)
	if _, err := c.Write(make([]byte, 150)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(c, make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	expectSilence(t, c)
}

// Up freezes the way to the target: what passed is echoed, the rest never
// reaches it.
func TestFreezeUp(t *testing.T) {
	p := start(t, Options{Target: echoServer(t, 0), Up: 64})
	c := dial(t, p)
	if _, err := c.Write(make([]byte, 200)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(c, make([]byte, 64)); err != nil {
		t.Fatal(err)
	}
	expectSilence(t, c)
}

// Without limits everything passes and a close of the target reaches the
// client.
func TestNoFreeze(t *testing.T) {
	closed := make(chan ConnStats, 1)
	p := start(t, Options{Target: echoServer(t, 1000), OnClose: func(s ConnStats) { closed <- s }})
	c := dial(t, p)
	data := bytes.Repeat([]byte{7}, 1000)
	if _, err := c.Write(data); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %d bytes back of %d", len(got), len(data))
	}
	if n := p.Frozen(); n != 0 {
		t.Fatalf("%d connections froze", n)
	}
	_ = c.Close()
	select {
	case s := <-closed:
		if s != (ConnStats{ID: 1, Down: 1000, Up: 1000}) {
			t.Fatalf("connection stats %+v", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no stats of the closed connection")
	}
}

// Close closes the frozen connections too.
func TestCloseEndsConnections(t *testing.T) {
	p, err := Listen("127.0.0.1:0", Options{Target: echoServer(t, 0), Down: 10})
	if err != nil {
		t.Fatal(err)
	}
	c := dial(t, p)
	if _, err := c.Write(make([]byte, 50)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(c, make([]byte, 10)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- p.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close hangs on a frozen connection")
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the client's connection after Close: %v, want closed", err)
	}
}

func TestListenOptions(t *testing.T) {
	if _, err := Listen("127.0.0.1:0", Options{}); err == nil {
		t.Error("no target accepted")
	}
	if _, err := Listen("127.0.0.1:0", Options{Target: "127.0.0.1:1", Down: -1}); err == nil {
		t.Error("a negative limit accepted")
	}
}

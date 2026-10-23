package testinfra

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// packet frames a MySQL client packet: 3-byte length, sequence 0, payload.
func packet(payload string) []byte {
	n := len(payload)
	return append([]byte{byte(n), byte(n >> 8), byte(n >> 16), 0}, payload...)
}

// echoServer answers every packet with "ok:" and the payload, and records
// what it received.
func echoServer(t *testing.T) (addr string, received chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	received = make(chan string, 16)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				h := make([]byte, 4)
				for {
					if _, err := io.ReadFull(r, h); err != nil {
						return
					}
					p := make([]byte, int(h[0])|int(h[1])<<8|int(h[2])<<16)
					if _, err := io.ReadFull(r, p); err != nil {
						return
					}
					received <- string(p)
					if _, err := c.Write([]byte("ok:" + string(p))); err != nil {
						return
					}
				}
			}()
		}
	}()
	return ln.Addr().String(), received
}

// roundTrip sends a packet and reads the reply; "" when the connection
// closed instead.
func roundTrip(t *testing.T, c net.Conn, payload string) string {
	t.Helper()
	if _, err := c.Write(packet(payload)); err != nil {
		return ""
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 256)
	n, err := c.Read(buf)
	if err != nil {
		return ""
	}
	return string(buf[:n])
}

func TestMySQLProxy(t *testing.T) {
	const commit = "\x03COMMIT"
	cases := []struct {
		name       string
		fault      CommitFault
		serverGets bool
	}{
		{"reply lost", CommitReplyLost, true},
		{"not sent", CommitNotSent, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr, received := echoServer(t)
			p, err := StartMySQLProxy(addr)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()

			conn, err := net.Dial("tcp", p.Addr())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()

			// Unarmed, everything passes, COMMIT included.
			for _, q := range []string{"\x03SELECT 1", commit} {
				if got := roundTrip(t, conn, q); got != "ok:"+q {
					t.Fatalf("reply %q, want %q", got, "ok:"+q)
				}
				<-received
			}

			p.FailNextCommit(c.fault)
			if got := roundTrip(t, conn, "\x03UPDATE t"); got != "ok:\x03UPDATE t" {
				t.Fatalf("an armed proxy broke another statement: %q", got)
			}
			<-received
			if got := roundTrip(t, conn, commit); got != "" {
				t.Fatalf("COMMIT got a reply %q", got)
			}
			select {
			case got := <-received:
				if !c.serverGets || got != commit {
					t.Errorf("server received %q", got)
				}
			case <-time.After(time.Second):
				if c.serverGets {
					t.Error("COMMIT did not reach the server")
				}
			}
			if p.Fired() != 1 {
				t.Errorf("fired %d times", p.Fired())
			}

			// The fault fired once: a new connection works again.
			conn2, err := net.Dial("tcp", p.Addr())
			if err != nil {
				t.Fatal(err)
			}
			defer conn2.Close()
			if got := roundTrip(t, conn2, commit); got != "ok:"+commit {
				t.Errorf("after the fault: %q", got)
			}
		})
	}
}

func TestIsCommit(t *testing.T) {
	for payload, want := range map[string]bool{
		"\x03COMMIT":         true,
		"\x03commit":         true,
		"\x03COMMIT ":        true,
		"\x03COMMIT AND":     false,
		"\x16COMMIT":         false, // COM_STMT_PREPARE
		"\x03":               false,
		"":                   false,
		"\x03START TRANSACT": false,
	} {
		if got := isCommit([]byte(payload)); got != want {
			t.Errorf("isCommit(%q) = %v", payload, got)
		}
	}
	if !bytes.Equal(packet("ab"), []byte{2, 0, 0, 0, 'a', 'b'}) {
		t.Error("packet framing")
	}
}

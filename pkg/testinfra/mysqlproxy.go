package testinfra

import (
	"bufio"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
)

// CommitFault is how MySQLProxy breaks the connection that sends COMMIT.
type CommitFault int

const (
	// CommitReplyLost: COMMIT reaches the server, which applies it; the
	// reply is dropped and the connection closed. The client cannot tell
	// whether it was applied.
	CommitReplyLost CommitFault = iota + 1
	// CommitNotSent: the connection is closed instead of passing COMMIT on,
	// so the server rolls the transaction back.
	CommitNotSent
)

// MySQLProxy is a TCP proxy in front of a MySQL server for tests in which a
// connection fails at a chosen statement: it passes everything through
// unchanged and, when armed, breaks the next connection that sends COMMIT
// as a text query (COM_QUERY). The client must not use TLS or compression
// (the driver's defaults).
type MySQLProxy struct {
	ln     net.Listener
	target string

	mu    sync.Mutex
	fault CommitFault
	fired int
	conns map[net.Conn]struct{}

	wg sync.WaitGroup
}

// StartMySQLProxy listens on a free local port and forwards each connection
// to target (host:port).
func StartMySQLProxy(target string) (*MySQLProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &MySQLProxy{ln: ln, target: target, conns: map[net.Conn]struct{}{}}
	p.wg.Add(1)
	go p.accept()
	return p, nil
}

// Addr is the proxy's host:port, to put in a DSN instead of the server's.
func (p *MySQLProxy) Addr() string { return p.ln.Addr().String() }

// FailNextCommit arms f for the next COMMIT on any connection.
func (p *MySQLProxy) FailNextCommit(f CommitFault) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fault = f
}

// Fired is the number of COMMITs broken so far.
func (p *MySQLProxy) Fired() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fired
}

// Close stops listening and closes every connection.
func (p *MySQLProxy) Close() error {
	err := p.ln.Close()
	p.mu.Lock()
	for c := range p.conns {
		_ = c.Close()
	}
	p.conns = nil
	p.mu.Unlock()
	p.wg.Wait()
	return err
}

func (p *MySQLProxy) accept() {
	defer p.wg.Done()
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		server, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = client.Close()
			continue
		}
		pair := &proxyPair{p: p, client: client, server: server}
		if !p.track(client, server) {
			pair.close()
			return
		}
		p.wg.Add(2)
		go func() { defer p.wg.Done(); p.clientToServer(pair) }()
		go func() { defer p.wg.Done(); pair.serverToClient() }()
	}
}

// track registers open connections; false once the proxy is closed.
func (p *MySQLProxy) track(conns ...net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conns == nil {
		return false
	}
	for _, c := range conns {
		p.conns[c] = struct{}{}
	}
	return true
}

func (p *MySQLProxy) untrack(conns ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range conns {
		delete(p.conns, c)
	}
}

// takeFault disarms and returns the fault to apply to a COMMIT.
func (p *MySQLProxy) takeFault() CommitFault {
	p.mu.Lock()
	defer p.mu.Unlock()
	f := p.fault
	if f != 0 {
		p.fault = 0
		p.fired++
	}
	return f
}

type proxyPair struct {
	p              *MySQLProxy
	client, server net.Conn
	dropReply      atomic.Bool // the next bytes from the server are dropped and the pair closed
}

func (pp *proxyPair) close() {
	_ = pp.client.Close()
	_ = pp.server.Close()
	pp.p.untrack(pp.client, pp.server)
}

// clientToServer forwards the client's packets (a 3-byte little-endian
// length, a sequence byte, the payload) and applies an armed fault to a
// COM_QUERY "COMMIT".
func (p *MySQLProxy) clientToServer(pp *proxyPair) {
	defer pp.close()
	r := bufio.NewReader(pp.client)
	header := make([]byte, 4)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			return
		}
		n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
		payload := make([]byte, n)
		if _, err := io.ReadFull(r, payload); err != nil {
			return
		}
		if isCommit(payload) {
			switch p.takeFault() {
			case CommitNotSent:
				return
			case CommitReplyLost:
				pp.dropReply.Store(true)
			}
		}
		if _, err := pp.server.Write(append(header, payload...)); err != nil {
			return
		}
	}
}

func (pp *proxyPair) serverToClient() {
	defer pp.close()
	buf := make([]byte, 32<<10)
	for {
		n, err := pp.server.Read(buf)
		if n > 0 {
			if pp.dropReply.Load() {
				return
			}
			if _, werr := pp.client.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// isCommit tells whether a packet is the text query COMMIT.
func isCommit(payload []byte) bool {
	const comQuery = 0x03
	return len(payload) > 1 && payload[0] == comQuery && strings.EqualFold(strings.TrimSpace(string(payload[1:])), "COMMIT")
}

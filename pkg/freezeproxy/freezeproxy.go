// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package freezeproxy is a TCP proxy that behaves as the filtered networks
// of task 092 do with a connection to a blocked address: it passes the
// first bytes of each connection and then goes silent, without a RST or a
// FIN, while the connection stays open. The client sees a working
// connection that stops answering; the server sees its bytes taken.
//
// It stands in front of an entry of the gateway in the end-to-end tests of
// the path probe (task 113) and, through tools/freezeproxy, in manual
// checks of the clients.
package freezeproxy

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Options configures a Proxy.
type Options struct {
	// Target is the host:port the proxy connects each client to.
	Target string
	// Down is how many bytes of each connection pass from the target to
	// the client before the connection freezes in that direction, and Up
	// the same from the client to the target; 0 passes all. A frozen
	// direction drops what comes, as a filter drops the packets.
	Down int64
	Up   int64
	// DialTimeout bounds the connection to Target (default 10 s).
	DialTimeout time.Duration
	// OnClose, when set, is called with the counts of each connection once
	// it has ended.
	OnClose func(ConnStats)
}

// ConnStats are the counts of one connection of the proxy.
type ConnStats struct {
	// ID numbers the connections from 1 in the order they came.
	ID int64
	// Down and Up are the bytes passed to the client and to the target.
	Down, Up int64
	// Frozen: the connection froze in either direction.
	Frozen bool
}

// Proxy is a running freezing proxy.
type Proxy struct {
	ln     net.Listener
	opts   Options
	wg     sync.WaitGroup
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	// frozen counts the connections that froze in either direction.
	frozen atomic.Int64
	lastID atomic.Int64
}

// Listen starts a proxy on addr (host:port; port 0 takes a free one).
func Listen(addr string, opts Options) (*Proxy, error) {
	if opts.Target == "" {
		return nil, errors.New("freezeproxy: no target")
	}
	if opts.Down < 0 || opts.Up < 0 {
		return nil, errors.New("freezeproxy: a negative limit")
	}
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = 10 * time.Second
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	p := &Proxy{ln: ln, opts: opts, conns: map[net.Conn]struct{}{}}
	p.wg.Add(1)
	go p.accept()
	return p, nil
}

// Addr returns the address the proxy listens on.
func (p *Proxy) Addr() string {
	return p.ln.Addr().String()
}

// Frozen returns how many connections have frozen so far.
func (p *Proxy) Frozen() int64 {
	return p.frozen.Load()
}

// Close stops the proxy and closes all its connections.
func (p *Proxy) Close() error {
	err := p.ln.Close()
	p.mu.Lock()
	p.closed = true
	for c := range p.conns {
		_ = c.Close()
	}
	p.mu.Unlock()
	p.wg.Wait()
	return err
}

// track adds c to the connections Close closes; false when the proxy is
// closed already (c is closed then).
func (p *Proxy) track(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = c.Close()
		return false
	}
	p.conns[c] = struct{}{}
	return true
}

func (p *Proxy) untrack(c net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.conns, c)
}

func (p *Proxy) accept() {
	defer p.wg.Done()
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.serve(client)
		}()
	}
}

func (p *Proxy) serve(client net.Conn) {
	stats := ConnStats{ID: p.lastID.Add(1)}
	if p.opts.OnClose != nil {
		defer func() { p.opts.OnClose(stats) }()
	}
	if !p.track(client) {
		return
	}
	defer func() {
		_ = client.Close()
		p.untrack(client)
	}()
	target, err := net.DialTimeout("tcp", p.opts.Target, p.opts.DialTimeout)
	if err != nil {
		return
	}
	if !p.track(target) {
		return
	}
	defer func() {
		_ = target.Close()
		p.untrack(target)
	}()

	var froze atomic.Bool
	onFreeze := func() {
		if froze.CompareAndSwap(false, true) {
			p.frozen.Add(1)
		}
	}
	upDone := make(chan struct{})
	go func() {
		defer close(upDone)
		stats.Up, _ = pipe(target, client, p.opts.Up, onFreeze)
	}()
	var downFroze bool
	stats.Down, downFroze = pipe(client, target, p.opts.Down, onFreeze)
	if !downFroze {
		// The target closed while the direction still passed: so does the
		// client's side. After a freeze the filter drops the target's FIN
		// too, and the client keeps waiting until it gives up.
		_ = client.Close()
	}
	<-upDone
	stats.Frozen = froze.Load()
}

// pipe copies src to dst until limit bytes (0: no limit) have passed, then
// reads and drops the rest. It returns when src ends or dst fails, with
// the bytes passed and whether the direction froze.
func pipe(dst, src net.Conn, limit int64, onFreeze func()) (passed int64, froze bool) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 && !froze {
			pass := int64(n)
			if limit > 0 && passed+pass > limit {
				pass = limit - passed
				froze = true
				onFreeze()
			}
			if pass > 0 {
				if _, werr := dst.Write(buf[:pass]); werr != nil {
					return passed, froze
				}
				passed += pass
			}
		}
		if err != nil {
			switch {
			case froze:
			case errors.Is(err, io.EOF):
				// Half-close: the other direction may still pass.
				if tc, ok := dst.(*net.TCPConn); ok {
					_ = tc.CloseWrite()
				}
			default:
				_ = dst.Close()
			}
			return passed, froze
		}
	}
}

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

// Package listen is the client port of a CDN node: TCP connections made
// with the obfuscation secret of the CDN DC (pkg/mtserver/codec), the key
// exchange (pkg/mtserver/handshake) with keys kept in memory, and the
// encrypted messages handed to the session layer. Everything is bounded:
// connections and key exchanges per address, requests in progress per
// connection, the size of a frame, and idle time.
package listen

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/teamgram/teamgram-server/app/service/cdn/internal/metrics"
	"github.com/teamgram/teamgram-server/app/service/cdn/internal/session"
	"github.com/teamgram/teamgram-server/pkg/mtserver/codec"
	"github.com/teamgram/teamgram-server/pkg/mtserver/handshake"
)

const (
	// maxBuffered bounds what a connection may send ahead of a complete
	// frame: a frame of 1 MiB with room for its headers. The requests a
	// node serves are a few dozen bytes.
	maxBuffered = 1<<20 + 64<<10
	// readChunk is the buffer a connection starts with.
	readChunk = 16 << 10
	// firstFrameTimeout is how long a new connection may take to send its
	// first frame.
	firstFrameTimeout = 30 * time.Second
	// writeTimeout bounds one write to a client.
	writeTimeout = 30 * time.Second
	// outQueue is how many packets wait for the writer. Every sender but
	// the quick ACK also waits until its packet is written, so a client
	// that stops reading holds at most MaxInFlight answers in memory.
	outQueue = 64
	// maxConnHandshakes bounds the key exchanges in progress on one
	// connection (a client runs perm, temp and media temp ones), each
	// within handshakeTTL seconds, as in the gateway.
	maxConnHandshakes = 3
	handshakeTTL      = 60
)

var errKeyExchangeLimit = errors.New("listen: too many key exchanges from the address")

// Config of the client port.
type Config struct {
	// Secret is the obfuscation secret of the CDN DC.
	Secret                  codec.Secret
	MaxConnsPerIP           int
	KeyExchangesPerIPMinute int
	MaxInFlight             int
	IdleTimeout             time.Duration
	// HandshakeWorkers bounds the RSA and DH work running at once.
	HandshakeWorkers int
	// Egress counts the bytes written to clients.
	Egress func(n int)
}

// Server serves the client port.
type Server struct {
	cfg     Config
	hs      *handshake.Server
	sess    *session.Server
	limits  *ipLimits
	hsSlots chan struct{}
	nextID  atomic.Int64

	mu     sync.Mutex
	ln     net.Listener
	conns  map[*conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

// New returns the client port of a node with the RSA key of hs and the
// session layer sess.
func New(cfg Config, hs *handshake.Server, sess *session.Server) (*Server, error) {
	if cfg.MaxConnsPerIP < 1 || cfg.KeyExchangesPerIPMinute < 1 || cfg.MaxInFlight < 1 || cfg.IdleTimeout <= 0 || cfg.HandshakeWorkers < 1 {
		return nil, errors.New("listen: the limits must be positive")
	}
	if cfg.Egress == nil {
		cfg.Egress = func(int) {}
	}
	return &Server{
		cfg:     cfg,
		hs:      hs,
		sess:    sess,
		limits:  newIPLimits(cfg.MaxConnsPerIP, cfg.KeyExchangesPerIPMinute),
		hsSlots: make(chan struct{}, cfg.HandshakeWorkers),
		conns:   map[*conn]struct{}{},
	}, nil
}

// Serve accepts connections on ln until Close; it returns nil then, or the
// error that stopped accepting.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	s.ln = ln
	s.mu.Unlock()

	var backoff time.Duration
	for {
		nc, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			if errors.Is(err, net.ErrClosed) {
				return fmt.Errorf("listen: accept: %w", err)
			}
			// Out of descriptors and the like: wait and go on, as net/http
			// does, rather than stop serving.
			backoff = min(max(2*backoff, 5*time.Millisecond), time.Second)
			logx.Errorf("cdn listen: accept: %v; retrying in %s", err, backoff)
			time.Sleep(backoff)
			continue
		}
		backoff = 0
		s.accept(nc)
	}
}

// Close stops accepting, closes every connection and waits for them.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	var err error
	if s.ln != nil {
		err = s.ln.Close()
	}
	conns := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		c.close()
	}
	s.wg.Wait()
	return err
}

func (s *Server) accept(nc net.Conn) {
	subject := ipSubject(nc.RemoteAddr())
	if !s.limits.openConn(subject) {
		metrics.ConnectionsRefused.WithLabelValues("conn").Inc()
		_ = nc.Close()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &conn{
		s:       s,
		id:      s.nextID.Add(1),
		nc:      nc,
		subject: subject,
		ctx:     ctx,
		cancel:  cancel,
		out:     make(chan outItem, outQueue),
		slots:   make(chan struct{}, s.cfg.MaxInFlight),
		buf:     make([]byte, readChunk),
	}
	c.exchange = &handshake.Exchange{
		States: handshake.NewStore(maxConnHandshakes, handshakeTTL),
		Keys:   s.sess.Keys(),
		Now:    func() int64 { return time.Now().Unix() },
		Admit: func() error {
			if !s.limits.allowKeyExchange(subject, time.Now()) {
				metrics.ConnectionsRefused.WithLabelValues("key_exchange").Inc()
				return errKeyExchangeLimit
			}
			return nil
		},
		Submit: c.submitHandshake,
		Reply:  func(answer mtproto.TLObject) { _ = c.Send(handshake.Payload(answer)) },
		Fail: func(err error) {
			metrics.Handshakes.WithLabelValues("none", "error").Inc()
			logx.Infof("cdn conn %d: key exchange: %v", c.id, err)
			c.close()
		},
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		_ = nc.Close()
		s.limits.closeConn(subject)
		return
	}
	s.conns[c] = struct{}{}
	s.wg.Add(2)
	s.mu.Unlock()
	metrics.Connections.Inc()

	go c.writeLoop()
	go c.readLoop()
}

// outItem is what the writer sends: a packet, or a quick ACK.
type outItem struct {
	payload    []byte
	quickAck   uint32
	isQuickAck bool
	// closeAfter closes the connection once the item is written (-404).
	closeAfter bool
	// written, when set, is closed once the item is written.
	written chan struct{}
}

// conn is a client connection. Its reader decodes frames and handles them
// in order; requests run on at most MaxInFlight goroutines; its writer
// alone encodes and writes (the transport's cipher is a stream).
type conn struct {
	s       *Server
	id      int64
	nc      net.Conn
	subject string
	ctx     context.Context
	cancel  context.CancelFunc
	out     chan outItem
	slots   chan struct{}
	workers sync.WaitGroup

	closeOnce sync.Once

	// Owned by the reader.
	codec        codec.Codec
	buf          []byte
	start, end   int
	key          *session.AuthKey // the key the connection uses
	exchange     *handshake.Exchange
	gotFrame     bool
	disconnectAt time.Time
}

// close closes the connection; the reader and writer stop.
func (c *conn) close() {
	c.closeOnce.Do(func() {
		c.cancel()
		_ = c.nc.Close()
	})
}

// Peek and Discard make the read buffer the codec's CodecReader.
func (c *conn) Peek(n int) ([]byte, error) {
	b := c.buf[c.start:c.end]
	if n < 0 {
		return b, nil
	}
	if n > len(b) {
		return b, io.ErrUnexpectedEOF
	}
	return b[:n], nil
}

func (c *conn) Discard(n int) (int, error) {
	if avail := c.end - c.start; n > avail {
		c.start = c.end
		return avail, io.ErrUnexpectedEOF
	}
	c.start += n
	return n, nil
}

// String names the connection in the codec's log lines, without the
// client's address.
func (c *conn) String() string { return fmt.Sprintf("cdn#%d", c.id) }

// space returns free room at the end of the buffer, moving what is left to
// its start and growing it up to maxBuffered; nil when a frame would not
// fit.
func (c *conn) space() []byte {
	if c.start > 0 {
		n := copy(c.buf, c.buf[c.start:c.end])
		c.start, c.end = 0, n
	}
	if c.end == len(c.buf) {
		if len(c.buf) >= maxBuffered {
			return nil
		}
		nb := make([]byte, min(2*len(c.buf), maxBuffered))
		copy(nb, c.buf[:c.end])
		c.buf = nb
	}
	return c.buf[c.end:]
}

func (c *conn) readLoop() {
	defer c.s.wg.Done()
	defer c.finish()
	for {
		deadline := time.Now().Add(c.s.cfg.IdleTimeout)
		if !c.gotFrame {
			deadline = time.Now().Add(firstFrameTimeout)
		}
		if !c.disconnectAt.IsZero() && c.disconnectAt.Before(deadline) {
			deadline = c.disconnectAt
		}
		if err := c.nc.SetReadDeadline(deadline); err != nil {
			return
		}
		room := c.space()
		if room == nil {
			logx.Infof("cdn conn %d: frame over %d bytes", c.id, maxBuffered)
			return
		}
		n, err := c.nc.Read(room)
		c.end += n
		if n > 0 && !c.process() {
			return
		}
		if err != nil {
			return
		}
	}
}

// process handles the complete frames in the buffer; false closes the
// connection.
func (c *conn) process() bool {
	if c.codec == nil {
		cd, err := codec.CreateSecretCodec(c, c.s.cfg.Secret)
		if errors.Is(err, codec.ErrUnexpectedEOF) {
			return true
		}
		if err != nil {
			// Not made with the secret, or not the padded intermediate
			// transport.
			logx.Infof("cdn conn %d: transport: %v", c.id, err)
			return false
		}
		c.codec = cd
	}
	for {
		quickAck, frame, err := c.codec.Decode(c)
		if errors.Is(err, codec.ErrUnexpectedEOF) {
			return true
		}
		if err != nil {
			logx.Infof("cdn conn %d: frame: %v", c.id, err)
			return false
		}
		if frame == nil {
			return true
		}
		c.gotFrame = true
		// The frame lives in the read buffer, which the next read reuses.
		if !c.frame(quickAck, append([]byte(nil), frame...)) {
			return false
		}
	}
}

// frame handles one frame: unencrypted ones are the key exchange.
func (c *conn) frame(quickAck bool, frame []byte) bool {
	if len(frame) < 8 {
		return false
	}
	if binary.LittleEndian.Uint64(frame) == 0 {
		answer, _, err := c.s.hs.Handle(c.exchange, frame)
		if err != nil {
			metrics.Handshakes.WithLabelValues("none", "error").Inc()
			logx.Infof("cdn conn %d: key exchange: %v", c.id, err)
			return false
		}
		if answer != nil {
			return c.Send(handshake.Payload(answer)) == nil
		}
		return true
	}

	k, err := c.s.sess.Key(frame)
	if errors.Is(err, session.ErrUnknownKey) {
		// Transport error -404: the client drops the key and makes a new
		// one.
		c.send404()
		return true
	}
	if err != nil {
		return false
	}
	if c.key != nil && c.key.ID != k.ID {
		// One key per connection, as the gateway has it.
		return false
	}
	c.key = k
	if err := c.s.sess.Handle(c, k, frame, quickAck); err != nil {
		logx.Infof("cdn conn %d: %v", c.id, err)
		return false
	}
	return true
}

// send404 sends transport error -404 and closes the connection after it.
func (c *conn) send404() {
	p := make([]byte, 4)
	binary.LittleEndian.PutUint32(p, uint32(0xfffffe6c)) // int32(-404)
	select {
	case c.out <- outItem{payload: p, closeAfter: true}:
	case <-c.ctx.Done():
	}
}

// finish runs when the reader stops: it closes the connection and waits
// for its requests before it lets the address count go.
func (c *conn) finish() {
	c.close()
	c.workers.Wait()
	c.s.limits.closeConn(c.subject)
	c.s.mu.Lock()
	delete(c.s.conns, c)
	c.s.mu.Unlock()
	metrics.Connections.Dec()
}

func (c *conn) writeLoop() {
	defer c.s.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		case it := <-c.out:
			var (
				b   []byte
				err error
			)
			if it.isQuickAck {
				b = c.codec.EncodeQuickAck(it.quickAck)
			} else {
				b, err = c.codec.Encode(nil, &mtproto.MTPRawMessage{Payload: it.payload})
			}
			if err == nil && len(b) > 0 {
				if err = c.nc.SetWriteDeadline(time.Now().Add(writeTimeout)); err == nil {
					var n int
					n, err = c.nc.Write(b)
					c.s.cfg.Egress(n)
				}
			}
			if err != nil || it.closeAfter {
				c.close()
				return
			}
			if it.written != nil {
				close(it.written)
			}
		}
	}
}

// Send queues a packet and waits until it is written (session.Conn): a
// request keeps its slot, and its answer stays bounded, until the client
// has taken it.
func (c *conn) Send(packet []byte) error {
	written := make(chan struct{})
	select {
	case c.out <- outItem{payload: packet, written: written}:
	case <-c.ctx.Done():
		return net.ErrClosed
	}
	select {
	case <-written:
		return nil
	case <-c.ctx.Done():
		return net.ErrClosed
	}
}

// SendQuickAck queues a quick ACK (session.Conn).
func (c *conn) SendQuickAck(token uint32) error {
	select {
	case c.out <- outItem{quickAck: token, isQuickAck: true}:
		return nil
	case <-c.ctx.Done():
		return net.ErrClosed
	}
}

// Go runs a request, waiting for one of MaxInFlight slots (session.Conn).
func (c *conn) Go(f func()) bool {
	select {
	case c.slots <- struct{}{}:
	case <-c.ctx.Done():
		return false
	}
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		defer func() { <-c.slots }()
		f()
	}()
	return true
}

// DelayDisconnect closes the connection d from now (session.Conn); d of
// zero or less cancels it. It is called by the reader, which applies it
// with its next read.
func (c *conn) DelayDisconnect(d time.Duration) {
	if d <= 0 {
		c.disconnectAt = time.Time{}
		return
	}
	c.disconnectAt = time.Now().Add(d)
}

// submitHandshake runs a key exchange step off the reader, on one of
// HandshakeWorkers slots.
func (c *conn) submitHandshake(f func()) error {
	if c.ctx.Err() != nil {
		return net.ErrClosed
	}
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		select {
		case c.s.hsSlots <- struct{}{}:
		case <-c.ctx.Done():
			return
		}
		defer func() { <-c.s.hsSlots }()
		f()
	}()
	return nil
}

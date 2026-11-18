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

package mtclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// The path probe of the gateway (task 113, ADR 0073; the contract is in
// docs/tasks/113-smart-path-selection.md, "Контракт пробы"): on a fresh
// connection of a path the client sends
//
//	pluma.probe#5c316d30 nonce:int128 down:int pad:bytes = pluma.Probe;
//
// unencrypted, and the gateway answers with the down bytes asked in
// messages
//
//	pluma.probeData#6ab3780b nonce:int128 seq:int total:int server_ms:long pad:bytes = pluma.ProbeData;
//
// The client times the bytes as they come off the socket: a path that
// freezes after the first ~16 KB of a connection (task 092) goes silent in
// the middle of the answer.

const (
	crc32PlumaProbe     = 0x5c316d30
	crc32PlumaProbeData = 0x6ab3780b

	// The defaults of ProbeOptions: the gate probe of the brief.
	DefaultProbeDown     = 64 << 10
	DefaultProbeUp       = 20 << 10
	DefaultProbeStall    = 1500 * time.Millisecond
	DefaultProbeDeadline = 6 * time.Second

	// probeScoreBytes is the size of the notional answer the score of a
	// path is the time of.
	probeScoreBytes = 256 << 10
)

// ProbeOutcome is what a probe found the path to be.
type ProbeOutcome string

const (
	// ProbeOK: all the bytes asked arrived.
	ProbeOK ProbeOutcome = "ok"
	// ProbeSlow: not all of them by the deadline, but they kept coming.
	ProbeSlow ProbeOutcome = "slow"
	// ProbeFrozen: the path carried some bytes (of the TLS or WebSocket
	// handshake, resPQ or the answer), then none for the stall time.
	ProbeFrozen ProbeOutcome = "frozen"
	// ProbeFailed: no connection, the connection closed, or no byte by the
	// deadline.
	ProbeFailed ProbeOutcome = "failed"
	// ProbeUnsupported: the gateway answered req_pq_multi and closed the
	// connection on pluma.probe without a byte of an answer, as a gateway
	// without probes (or one that refused this probe) does. Not a verdict
	// on the path.
	ProbeUnsupported ProbeOutcome = "unsupported"
)

// ProbeOptions configures a probe; zero fields take the defaults.
type ProbeOptions struct {
	// Down is the number of bytes asked (DefaultProbeDown); Up the pad sent
	// with the request (DefaultProbeUp; -1 sends none).
	Down int
	Up   int
	// Stall is how long the path may be silent (DefaultProbeStall), and
	// Deadline how long the answer may take from the request
	// (DefaultProbeDeadline).
	Stall    time.Duration
	Deadline time.Duration
	// ResPQ sends req_pq_multi first and the probe after resPQ on the same
	// connection, as the probing clients that start with the key exchange
	// do; RTT is then the time of req_pq_multi.
	ResPQ bool
}

func (o *ProbeOptions) defaults() {
	if o.Down == 0 {
		o.Down = DefaultProbeDown
	}
	switch {
	case o.Up == 0:
		o.Up = DefaultProbeUp
	case o.Up < 0:
		o.Up = 0
	}
	if o.Stall == 0 {
		o.Stall = DefaultProbeStall
	}
	if o.Deadline == 0 {
		o.Deadline = DefaultProbeDeadline
	}
}

// ProbeResult is what a probe measured.
type ProbeResult struct {
	Outcome ProbeOutcome
	// RTT is the time from req_pq_multi to resPQ (ResPQ) or from
	// pluma.probe to the first byte after it.
	RTT time.Duration
	// BPS is the speed of the answer in bytes a second: the bytes that came
	// off the socket after the first read of the answer, over the time from
	// that read to the last one; 0 when unknown.
	BPS float64
	// Received is the pad bytes of the pluma.probeData received whole, of
	// Down asked (the gateway clamps it to 16..512 KiB); Total the number
	// of messages the gateway announced.
	Received int
	Down     int
	Total    int
	// Elapsed is the time from pluma.probe to the outcome.
	Elapsed time.Duration
	// Err says why a probe did not end ok.
	Err error
}

// Score returns the score of the path (task 113, "Метрика выбора"): RTT
// plus the time of a 256 KiB answer at BPS; 0 when the speed is unknown.
func (r *ProbeResult) Score() time.Duration {
	if r.BPS <= 0 {
		return 0
	}
	return r.RTT + time.Duration(float64(probeScoreBytes)/r.BPS*float64(time.Second))
}

func (r *ProbeResult) String() string {
	s := fmt.Sprintf("%s rtt=%s bps=%.0f received=%d/%d elapsed=%s", r.Outcome, r.RTT.Round(time.Millisecond), r.BPS, r.Received, r.Down, r.Elapsed.Round(time.Millisecond))
	if r.Err != nil {
		s += " err=" + r.Err.Error()
	}
	return s
}

// ProbeTCP probes the intermediate transport of the gateway at addr
// (host:port).
func ProbeTCP(ctx context.Context, addr string, opts ProbeOptions) *ProbeResult {
	opts.defaults()
	start := time.Now()
	dctx, cancel := context.WithTimeout(ctx, opts.Deadline)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(dctx, "tcp", addr)
	if err != nil {
		return &ProbeResult{Outcome: ProbeFailed, Down: opts.Down, Err: err}
	}
	pc := newProbeConn(conn)
	defer pc.Close()
	t, err := NewIntermediate(pc)
	if err != nil {
		return &ProbeResult{Outcome: ProbeFailed, Down: opts.Down, Err: err}
	}
	return runProbe(ctx, t, pc, time.Since(start), opts)
}

// ProbeWebSocket probes the WebSocket transport of the gateway at url
// (ws:// or wss://, as DialWebSocket) with the extra header.
func ProbeWebSocket(ctx context.Context, url string, header http.Header, opts ProbeOptions) *ProbeResult {
	return ProbeWebSocketVia(ctx, url, header, nil, nil, opts)
}

// ProbeWebSocketVia is ProbeWebSocket with the TCP dialer and the TLS
// config of DialWebSocketVia (nil: the defaults).
func ProbeWebSocketVia(ctx context.Context, url string, header http.Header, dial func(ctx context.Context, network, addr string) (net.Conn, error), tlsConfig *tls.Config, opts ProbeOptions) *ProbeResult {
	opts.defaults()
	start := time.Now()
	dctx, cancel := context.WithTimeout(ctx, opts.Deadline)
	defer cancel()
	if dial == nil {
		var d net.Dialer
		dial = d.DialContext
	}
	var pc *probeConn
	counted := func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		pc = newProbeConn(conn)
		return pc, nil
	}
	t, err := DialWebSocketVia(dctx, url, header, counted, tlsConfig)
	if err != nil {
		if pc != nil {
			_ = pc.Close()
		}
		return &ProbeResult{Outcome: ProbeFailed, Down: opts.Down, Err: err}
	}
	defer t.Close()
	return runProbe(ctx, t, pc, time.Since(start), opts)
}

// probeConn is a connection that counts and times the bytes read from it.
type probeConn struct {
	net.Conn
	mu       sync.Mutex
	read     int
	lastRead time.Time
	// reads since mark: their bytes, the first and the last one.
	marked     bool
	sinceMark  int
	firstBytes int
	firstAt    time.Time
}

func newProbeConn(c net.Conn) *probeConn {
	return &probeConn{Conn: c}
}

func (c *probeConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		now := time.Now()
		c.mu.Lock()
		c.read += n
		c.lastRead = now
		if c.marked {
			if c.sinceMark == 0 {
				c.firstBytes, c.firstAt = n, now
			}
			c.sinceMark += n
		}
		c.mu.Unlock()
	}
	return n, err
}

// mark starts counting the reads of the answer.
func (c *probeConn) mark() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.marked, c.sinceMark, c.firstBytes, c.firstAt = true, 0, 0, time.Time{}
}

type probeConnStats struct {
	read       int
	lastRead   time.Time
	sinceMark  int
	firstBytes int
	firstAt    time.Time
}

func (c *probeConn) stats() probeConnStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return probeConnStats{read: c.read, lastRead: c.lastRead, sinceMark: c.sinceMark, firstBytes: c.firstBytes, firstAt: c.firstAt}
}

type probePacket struct {
	p   []byte
	err error
}

// runProbe runs the probe on transport t over pc, connected in connected.
func runProbe(ctx context.Context, t Transport, pc *probeConn, connected time.Duration, opts ProbeOptions) *ProbeResult {
	res := &ProbeResult{Down: opts.Down}
	packets := make(chan probePacket, 64)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			p, err := t.ReadPacket()
			select {
			case packets <- probePacket{p: p, err: err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	// Closing the connection ends the reader.
	defer func() { _ = t.Close() }()

	// wait returns the next packet, or the outcome when the path goes
	// silent for opts.Stall (counted from the later of the last byte read
	// and sent + rtt) or deadline passes.
	wait := func(sent time.Time, rtt time.Duration, deadline time.Time) ([]byte, ProbeOutcome, error) {
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case pk := <-packets:
				if pk.err != nil {
					return nil, ProbeFailed, pk.err
				}
				return pk.p, "", nil
			case <-ctx.Done():
				return nil, ProbeFailed, ctx.Err()
			case now := <-tick.C:
				st := pc.stats()
				from := sent.Add(rtt)
				if st.lastRead.After(from) {
					from = st.lastRead
				}
				if st.read > 0 && now.Sub(from) >= opts.Stall {
					return nil, ProbeFrozen, fmt.Errorf("mtclient: probe: no byte for %s", now.Sub(from).Round(time.Millisecond))
				}
				if !now.Before(deadline) {
					if st.read == 0 {
						return nil, ProbeFailed, errors.New("mtclient: probe: no byte by the deadline")
					}
					return nil, ProbeSlow, errors.New("mtclient: probe: not all by the deadline")
				}
			}
		}
	}

	var msgID int64
	plain := func(body []byte) []byte {
		msgID = nextMsgID(msgID, 0)
		p := make([]byte, 20+len(body))
		binary.LittleEndian.PutUint64(p[8:], uint64(msgID))
		binary.LittleEndian.PutUint32(p[16:], uint32(len(body)))
		copy(p[20:], body)
		return p
	}

	rtt := connected
	if opts.ResPQ {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			res.Outcome, res.Err = ProbeFailed, err
			return res
		}
		body, err := encodeObject(&mtproto.TLReqPqMulti{Constructor: mtproto.CRC32_req_pq_multi, Nonce: nonce}, 0)
		if err != nil {
			res.Outcome, res.Err = ProbeFailed, err
			return res
		}
		sent := time.Now()
		if err = t.WritePacket(plain(body)); err != nil {
			res.Outcome, res.Err = ProbeFailed, err
			return res
		}
		p, outcome, err := wait(sent, connected, sent.Add(opts.Deadline))
		if outcome != "" {
			res.Outcome, res.Err = outcome, err
			if outcome == ProbeSlow {
				// No resPQ at all is no answer to measure.
				res.Outcome = ProbeFailed
			}
			return res
		}
		if !isResPQ(p, nonce) {
			res.Outcome, res.Err = ProbeFailed, errors.New("mtclient: probe: req_pq_multi not answered by its resPQ")
			return res
		}
		rtt = time.Since(sent)
		res.RTT = rtt
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		res.Outcome, res.Err = ProbeFailed, err
		return res
	}
	req := probeRequest(nonce, opts.Down, opts.Up)
	pc.mark()
	sent := time.Now()
	if err := t.WritePacket(plain(req)); err != nil {
		res.Outcome, res.Err = ProbeFailed, err
		return res
	}
	defer func() {
		res.Elapsed = time.Since(sent)
		st := pc.stats()
		if !opts.ResPQ && !st.firstAt.IsZero() {
			res.RTT = st.firstAt.Sub(sent)
		}
		if span := st.lastRead.Sub(st.firstAt); st.sinceMark > st.firstBytes && span > 0 {
			res.BPS = float64(st.sinceMark-st.firstBytes) / span.Seconds()
		}
	}()

	deadline := sent.Add(opts.Deadline)
	for next := 0; ; next++ {
		p, outcome, err := wait(sent, rtt, deadline)
		if outcome != "" {
			res.Outcome, res.Err = outcome, err
			if outcome == ProbeFailed && opts.ResPQ && next == 0 && pc.stats().sinceMark == 0 && isClosed(err) {
				res.Outcome = ProbeUnsupported
			}
			return res
		}
		seq, total, pad, err := parseProbeData(p, nonce)
		if err != nil {
			res.Outcome, res.Err = ProbeFailed, err
			return res
		}
		if seq != next || (res.Total != 0 && total != res.Total) || total < 1 {
			res.Outcome, res.Err = ProbeFailed, fmt.Errorf("mtclient: probe: pluma.probeData %d of %d after %d of %d", seq, total, next, res.Total)
			return res
		}
		res.Total = total
		res.Received += pad
		if seq == total-1 {
			res.Outcome = ProbeOK
			if res.Received != clampDown(opts.Down) {
				res.Outcome, res.Err = ProbeFailed, fmt.Errorf("mtclient: probe: %d bytes of %d", res.Received, opts.Down)
			}
			return res
		}
	}
}

// clampDown is the number of bytes the gateway sends when down are asked.
func clampDown(down int) int {
	return min(max(down, 16<<10), 512<<10)
}

// isClosed reports whether err is the connection closing.
func isClosed(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET)
}

// probeRequest serializes pluma.probe with up bytes of random pad.
func probeRequest(nonce [16]byte, down, up int) []byte {
	b := make([]byte, 0, 4+16+4+4+up+3)
	b = binary.LittleEndian.AppendUint32(b, crc32PlumaProbe)
	b = append(b, nonce[:]...)
	b = binary.LittleEndian.AppendUint32(b, uint32(int32(down))) //nolint:gosec // down is a probe size, far below 2^31
	pad := make([]byte, up)
	_, _ = rand.Read(pad) // crypto/rand.Read never fails (Go 1.24)
	return appendTLBytes(b, pad)
}

// appendTLBytes appends the TL bytes value v to b.
func appendTLBytes(b, v []byte) []byte {
	start := len(b)
	if len(v) < 254 {
		b = append(b, byte(len(v)))
	} else {
		b = append(b, 254, byte(len(v)), byte(len(v)>>8), byte(len(v)>>16))
	}
	b = append(b, v...)
	for (len(b)-start)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// parseProbeData parses the unencrypted message p as pluma.probeData of
// the probe with nonce; it returns seq, total and the length of the pad.
func parseProbeData(p []byte, nonce [16]byte) (seq, total, pad int, err error) {
	if len(p) < 20+40 || binary.LittleEndian.Uint64(p) != 0 {
		return 0, 0, 0, errors.New("mtclient: probe: not an unencrypted message")
	}
	n := int(binary.LittleEndian.Uint32(p[16:]))
	if n > len(p)-20 || n < 40 {
		return 0, 0, 0, errors.New("mtclient: probe: bad message length")
	}
	d := p[20 : 20+n]
	if c := binary.LittleEndian.Uint32(d); c != crc32PlumaProbeData {
		return 0, 0, 0, fmt.Errorf("mtclient: probe: answered by %#08x, not pluma.probeData", c)
	}
	if !bytes.Equal(d[4:20], nonce[:]) {
		return 0, 0, 0, errors.New("mtclient: probe: pluma.probeData of another probe")
	}
	seq = int(int32(binary.LittleEndian.Uint32(d[20:])))
	total = int(int32(binary.LittleEndian.Uint32(d[24:])))
	switch l := d[36]; {
	case l < 254:
		pad = int(l)
		if 36+1+pad > len(d) {
			return 0, 0, 0, errors.New("mtclient: probe: pad past the message")
		}
	case l == 254:
		pad = int(d[37]) | int(d[38])<<8 | int(d[39])<<16
		if 36+4+pad > len(d) {
			return 0, 0, 0, errors.New("mtclient: probe: pad past the message")
		}
	default:
		return 0, 0, 0, errors.New("mtclient: probe: bad pad")
	}
	return seq, total, pad, nil
}

// isResPQ reports whether the unencrypted message p is resPQ with nonce.
func isResPQ(p []byte, nonce []byte) bool {
	if len(p) < 20+4+16 || binary.LittleEndian.Uint64(p) != 0 {
		return false
	}
	n := binary.LittleEndian.Uint32(p[16:])
	if uint64(n) > uint64(len(p)-20) {
		return false
	}
	d := mtproto.NewDecodeBuf(p[20 : 20+n])
	res, ok := d.Object().(*mtproto.TLResPQ)
	return ok && d.GetError() == nil && bytes.Equal(res.GetNonce(), nonce)
}

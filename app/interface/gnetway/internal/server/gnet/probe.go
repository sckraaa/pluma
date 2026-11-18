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

package gnet

import (
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtserver/handshake"

	"github.com/panjf2000/gnet/v2"
	"github.com/zeromicro/go-zero/core/logx"
)

// The path probe (task 113, ADR 0073; the messages are in
// pkg/mtserver/handshake/probe.go): a client asks for down bytes on a
// connection of the path it wants to judge, on any entry, before or
// without a key. Each request costs the gateway up to 512 KiB of its
// outgoing link and gives the client 3 to 10 times what it sent, so it is
// bounded per connection, per address and minute, per address at once and
// for the whole gateway at once. A refused or malformed request closes the
// connection without an answer, as any unexpected unencrypted message did
// before probes: a client cannot tell a refusal from a gateway without
// probes, and judges no path by either.

const (
	// maxConnProbes: a client probes a connection once, to let it carry
	// MTProto, or twice when it measures on it too.
	maxConnProbes = 2
	// defaultProbeConcurrency is GnetwayConfig.ProbeConcurrency's default.
	defaultProbeConcurrency = 32
	// maxAddrProbesInFlight: the probes of one address being sent at once.
	// A device probes at most three paths at a time, all of which reach the
	// gateway with its address (the edges pass it on).
	maxAddrProbesInFlight = 8
	// probeHoldMax: a probe whose answer is still queued after this long
	// gives its place up. Its connection is stuck (a filtered path that
	// takes nothing more, a client that does not read) and uses no link.
	probeHoldMax = 10 * time.Second
)

// The results of gnetway_probe_total.
const (
	probeResultServed = "served"
	// probeResultRefusedRate: the address asked too many probes in the last
	// minute or has too many being sent.
	probeResultRefusedRate = "refused_rate"
	// probeResultRefusedConn: the connection asked more than maxConnProbes.
	probeResultRefusedConn = "refused_conn"
	// probeResultRefusedBusy: the gateway is sending ProbeConcurrency
	// probes already.
	probeResultRefusedBusy = "refused_busy"
	probeResultBad         = "bad"
)

// probeSlots counts the probes whose answers the gateway is sending: a probe
// holds its place from the request until its answer has left the gateway
// (the outbound buffer of the connection and the send queue of its socket
// are empty), its connection closes, or probeHoldMax passes.
type probeSlots struct {
	mu      sync.Mutex
	max     int
	total   int
	byAddr  map[string]int
	byConns map[int64][]probeHold
}

type probeHold struct {
	subject string
	since   time.Time
}

// newProbeSlots returns places for max probes at once; 0 takes the default.
func newProbeSlots(max int) *probeSlots {
	if max <= 0 {
		max = defaultProbeConcurrency
	}
	return &probeSlots{max: max, byAddr: map[string]int{}, byConns: map[int64][]probeHold{}}
}

// acquire takes a place for a probe of connection connId from subject; it
// returns the result to refuse it with, or "" when it has the place.
func (p *probeSlots) acquire(connId int64, subject string, now time.Time) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.byAddr[subject] >= maxAddrProbesInFlight:
		return probeResultRefusedRate
	case p.total >= p.max:
		return probeResultRefusedBusy
	}
	p.total++
	p.byAddr[subject]++
	p.byConns[connId] = append(p.byConns[connId], probeHold{subject: subject, since: now})
	return ""
}

// release gives up the places of connection connId.
func (p *probeSlots) release(connId int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, h := range p.byConns[connId] {
		p.total--
		if p.byAddr[h.subject]--; p.byAddr[h.subject] <= 0 {
			delete(p.byAddr, h.subject)
		}
	}
	delete(p.byConns, connId)
}

// expire gives up the places held since before now-probeHoldMax and returns
// the connections that still hold any.
func (p *probeSlots) expire(now time.Time) []int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	held := make([]int64, 0, len(p.byConns))
	for connId, holds := range p.byConns {
		kept := holds[:0]
		for _, h := range holds {
			if now.Sub(h.since) < probeHoldMax {
				kept = append(kept, h)
				continue
			}
			p.total--
			if p.byAddr[h.subject]--; p.byAddr[h.subject] <= 0 {
				delete(p.byAddr, h.subject)
			}
		}
		if len(kept) == 0 {
			delete(p.byConns, connId)
			continue
		}
		p.byConns[connId] = kept
		held = append(held, connId)
	}
	return held
}

// inFlight returns the number of places taken.
func (p *probeSlots) inFlight() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.total
}

// queuedBytes returns the bytes written to c that have not left the
// gateway yet: those in its outbound buffer and in its socket's send queue.
func queuedBytes(c gnet.Conn) int {
	return c.OutboundBuffered() + socketQueued(c.Fd())
}

// onProbe answers pluma.probe on connection c: total messages
// pluma.probeData, written at once, in order.
func (s *Server) onProbe(c gnet.Conn, ctx *connContext, frame []byte) gnet.Action {
	p, err := handshake.ParseProbe(frame)
	if err != nil {
		metricProbe.Inc(ctx.entry, ctx.via, probeResultBad)
		logx.Errorf("conn(%d) probe: %v", c.ConnId(), err)
		return gnet.Close
	}
	if ctx.probes >= maxConnProbes {
		metricProbe.Inc(ctx.entry, ctx.via, probeResultRefusedConn)
		logx.Errorf("conn(%d) probe refused: %d probes on the connection already", c.ConnId(), ctx.probes)
		return gnet.Close
	}
	ctx.probes++

	subject := ipSubject(ctx.clientIp)
	now := time.Now()
	var result string
	if !s.limits.allowProbe(subject, now) {
		result = probeResultRefusedRate
	} else {
		result = s.probeSlots.acquire(c.ConnId(), subject, now)
	}
	if result != "" {
		if result == probeResultRefusedRate {
			metricLimitRefused.Inc("probe")
		}
		metricProbe.Inc(ctx.entry, ctx.via, result)
		logx.Errorf("conn(%d) probe refused: %s", c.ConnId(), result)
		return gnet.Close
	}

	metricProbeBytes.Add(float64(len(frame)), ctx.entry, ctx.via, "in")
	written := ctx.bytesOut
	defer func() {
		metricProbeBytes.Add(float64(ctx.bytesOut-written), ctx.entry, ctx.via, "out")
	}()
	total := p.Chunks()
	for seq := 0; seq < total; seq++ {
		data := handshake.ProbeData(p, seq, time.Now().UnixMilli(), mtproto.GenerateMessageId())
		if err := UnThreadSafeWrite(c, &mtproto.MTPRawMessage{Payload: data}); err != nil {
			// The connection is going away: OnClose gives the place up.
			logx.Errorf("conn(%d) probe: %v", c.ConnId(), err)
			return gnet.Close
		}
	}
	metricProbe.Inc(ctx.entry, ctx.via, probeResultServed)
	if s.probeQueued(c) == 0 {
		s.probeSlots.release(c.ConnId())
	}
	return gnet.None
}

// releaseSentProbes gives up the places of the probes whose answers have
// left the gateway or have waited probeHoldMax (OnTick, every second).
func (s *Server) releaseSentProbes(now time.Time) {
	for _, connId := range s.probeSlots.expire(now) {
		s.trigger(connId, func(c gnet.Conn) {
			if s.probeQueued(c) == 0 {
				s.probeSlots.release(connId)
			}
		})
	}
}

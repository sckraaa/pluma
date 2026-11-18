package gnet

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/mtserver/handshake"

	wsproto "github.com/gobwas/ws"
	"github.com/panjf2000/gnet/v2"
)

// probeFrame is pluma.probe as a client serializes it: an unencrypted
// message asking down bytes with pad bytes of its own.
func probeFrame(nonce byte, down int32, pad int) []byte {
	var d []byte
	d = binary.LittleEndian.AppendUint32(d, 0x5c316d30)
	d = append(d, bytes.Repeat([]byte{nonce}, 16)...)
	d = binary.LittleEndian.AppendUint32(d, uint32(down))
	if pad < 254 {
		d = append(d, byte(pad))
	} else {
		d = append(d, 254, byte(pad), byte(pad>>8), byte(pad>>16))
	}
	d = append(d, make([]byte, pad)...)
	for len(d)%4 != 0 {
		d = append(d, 0)
	}
	var m []byte
	m = binary.LittleEndian.AppendUint64(m, 0)
	m = binary.LittleEndian.AppendUint64(m, uint64(time.Now().Unix())<<32)
	m = binary.LittleEndian.AppendUint32(m, uint32(len(d)))
	return append(m, d...)
}

// checkProbeAnswer checks that msgs are the answer to a probe with nonce
// byte nonce for down bytes: pluma.probeData seq 0..total-1, pads adding up
// to down.
func checkProbeAnswer(t *testing.T, msgs [][]byte, nonce byte, down int) {
	t.Helper()
	total := (down + handshake.ProbeChunkSize - 1) / handshake.ProbeChunkSize
	if len(msgs) != total {
		t.Fatalf("%d messages, want %d", len(msgs), total)
	}
	sum := 0
	for i, m := range msgs {
		if len(m) < 20+40 || binary.LittleEndian.Uint64(m) != 0 {
			t.Fatalf("message %d: not unencrypted", i)
		}
		d := m[20:]
		if binary.LittleEndian.Uint32(d) != 0x6ab3780b {
			t.Fatalf("message %d: constructor %#08x", i, binary.LittleEndian.Uint32(d))
		}
		if !bytes.Equal(d[4:20], bytes.Repeat([]byte{nonce}, 16)) {
			t.Fatalf("message %d: nonce %x", i, d[4:20])
		}
		if seq, n := binary.LittleEndian.Uint32(d[20:]), binary.LittleEndian.Uint32(d[24:]); int(seq) != i || int(n) != total {
			t.Fatalf("message %d: seq %d total %d", i, seq, n)
		}
		if ms := int64(binary.LittleEndian.Uint64(d[28:])); time.Since(time.UnixMilli(ms)).Abs() > time.Minute {
			t.Fatalf("message %d: server_ms %d", i, ms)
		}
		padLen := int(d[36])
		if d[36] == 254 {
			padLen = int(d[37]) | int(d[38])<<8 | int(d[39])<<16
		}
		if padLen > handshake.ProbeChunkSize {
			t.Fatalf("message %d: pad of %d bytes", i, padLen)
		}
		sum += padLen
	}
	if sum != down {
		t.Fatalf("the pads add up to %d, want %d", sum, down)
	}
}

func fromAddr(ip string) func(ctx *connContext) {
	return func(ctx *connContext) { ctx.clientIp = ip }
}

// The gateway answers pluma.probe at once with the bytes asked, in
// pluma.probeData of at most 16 KiB; the key exchange on the connection
// goes on before and after it as if there were no probe.
func TestProbeAnswer(t *testing.T) {
	h := newTestHarness(t)
	c := h.newConn(1, fromAddr("203.0.113.20"))
	tc := newTestClient(t, &h.key.PublicKey)

	if a := h.sendFrame(c, tc.reqPqMulti()); a != gnet.None {
		t.Fatalf("req_pq_multi: %v", a)
	}
	h.waitWrites(c, 1)
	tc.onResPQ(h.lastUnencrypted(c))

	if a := h.sendFrame(c, probeFrame(7, 64<<10, 20<<10)); a != gnet.None {
		t.Fatalf("pluma.probe: %v", a)
	}
	var answer [][]byte
	h.do(func() { answer = append(answer, c.out[1:]...) })
	checkProbeAnswer(t, answer, 7, 64<<10)

	if a := h.sendFrame(c, tc.reqDHParams(false)); a != gnet.None {
		t.Fatalf("req_DH_params after the probe: %v", a)
	}
	h.waitWrites(c, 1+4+1)
	tc.onServerDHParams(h.lastUnencrypted(c))
	if a := h.sendFrame(c, tc.setClientDHParams()); a != gnet.None {
		t.Fatalf("set_client_DH_params: %v", a)
	}
	h.waitWrites(c, 1+4+2)
	tc.onDHGen(h.lastUnencrypted(c))

	// Asked out of bounds, the gateway sends its bounds.
	c2 := h.newConn(2, fromAddr("203.0.113.20"))
	h.sendFrame(c2, probeFrame(1, 1, 0))
	h.sendFrame(c2, probeFrame(2, 1<<30, 0))
	h.do(func() {
		checkProbeAnswer(t, c2.out[:1], 1, handshake.ProbeMinDown)
		checkProbeAnswer(t, c2.out[1:], 2, handshake.ProbeMaxDown)
	})
}

// Without the probe a gateway closes the connection on pluma.probe, as on
// any unknown unencrypted message; a malformed probe still closes it.
func TestProbeMalformed(t *testing.T) {
	h := newTestHarness(t)
	c := h.newConn(1, nil)
	if a := h.sendFrame(c, probeFrame(1, 64<<10, handshake.ProbeMaxPad+4)); a != gnet.Close {
		t.Fatalf("a probe with a pad over the bound: %v, want the connection closed", a)
	}
	h.do(func() {
		if len(c.out) != 0 {
			t.Fatalf("%d messages answered", len(c.out))
		}
	})
}

// A connection gets two probes; the third closes it.
func TestProbeLimitPerConnection(t *testing.T) {
	h := newTestHarness(t)
	c := h.newConn(1, fromAddr("203.0.113.21"))
	for i := byte(1); i <= maxConnProbes; i++ {
		if a := h.sendFrame(c, probeFrame(i, 16<<10, 0)); a != gnet.None {
			t.Fatalf("probe %d: %v", i, a)
		}
	}
	if a := h.sendFrame(c, probeFrame(9, 16<<10, 0)); a != gnet.Close {
		t.Fatalf("probe %d on a connection: %v, want the connection closed", maxConnProbes+1, a)
	}
	h.do(func() {
		if len(c.out) != maxConnProbes {
			t.Fatalf("%d messages answered, want %d", len(c.out), maxConnProbes)
		}
	})
}

// An address gets ProbesPerIPMinute probes a minute over all its
// connections; others go on.
func TestProbeLimitPerAddress(t *testing.T) {
	h := newTestHarness(t)
	h.s.limits = newIPLimits(10, 10, 3)
	for i := int64(1); i <= 3; i++ {
		c := h.newConn(i, fromAddr("203.0.113.22"))
		if a := h.sendFrame(c, probeFrame(byte(i), 16<<10, 0)); a != gnet.None {
			t.Fatalf("probe %d: %v", i, a)
		}
	}
	c := h.newConn(4, fromAddr("203.0.113.22"))
	if a := h.sendFrame(c, probeFrame(4, 16<<10, 0)); a != gnet.Close {
		t.Fatalf("a probe over the budget of the address: %v, want the connection closed", a)
	}
	// The key exchange budget is apart.
	h.handshake(h.newConn(5, fromAddr("203.0.113.22")), false)
	if a := h.sendFrame(h.newConn(6, fromAddr("203.0.113.23")), probeFrame(6, 16<<10, 0)); a != gnet.None {
		t.Fatalf("a probe of another address: %v", a)
	}
}

// The probes being sent are bounded for the gateway and for one address;
// a probe gives its place up once its answer has left the gateway, when its
// connection closes, or after probeHoldMax.
func TestProbeConcurrency(t *testing.T) {
	h := newTestHarness(t)
	h.s.probeSlots = newProbeSlots(maxAddrProbesInFlight + 1)
	h.do(func() { h.queued = 1 }) // nothing leaves

	id := int64(0)
	probe := func(ip string) gnet.Action {
		id++
		return h.sendFrame(h.newConn(id, fromAddr(ip)), probeFrame(byte(id), 16<<10, 0))
	}
	for i := 0; i < maxAddrProbesInFlight; i++ {
		if a := probe("203.0.113.30"); a != gnet.None {
			t.Fatalf("probe %d of the address: %v", i+1, a)
		}
	}
	if a := probe("203.0.113.30"); a != gnet.Close {
		t.Fatalf("probe %d of the address at once: %v, want the connection closed", maxAddrProbesInFlight+1, a)
	}
	if a := probe("203.0.113.31"); a != gnet.None {
		t.Fatalf("a probe of another address: %v", a)
	}
	if a := probe("203.0.113.32"); a != gnet.Close {
		t.Fatalf("a probe over the bound of the gateway: %v, want the connection closed", a)
	}
	if n := h.s.probeSlots.inFlight(); n != maxAddrProbesInFlight+1 {
		t.Fatalf("%d probes in flight", n)
	}

	// The connection closes.
	h.do(func() { h.s.OnClose(h.conns[1], nil) })
	if n := h.s.probeSlots.inFlight(); n != maxAddrProbesInFlight {
		t.Fatalf("%d probes in flight after a close", n)
	}
	// The answers leave.
	h.s.releaseSentProbes(time.Now())
	if n := h.s.probeSlots.inFlight(); n != maxAddrProbesInFlight {
		t.Fatalf("%d probes in flight while their answers wait", n)
	}
	h.do(func() { h.queued = 0 })
	h.s.releaseSentProbes(time.Now())
	if n := h.s.probeSlots.inFlight(); n != 0 {
		t.Fatalf("%d probes in flight after their answers left", n)
	}
	// Sent at once, a probe does not hold its place.
	if a := probe("203.0.113.32"); a != gnet.None {
		t.Fatalf("a probe after the places came free: %v", a)
	}
	if n := h.s.probeSlots.inFlight(); n != 0 {
		t.Fatalf("%d probes in flight after an answer left at once", n)
	}

	// Stuck answers give their places up after probeHoldMax.
	h.do(func() { h.queued = 1 })
	for i := 0; i < 3; i++ {
		if a := probe("203.0.113.33"); a != gnet.None {
			t.Fatalf("a probe: %v", a)
		}
	}
	h.s.releaseSentProbes(time.Now().Add(probeHoldMax - time.Second))
	if n := h.s.probeSlots.inFlight(); n != 3 {
		t.Fatalf("%d probes in flight before probeHoldMax", n)
	}
	h.s.releaseSentProbes(time.Now().Add(probeHoldMax))
	if n := h.s.probeSlots.inFlight(); n != 0 {
		t.Fatalf("%d probes in flight after probeHoldMax", n)
	}
}

// readServerFrames splits what the server wrote to a WebSocket connection
// into the payloads of its binary frames, concatenated.
func readServerFrames(t *testing.T, b []byte) []byte {
	t.Helper()
	r := bytes.NewReader(b)
	var out []byte
	for r.Len() > 0 {
		hdr, err := wsproto.ReadHeader(r)
		if err != nil {
			t.Fatal(err)
		}
		p := make([]byte, hdr.Length)
		if _, err := io.ReadFull(r, p); err != nil {
			t.Fatal(err)
		}
		if hdr.OpCode == wsproto.OpBinary {
			out = append(out, p...)
		}
	}
	return out
}

// Through the Edge entry a probe is answered in WebSocket frames over the
// transport the client chose, and counts against the client's address from
// X-Pluma-Client-Addr, not the origin proxy's.
func TestProbeEdgeEntry(t *testing.T) {
	h := newTestHarness(t)
	h.s.limits = newIPLimits(10, 10, 1)
	edge := func(id int64, clientAddr string) (*fakeConn, int) {
		c := h.edgeConn(id)
		if a := h.traffic(c, []byte(upgradeRequest("X-Pluma-Client-Addr: "+clientAddr+"\r\nX-Pluma-Edge-Via: ru\r\n"))); a != gnet.None {
			t.Fatalf("the upgrade: %v", a)
		}
		return c, len(h.written(c))
	}
	intermediate := func(frame []byte, tag bool) []byte {
		var b []byte
		if tag {
			b = binary.LittleEndian.AppendUint32(b, 0xeeeeeeee)
		}
		b = binary.LittleEndian.AppendUint32(b, uint32(len(frame)))
		return append(b, frame...)
	}

	c, upgraded := edge(1, "203.0.113.40")
	if a := h.traffic(c, clientFrame(t, wsproto.NewBinaryFrame(intermediate(probeFrame(3, 64<<10, 20<<10), true)))); a != gnet.None {
		t.Fatalf("a probe through the edge: %v", a)
	}
	stream := readServerFrames(t, []byte(h.written(c)[upgraded:]))
	var msgs [][]byte
	for len(stream) > 0 {
		n := binary.LittleEndian.Uint32(stream)
		msgs = append(msgs, stream[4:4+n])
		stream = stream[4+n:]
	}
	checkProbeAnswer(t, msgs, 3, 64<<10)

	// The same client through another connection of the edge is over its
	// budget of one; another client of the same edge is not.
	c2, _ := edge(2, "203.0.113.40")
	if a := h.traffic(c2, clientFrame(t, wsproto.NewBinaryFrame(intermediate(probeFrame(4, 16<<10, 0), true)))); a != gnet.Close {
		t.Fatalf("a second probe of the client: %v, want the connection closed", a)
	}
	c3, _ := edge(3, "203.0.113.41")
	if a := h.traffic(c3, clientFrame(t, wsproto.NewBinaryFrame(intermediate(probeFrame(5, 16<<10, 0), true)))); a != gnet.None {
		t.Fatalf("a probe of another client: %v", a)
	}
}

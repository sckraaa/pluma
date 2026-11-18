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
	"bytes"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/config"
	wscodec "github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/ws"

	wsproto "github.com/gobwas/ws"
	"github.com/panjf2000/gnet/v2"
)

func TestEdgeClientAddr(t *testing.T) {
	tests := []struct {
		name, value, want string
	}{
		{"one address", "203.0.113.9", "203.0.113.9"},
		{"a list: the last element", "198.51.100.1, 203.0.113.9", "203.0.113.9"},
		{"a forged left element", "10.0.0.1,192.0.2.77 ,  203.0.113.9", "203.0.113.9"},
		{"IPv6", "2001:db8::1", "2001:db8::1"},
		{"IPv6 in a list", "203.0.113.9, 2001:DB8:0:0::7", "2001:db8::7"},
		{"a port after IPv4", "203.0.113.9:50000", "203.0.113.9"},
		{"a port after IPv6", "[2001:db8::1]:443", "2001:db8::1"},
		{"IPv4-mapped IPv6", "::ffff:203.0.113.9", "203.0.113.9"},
		{"garbage last: the last address before it", "203.0.113.9, unknown", "203.0.113.9"},
		{"an empty last element", "203.0.113.9,", "203.0.113.9"},
		{"empty", "", ""},
		{"blank", "  ,  ", ""},
		{"garbage", "unknown", ""},
		{"a host name", "example.com", ""},
		{"unspecified", "0.0.0.0", ""},
		{"unspecified IPv6", "::", ""},
		{"multicast", "224.0.0.1", ""},
		{"a zone", "fe80::1%eth0", ""},
		{"an address with a mask", "203.0.113.0/24", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := edgeClientAddr(tt.value)
			if got != tt.want || ok != (tt.want != "") {
				t.Fatalf("edgeClientAddr(%q) = %q, %v; want %q", tt.value, got, ok, tt.want)
			}
		})
	}
}

// upgradeRequest is a WebSocket upgrade request to /apiws with the extra
// header lines (each ending in CRLF).
func upgradeRequest(extra string) string {
	return "GET /apiws HTTP/1.1\r\n" +
		"Host: edge.example\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Protocol: binary\r\n" +
		extra +
		"\r\n"
}

// edgeConn opens a connection of the Edge entry: from the origin proxy,
// whose address is the fake connection's remote one.
func (h *testHarness) edgeConn(id int64) *fakeConn {
	c := h.newConn(id, func(ctx *connContext) {
		ctx.codec = nil
		ctx.tcp = false
		ctx.websocket = true
		ctx.edge = true
		ctx.clientIp = "192.0.2.1" // the origin proxy
		ctx.entry = connEntry(ctx)
		ctx.via = config.EdgeViaOther
	})
	ctx := c.ctx.(*connContext)
	ctx.wsCodec = &wscodec.WsCodec{Headers: &edgeUpgrade{s: h.s, c: c, ctx: ctx}}
	return c
}

func (h *testHarness) traffic(c *fakeConn, data []byte) (action gnet.Action) {
	h.do(func() {
		defer recoverConn(c, "traffic", &action)
		c.in = append(c.in, data...)
		action = h.s.OnTraffic(c)
	})
	return action
}

// written returns what the server wrote to c.
func (h *testHarness) written(c *fakeConn) string {
	var b strings.Builder
	h.do(func() {
		for _, w := range c.out {
			b.Write(w)
		}
	})
	return b.String()
}

// clientFrame is a masked WebSocket frame as a client sends it.
func clientFrame(t *testing.T, f wsproto.Frame) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := wsproto.WriteFrame(&b, wsproto.MaskFrame(f)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The address of an Edge connection is the last one of X-Pluma-Client-Addr,
// the via label comes from X-Pluma-Edge-Via, and the connection is pinged
// from its upgrade on.
func TestEdgeUpgrade(t *testing.T) {
	h := newTestHarness(t)
	c := h.edgeConn(1)
	req := upgradeRequest("X-Pluma-Client-Addr: 198.51.100.1, 203.0.113.9\r\nX-Pluma-Edge-Via: gcore\r\nUser-Agent: Pluma\r\n")
	// The request comes in two parts, the first one ending inside the
	// headers: the second read starts over and takes the address once.
	cut := strings.Index(req, "X-Pluma-Edge-Via")
	if a := h.traffic(c, []byte(req[:cut])); a != gnet.None {
		t.Fatalf("the first part of the request: %v", a)
	}
	if a := h.traffic(c, []byte(req[cut:])); a != gnet.None {
		t.Fatalf("the upgrade: %v", a)
	}
	if out := h.written(c); !strings.HasPrefix(out, "HTTP/1.1 101 ") || !strings.Contains(out, "Sec-WebSocket-Protocol: binary\r\n") {
		t.Fatalf("the upgrade answered %q", out)
	}
	h.do(func() {
		ctx := c.ctx.(*connContext)
		if ctx.clientIp != "203.0.113.9" || ctx.limitSubject != "203.0.113.9" {
			t.Fatalf("client %q counted as %q, want 203.0.113.9", ctx.clientIp, ctx.limitSubject)
		}
		if ctx.via != "gcore" || ctx.entry != entryEdge || !ctx.entryCounted {
			t.Fatalf("labels %q/%q, counted %v", ctx.entry, ctx.via, ctx.entryCounted)
		}
		if ctx.nextPing != testNow+20 {
			t.Fatalf("next ping at %d, want %d", ctx.nextPing, testNow+20)
		}
	})

	// A via of no configured edge is "other".
	c2 := h.edgeConn(2)
	if a := h.traffic(c2, []byte(upgradeRequest("X-Pluma-Client-Addr: 203.0.113.10\r\nX-Pluma-Edge-Via: elsewhere\r\n"))); a != gnet.None {
		t.Fatalf("the upgrade: %v", a)
	}
	h.do(func() {
		if via := c2.ctx.(*connContext).via; via != config.EdgeViaOther {
			t.Fatalf("via %q, want other", via)
		}
	})
}

// Without a client address the upgrade is refused and the connection
// closed before the limits of any address are counted.
func TestEdgeUpgradeWithoutClientAddr(t *testing.T) {
	for name, extra := range map[string]string{
		"no header":      "X-Forwarded-For: 203.0.113.9\r\n",
		"an empty value": "X-Pluma-Client-Addr: \r\n",
		"garbage":        "X-Pluma-Client-Addr: unknown, edge-host\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newTestHarness(t)
			h.s.limits = newIPLimits(1, 10, 0)
			c := h.edgeConn(1)
			if a := h.traffic(c, []byte(upgradeRequest(extra))); a != gnet.Close {
				t.Fatalf("an upgrade without an address: %v, want closed", a)
			}
			if out := h.written(c); !strings.HasPrefix(out, "HTTP/1.1 400 ") {
				t.Fatalf("answered %q, want 400", out)
			}
			h.do(func() {
				ctx := c.ctx.(*connContext)
				if ctx.limitSubject != "" || ctx.closeReason != closeReasonNoAddr || ctx.nextPing != 0 || ctx.entryCounted {
					t.Fatalf("counted as %q, close reason %q, next ping %d, in the gauge %v", ctx.limitSubject, ctx.closeReason, ctx.nextPing, ctx.entryCounted)
				}
				// Nothing was counted: the proxy's own address is not
				// at its limit.
				if !h.s.limits.openConn(ipSubject("192.0.2.1")) {
					t.Fatal("the proxy's address was counted")
				}
			})
		})
	}
}

// The limits count the address from the header: two connections from the
// same proxy with different client addresses do not share a limit, a
// second one of the same client does.
func TestEdgeLimitsByClientAddr(t *testing.T) {
	h := newTestHarness(t)
	h.s.limits = newIPLimits(1, 10, 0)
	upgrade := func(id int64, addr string) (*fakeConn, gnet.Action) {
		c := h.edgeConn(id)
		return c, h.traffic(c, []byte(upgradeRequest("X-Pluma-Client-Addr: "+addr+"\r\n")))
	}
	if _, a := upgrade(1, "203.0.113.9"); a != gnet.None {
		t.Fatalf("the first client: %v", a)
	}
	if _, a := upgrade(2, "203.0.113.10"); a != gnet.None {
		t.Fatalf("another client through the same proxy: %v", a)
	}
	c3, a := upgrade(3, "198.51.100.1, 203.0.113.9")
	if a != gnet.Close {
		t.Fatalf("a second connection of the first client over its limit: %v, want closed", a)
	}
	if out := h.written(c3); !strings.HasPrefix(out, "HTTP/1.1 503 ") {
		t.Fatalf("answered %q, want 503", out)
	}
	h.do(func() {
		if r := c3.ctx.(*connContext).closeReason; r != closeReasonLimit {
			t.Fatalf("close reason %q, want limit", r)
		}
	})
}

// The Edge entry pings an upgraded connection every interval, also when a
// tick comes late; neither the ping nor the client's pong or ping extends
// its idle deadline.
func TestEdgePing(t *testing.T) {
	h := newTestHarness(t)
	c := h.edgeConn(1)
	if a := h.traffic(c, []byte(upgradeRequest("X-Pluma-Client-Addr: 203.0.113.9\r\n"))); a != gnet.None {
		t.Fatalf("the upgrade: %v", a)
	}
	const closeDate = testNow + 300
	h.do(func() {
		ctx := c.ctx.(*connContext)
		ctx.authenticated = true
		ctx.closeDate = closeDate
		c.out = nil
	})
	pings := func() int {
		n := 0
		h.do(func() {
			for _, w := range c.out {
				if len(w) == 2 && w[0] == 0x89 && w[1] == 0 { // an unmasked ping without payload
					n++
				}
			}
		})
		return n
	}

	h.s.expirePings(testNow)
	h.s.expirePings(testNow + 19)
	if n := pings(); n != 0 {
		t.Fatalf("%d pings before the interval", n)
	}
	// The tick of testNow+20 is late: the next one is at testNow+21.
	h.s.expirePings(testNow + 21)
	if n := pings(); n != 1 {
		t.Fatalf("%d pings after the interval, want 1", n)
	}
	h.do(func() {
		if next := c.ctx.(*connContext).nextPing; next != testNow+41 {
			t.Fatalf("next ping at %d, want %d", next, testNow+41)
		}
	})
	h.s.expirePings(testNow + 40)
	h.s.expirePings(testNow + 41)
	if n := pings(); n != 2 {
		t.Fatalf("%d pings after two intervals, want 2", n)
	}

	// The client answers with a pong and pings itself.
	h.s.cachedNow.Store(testNow + 41)
	if a := h.traffic(c, clientFrame(t, wsproto.NewPongFrame(nil))); a != gnet.None {
		t.Fatalf("a pong: %v", a)
	}
	if a := h.traffic(c, clientFrame(t, wsproto.NewPingFrame([]byte("x")))); a != gnet.None {
		t.Fatalf("a ping: %v", a)
	}
	h.do(func() {
		if d := c.ctx.(*connContext).closeDate; d != closeDate {
			t.Fatalf("the idle deadline moved to %d by control frames, want %d", d, closeDate)
		}
	})
	if out := h.written(c); !strings.Contains(out, "\x8a\x01x") {
		t.Fatalf("the client's ping got no pong: %q", out)
	}

	// Closed, it leaves the wheel: no ping is due any more.
	h.do(func() { h.s.OnClose(c, nil) })
	h.s.expirePings(testNow + 61)
	if n := pings(); n != 2 {
		t.Fatalf("%d pings, a closed connection was pinged", n)
	}
}

// After the clock goes back, a connection whose ping was scheduled on the
// old clock is pinged within a lap of the wheel, not when the clock
// catches up: the CDN would drop it after 30 s.
func TestEdgePingAfterClockWentBack(t *testing.T) {
	h := newTestHarness(t)
	c := h.edgeConn(1)
	if a := h.traffic(c, []byte(upgradeRequest("X-Pluma-Client-Addr: 203.0.113.9\r\n"))); a != gnet.None {
		t.Fatalf("the upgrade: %v", a)
	}
	h.do(func() { c.out = nil })
	h.s.expirePings(testNow)
	back := int64(testNow - 3600)
	for now := back; now <= back+pingWheelNumSlots; now++ {
		h.s.expirePings(now)
	}
	var pinged bool
	h.do(func() {
		for _, w := range c.out {
			if len(w) == 2 && w[0] == 0x89 && w[1] == 0 {
				pinged = true
			}
		}
		if next := c.ctx.(*connContext).nextPing; next > back+pingWheelNumSlots+20 {
			t.Fatalf("next ping at %d, on the old clock", next)
		}
	})
	if !pinged {
		t.Fatal("no ping within a lap of the wheel after the clock went back")
	}
}

// A close frame of the client closes the connection as the peer's close.
func TestWebsocketCloseFrameIsPeer(t *testing.T) {
	h := newTestHarness(t)
	c := h.edgeConn(1)
	if a := h.traffic(c, []byte(upgradeRequest("X-Pluma-Client-Addr: 203.0.113.9\r\n"))); a != gnet.None {
		t.Fatalf("the upgrade: %v", a)
	}
	if a := h.traffic(c, clientFrame(t, wsproto.NewCloseFrame(wsproto.NewCloseFrameBody(wsproto.StatusNormalClosure, "")))); a != gnet.Close {
		t.Fatalf("a close frame: %v, want closed", a)
	}
	h.do(func() {
		if r := c.ctx.(*connContext).closeReason; r != closeReasonPeer {
			t.Fatalf("close reason %q, want %q", r, closeReasonPeer)
		}
	})
}

// A data message extends an authenticated WebSocket connection.
func TestWebsocketDataExtendsDeadline(t *testing.T) {
	h := newTestHarness(t)
	c := h.edgeConn(1)
	if a := h.traffic(c, []byte(upgradeRequest("X-Pluma-Client-Addr: 203.0.113.9\r\n"))); a != gnet.None {
		t.Fatalf("the upgrade: %v", a)
	}
	h.do(func() {
		ctx := c.ctx.(*connContext)
		ctx.authenticated = true
		ctx.closeDate = testNow + 5
	})
	h.s.cachedNow.Store(testNow + 4)
	// The start of an abridged transport: a data message, though not a
	// whole frame yet.
	if a := h.traffic(c, clientFrame(t, wsproto.NewBinaryFrame([]byte{0xef}))); a != gnet.None {
		t.Fatalf("a data message: %v", a)
	}
	h.do(func() {
		if d := c.ctx.(*connContext).closeDate; d < testNow+4+tcpIdleTimeout {
			t.Fatalf("the idle deadline is %d after a data message, want at least %d", d, testNow+4+tcpIdleTimeout)
		}
	})
}

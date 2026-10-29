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
	"testing"

	"github.com/panjf2000/gnet/v2"
)

// On a PROXY protocol listener the client's address comes from the header,
// and the limits of that address apply; a connection without the header is
// closed.
func TestProxyProtocolV1(t *testing.T) {
	h := newTestHarness(t)
	h.s.limits = newIPLimits(1, 10)
	behindProxy := func(ctx *connContext) {
		ctx.ppv1 = true
		ctx.clientIp = "172.20.0.30" // the TLS proxy
	}
	traffic := func(c *fakeConn, data string) (action gnet.Action) {
		h.do(func() {
			c.in = append(c.in, data...)
			action = h.s.OnTraffic(c)
		})
		return action
	}

	c1 := h.newConn(1, behindProxy)
	if a := traffic(c1, "PROXY TCP4 203.0.113.9 172.20.0.5 50000 11444\r\n"); a != gnet.None {
		t.Fatalf("a PROXY header: %v", a)
	}
	h.do(func() {
		ctx := c1.ctx.(*connContext)
		if ctx.clientIp != "203.0.113.9" || ctx.ppv1 {
			t.Fatalf("after the header: client %q, ppv1 %v; want 203.0.113.9, false", ctx.clientIp, ctx.ppv1)
		}
		if len(c1.in) != 0 {
			t.Fatalf("%d bytes of the header left unread", len(c1.in))
		}
	})

	c2 := h.newConn(2, behindProxy)
	if a := traffic(c2, "PROXY TCP4 203.0.113.9 172.20.0.5 50001 11444\r\n"); a != gnet.Close {
		t.Fatalf("a second connection of the address over its limit: %v, want closed", a)
	}

	c3 := h.newConn(3, behindProxy)
	if a := traffic(c3, "PRO"); a != gnet.None {
		t.Fatalf("the start of a header: %v, want to wait for the rest", a)
	}
	c4 := h.newConn(4, behindProxy)
	if a := traffic(c4, "GET /apiws HTTP/1.1\r\nHost: web.pluma.chat\r\n\r\n"); a != gnet.Close {
		t.Fatalf("no PROXY header: %v, want closed", a)
	}
}

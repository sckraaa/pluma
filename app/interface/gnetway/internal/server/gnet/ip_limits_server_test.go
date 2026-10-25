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

// The key exchanges of one address stop at its budget, before any RSA or
// DH work; other addresses go on.
func TestKeyExchangeLimitPerAddress(t *testing.T) {
	h := newTestHarness(t)
	h.s.limits = newIPLimits(10, 2)
	from := func(ip string) func(ctx *connContext) {
		return func(ctx *connContext) { ctx.clientIp = ip }
	}

	c1 := h.newConn(1, from("203.0.113.7"))
	h.handshake(c1, false)
	h.handshake(c1, true)

	c2 := h.newConn(2, from("203.0.113.7"))
	tc := newTestClient(t, &h.key.PublicKey)
	if a := h.sendFrame(c2, tc.reqPqMulti()); a != gnet.None {
		t.Fatalf("req_pq_multi: %v", a)
	}
	h.waitWrites(c2, 1)
	tc.onResPQ(h.lastUnencrypted(c2))
	if a := h.sendFrame(c2, tc.reqDHParams(false)); a != gnet.Close {
		t.Fatalf("req_DH_params over the budget: %v, want the connection closed", a)
	}
	h.do(func() {
		if len(h.saved) != 2 {
			t.Fatalf("%d keys saved, want 2", len(h.saved))
		}
	})

	c3 := h.newConn(3, from("203.0.113.8"))
	h.handshake(c3, false)
}

// The connections of one address stop at the limit and are counted back
// when they close.
func TestConnLimitPerAddress(t *testing.T) {
	h := newTestHarness(t)
	h.s.limits = newIPLimits(1, 10)
	from := func(ctx *connContext) { ctx.clientIp = "2001:db8::1" }
	c1 := h.newConn(1, from)
	c2 := h.newConn(2, func(ctx *connContext) { ctx.clientIp = "2001:db8::2" }) // the same /64

	h.do(func() {
		if !h.s.countConn(c1, c1.ctx.(*connContext)) {
			t.Fatal("the first connection refused")
		}
		if h.s.countConn(c2, c2.ctx.(*connContext)) {
			t.Fatal("a second connection of the /64 accepted")
		}
		h.s.OnClose(c1, nil)
		if !h.s.countConn(c2, c2.ctx.(*connContext)) {
			t.Fatal("refused after the first connection closed")
		}
	})
}

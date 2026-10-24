package gnet

import (
	"encoding/binary"
	"testing"

	"github.com/teamgram/proto/mtproto"

	"github.com/panjf2000/gnet/v2"
)

// Tests of the checks of the key exchange
// (https://core.telegram.org/mtproto/auth_key): a message that fails one is
// rejected and the connection closed, with no answer and no key.

// startDH runs req_pq_multi on a new connection: the client is ready for
// req_DH_params.
func (h *testHarness) startDH(id int64) (*fakeConn, *testClient) {
	h.t.Helper()
	c := h.newConn(id, nil)
	tc := newTestClient(h.t, &h.key.PublicKey)
	if a := h.sendFrame(c, tc.reqPqMulti()); a != gnet.None {
		h.t.Fatalf("req_pq_multi: %v", a)
	}
	h.waitWrites(c, 1)
	tc.onResPQ(h.lastUnencrypted(c))
	return c, tc
}

// startSetDH also runs req_DH_params: the client is ready for
// set_client_DH_params.
func (h *testHarness) startSetDH(id int64) (*fakeConn, *testClient) {
	h.t.Helper()
	c, tc := h.startDH(id)
	if a := h.sendFrame(c, tc.reqDHParams(false)); a != gnet.None {
		h.t.Fatalf("req_DH_params: %v", a)
	}
	h.waitWrites(c, 2)
	tc.onServerDHParams(h.lastUnencrypted(c))
	return c, tc
}

// expectRejected sends frame on c: the connection must be closed, at once or
// by the worker pool, without an answer and without a new key.
func (h *testHarness) expectRejected(c *fakeConn, frame []byte) {
	h.t.Helper()
	var writes, saved int
	h.do(func() { writes, saved = len(c.out), len(h.saved) })
	if a := h.sendFrame(c, frame); a != gnet.Close {
		h.waitFor("close", func() bool { return c.closed })
	}
	h.do(func() {
		if len(c.out) != writes {
			h.t.Fatalf("%d answers to a rejected message", len(c.out)-writes)
		}
		if len(h.saved) != saved {
			h.t.Fatal("a key was created")
		}
	})
}

// TestResPQIsNew: every req_pq gets its own pq, and the handshake keeps its
// factors.
func TestResPQIsNew(t *testing.T) {
	h := newTestHarness(t)
	c := h.newConn(1, nil)
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		tc := newTestClient(t, &h.key.PublicKey)
		h.sendFrame(c, tc.reqPqMulti())
		h.waitWrites(c, i+1)
		tc.onResPQ(h.lastUnencrypted(c))
		if seen[tc.pq] {
			t.Fatal("the same pq twice")
		}
		seen[tc.pq] = true

		st, ok := c.ctx.(*connContext).handshakes.states.get(tc.nonce, testNow)
		if !ok || st.Pq != binary.BigEndian.Uint64([]byte(tc.pq)) ||
			uint64(binary.BigEndian.Uint32([]byte(tc.p))) != st.P || uint64(binary.BigEndian.Uint32([]byte(tc.q))) != st.Q {
			t.Fatal("the handshake state does not hold pq and its factors")
		}
	}
}

// TestReqDHParamsChecksFactors: req_DH_params and p_q_inner_data must carry
// the factors of the pq of their handshake.
func TestReqDHParamsChecksFactors(t *testing.T) {
	str := func(v uint64) string { return string(binary.BigEndian.AppendUint32(nil, uint32(v))) }
	cases := []struct {
		name  string
		edits func(tc *testClient, other *testClient) reqDHParamsEdits
	}{
		{"p and q swapped", func(tc, _ *testClient) reqDHParamsEdits {
			return reqDHParamsEdits{outer: func(r *mtproto.TLReq_DHParams) { r.P, r.Q = tc.q, tc.p }}
		}},
		{"p + 2", func(tc, _ *testClient) reqDHParamsEdits {
			return reqDHParamsEdits{outer: func(r *mtproto.TLReq_DHParams) {
				r.P = str(uint64(binary.BigEndian.Uint32([]byte(tc.p))) + 2)
			}}
		}},
		{"1 and pq", func(tc, _ *testClient) reqDHParamsEdits {
			return reqDHParamsEdits{outer: func(r *mtproto.TLReq_DHParams) { r.P, r.Q = "\x01", tc.pq }}
		}},
		{"the factors of another handshake", func(_, other *testClient) reqDHParamsEdits {
			return reqDHParamsEdits{outer: func(r *mtproto.TLReq_DHParams) { r.P, r.Q = other.p, other.q }}
		}},
		{"the fixed pq of the old gateway", func(_, _ *testClient) reqDHParamsEdits {
			return reqDHParamsEdits{outer: func(r *mtproto.TLReq_DHParams) {
				r.P, r.Q = "\x49\x4C\x55\x3B", "\x53\x91\x10\x73"
			}}
		}},
		{"empty p and q", func(_, _ *testClient) reqDHParamsEdits {
			return reqDHParamsEdits{outer: func(r *mtproto.TLReq_DHParams) { r.P, r.Q = "", "" }}
		}},
		{"p_q_inner_data with another pq", func(_, other *testClient) reqDHParamsEdits {
			return reqDHParamsEdits{inner: func(in *mtproto.P_QInnerData) { in.Pq = other.pq }}
		}},
		{"p_q_inner_data with other factors", func(_, other *testClient) reqDHParamsEdits {
			return reqDHParamsEdits{inner: func(in *mtproto.P_QInnerData) { in.P, in.Q = other.p, other.q }}
		}},
	}
	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			h := newTestHarness(t)
			_, other := h.startDH(2)
			c, tc := h.startDH(1)
			h.expectRejected(c, tc.reqDHParamsWith(false, tcase.edits(tc, other)))
		})
	}

	// p and q are compared as numbers: minimal big-endian bytes (iOS, Web K)
	// do as well as 4 bytes (Android).
	h := newTestHarness(t)
	c, tc := h.startDH(1)
	trim := func(s string) string {
		for len(s) > 1 && s[0] == 0 {
			s = s[1:]
		}
		return s
	}
	tc.p, tc.q = "\x00\x00"+tc.p, trim(tc.q)
	if a := h.sendFrame(c, tc.reqDHParams(false)); a != gnet.None {
		t.Fatalf("action %v", a)
	}
	h.waitWrites(c, 2)
	tc.onServerDHParams(h.lastUnencrypted(c))
}

package gnet

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/codec"
	httpcodec "github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/http"
	wscodec "github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/ws"

	gobwasws "github.com/gobwas/ws"
	"github.com/panjf2000/gnet/v2"
)

// lastUnencrypted returns the last message written on c.
func (h *testHarness) lastUnencrypted(c *fakeConn) mtproto.TLObject {
	h.t.Helper()
	var out []byte
	h.do(func() { out = c.out[len(c.out)-1] })
	return parseUnencrypted(h.t, out)
}

func (h *testHarness) waitWrites(c *fakeConn, n int) {
	h.t.Helper()
	h.waitFor(fmt.Sprintf("%d writes", n), func() bool { return len(c.out) >= n || c.closed })
	h.do(func() {
		if c.closed {
			h.t.Fatalf("connection closed after %d writes", len(c.out))
		}
	})
}

// handshake runs a whole key exchange on c and returns the auth key id.
func (h *testHarness) handshake(c *fakeConn, temp bool) (*testClient, int64) {
	h.t.Helper()
	tc := newTestClient(h.t, &h.key.PublicKey)
	writes := len(c.out)

	if a := h.sendFrame(c, tc.reqPqMulti()); a != gnet.None {
		h.t.Fatalf("req_pq_multi: %v", a)
	}
	h.waitWrites(c, writes+1)
	tc.onResPQ(h.lastUnencrypted(c))

	if a := h.sendFrame(c, tc.reqDHParams(temp)); a != gnet.None {
		h.t.Fatalf("req_DH_params: %v", a)
	}
	h.waitWrites(c, writes+2)
	tc.onServerDHParams(h.lastUnencrypted(c))

	if a := h.sendFrame(c, tc.setClientDHParams()); a != gnet.None {
		h.t.Fatalf("set_client_DH_params: %v", a)
	}
	h.waitWrites(c, writes+3)
	return tc, tc.onDHGen(h.lastUnencrypted(c))
}

func TestHandshakeCompletes(t *testing.T) {
	for _, temp := range []bool{false, true} {
		t.Run(fmt.Sprintf("temp=%v", temp), func(t *testing.T) {
			h := newTestHarness(t)
			c := h.newConn(1, nil)
			tc, authKeyId := h.handshake(c, temp)

			h.do(func() {
				if len(h.saved) != 1 {
					t.Fatalf("saved %d keys", len(h.saved))
				}
				k := h.saved[0]
				if k.AuthKeyId != authKeyId || !bytes.Equal(k.AuthKey, tc.authKey) {
					t.Fatal("the server saved another key than the client computed")
				}
				wantType := mtproto.AuthKeyTypePerm
				if temp {
					wantType = mtproto.AuthKeyTypeTemp
				}
				if k.AuthKeyType != int32(wantType) {
					t.Fatalf("key type %d, want %d", k.AuthKeyType, wantType)
				}
				// dh_gen_ok ends the handshake.
				if n := c.ctx.(*connContext).handshakes.states.len(); n != 0 {
					t.Fatalf("%d handshakes left", n)
				}
			})

			// A second handshake on the same connection works too.
			if _, id2 := h.handshake(c, false); id2 == authKeyId {
				t.Fatal("same key twice")
			}
		})
	}
}

// TestHandshakeOutOfOrder sends handshake messages in every wrong order: the
// connection is closed, nothing panics, and no key is created.
func TestHandshakeOutOfOrder(t *testing.T) {
	cases := []struct {
		name  string
		steps func(h *testHarness, c *fakeConn, tc *testClient) []byte
	}{
		{"set_client_DH_params first", func(h *testHarness, c *fakeConn, tc *testClient) []byte {
			tc.serverNonce = crypto.GenerateNonce(16)
			tc.g, tc.dhPrime, tc.gA = 3, gBigIntDH2048P, gBigIntDH2048G
			return tc.setClientDHParams()
		}},
		{"req_DH_params first", func(h *testHarness, c *fakeConn, tc *testClient) []byte {
			tc.serverNonce = crypto.GenerateNonce(16)
			return tc.reqDHParams(false)
		}},
		{"set_client_DH_params right after req_pq", func(h *testHarness, c *fakeConn, tc *testClient) []byte {
			h.sendFrame(c, tc.reqPqMulti())
			tc.onResPQ(h.lastUnencrypted(c))
			// nonce and server_nonce are right, new_nonce is unknown to the server.
			tc.g, tc.dhPrime, tc.gA = 3, gBigIntDH2048P, gBigIntDH2048G
			return tc.setClientDHParams()
		}},
		{"req_DH_params with a wrong server_nonce", func(h *testHarness, c *fakeConn, tc *testClient) []byte {
			h.sendFrame(c, tc.reqPqMulti())
			tc.onResPQ(h.lastUnencrypted(c))
			tc.serverNonce = crypto.GenerateNonce(16)
			return tc.reqDHParams(false)
		}},
		{"req_DH_params twice", func(h *testHarness, c *fakeConn, tc *testClient) []byte {
			h.sendFrame(c, tc.reqPqMulti())
			tc.onResPQ(h.lastUnencrypted(c))
			frame := tc.reqDHParams(false)
			h.sendFrame(c, frame)
			h.waitWrites(c, 2)
			return frame
		}},
		{"set_client_DH_params after dh_gen_ok", func(h *testHarness, c *fakeConn, tc *testClient) []byte {
			tc2, _ := h.handshake(c, false)
			return tc2.setClientDHParams()
		}},
	}
	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			h := newTestHarness(t)
			c := h.newConn(1, nil)
			tc := newTestClient(t, &h.key.PublicKey)
			frame := tcase.steps(h, c, tc)
			saved := len(h.saved)
			if a := h.sendFrame(c, frame); a != gnet.Close {
				t.Fatalf("action %v, want close", a)
			}
			if len(h.saved) != saved {
				t.Fatal("a key was created")
			}
		})
	}
}

// TestHandshakeBadDHStepsFailInWorker: messages that pass the state check
// but carry bad encrypted data are rejected by the worker, which closes the
// connection (it used to panic on short data).
func TestHandshakeBadDHStepsFailInWorker(t *testing.T) {
	for _, tcase := range []struct {
		name string
		data []byte
	}{
		{"set_client_DH_params with 16 bytes", make([]byte, 16)},
		{"set_client_DH_params with 20 bytes", make([]byte, 20)},
		{"set_client_DH_params with 33 bytes", make([]byte, 33)},
		{"set_client_DH_params with garbage", crypto.GenerateNonce(320)},
	} {
		t.Run(tcase.name, func(t *testing.T) {
			h := newTestHarness(t)
			c := h.newConn(1, nil)
			tc := newTestClient(t, &h.key.PublicKey)
			h.sendFrame(c, tc.reqPqMulti())
			tc.onResPQ(h.lastUnencrypted(c))
			h.sendFrame(c, tc.reqDHParams(false))
			h.waitWrites(c, 2)

			frame := unencrypted(&mtproto.TLSetClient_DHParams{
				Constructor:   mtproto.CRC32_set_client_DH_params,
				Nonce:         tc.nonce,
				ServerNonce:   tc.serverNonce,
				EncryptedData: string(tcase.data),
			})
			if a := h.sendFrame(c, frame); a != gnet.None {
				t.Fatalf("action %v", a)
			}
			h.waitFor("close", func() bool { return c.closed })
			if len(h.saved) != 0 {
				t.Fatal("a key was created")
			}
		})
	}

	t.Run("req_DH_params with short encrypted_data", func(t *testing.T) {
		h := newTestHarness(t)
		c := h.newConn(1, nil)
		tc := newTestClient(t, &h.key.PublicKey)
		h.sendFrame(c, tc.reqPqMulti())
		tc.onResPQ(h.lastUnencrypted(c))
		frame := unencrypted(&mtproto.TLReq_DHParams{
			Constructor:          mtproto.CRC32_req_DH_params,
			Nonce:                tc.nonce,
			ServerNonce:          tc.serverNonce,
			P:                    tc.p,
			Q:                    tc.q,
			PublicKeyFingerprint: tc.fingerprint,
			EncryptedData:        "short",
		})
		h.sendFrame(c, frame)
		h.waitFor("close", func() bool { return c.closed })
	})
}

// TestHandshakeMessagesRejected: only handshake objects are decoded from
// unencrypted messages, and their lengths must be right.
func TestHandshakeMessagesRejected(t *testing.T) {
	huge := (&bytes.Buffer{})
	_ = binary.Write(huge, binary.LittleEndian, []int32{int32(mtproto.CRC32_msgs_ack), int32(mtproto.CRC32_vector), 0x7fffffff})
	withLen := func(n uint32, body []byte) []byte {
		b := make([]byte, 20, 20+len(body))
		binary.LittleEndian.PutUint64(b[8:], uint64(mtproto.GenerateMessageId()))
		binary.LittleEndian.PutUint32(b[16:], n)
		return append(b, body...)
	}
	getConfig := (&mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig})
	reqPq := (&mtproto.TLReqPqMulti{Constructor: mtproto.CRC32_req_pq_multi, Nonce: make([]byte, 16)})
	reqPqBody := unencrypted(reqPq)[20:]

	cases := []struct {
		name  string
		frame []byte
		ok    bool
	}{
		{"req_pq_multi", unencrypted(reqPq), true},
		{"req_pq_multi with transport padding after it", append(unencrypted(reqPq), 1, 2, 3), true},
		{"not a handshake object", unencrypted(getConfig), false},
		{"msgs_ack with a huge vector", withLen(uint32(huge.Len()), huge.Bytes()), false},
		{"length beyond the frame", withLen(uint32(len(reqPqBody)+4), reqPqBody), false},
		{"length 0", withLen(0, reqPqBody), false},
		{"length 0xffffffff", withLen(0xffffffff, reqPqBody), false},
		{"header only", withLen(4, nil), false},
		{"8 bytes", make([]byte, 8), false},
		{"truncated req_pq_multi", withLen(uint32(len(reqPqBody)-4), reqPqBody[:len(reqPqBody)-4]), false},
	}
	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			h := newTestHarness(t)
			c := h.newConn(1, nil)
			a := h.sendFrame(c, tcase.frame)
			if tcase.ok != (a == gnet.None) {
				t.Fatalf("action %v", a)
			}
		})
	}
}

// TestShortFrames feeds frames of every short length, with zero, cached and
// unknown auth key ids: none panics; frames that cannot be valid close the
// connection without asking authsession.
func TestShortFrames(t *testing.T) {
	h := newTestHarness(t)
	known := testKeyInfo(t, testPermKeyId, mtproto.AuthKeyTypePerm, 0)
	h.s.PutAuthKey(newCheckedAuthKeyUtil(known, testNow, 0))
	queries := 0
	h.s.queryKey = func(int64, *authKeyUtil) (*authKeyUtil, error) {
		h.mu.Lock()
		queries++
		h.mu.Unlock()
		return nil, mtproto.ErrAuthKeyUnregistered
	}

	id := int64(1)
	var queried []*fakeConn
	for _, keyId := range []int64{0, testPermKeyId, 0x0123_4567_89ab_cdef} {
		for n := 0; n < encryptedFrameMinSize+20; n++ {
			frame := crypto.GenerateNonce(n)
			if n >= 8 {
				binary.LittleEndian.PutUint64(frame, uint64(keyId))
			}
			c := h.newConn(id, nil)
			id++
			a := h.sendFrame(c, frame)
			if (n < encryptedFrameMinSize || keyId == testPermKeyId) && a != gnet.Close {
				t.Fatalf("key %x, %d bytes: action %v, want close", keyId, n, a)
			}
			if a == gnet.None && keyId != 0 {
				queried = append(queried, c)
			}
		}
	}
	h.waitFor("unknown keys answered", func() bool {
		for _, c := range queried {
			if !c.closed {
				return false
			}
		}
		return true
	})
	h.do(func() {
		if queries != 20 {
			t.Fatalf("%d authsession queries, want 20 (frames long enough, unknown key)", queries)
		}
	})
}

func TestSplitEncryptedFrame(t *testing.T) {
	for _, tc := range []struct {
		n, dataLen int
		ok         bool
	}{
		{0, 0, false},
		{71, 0, false},
		{72, 48, true},
		{72 + 15, 48, true}, // padded intermediate transport
		{72 + 16, 64, true},
	} {
		msgKey, data, err := splitEncryptedFrame(make([]byte, tc.n))
		if (err == nil) != tc.ok || len(data) != tc.dataLen || (tc.ok && len(msgKey) != 16) {
			t.Errorf("%d bytes: msg_key %d, data %d, err %v", tc.n, len(msgKey), len(data), err)
		}
	}
}

// TestPanicClosesOnlyThatConnection: a panic while processing a packet
// closes the connection instead of crashing the gateway.
func TestPanicClosesOnlyThatConnection(t *testing.T) {
	h := newTestHarness(t)
	other := h.newConn(4, nil)
	c := h.newConn(1, func(ctx *connContext) { ctx.codec = panicCodec{} })
	var a gnet.Action
	h.do(func() { a = h.s.OnTraffic(c) })
	if a != gnet.Close {
		t.Fatalf("OnTraffic: action %v, want close", a)
	}

	// Callbacks on the event loop.
	c2 := h.newConn(2, nil)
	h.s.trigger(2, func(gnet.Conn) {
		var m map[string]int
		m["x"]++ //nolint:govet,staticcheck // nilness, SA5000: the panic is the point
	})
	if !c2.closed {
		t.Fatal("trigger: connection not closed")
	}

	// Tasks in the worker pool.
	c3 := h.newConn(3, nil)
	if err := h.s.submit(3, func() { panic(errors.New("boom")) }); err != nil {
		t.Fatal(err)
	}
	h.waitFor("close", func() bool { return c3.closed })

	h.do(func() {
		if other.closed {
			t.Fatal("another connection closed")
		}
	})
}

func TestPanicSite(t *testing.T) {
	var site string
	func() {
		defer func() {
			if r := recover(); r != nil {
				site = panicSite()
			}
		}()
		var b []byte
		_ = b[3] //nolint:govet // nilness: the panic is the point
	}()
	if !strings.Contains(site, "dos_test.go:") {
		t.Fatalf("site %q", site)
	}
}

// TestUnauthenticatedDeadline: before a message decrypts with a known key,
// traffic does not keep a connection open; afterwards it does.
func TestUnauthenticatedDeadline(t *testing.T) {
	h := newTestHarness(t)
	c := h.newConn(1, nil)
	ctx := c.ctx.(*connContext)
	open := ctx.closeDate

	h.s.cachedNow.Store(testNow + 50)
	c.in = []byte{0xef}
	ctx.codec = rawCodec{}
	h.do(func() { h.s.OnTraffic(c) })
	if ctx.closeDate != open {
		t.Fatalf("deadline moved from %d to %d before authentication", open, ctx.closeDate)
	}

	h.do(func() { h.s.markAuthenticated(c, ctx) })
	if ctx.closeDate < testNow+50+tcpIdleTimeout {
		t.Fatalf("deadline %d after authentication", ctx.closeDate)
	}
	h.s.cachedNow.Store(testNow + 200)
	h.do(func() { h.s.OnTraffic(c) })
	if ctx.closeDate < testNow+200+tcpIdleTimeout {
		t.Fatalf("deadline %d not extended by traffic", ctx.closeDate)
	}
}

// TestKeyQueryPerConnection: messages under an unknown key cost one
// authsession query at a time per connection, and wait for it in bounded
// number.
func TestKeyQueryPerConnection(t *testing.T) {
	h := newTestHarness(t)
	release := make(chan struct{})
	queries := 0
	h.s.queryKey = func(int64, *authKeyUtil) (*authKeyUtil, error) {
		h.mu.Lock()
		queries++
		h.mu.Unlock()
		<-release
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	frame := func(keyId int64) []byte {
		b := crypto.GenerateNonce(encryptedFrameMinSize)
		binary.LittleEndian.PutUint64(b, uint64(keyId))
		return b
	}

	c := h.newConn(1, nil)
	for i := 0; i < maxPendingFrames; i++ {
		if a := h.sendFrame(c, frame(77)); a != gnet.None {
			t.Fatalf("message %d: action %v", i, a)
		}
	}
	if a := h.sendFrame(c, frame(77)); a != gnet.Close {
		t.Fatalf("message over the limit: action %v", a)
	}
	c2 := h.newConn(2, nil)
	h.sendFrame(c2, frame(77))
	if a := h.sendFrame(c2, frame(78)); a != gnet.Close {
		t.Fatalf("another key while one is read: action %v", a)
	}
	close(release)
	h.waitFor("closes", func() bool { return c.closed && c2.closed })
	h.do(func() {
		if queries != 2 {
			t.Fatalf("%d queries, want 2", queries)
		}
		// -404 for the unknown key.
		if len(c.out) != 1 || !bytes.Equal(c.out[0], []byte{0x6c, 0xfe, 0xff, 0xff}) {
			t.Fatalf("writes %x", c.out)
		}
	})
}

func TestHandshakeStore(t *testing.T) {
	st := func(i byte) HandshakeStateCtx {
		return HandshakeStateCtx{State: STATE_pq_res, Nonce: bytes.Repeat([]byte{i}, 16), ServerNonce: []byte{i}}
	}

	t.Run("bounded, oldest first", func(t *testing.T) {
		hs := newHandshakeStore(3, 0)
		for i := byte(0); i < 10; i++ {
			hs.put(st(i), testNow)
		}
		if hs.len() != 3 {
			t.Fatalf("len %d", hs.len())
		}
		if _, ok := hs.get(st(6).Nonce, testNow); ok {
			t.Fatal("old state kept")
		}
		if _, ok := hs.get(st(9).Nonce, testNow); !ok {
			t.Fatal("new state dropped")
		}
	})

	t.Run("expires", func(t *testing.T) {
		hs := newHandshakeStore(10, 60)
		hs.put(st(1), testNow)
		if _, err := hs.begin(st(1).Nonce, st(1).ServerNonce, STATE_pq_res, STATE_DH_params, testNow+60); err == nil {
			t.Fatal("expired state used")
		}
		if hs.len() != 0 {
			t.Fatal("expired state kept")
		}
	})

	t.Run("transitions", func(t *testing.T) {
		hs := newHandshakeStore(10, 0)
		hs.put(st(1), testNow)
		n, sn := st(1).Nonce, st(1).ServerNonce
		if _, err := hs.begin(n, []byte{9}, STATE_pq_res, STATE_DH_params, testNow); err == nil {
			t.Fatal("wrong server_nonce accepted")
		}
		if _, err := hs.begin(n, sn, STATE_DH_params_res, STATE_dh_gen, testNow); err == nil {
			t.Fatal("wrong state accepted")
		}
		snap, err := hs.begin(n, sn, STATE_pq_res, STATE_DH_params, testNow)
		if err != nil || snap.State != STATE_DH_params {
			t.Fatalf("begin: %v %v", snap.State, err)
		}
		if _, err := hs.begin(n, sn, STATE_pq_res, STATE_DH_params, testNow); err == nil {
			t.Fatal("the same step started twice")
		}
		// A new req_pq with the nonce restarts it: the step's result is dropped.
		hs.put(st(1), testNow)
		if hs.finish(n, STATE_DH_params, testNow, func(*HandshakeStateCtx) bool { return true }) {
			t.Fatal("finished a restarted handshake")
		}
		got, _ := hs.get(n, testNow)
		if got.NewNonce != nil || got.State != STATE_pq_res {
			t.Fatal("state changed")
		}
	})
}

// TestHttpHandshakeFloodKeepsAuthKeys: req_pq over HTTP is kept apart from
// the auth key cache and bounded.
func TestHttpHandshakeFloodKeepsAuthKeys(t *testing.T) {
	h := newTestHarness(t)
	known := testKeyInfo(t, testPermKeyId, mtproto.AuthKeyTypePerm, 0)
	h.s.PutAuthKey(newCheckedAuthKeyUtil(known, testNow, 0))

	c := h.newConn(1, func(ctx *connContext) {
		ctx.http = true
		ctx.tcp = false
		ctx.httpCodec = new(httpcodec.HttpCodec)
	})
	ctx := c.ctx.(*connContext)
	for i := 0; i < maxHttpHandshakes+100; i++ {
		tc := newTestClient(t, &h.key.PublicKey)
		h.do(func() { h.s.onHttpHandshake(ctx, c, tc.reqPqMulti()) })
	}
	if n := h.s.httpHandshakes.len(); n != maxHttpHandshakes {
		t.Fatalf("%d HTTP handshakes kept", n)
	}
	if h.s.GetAuthKey(testPermKeyId) == nil {
		t.Fatal("auth key evicted")
	}
}

// TestHttpHandshakeAcrossConnections runs the key exchange over the HTTP
// transport, each request on its own connection.
func TestHttpHandshakeAcrossConnections(t *testing.T) {
	h := newTestHarness(t)
	httpConn := func(id int64) *fakeConn {
		return h.newConn(id, func(ctx *connContext) {
			ctx.http = true
			ctx.tcp = false
			ctx.httpCodec = new(httpcodec.HttpCodec)
		})
	}
	post := func(c *fakeConn, body []byte) mtproto.TLObject {
		t.Helper()
		req := fmt.Sprintf("POST /api HTTP/1.1\r\nHost: x\r\nContent-Length: %d\r\n\r\n", len(body))
		c.in = append([]byte(req), body...)
		h.do(func() {
			if a := h.s.OnTraffic(c); a != gnet.None {
				t.Fatalf("action %v", a)
			}
		})
		h.waitWrites(c, 1)
		var resp []byte
		h.do(func() { resp = c.out[0] })
		i := bytes.Index(resp, []byte("\r\n\r\n"))
		if !bytes.HasPrefix(resp, []byte("HTTP/1.1 200")) || i < 0 {
			t.Fatalf("response %q", resp)
		}
		return parseUnencrypted(t, resp[i+4:])
	}

	tc := newTestClient(t, &h.key.PublicKey)
	tc.onResPQ(post(httpConn(1), tc.reqPqMulti()))
	tc.onServerDHParams(post(httpConn(2), tc.reqDHParams(false)))
	id := tc.onDHGen(post(httpConn(3), tc.setClientDHParams()))
	h.do(func() {
		if len(h.saved) != 1 || h.saved[0].AuthKeyId != id {
			t.Fatal("key not saved")
		}
	})
}

func TestHttpCodecLimits(t *testing.T) {
	parse := func(raw string) (*httpcodec.HttpCodec, []byte, bool, error) {
		hc := new(httpcodec.HttpCodec)
		c := &fakeConn{in: []byte(raw)}
		if a := hc.ReadBufferBytes(c); a != gnet.None {
			return hc, nil, false, errors.New("closed")
		}
		body, done, err := hc.ParseRequest()
		return hc, body, done, err
	}

	if _, _, _, err := parse("POST /api HTTP/1.1\r\nContent-Length: -5\r\n\r\nabc"); err == nil {
		t.Fatal("negative Content-Length accepted")
	}
	if _, _, _, err := parse("POST /api HTTP/1.1\r\nContent-Length: 99999999\r\n\r\n"); err == nil {
		t.Fatal("huge Content-Length accepted")
	}
	if _, _, _, err := parse("POST /api HTTP/1.1\r\nX: " + strings.Repeat("a", 9000) + "\r\n\r\n"); err == nil {
		t.Fatal("huge headers accepted")
	}

	// One request at a time: until it is answered (Reset), more data does
	// not make it parse again.
	hc, body, done, err := parse("POST /api HTTP/1.1\r\nContent-Length: 3\r\n\r\nabcPOST")
	if err != nil || !done || string(body) != "abc" {
		t.Fatalf("parse: %q %v %v", body, done, err)
	}
	if _, done, _ := hc.ParseRequest(); done {
		t.Fatal("request parsed twice")
	}
	hc.Reset()
	c := &fakeConn{in: []byte(" /api HTTP/1.1\r\nContent-Length: 1\r\n\r\nz")}
	hc.ReadBufferBytes(c)
	if body, done, err := hc.ParseRequest(); err != nil || !done || string(body) != "z" {
		t.Fatalf("next request: %q %v %v", body, done, err)
	}

	// Buffered data is bounded.
	hc = new(httpcodec.HttpCodec)
	big := make([]byte, 1<<20)
	closed := false
	for i := 0; i < 4 && !closed; i++ {
		closed = hc.ReadBufferBytes(&fakeConn{in: big}) == gnet.Close
	}
	if !closed {
		t.Fatal("unbounded HTTP buffer")
	}
}

// obfuscatedAbridged returns the 64-byte obfuscation header of the abridged
// transport followed by data, encrypted as a client does.
func obfuscatedAbridged(t *testing.T, data []byte) []byte {
	t.Helper()
	init := crypto.GenerateNonce(64)
	init[0] = 0x55 // not a plain transport tag
	copy(init[56:60], []byte{0xef, 0xef, 0xef, 0xef})
	init[60], init[61] = 0, 2 // dc 2
	enc, err := crypto.NewAesCTR128Encrypt(init[8:40], init[40:56])
	if err != nil {
		t.Fatal(err)
	}
	encrypted := enc.Encrypt(append([]byte(nil), init...))
	out := append(append([]byte(nil), init[:56]...), encrypted[56:]...)
	return append(out, enc.Encrypt(append([]byte(nil), data...))...)
}

func TestAbridgedRejectsTinyFrames(t *testing.T) {
	for _, n := range []byte{1, 2} { // 4 and 8 bytes
		c := &fakeConn{in: obfuscatedAbridged(t, append([]byte{n}, make([]byte, int(n)*4+64)...))}
		cd, err := codec.CreateCodec(c)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = cd.Decode(c)
		if !errors.Is(err, codec.ErrProtoBadLength) {
			t.Fatalf("%d-byte frame: err %v", n*4, err)
		}
	}
}

// FuzzHandshakeMessage feeds arbitrary unencrypted messages to the handshake
// of a connection with a handshake in progress: nothing may panic.
func FuzzHandshakeMessage(f *testing.F) {
	h := newTestHarness(f)
	tc := newTestClient(f, &h.key.PublicKey)
	f.Add(tc.reqPqMulti())
	f.Add(unencrypted(&mtproto.TLSetClient_DHParams{Constructor: mtproto.CRC32_set_client_DH_params, Nonce: tc.nonce, ServerNonce: make([]byte, 16), EncryptedData: "x"}))
	f.Add(unencrypted(&mtproto.TLReq_DHParams{Constructor: mtproto.CRC32_req_DH_params, Nonce: tc.nonce, ServerNonce: make([]byte, 16), EncryptedData: string(make([]byte, 256))}))
	f.Add(unencrypted(&mtproto.TLMsgsAck{Data2: &mtproto.MsgsAck{MsgIds: []int64{1, 2}}}))

	var id int64
	f.Fuzz(func(t *testing.T, frame []byte) {
		if len(frame) >= 8 {
			binary.LittleEndian.PutUint64(frame, 0)
		}
		id++
		c := h.newConn(id, nil)
		h.sendFrame(c, tc.reqPqMulti())
		h.sendFrame(c, frame)
		h.do(func() {
			if n := c.ctx.(*connContext).handshakes.states.len(); n > maxConnHandshakes {
				t.Fatalf("%d handshakes", n)
			}
		})
	})
}

// TestWebsocketFramesAcrossMessages: every transport frame of a WebSocket
// message is processed, and a frame split over two messages is reassembled
// (only the unfinished frame stays buffered).
func TestWebsocketFramesAcrossMessages(t *testing.T) {
	h := newTestHarness(t)
	c := h.newConn(1, func(ctx *connContext) {
		ctx.codec = nil
		ctx.tcp = false
		ctx.websocket = true
		ctx.wsCodec = new(wscodec.WsCodec)
	})
	ctx := c.ctx.(*connContext)

	send := func(b []byte) {
		t.Helper()
		c.in = append(c.in, b...)
		h.do(func() {
			if a := h.s.OnTraffic(c); a != gnet.None {
				t.Fatalf("action %v", a)
			}
		})
	}
	wsMessage := func(payload []byte) []byte {
		var b bytes.Buffer
		if err := gobwasws.WriteFrame(&b, gobwasws.MaskFrame(gobwasws.NewBinaryFrame(payload))); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}

	send([]byte("GET /apiws HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: binary\r\n\r\n"))

	var stream []byte
	for i := 0; i < 3; i++ {
		msg := newTestClient(t, &h.key.PublicKey).reqPqMulti()
		stream = append(stream, byte(len(msg)/4))
		stream = append(stream, msg...)
	}
	stream = obfuscatedAbridged(t, stream)
	cut := len(stream) - 10 // the third frame continues in the next message

	send(wsMessage(stream[:cut]))
	if n := ctx.handshakes.states.len(); n != 2 {
		t.Fatalf("%d frames processed from the first message, want 2", n)
	}
	// The abridged codec has consumed the length byte of the third frame.
	if n := ctx.wsCodec.Conn.InboundBuffered(); n != len(stream[:cut])-64-2*41-1 {
		t.Fatalf("%d bytes kept", n)
	}
	send(wsMessage(stream[cut:]))
	if n := ctx.handshakes.states.len(); n != 3 {
		t.Fatalf("%d frames processed, want 3", n)
	}
	if n := ctx.wsCodec.Conn.InboundBuffered(); n != 0 {
		t.Fatalf("%d bytes left", n)
	}
}

package gnet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/dao"
	httpcodec "github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/http"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/svc"
	"github.com/teamgram/teamgram-server/app/interface/session/session"

	"github.com/panjf2000/gnet/v2"
)

// fakeDispatcher stands in for session: it counts forwarded messages.
type fakeDispatcher struct {
	mu   sync.Mutex
	sent int
}

func (f *fakeDispatcher) SendData(context.Context, int64, *session.TLSessionSendDataToSession) error {
	f.mu.Lock()
	f.sent++
	f.mu.Unlock()
	return nil
}

func (f *fakeDispatcher) SendHttpData(context.Context, int64, *session.TLSessionSendHttpDataToSession) (*session.HttpSessionData, error) {
	f.mu.Lock()
	f.sent++
	f.mu.Unlock()
	return &session.HttpSessionData{Payload: make([]byte, 64)}, nil
}

func (f *fakeDispatcher) CloseSession(context.Context, int64, *session.TLSessionCloseSession) error {
	return nil
}

func (f *fakeDispatcher) QueryAuthKey(context.Context, int64, *session.TLSessionQueryAuthKey) (*mtproto.AuthKeyInfo, error) {
	return nil, errors.New("not used")
}

func (f *fakeDispatcher) Close() {}

func (f *fakeDispatcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sent
}

// withSession lets decrypted messages reach a fake session.
func (h *testHarness) withSession() *fakeDispatcher {
	d := &fakeDispatcher{}
	h.s.svcCtx = &svc.ServiceContext{Dao: &dao.Dao{SessionDispatcher: d}, GatewayId: "test-gateway"}
	return d
}

// clientPlaintext is a message as a client builds it before encryption:
// salt, session_id, msg_id, seq_no, length, a ping and 20 bytes of padding.
func clientPlaintext() []byte {
	x := mtproto.NewEncodeBuf(64)
	x.Long(0x0102030405060708) // salt
	x.Long(0x1111222233334444) // session_id
	x.Long(mtproto.GenerateMessageId())
	x.Int(1)
	x.Int(12)
	x.Int(int32(mtproto.CRC32_ping))
	x.Long(77)
	b := append([]byte(nil), x.GetBuf()...)
	return append(b, crypto.GenerateNonce(64-len(b))...)
}

// clientEncrypt encrypts plaintext the way a client does (MTProto 2.0,
// client to server, x = 0), independently of the server code, and returns
// the frame and msg_key_large.
func clientEncrypt(authKeyId int64, authKey, plaintext []byte) (frame, msgKeyLarge []byte) {
	h := sha256.New()
	h.Write(authKey[88 : 88+32])
	h.Write(plaintext)
	msgKeyLarge = h.Sum(nil)
	msgKey := msgKeyLarge[8:24]

	a := sha256.Sum256(append(append([]byte(nil), msgKey...), authKey[0:36]...))
	b := sha256.Sum256(append(append([]byte(nil), authKey[40:76]...), msgKey...))
	aesKey := append(append(append([]byte(nil), a[0:8]...), b[8:24]...), a[24:32]...)
	aesIV := append(append(append([]byte(nil), b[0:8]...), a[8:24]...), b[24:32]...)
	enc, err := crypto.NewAES256IGECryptor(aesKey, aesIV).Encrypt(plaintext)
	if err != nil {
		panic(err)
	}
	frame = binary.LittleEndian.AppendUint64(nil, uint64(authKeyId))
	frame = append(frame, msgKey...)
	return append(frame, enc...), msgKeyLarge
}

// ackCodec records Quick ACK tokens.
type ackCodec struct {
	rawCodec
	acks *[]uint32
}

func (c ackCodec) EncodeQuickAck(token uint32) []byte {
	*c.acks = append(*c.acks, token)
	return binary.BigEndian.AppendUint32(nil, token)
}

// TestQuickAckToken: the token is what the client expects, the first 32 bits
// of msg_key_large with the high bit set, not a hash of the ciphertext.
func TestQuickAckToken(t *testing.T) {
	h := newTestHarness(t)
	sent := h.withSession()
	info := testKeyInfo(t, testPermKeyId, mtproto.AuthKeyTypePerm, 0)
	h.s.PutAuthKey(newCheckedAuthKeyUtil(info, testNow, 0))

	var acks []uint32
	c := h.newConn(1, func(ctx *connContext) { ctx.codec = ackCodec{acks: &acks} })
	plaintext := clientPlaintext()
	frame, large := clientEncrypt(testPermKeyId, info.AuthKey, plaintext)
	want := binary.LittleEndian.Uint32(large) | 0x80000000

	h.do(func() {
		if a := h.s.onMTPRawMessage(c.ctx.(*connContext), c, true, frame); a != gnet.None {
			t.Fatalf("action %v", a)
		}
	})
	h.waitFor("message forwarded", func() bool { return sent.count() == 1 })
	h.do(func() {
		if len(acks) != 1 || acks[0] != want {
			t.Fatalf("quick ack %x, want %x", acks, want)
		}
		// What Android does with it: the low 31 bits identify the request.
		if acks[0]&0x7fffffff != binary.LittleEndian.Uint32(large)&0x7fffffff {
			t.Fatal("request id mismatch")
		}
	})
	if got := computeQuickAckToken(info.AuthKey, plaintext); got != want {
		t.Fatalf("computeQuickAckToken = %x, want %x", got, want)
	}
}

// unboundKeySource makes the harness read an unbound temporary key from a
// fake authsession, the way fetchAuthKey does, counting the reads. Each read
// returns a fresh copy of *info.
func unboundKeySource(h *testHarness, info *mtproto.AuthKeyInfo) *int {
	reads := 0
	h.s.queryKey = func(authKeyId int64, known *authKeyUtil) (*authKeyUtil, error) {
		h.mu.Lock()
		reads++
		data := mtproto.NewAuthKeyInfo(info.AuthKeyId, info.AuthKey, int(info.AuthKeyType))
		data.PermAuthKeyId = info.PermAuthKeyId
		h.mu.Unlock()
		key, err := checkQueriedAuthKey(authKeyId, data, testNow, known)
		if err == nil {
			h.s.PutAuthKey(key)
		}
		return key, err
	}
	return &reads
}

var transportError404 = []byte{0x6c, 0xfe, 0xff, 0xff}

// TestUnboundTempKeyReadsBounded: an unbound temporary key costs an
// authsession read per message, so it may not stay unbound forever.
func TestUnboundTempKeyReadsBounded(t *testing.T) {
	h := newTestHarness(t)
	sent := h.withSession()
	info := testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeTemp, 0)
	reads := unboundKeySource(h, info)

	frame, _ := clientEncrypt(testTempKeyId, info.AuthKey, clientPlaintext())
	c := h.newConn(1, nil)
	for i := 1; i <= maxUnboundTempKeyReads; i++ {
		if a := h.sendFrame(c, frame); a != gnet.None {
			t.Fatalf("message %d: action %v", i, a)
		}
		h.waitFor("key read", func() bool { return c.ctx.(*connContext).keyQuery == nil })
	}
	h.waitFor("messages forwarded", func() bool { return sent.count() == maxUnboundTempKeyReads })
	if *reads != maxUnboundTempKeyReads || c.closed {
		t.Fatalf("%d reads, closed %v", *reads, c.closed)
	}

	// One read too many: -404, and the key stays known as exhausted.
	h.sendFrame(c, frame)
	h.waitFor("close", func() bool { return c.closed })
	h.do(func() {
		if !bytes.Equal(c.out[len(c.out)-1], transportError404) {
			t.Fatalf("last write %x", c.out[len(c.out)-1])
		}
	})

	// Other connections get -404 at once, without reading the key again.
	for id := int64(2); id < 5; id++ {
		c2 := h.newConn(id, nil)
		if a := h.sendFrame(c2, frame); a != gnet.Close {
			t.Fatalf("conn %d: action %v", id, a)
		}
		if len(c2.out) != 1 || !bytes.Equal(c2.out[0], transportError404) {
			t.Fatalf("conn %d writes %x", id, c2.out)
		}
	}
	if *reads != maxUnboundTempKeyReads+1 || sent.count() != maxUnboundTempKeyReads {
		t.Fatalf("%d reads, %d forwarded", *reads, sent.count())
	}

	// Connections count together.
	h2 := newTestHarness(t)
	h2.withSession()
	reads2 := unboundKeySource(h2, info)
	conns := []*fakeConn{h2.newConn(1, nil), h2.newConn(2, nil), h2.newConn(3, nil)}
	for i := 0; i < maxUnboundTempKeyReads+len(conns); i++ {
		c := conns[i%len(conns)]
		if c.closed {
			continue
		}
		h2.sendFrame(c, frame)
		h2.waitFor("key read", func() bool { return c.closed || c.ctx.(*connContext).keyQuery == nil })
	}
	if *reads2 > maxUnboundTempKeyReads+1 {
		t.Fatalf("%d reads over %d connections", *reads2, len(conns))
	}
}

// TestBindingStopsUnboundReads: once authsession reports the binding, the
// key is read once a recheck interval, and the earlier reads do not count.
func TestBindingStopsUnboundReads(t *testing.T) {
	h := newTestHarness(t)
	sent := h.withSession()
	info := testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeTemp, 0)
	reads := unboundKeySource(h, info)
	frame, _ := clientEncrypt(testTempKeyId, info.AuthKey, clientPlaintext())
	c := h.newConn(1, nil)
	for i := 0; i < maxUnboundTempKeyReads-1; i++ {
		h.sendFrame(c, frame)
		h.waitFor("key read", func() bool { return c.ctx.(*connContext).keyQuery == nil })
	}
	h.do(func() { info.PermAuthKeyId = testPermKeyId }) // auth.bindTempAuthKey succeeded
	for i := 0; i < 2*maxUnboundTempKeyReads; i++ {
		if a := h.sendFrame(c, frame); a != gnet.None {
			t.Fatalf("message %d after the binding: action %v", i, a)
		}
		h.waitFor("key read", func() bool { return c.ctx.(*connContext).keyQuery == nil })
	}
	h.waitFor("messages forwarded", func() bool { return sent.count() == 3*maxUnboundTempKeyReads-1 })
	if *reads != maxUnboundTempKeyReads {
		t.Fatalf("%d reads", *reads)
	}
}

func httpConn(h *testHarness, id int64) *fakeConn {
	return h.newConn(id, func(ctx *connContext) {
		ctx.http = true
		ctx.tcp = false
		ctx.httpCodec = new(httpcodec.HttpCodec)
	})
}

// postAsync sends one HTTP request on c without waiting for the answer.
func (h *testHarness) postAsync(c *fakeConn, body []byte, then func()) {
	h.t.Helper()
	req := fmt.Sprintf("POST /api HTTP/1.1\r\nHost: x\r\nContent-Length: %d\r\n\r\n", len(body))
	h.do(func() {
		c.in = append([]byte(req), body...)
		if a := h.s.OnTraffic(c); a != gnet.None {
			h.t.Fatalf("action %v", a)
		}
		if then != nil {
			then()
		}
	})
}

// response waits for the n-th response on c and returns its status line and body.
func (h *testHarness) response(c *fakeConn, n int) (string, []byte) {
	h.t.Helper()
	h.waitFor(fmt.Sprintf("response %d", n), func() bool { return len(c.out) >= n })
	var resp []byte
	h.do(func() { resp = c.out[n-1] })
	i := bytes.Index(resp, []byte("\r\n\r\n"))
	if i < 0 {
		h.t.Fatalf("response %q", resp)
	}
	return string(resp[:bytes.IndexByte(resp, '\r')]), resp[i+4:]
}

// TestHttpKeyReadErrors: only an unknown key is answered with -404; a failed
// read (authsession restarting, a timeout) must not make clients drop a key.
func TestHttpKeyReadErrors(t *testing.T) {
	h := newTestHarness(t)
	info := testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeTemp, testPermKeyId)
	frame, _ := clientEncrypt(testTempKeyId, info.AuthKey, clientPlaintext())

	h.s.queryKey = func(int64, *authKeyUtil) (*authKeyUtil, error) {
		return nil, errors.New("authsession unavailable")
	}
	c := httpConn(h, 1)
	h.postAsync(c, frame, nil)
	if status, body := h.response(c, 1); status != "HTTP/1.1 502 Bad Gateway" || bytes.Equal(body, transportError404) {
		t.Fatalf("authsession failure answered %q %x", status, body)
	}

	h.s.queryKey = func(int64, *authKeyUtil) (*authKeyUtil, error) {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	c2 := httpConn(h, 2)
	h.postAsync(c2, frame, nil)
	if status, body := h.response(c2, 1); status != "HTTP/1.1 200 OK" || !bytes.Equal(body, transportError404) {
		t.Fatalf("unknown key answered %q %x", status, body)
	}
}

// TestHttpUnboundTempKeyReadsBounded: the HTTP transport applies the same
// bound to unbound temporary keys.
func TestHttpUnboundTempKeyReadsBounded(t *testing.T) {
	h := newTestHarness(t)
	h.withSession()
	info := testKeyInfo(t, testTempKeyId, mtproto.AuthKeyTypeTemp, 0)
	reads := unboundKeySource(h, info)
	frame, _ := clientEncrypt(testTempKeyId, info.AuthKey, clientPlaintext())

	c := httpConn(h, 1)
	for i := 1; i <= maxUnboundTempKeyReads; i++ {
		h.postAsync(c, frame, nil)
		if status, body := h.response(c, i); status != "HTTP/1.1 200 OK" || bytes.Equal(body, transportError404) {
			t.Fatalf("request %d: %q %x", i, status, body)
		}
	}
	for i := 1; i <= 2; i++ {
		h.postAsync(c, frame, nil)
		if _, body := h.response(c, maxUnboundTempKeyReads+i); !bytes.Equal(body, transportError404) {
			t.Fatalf("request past the bound: %x", body)
		}
	}
	if *reads != maxUnboundTempKeyReads+1 {
		t.Fatalf("%d reads", *reads)
	}
}

// TestHttpHandshakeStateGoneStillAnswered: when the state of a DH step
// disappears while it is computed (a new req_pq with the same nonce, or
// eviction by other handshakes), the HTTP request is still answered and the
// connection keeps working.
func TestHttpHandshakeStateGoneStillAnswered(t *testing.T) {
	h := newTestHarness(t)
	evicted := 0
	for attempt := 0; attempt < 10 && evicted < 3; attempt++ {
		tc := newTestClient(t, &h.key.PublicKey)
		c := httpConn(h, int64(attempt+1))
		h.postAsync(c, tc.reqPqMulti(), nil)
		_, body := h.response(c, 1)
		tc.onResPQ(parseUnencrypted(t, body))

		// The worker computes the step while the state is dropped.
		h.postAsync(c, tc.reqDHParams(false), func() { h.s.httpHandshakes.drop(tc.nonce) })
		status, body := h.response(c, 2)
		if status != "HTTP/1.1 200 OK" {
			t.Fatalf("DH step answered %q", status)
		}
		if len(body) == 0 {
			evicted++
		} else if _, ok := parseUnencrypted(t, body).(*mtproto.TLServer_DHParamsOk); !ok {
			t.Fatalf("unexpected answer %x", body)
		}

		// The next request on the connection is parsed and answered.
		tc2 := newTestClient(t, &h.key.PublicKey)
		h.postAsync(c, tc2.reqPqMulti(), nil)
		_, body = h.response(c, 3)
		tc2.onResPQ(parseUnencrypted(t, body))
	}
	if evicted == 0 {
		t.Fatal("the state was never dropped before the worker finished")
	}
}

// TestTempKeyNeedsExpiry: a temporary key handshake without a positive
// expires_in is refused (it would be stored as never expiring).
func TestTempKeyNeedsExpiry(t *testing.T) {
	for _, expiresIn := range []int32{0, -1} {
		h := newTestHarness(t)
		c := h.newConn(1, nil)
		tc := newTestClient(t, &h.key.PublicKey)
		tc.expiresIn = expiresIn
		h.sendFrame(c, tc.reqPqMulti())
		tc.onResPQ(h.lastUnencrypted(c))
		h.sendFrame(c, tc.reqDHParams(true))
		h.waitFor("close", func() bool { return c.closed })
		h.do(func() {
			if len(c.out) != 1 || len(h.saved) != 0 {
				t.Fatalf("expires_in %d: %d writes, %d keys", expiresIn, len(c.out), len(h.saved))
			}
		})
	}
}

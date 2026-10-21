package gnet

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/cache"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/server/gnet/codec"
	"github.com/teamgram/teamgram-server/pkg/serverkey"

	"github.com/panjf2000/gnet/v2"
	"github.com/panjf2000/gnet/v2/pkg/pool/goroutine"
)

// fakeConn is a gnet.Conn for tests: inbound data, recorded writes and
// close. Methods the gateway does not use are left to the embedded nil
// interface (they panic).
type fakeConn struct {
	gnet.Conn
	id     int64
	ctx    any
	in     []byte
	out    [][]byte
	closed bool
}

func (c *fakeConn) Context() any         { return c.ctx }
func (c *fakeConn) SetContext(ctx any)   { c.ctx = ctx }
func (c *fakeConn) ConnId() int64        { return c.id }
func (c *fakeConn) String() string       { return fmt.Sprintf("fake(%d)", c.id) }
func (c *fakeConn) Close() error         { c.closed = true; return nil }
func (c *fakeConn) InboundBuffered() int { return len(c.in) }
func (c *fakeConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 40000}
}
func (c *fakeConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(192, 0, 2, 2), Port: 10443}
}

func (c *fakeConn) Write(b []byte) (int, error) {
	c.out = append(c.out, append([]byte(nil), b...))
	return len(b), nil
}

func (c *fakeConn) Peek(n int) ([]byte, error) {
	if n <= 0 {
		return c.in, nil
	}
	if n > len(c.in) {
		return c.in, io.ErrShortBuffer
	}
	return c.in[:n], nil
}

func (c *fakeConn) Next(n int) ([]byte, error) {
	b, err := c.Peek(n)
	if err != nil {
		return nil, err
	}
	c.in = c.in[len(b):]
	return b, nil
}

func (c *fakeConn) Discard(n int) (int, error) {
	if n <= 0 || n > len(c.in) {
		n = len(c.in)
	}
	c.in = c.in[n:]
	return n, nil
}

func (c *fakeConn) Read(p []byte) (int, error) {
	n := copy(p, c.in)
	c.in = c.in[n:]
	return n, nil
}

// rawCodec hands frames through unchanged: Encode returns the payload of an
// MTPRawMessage, Decode is not used.
type rawCodec struct{}

func (rawCodec) Encode(_ codec.CodecWriter, msg interface{}) ([]byte, error) {
	return append([]byte(nil), msg.(*mtproto.MTPRawMessage).Payload...), nil
}
func (rawCodec) Decode(codec.CodecReader) (bool, []byte, error) { return false, nil, nil }
func (rawCodec) EncodeQuickAck(uint32) []byte                   { return nil }

// panicCodec panics on Decode, standing in for any bug reached by a packet.
type panicCodec struct{ rawCodec }

func (panicCodec) Decode(codec.CodecReader) (bool, []byte, error) {
	var b []byte
	return false, b[:8], nil // index out of range
}

// testHarness is a Server with an "event loop" the test controls: code that
// gnet would run on the loop runs under mu (test code too, through do).
type testHarness struct {
	t     testing.TB
	s     *Server
	key   *rsa.PrivateKey
	mu    sync.Mutex
	conns map[int64]*fakeConn
	saved []*mtproto.AuthKeyInfo
}

var (
	testRSAKeyOnce sync.Once
	testRSAKey     *rsa.PrivateKey
)

func newTestHarness(t testing.TB) *testHarness {
	t.Helper()
	testRSAKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, serverkey.Bits)
		if err != nil {
			panic(err)
		}
		testRSAKey = k
	})
	key := &serverkey.Key{
		Path:        "test",
		Private:     testRSAKey,
		PEM:         pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testRSAKey)}),
		Fingerprint: serverkey.Fingerprint(&testRSAKey.PublicKey),
	}
	h := &testHarness{t: t, key: testRSAKey, conns: map[int64]*fakeConn{}}
	hs, err := newHandshake([]*serverkey.Key{key})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		handshake:      hs,
		pool:           goroutine.Default(),
		cache:          cache.NewLRUCache(10 << 20),
		authSessionMgr: NewAuthSessionManager(),
		timeoutWheel:   newTimeoutWheel(),
		httpHandshakes: newHandshakeStore(maxHttpHandshakes, httpHandshakeTTL),
	}
	s.cachedNow.Store(testNow)
	s.saveAuthKey = func(st *HandshakeStateCtx, k *mtproto.AuthKeyInfo) bool {
		h.mu.Lock() // called from the pool
		h.saved = append(h.saved, k)
		h.mu.Unlock()
		s.PutAuthKey(newCheckedAuthKeyUtil(k, testNow, 0))
		return true
	}
	s.queryKey = func(int64, *authKeyUtil) (*authKeyUtil, error) {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	s.triggerFn = func(connId int64, cb func(c gnet.Conn)) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if c := h.conns[connId]; c != nil && !c.closed {
			cb(c)
		}
	}
	h.s = s
	return h
}

// newConn opens a connection with the given transport flags.
func (h *testHarness) newConn(id int64, mutate func(ctx *connContext)) *fakeConn {
	c := &fakeConn{id: id}
	ctx := newConnContext()
	ctx.codec = rawCodec{}
	ctx.tcp = true
	ctx.closeDate = testNow + unauthenticatedConnTimeout
	if mutate != nil {
		mutate(ctx)
	}
	c.SetContext(ctx)
	h.mu.Lock()
	h.conns[id] = c
	h.mu.Unlock()
	return c
}

// do runs f on the "event loop".
func (h *testHarness) do(f func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f()
}

// waitFor waits until cond (checked on the event loop) holds.
func (h *testHarness) waitFor(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		ok := cond()
		h.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s", what)
}

// sendFrame feeds one transport frame to the connection as the TCP and
// WebSocket transports do.
func (h *testHarness) sendFrame(c *fakeConn, frame []byte) (action gnet.Action) {
	h.do(func() {
		defer recoverConn(c, "traffic", &action)
		action = h.s.onMTPRawMessage(c.ctx.(*connContext), c, false, frame)
	})
	return action
}

// unencrypted builds an unencrypted message frame for obj.
func unencrypted(obj mtproto.TLObject) []byte {
	x := mtproto.NewEncodeBuf(512)
	_ = serializeToBuffer(x, mtproto.GenerateMessageId(), obj)
	return x.GetBuf()
}

// parseUnencrypted decodes an unencrypted message the server wrote.
func parseUnencrypted(t testing.TB, b []byte) mtproto.TLObject {
	t.Helper()
	if len(b) < 24 || binary.LittleEndian.Uint64(b) != 0 {
		t.Fatalf("not an unencrypted message: %x", b)
	}
	n := binary.LittleEndian.Uint32(b[16:])
	d := mtproto.NewDecodeBuf(b[20 : 20+n])
	o := d.Object()
	if d.GetError() != nil {
		t.Fatal(d.GetError())
	}
	return o
}

// testClient performs the client side of the key exchange, independently of
// the server code, as the official clients do.
type testClient struct {
	t           testing.TB
	pub         *rsa.PublicKey
	fingerprint int64
	nonce       []byte
	serverNonce []byte
	newNonce    []byte
	gA          *big.Int
	dhPrime     *big.Int
	g           int64
	b           *big.Int
	authKey     []byte
	expiresIn   int32 // of a temporary key
}

func newTestClient(t testing.TB, pub *rsa.PublicKey) *testClient {
	return &testClient{
		t:           t,
		pub:         pub,
		fingerprint: int64(serverkey.Fingerprint(pub)),
		nonce:       crypto.GenerateNonce(16),
		newNonce:    crypto.GenerateNonce(32),
		expiresIn:   86400,
	}
}

func (tc *testClient) reqPqMulti() []byte {
	return unencrypted(&mtproto.TLReqPqMulti{Constructor: mtproto.CRC32_req_pq_multi, Nonce: tc.nonce})
}

func (tc *testClient) onResPQ(o mtproto.TLObject) {
	tc.t.Helper()
	res, ok := o.(*mtproto.TLResPQ)
	if !ok {
		tc.t.Fatalf("got %T, want resPQ", o)
	}
	if !bytes.Equal(res.GetNonce(), tc.nonce) || len(res.GetServerNonce()) != 16 {
		tc.t.Fatal("resPQ nonce")
	}
	if !slices.Contains(res.GetServerPublicKeyFingerprints(), tc.fingerprint) {
		tc.t.Fatal("resPQ does not list the key")
	}
	tc.serverNonce = res.GetServerNonce()
}

// rsaPad implements RSA_PAD(data, server_public_key).
func (tc *testClient) rsaPad(data []byte) []byte {
	tc.t.Helper()
	if len(data) > 144 {
		tc.t.Fatalf("data is %d bytes", len(data))
	}
	for {
		dataWithPadding := append(append([]byte(nil), data...), crypto.GenerateNonce(192-len(data))...)
		reversed := slices.Clone(dataWithPadding)
		slices.Reverse(reversed)
		tempKey := crypto.GenerateNonce(32)
		h := sha256.Sum256(append(append([]byte(nil), tempKey...), dataWithPadding...))
		dataWithHash := append(reversed, h[:]...)
		aesEncrypted, err := crypto.NewAES256IGECryptor(tempKey, make([]byte, 32)).Encrypt(dataWithHash)
		if err != nil {
			tc.t.Fatal(err)
		}
		h2 := sha256.Sum256(aesEncrypted)
		keyAesEncrypted := make([]byte, 0, 256)
		for i := 0; i < 32; i++ {
			keyAesEncrypted = append(keyAesEncrypted, tempKey[i]^h2[i])
		}
		keyAesEncrypted = append(keyAesEncrypted, aesEncrypted...)
		m := new(big.Int).SetBytes(keyAesEncrypted)
		if m.Cmp(tc.pub.N) >= 0 {
			continue
		}
		c := new(big.Int).Exp(m, big.NewInt(int64(tc.pub.E)), tc.pub.N)
		out := make([]byte, 256)
		return c.FillBytes(out)
	}
}

func (tc *testClient) reqDHParams(temp bool) []byte {
	inner := &mtproto.P_QInnerData{
		Pq:          pq,
		P:           string(p),
		Q:           string(q),
		Nonce:       tc.nonce,
		ServerNonce: tc.serverNonce,
		NewNonce:    tc.newNonce,
		Dc:          2,
		ExpiresIn:   tc.expiresIn,
	}
	x := mtproto.NewEncodeBuf(256)
	if temp {
		_ = mtproto.MakeTLPQInnerDataTempDc(inner).Encode(x, 0)
	} else {
		_ = mtproto.MakeTLPQInnerDataDc(inner).Encode(x, 0)
	}
	return unencrypted(&mtproto.TLReq_DHParams{
		Constructor:          mtproto.CRC32_req_DH_params,
		Nonce:                tc.nonce,
		ServerNonce:          tc.serverNonce,
		P:                    string(p),
		Q:                    string(q),
		PublicKeyFingerprint: tc.fingerprint,
		EncryptedData:        string(tc.rsaPad(x.GetBuf())),
	})
}

func (tc *testClient) tmpAes() (key, iv []byte) {
	s := func(a, b []byte) []byte {
		h := sha1.Sum(append(append([]byte(nil), a...), b...))
		return h[:]
	}
	ns := s(tc.newNonce, tc.serverNonce)
	sn := s(tc.serverNonce, tc.newNonce)
	nn := s(tc.newNonce, tc.newNonce)
	key = append(append([]byte(nil), ns...), sn[:12]...)
	iv = append(append(append([]byte(nil), sn[12:20]...), nn...), tc.newNonce[:4]...)
	return key, iv
}

func (tc *testClient) onServerDHParams(o mtproto.TLObject) {
	tc.t.Helper()
	res, ok := o.(*mtproto.TLServer_DHParamsOk)
	if !ok {
		tc.t.Fatalf("got %T, want server_DH_params_ok", o)
	}
	key, iv := tc.tmpAes()
	answer, err := crypto.NewAES256IGECryptor(key, iv).Decrypt([]byte(res.GetEncryptedAnswer()))
	if err != nil {
		tc.t.Fatal(err)
	}
	d := mtproto.NewDecodeBuf(answer[20:])
	inner, ok := d.Object().(*mtproto.TLServer_DHInnerData)
	if !ok || d.GetError() != nil {
		tc.t.Fatalf("server_DH_inner_data: %v", d.GetError())
	}
	if h := sha1.Sum(answer[20 : 20+d.GetOffset()]); !bytes.Equal(h[:], answer[:20]) {
		tc.t.Fatal("answer_with_hash: SHA1 mismatch")
	}
	if !bytes.Equal(inner.GetNonce(), tc.nonce) || !bytes.Equal(inner.GetServerNonce(), tc.serverNonce) {
		tc.t.Fatal("server_DH_inner_data nonces")
	}
	tc.g = int64(inner.GetG())
	tc.dhPrime = new(big.Int).SetBytes([]byte(inner.GetDhPrime()))
	tc.gA = new(big.Int).SetBytes([]byte(inner.GetGA()))
	if tc.dhPrime.BitLen() != 2048 || tc.gA.Cmp(big.NewInt(1)) <= 0 || tc.gA.Cmp(tc.dhPrime) >= 0 {
		tc.t.Fatal("dh_prime or g_a out of range")
	}
}

func (tc *testClient) setClientDHParams() []byte {
	tc.b = new(big.Int).SetBytes(crypto.GenerateNonce(256))
	gB := new(big.Int).Exp(big.NewInt(tc.g), tc.b, tc.dhPrime)
	x := mtproto.NewEncodeBuf(512)
	_ = mtproto.MakeTLClient_DHInnerData(&mtproto.Client_DH_Inner_Data{
		Nonce:       tc.nonce,
		ServerNonce: tc.serverNonce,
		RetryId:     0,
		GB:          string(gB.Bytes()),
	}).Encode(x, 0)
	data := x.GetBuf()
	h := sha1.Sum(data)
	plain := append(append([]byte(nil), h[:]...), data...)
	if len(plain)%16 != 0 {
		plain = append(plain, crypto.GenerateNonce(16-len(plain)%16)...)
	}
	key, iv := tc.tmpAes()
	enc, err := crypto.NewAES256IGECryptor(key, iv).Encrypt(plain)
	if err != nil {
		tc.t.Fatal(err)
	}
	tc.authKey = new(big.Int).Exp(tc.gA, tc.b, tc.dhPrime).FillBytes(make([]byte, 256))
	return unencrypted(&mtproto.TLSetClient_DHParams{
		Constructor:   mtproto.CRC32_set_client_DH_params,
		Nonce:         tc.nonce,
		ServerNonce:   tc.serverNonce,
		EncryptedData: string(enc),
	})
}

// onDHGen checks dh_gen_ok and returns the auth key id.
func (tc *testClient) onDHGen(o mtproto.TLObject) int64 {
	tc.t.Helper()
	res, ok := o.(*mtproto.TLDhGenOk)
	if !ok {
		tc.t.Fatalf("got %T, want dh_gen_ok", o)
	}
	keyHash := sha1.Sum(tc.authKey)
	h := sha1.Sum(append(append(append([]byte(nil), tc.newNonce...), 1), keyHash[:8]...))
	if !bytes.Equal(res.GetNewNonceHash1(), h[4:20]) {
		tc.t.Fatal("new_nonce_hash1 mismatch")
	}
	return int64(binary.LittleEndian.Uint64(keyHash[12:20]))
}

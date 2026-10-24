package gnet

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"

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

// rsaPadWith is RSA_PAD(data, server_public_key)
// (https://core.telegram.org/mtproto/auth_key, step 4.1) written out, with
// edit applied to data_with_hash before it is encrypted.
func rsaPadWith(t testing.TB, pub *rsa.PublicKey, data []byte, edit func(tempKey, dataWithHash []byte)) []byte {
	t.Helper()
	dataWithPadding := append(slices.Clone(data), crypto.GenerateNonce(192-len(data))...)
	for {
		tempKey := crypto.GenerateNonce(32)
		dataWithHash := slices.Clone(dataWithPadding)
		slices.Reverse(dataWithHash)
		h := sha256.Sum256(append(slices.Clone(tempKey), dataWithPadding...))
		dataWithHash = append(dataWithHash, h[:]...)
		if edit != nil {
			edit(tempKey, dataWithHash)
		}
		aesEncrypted, err := crypto.NewAES256IGECryptor(tempKey, make([]byte, 32)).Encrypt(dataWithHash)
		if err != nil {
			t.Fatal(err)
		}
		h2 := sha256.Sum256(aesEncrypted)
		keyAesEncrypted := make([]byte, 0, 256)
		for i := range tempKey {
			keyAesEncrypted = append(keyAesEncrypted, tempKey[i]^h2[i])
		}
		m := new(big.Int).SetBytes(append(keyAesEncrypted, aesEncrypted...))
		if m.Cmp(pub.N) >= 0 {
			continue // as a client does: another temp_key
		}
		return new(big.Int).Exp(m, big.NewInt(int64(pub.E)), pub.N).FillBytes(make([]byte, 256))
	}
}

// TestReqDHParamsChecksRSAPadHash: data_with_hash must end with
// SHA256(temp_key + data_with_padding).
func TestReqDHParamsChecksRSAPadHash(t *testing.T) {
	cases := []struct {
		name string
		edit func(tempKey, dataWithHash []byte)
		ok   bool
	}{
		{"valid", nil, true},
		{"a bit of the hash flipped", func(_, d []byte) { d[200] ^= 0x10 }, false},
		{"a padding byte changed", func(_, d []byte) { d[0] ^= 0x01 }, false},
		{"the hash without temp_key", func(_, d []byte) {
			plain := slices.Clone(d[:192])
			slices.Reverse(plain)
			h := sha256.Sum256(plain)
			copy(d[192:], h[:])
		}, false},
		{"the hash of data_pad_reversed", func(k, d []byte) {
			h := sha256.Sum256(append(slices.Clone(k), d[:192]...))
			copy(d[192:], h[:])
		}, false},
	}
	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			h := newTestHarness(t)
			c, tc := h.startDH(1)
			frame := tc.reqDHParamsWith(false, reqDHParamsEdits{rsaPad: func(data []byte) []byte {
				return rsaPadWith(t, &h.key.PublicKey, data, tcase.edit)
			}})
			if !tcase.ok {
				h.expectRejected(c, frame)
				return
			}
			if a := h.sendFrame(c, frame); a != gnet.None {
				t.Fatalf("action %v", a)
			}
			h.waitWrites(c, 2)
			tc.onServerDHParams(h.lastUnencrypted(c))
		})
	}
}

// TestSetClientDHParamsChecksHash: encrypted_data must decrypt to
// SHA1(client_DH_inner_data) + client_DH_inner_data + 0-15 bytes of padding.
func TestSetClientDHParamsChecksHash(t *testing.T) {
	cases := []struct {
		name  string
		plain func(plain []byte) []byte
		ok    bool
	}{
		{"valid", nil, true},
		{"a bit of the hash flipped", func(p []byte) []byte { p[3] ^= 0x40; return p }, false},
		{"a bit of g_b flipped", func(p []byte) []byte { p[100] ^= 0x01; return p }, false},
		{"the hash over the padding too", func(p []byte) []byte {
			h := sha1.Sum(p[20:])
			copy(p, h[:])
			return p
		}, false},
		{"16 more bytes of padding", func(p []byte) []byte { return append(p, crypto.GenerateNonce(16)...) }, false},
	}
	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			h := newTestHarness(t)
			c, tc := h.startSetDH(1)
			frame := tc.setClientDHParamsWith(setClientDHParamsEdits{plain: tcase.plain})
			if !tcase.ok {
				h.expectRejected(c, frame)
				return
			}
			if a := h.sendFrame(c, frame); a != gnet.None {
				t.Fatalf("action %v", a)
			}
			h.waitWrites(c, 3)
			tc.onDHGen(h.lastUnencrypted(c))
		})
	}
}

func TestIsGoodDHValue(t *testing.T) {
	one := big.NewInt(1)
	p := gBigIntDH2048P
	low := new(big.Int).Lsh(one, 2048-64)
	high := new(big.Int).Sub(p, low)
	add := func(a, b *big.Int) *big.Int { return new(big.Int).Add(a, b) }
	sub := func(a, b *big.Int) *big.Int { return new(big.Int).Sub(a, b) }
	gB := new(big.Int).Exp(gBigIntDH2048G, new(big.Int).SetBytes(crypto.GenerateNonce(256)), p)

	for _, c := range []struct {
		name string
		v    *big.Int
		ok   bool
	}{
		{"0", big.NewInt(0), false},
		{"1", one, false},
		{"2", big.NewInt(2), false},
		{"2^(2048-64) - 1", sub(low, one), false},
		{"2^(2048-64)", low, false},
		{"2^(2048-64) + 1", add(low, one), true},
		{"g^b", gB, true},
		{"dh_prime - 2^(2048-64) - 1", sub(high, one), true},
		{"dh_prime - 2^(2048-64)", high, false},
		{"dh_prime - 2^(2048-64) + 1", add(high, one), false},
		{"dh_prime - 1", sub(p, one), false},
		{"dh_prime", p, false},
		{"dh_prime + 1", add(p, one), false},
	} {
		if isGoodDHValue(c.v) != c.ok {
			t.Errorf("%s: isGoodDHValue = %v", c.name, !c.ok)
		}
	}
}

// TestSetClientDHParamsChecksGB: g_b must be in 2^(2048-64) < g_b <
// dh_prime - 2^(2048-64), which also excludes 1 and dh_prime - 1.
func TestSetClientDHParamsChecksGB(t *testing.T) {
	one := big.NewInt(1)
	p := gBigIntDH2048P
	low := new(big.Int).Lsh(one, 2048-64)
	high := new(big.Int).Sub(p, low)
	for _, c := range []struct {
		name string
		gB   *big.Int
		ok   bool
	}{
		{"1", one, false},
		{"dh_prime - 1", new(big.Int).Sub(p, one), false},
		{"dh_prime", p, false},
		{"2^(2048-64)", low, false},
		{"dh_prime - 2^(2048-64)", high, false},
		{"2^(2048-64) + 1", new(big.Int).Add(low, one), true},
		{"dh_prime - 2^(2048-64) - 1", new(big.Int).Sub(high, one), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newTestHarness(t)
			conn, tc := h.startSetDH(1)
			frame := tc.setClientDHParamsWith(setClientDHParamsEdits{gB: c.gB})
			if !c.ok {
				h.expectRejected(conn, frame)
				return
			}
			if a := h.sendFrame(conn, frame); a != gnet.None {
				t.Fatalf("action %v", a)
			}
			h.waitWrites(conn, 3)
			if o, ok := h.lastUnencrypted(conn).(*mtproto.TLDhGenOk); !ok {
				t.Fatalf("got %T, want dh_gen_ok", o)
			}
		})
	}
}

// refuseFirst makes the harness refuse the first key it is asked to save
// with err.
func (h *testHarness) refuseFirst(err error) {
	refused := false
	h.do(func() {
		h.refuse = func(*mtproto.AuthKeyInfo) error {
			if refused {
				return nil
			}
			refused = true
			return err
		}
	})
}

// TestDHGenRetryOnCollision: a key whose auth_key_id is taken is answered
// with dh_gen_retry; the client tries a new b with retry_id =
// auth_key_aux_hash of that key and gets dh_gen_ok.
func TestDHGenRetryOnCollision(t *testing.T) {
	h := newTestHarness(t)
	h.refuseFirst(errAuthKeyIdTaken)
	c, tc := h.startSetDH(1)

	if a := h.sendFrame(c, tc.setClientDHParams()); a != gnet.None {
		t.Fatalf("action %v", a)
	}
	h.waitWrites(c, 3)
	retryId := tc.onDHGenRetry(h.lastUnencrypted(c))
	h.do(func() {
		st, ok := c.ctx.(*connContext).handshakes.states.get(tc.nonce, testNow)
		if !ok || st.State != STATE_DH_params_res || st.RetryId != retryId || len(h.saved) != 0 {
			t.Fatalf("after dh_gen_retry: state %v, retry_id %d (want %d), %d keys", ok, st.RetryId, retryId, len(h.saved))
		}
	})

	if a := h.sendFrame(c, tc.setClientDHParamsWith(setClientDHParamsEdits{retryId: retryId})); a != gnet.None {
		t.Fatalf("action %v", a)
	}
	h.waitWrites(c, 4)
	id := tc.onDHGen(h.lastUnencrypted(c))
	h.do(func() {
		if len(h.saved) != 1 || h.saved[0].AuthKeyId != id || !bytes.Equal(h.saved[0].AuthKey, tc.authKey) {
			t.Fatal("the key of the second attempt was not saved")
		}
		if n := c.ctx.(*connContext).handshakes.states.len(); n != 0 {
			t.Fatalf("%d handshakes left", n)
		}
	})
}

// TestSetClientDHParamsChecksRetryId: retry_id is 0 at the first attempt
// and auth_key_aux_hash of the colliding key after dh_gen_retry.
func TestSetClientDHParamsChecksRetryId(t *testing.T) {
	t.Run("first attempt with a retry_id", func(t *testing.T) {
		h := newTestHarness(t)
		c, tc := h.startSetDH(1)
		h.expectRejected(c, tc.setClientDHParamsWith(setClientDHParamsEdits{retryId: 1}))
	})

	for _, tcase := range []struct {
		name    string
		retryId func(auxHash int64) int64
	}{
		{"retry with retry_id 0", func(int64) int64 { return 0 }},
		{"retry with another retry_id", func(aux int64) int64 { return aux ^ 1 }},
		{"retry with the attempt counter", func(int64) int64 { return 1 }},
	} {
		t.Run(tcase.name, func(t *testing.T) {
			h := newTestHarness(t)
			h.refuseFirst(errAuthKeyIdTaken)
			c, tc := h.startSetDH(1)
			h.sendFrame(c, tc.setClientDHParams())
			h.waitWrites(c, 3)
			aux := tc.onDHGenRetry(h.lastUnencrypted(c))
			h.expectRejected(c, tc.setClientDHParamsWith(setClientDHParamsEdits{retryId: tcase.retryId(aux)}))
		})
	}
}

// TestDHGenFailOnStorageError: a key that cannot be stored for another
// reason than a collision is answered with dh_gen_fail, which ends the
// handshake.
func TestDHGenFailOnStorageError(t *testing.T) {
	h := newTestHarness(t)
	h.refuseFirst(errors.New("storage unavailable"))
	c, tc := h.startSetDH(1)

	if a := h.sendFrame(c, tc.setClientDHParams()); a != gnet.None {
		t.Fatalf("action %v", a)
	}
	h.waitWrites(c, 3)
	tc.onDHGenFail(h.lastUnencrypted(c))
	h.do(func() {
		if n := c.ctx.(*connContext).handshakes.states.len(); n != 0 || len(h.saved) != 0 {
			t.Fatalf("after dh_gen_fail: %d handshakes, %d keys", n, len(h.saved))
		}
	})
	// The client starts over; set_client_DH_params of the failed handshake
	// is out of order.
	h.expectRejected(c, tc.setClientDHParams())
}

// TestSetAuthKeyResult: boolFalse of session.setAuthKey is a collision, an
// error or an empty answer a failure to store the key.
func TestSetAuthKeyResult(t *testing.T) {
	failed := errors.New("unavailable")
	if err := setAuthKeyResult(mtproto.BoolTrue, nil); err != nil {
		t.Errorf("boolTrue: %v", err)
	}
	if err := setAuthKeyResult(mtproto.BoolFalse, nil); !errors.Is(err, errAuthKeyIdTaken) {
		t.Errorf("boolFalse: %v", err)
	}
	if err := setAuthKeyResult(nil, failed); !errors.Is(err, failed) {
		t.Errorf("error: %v", err)
	}
	if err := setAuthKeyResult(nil, nil); err == nil || errors.Is(err, errAuthKeyIdTaken) {
		t.Errorf("no answer: %v", err)
	}
}

// FuzzProcessReqDHParams feeds arbitrary data_with_padding, correctly
// RSA_PAD-ed, to req_DH_params: nothing may panic, and only a
// p_q_inner_data that matches the handshake is accepted.
func FuzzProcessReqDHParams(f *testing.F) {
	h := newTestHarness(f)
	pq, p, q := newPQ()
	tc := newTestClient(f, &h.key.PublicKey)
	tc.serverNonce = crypto.GenerateNonce(16)
	tc.pq = putPQ(pq)
	tc.p = string(binary.BigEndian.AppendUint32(nil, uint32(p)))
	tc.q = string(binary.BigEndian.AppendUint32(nil, uint32(q)))
	st := HandshakeStateCtx{State: STATE_DH_params, Nonce: tc.nonce, ServerNonce: tc.serverNonce, Pq: pq, P: p, Q: q}

	var valid []byte
	for _, temp := range []bool{false, true} {
		tc.reqDHParamsWith(temp, reqDHParamsEdits{rsaPad: func(data []byte) []byte {
			valid = data
			f.Add(data)
			return make([]byte, 256)
		}})
	}
	f.Add(valid[:4])
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 192 {
			data = data[:192]
		}
		req := &mtproto.TLReq_DHParams{
			Nonce:                st.Nonce,
			ServerNonce:          st.ServerNonce,
			P:                    tc.p,
			Q:                    tc.q,
			PublicKeyFingerprint: tc.fingerprint,
			EncryptedData:        string(rsaPadWith(t, &h.key.PublicKey, data, nil)),
		}
		res, err := h.s.handshake.processReqDHParams(&st, req, testNow)
		if err == nil && (res.answer == nil || len(res.newNonce) != 32 || len(res.a) != dhSecretSize) {
			t.Fatal("accepted without a full answer")
		}
	})
}

// FuzzProcessSetClientDHParams feeds arbitrary plaintext (encrypted with the
// right tmp_aes_key) to set_client_DH_params: nothing may panic, and only a
// hashed client_DH_inner_data with a good g_b is accepted.
func FuzzProcessSetClientDHParams(f *testing.F) {
	tc := newTestClient(f, &newTestHarness(f).key.PublicKey)
	tc.serverNonce = crypto.GenerateNonce(16)
	tc.g, tc.dhPrime, tc.gA = 3, gBigIntDH2048P, new(big.Int).Exp(gBigIntDH2048G, big.NewInt(1<<40+1), gBigIntDH2048P)
	st := HandshakeStateCtx{
		State:       STATE_dh_gen,
		Nonce:       tc.nonce,
		ServerNonce: tc.serverNonce,
		NewNonce:    tc.newNonce,
		A:           crypto.GenerateNonce(dhSecretSize),
	}
	key, iv := tc.tmpAes()

	tc.setClientDHParamsWith(setClientDHParamsEdits{plain: func(plain []byte) []byte {
		f.Add(slices.Clone(plain))
		f.Add(slices.Clone(plain[:32]))
		return plain
	}})
	f.Add(make([]byte, 16))

	f.Fuzz(func(t *testing.T, plain []byte) {
		if len(plain)%16 != 0 {
			plain = append(plain, make([]byte, 16-len(plain)%16)...)
		}
		enc, err := crypto.NewAES256IGECryptor(key, iv).Encrypt(plain)
		if err != nil {
			return
		}
		res, err := processSetClientDHParams(&st, &mtproto.TLSetClient_DHParams{
			Nonce:         st.Nonce,
			ServerNonce:   st.ServerNonce,
			EncryptedData: string(enc),
		})
		if err != nil {
			return
		}
		if len(res.authKey) != 256 {
			t.Fatal("accepted without a key")
		}
	})
}

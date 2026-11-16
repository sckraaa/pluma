// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

// Package handshake is the server side of the MTProto key exchange
// (https://core.telegram.org/mtproto/auth_key): req_pq / req_pq_multi,
// req_DH_params with RSA_PAD and set_client_DH_params, for permanent and
// temporary keys. A transport feeds it unencrypted messages through an
// Exchange, which says how handshake state is kept, where the expensive
// steps run and how answers and new keys leave; it is shared by the
// gateway (keys saved through the session service) and by any other
// MTProto server of the project (keys kept in memory).
package handshake

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // MTProto's key exchange is defined over SHA-1
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/teamgram/marmota/pkg/hack"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

type rsaKeyHelper struct {
	rsa            *crypto.RSACryptor
	keyFingerprint int64
}

// Server holds the server RSA keys a handshake decrypts req_DH_params with.
// It keeps no handshake state (see Store) and is safe for concurrent use.
type Server struct {
	keyFingerprints []int64
	rsaList         []rsaKeyHelper
	dh2048p         []byte
	dh2048g         []byte
}

// New returns a Server for the given server RSA keys, all of which resPQ
// advertises (several keys rotate without downtime). The keys are expected
// to be loaded and verified (serverkey.Load).
func New(keys []*serverkey.Key) (*Server, error) {
	if len(keys) == 0 {
		return nil, errors.New("handshake: no server RSA key")
	}
	h := &Server{
		keyFingerprints: make([]int64, 0, len(keys)),
		rsaList:         make([]rsaKeyHelper, 0, len(keys)),
		dh2048p:         dh2048P,
		dh2048g:         dh2048G,
	}

	for _, key := range keys {
		rsa, err := crypto.NewRSACryptorByKeyData(key.PEM)
		if err != nil {
			return nil, fmt.Errorf("server RSA key %s: %w", key.Path, err)
		}

		h.keyFingerprints = append(h.keyFingerprints, int64(key.Fingerprint))
		h.rsaList = append(h.rsaList, rsaKeyHelper{
			rsa:            rsa,
			keyFingerprint: int64(key.Fingerprint),
		})
	}

	return h, nil
}

// Fingerprints returns the fingerprints resPQ advertises.
func (m *Server) Fingerprints() []int64 {
	return slices.Clone(m.keyFingerprints)
}

// Cryptor returns the RSA key with the fingerprint, or nil.
func (m *Server) Cryptor(keyFingerprint int64) *crypto.RSACryptor {
	for _, v := range m.rsaList {
		if v.keyFingerprint == keyFingerprint {
			return v.rsa
		}
	}

	return nil
}

// //////////////////////////////////////////////////////////////////////////////////////////////////
// Handshake messages (https://core.telegram.org/mtproto/auth_key) are
// untrusted input that arrives before any authentication. Each step checks the
// handshake state of its nonce (state.go) and the lengths of every field
// before using them, and the expensive steps (RSA, DH) run off the event
// loop (Exchange.Submit) on a copy of the state, never on state the event
// loop may change at the same time.

const (
	// unencrypted message: auth_key_id:long msg_id:long message_data_length:int message_data
	unencryptedHeaderSize = 20
	rsaEncryptedDataSize  = 256
	rsaPadDataSize        = 192 // data_with_padding of RSA_PAD
	newNonceSize          = 32
	// DHSecretSize is the length of the server's DH secret a.
	DHSecretSize = 256
)

var (
	errHandshakeMessage      = errors.New("handshake: malformed message")
	errHandshakeData         = errors.New("handshake: invalid encrypted data")
	errHandshakePQ           = errors.New("handshake: req_DH_params: p and q are not the factors of pq")
	errRSAPadHash            = errors.New("handshake: req_DH_params: RSA_PAD hash mismatch")
	errClientDHInnerDataHash = errors.New("handshake: set_client_DH_params: SHA1 of client_DH_inner_data mismatch")
	errDHValue               = errors.New("handshake: set_client_DH_params: g_b out of range")
	errRetryId               = errors.New("handshake: set_client_DH_params: wrong retry_id")
	errTempKeyExpiresIn      = errors.New("handshake: temporary key without a positive expires_in")
)

// ErrKeyIdTaken is what a KeySaver returns when another key has the
// auth_key_id of a new key: the client is answered dh_gen_retry and may try
// a new b.
var ErrKeyIdTaken = errors.New("handshake: auth_key_id is taken")

// AuthKey is a key a handshake created.
type AuthKey struct {
	AuthKeyId int64
	AuthKey   []byte
	// AuthKeyType is mtproto.AuthKeyTypePerm, AuthKeyTypeTemp or
	// AuthKeyTypeMediaTemp, after the p_q_inner_data the client sent.
	AuthKeyType int
	// ServerSalt is the first server salt of the key (ServerSalt).
	ServerSalt int64
}

// KeySaver stores the keys handshakes create.
type KeySaver interface {
	// Save stores key; expiresIn is the expires_in of a temporary key (0
	// for a permanent one). It returns ErrKeyIdTaken when another key has
	// key.AuthKeyId; any other error is a failure to store the key, which
	// the client is answered dh_gen_fail for.
	Save(ctx context.Context, key *AuthKey, expiresIn int32) error
}

// KeySaverFunc adapts a function to KeySaver.
type KeySaverFunc func(ctx context.Context, key *AuthKey, expiresIn int32) error

// Save calls f.
func (f KeySaverFunc) Save(ctx context.Context, key *AuthKey, expiresIn int32) error {
	return f(ctx, key, expiresIn)
}

// Exchange is how one transport keeps and answers handshakes: those of one
// connection, or of a transport whose requests arrive on different
// connections (HTTP).
type Exchange struct {
	// States keeps the handshakes in progress.
	States *Store
	// Keys stores the keys the handshakes create.
	Keys KeySaver
	// Now returns the unix time States are kept by (the gateway: the time
	// its event loop caches).
	Now func() int64
	// Admit, when set, is asked before the RSA and DH work of
	// req_DH_params, once its proof of work checked out; an error rejects
	// the message (a limit on key exchanges).
	Admit func() error
	// Submit runs f off the event loop (a worker pool); an error means f
	// will not run.
	Submit func(f func()) error
	// Reply sends the answer of a step that Submit ran; Fail reports that
	// step's failure. Both are called on Submit's goroutine.
	Reply func(answer mtproto.TLObject)
	Fail  func(err error)
}

// handshakeConstructors are the only objects a client may send unencrypted.
var handshakeConstructors = []mtproto.TLConstructor{
	mtproto.CRC32_req_pq,
	mtproto.CRC32_req_pq_multi,
	mtproto.CRC32_req_DH_params,
	mtproto.CRC32_set_client_DH_params,
	mtproto.CRC32_msgs_ack,
}

var pqInnerDataConstructors = []mtproto.TLConstructor{
	mtproto.CRC32_p_q_inner_data,
	mtproto.CRC32_p_q_inner_data_dc,
	mtproto.CRC32_p_q_inner_data_temp,
	mtproto.CRC32_p_q_inner_data_temp_dc,
}

var clientDHInnerDataConstructors = []mtproto.TLConstructor{
	mtproto.CRC32_client_DH_inner_data,
}

// decodeObjectOf decodes the start of b as one of the allowed objects and
// returns it with its length; nothing else is decoded at all.
func decodeObjectOf(b []byte, allowed []mtproto.TLConstructor) (mtproto.TLObject, int, error) {
	if len(b) < 4 {
		return nil, 0, errHandshakeMessage
	}
	if !slices.Contains(allowed, mtproto.TLConstructor(binary.LittleEndian.Uint32(b))) {
		return nil, 0, errHandshakeMessage
	}
	dBuf := mtproto.GetDecodeBuf(b)
	defer mtproto.PutDecodeBuf(dBuf)
	obj := dBuf.Object()
	if err := dBuf.GetError(); err != nil || obj == nil {
		return nil, 0, errHandshakeMessage
	}
	return obj, dBuf.GetOffset(), nil
}

// parseHandshakeMessage parses an unencrypted transport frame (auth_key_id
// 0). message_data_length must fit the frame; bytes after the message (the
// padding of the padded intermediate transport) are ignored.
func parseHandshakeMessage(frame []byte) (mtproto.TLObject, error) {
	if len(frame) < unencryptedHeaderSize+4 {
		return nil, errHandshakeMessage
	}
	n := binary.LittleEndian.Uint32(frame[16:unencryptedHeaderSize])
	if n < 4 || uint64(n) > uint64(len(frame)-unencryptedHeaderSize) {
		return nil, errHandshakeMessage
	}
	obj, _, err := decodeObjectOf(frame[unencryptedHeaderSize:unencryptedHeaderSize+int(n)], handshakeConstructors)
	return obj, err
}

// Payload serializes an answer as an unencrypted message:
// auth_key_id = 0, msg_id, message_data_length, message_data.
func Payload(obj mtproto.TLObject) []byte {
	x := mtproto.GetEncodeBuf()
	defer mtproto.PutEncodeBuf(x)
	x.Long(0)
	x.Long(mtproto.GenerateMessageId())
	offset := x.GetOffset()
	x.Int(0)
	// The answers are objects this package builds; encoding them does not
	// fail.
	_ = obj.Encode(x, 0)
	x.IntOffset(offset, int32(x.GetOffset()-offset-4))
	return append([]byte(nil), x.GetBuf()...)
}

// Handle processes an unencrypted message. req_pq is answered at once (the
// answer is returned); the DH steps run through x.Submit and answer through
// x.Reply or x.Fail later (pending). msgs_ack needs no answer. An error
// means the message is malformed or does not match the handshake state.
func (m *Server) Handle(x *Exchange, frame []byte) (answer mtproto.TLObject, pending bool, err error) {
	obj, err := parseHandshakeMessage(frame)
	if err != nil {
		return nil, false, err
	}

	now := x.Now()
	switch req := obj.(type) {
	case *mtproto.TLReqPq:
		answer, err = m.startHandshake(x.States, req.GetNonce(), now)
		return answer, false, err
	case *mtproto.TLReqPqMulti:
		answer, err = m.startHandshake(x.States, req.GetNonce(), now)
		return answer, false, err
	case *mtproto.TLReq_DHParams:
		err = m.onReqDHParams(x, req, now)
		return nil, err == nil, err
	case *mtproto.TLSetClient_DHParams:
		err = m.onSetClientDHParams(x, req, now)
		return nil, err == nil, err
	case *mtproto.TLMsgsAck:
		return nil, false, nil
	default:
		return nil, false, errHandshakeMessage
	}
}

// startHandshake answers req_pq / req_pq_multi with a new pq (pq.go):
// resPQ#05162463 nonce:int128 server_nonce:int128 pq:string server_public_key_fingerprints:Vector<long> = ResPQ;
func (m *Server) startHandshake(states *Store, nonce []byte, now int64) (mtproto.TLObject, error) {
	if len(nonce) != 16 {
		return nil, errHandshakeMessage
	}

	pq, p, q := NewPQ()
	resPQ := mtproto.MakeTLResPQ(&mtproto.ResPQ{
		Nonce:                       nonce,
		ServerNonce:                 crypto.GenerateNonce(16),
		Pq:                          PutPQ(pq),
		ServerPublicKeyFingerprints: m.keyFingerprints,
	}).To_ResPQ()

	states.Put(HandshakeStateCtx{
		State:       StatePQRes,
		Nonce:       resPQ.GetNonce(),
		ServerNonce: resPQ.GetServerNonce(),
		Pq:          pq,
		P:           p,
		Q:           q,
	}, now)

	return resPQ, nil
}

// onReqDHParams starts req_DH_params; it is answered by server_DH_params_ok
// computed through x.Submit.
func (m *Server) onReqDHParams(x *Exchange, req *mtproto.TLReq_DHParams, now int64) error {
	st, err := x.States.Begin(req.GetNonce(), req.GetServerNonce(), StatePQRes, StateDHParams, now)
	if err != nil {
		return err
	}

	nonce := req.GetNonce()
	// The proof of work, before anything costly: p and q must be the factors
	// of the pq this handshake got.
	if !st.hasFactors(req.GetP(), req.GetQ()) {
		x.States.Drop(nonce)
		return errHandshakePQ
	}
	// Then the budget of the transport, before the RSA and DH work.
	if x.Admit != nil {
		if err = x.Admit(); err != nil {
			x.States.Drop(nonce)
			return err
		}
	}
	err = x.Submit(func() {
		res, err := m.ProcessReqDHParams(&st, req, time.Now().Unix())
		if err != nil {
			x.States.Drop(nonce)
			x.Fail(err)
			return
		}

		if !x.States.Finish(nonce, StateDHParams, x.Now(), func(st *HandshakeStateCtx) bool {
			st.NewNonce = res.NewNonce
			st.A = res.A
			st.HandshakeType = res.HandshakeType
			st.ExpiresIn = res.ExpiresIn
			st.State = StateDHParamsRes
			return true
		}) {
			// Restarted by a new req_pq with this nonce, or evicted, meanwhile.
			// The request still needs an answer: over HTTP the connection
			// waits for it and parses nothing else until then.
			x.Fail(errHandshakeState)
			return
		}
		x.Reply(res.Answer)
	})
	if err != nil {
		x.States.Drop(nonce)
		return err
	}
	return nil
}

// onSetClientDHParams starts set_client_DH_params, computed through
// x.Submit. It is answered (https://core.telegram.org/mtproto/auth_key,
// step 9) by dh_gen_ok when the key is saved, dh_gen_retry when its
// auth_key_id is taken (auth_key_hash collides) and the client may try a new
// b, or dh_gen_fail when the key could not be stored; ok and fail end the
// handshake.
func (m *Server) onSetClientDHParams(x *Exchange, req *mtproto.TLSetClient_DHParams, now int64) error {
	st, err := x.States.Begin(req.GetNonce(), req.GetServerNonce(), StateDHParamsRes, StateDHGen, now)
	if err != nil {
		return err
	}

	nonce := req.GetNonce()
	err = x.Submit(func() {
		key, err := ProcessSetClientDHParams(&st, req)
		if err != nil {
			x.States.Drop(nonce)
			x.Fail(err)
			return
		}

		var (
			answer *mtproto.SetClient_DHParamsAnswer
			retry  bool
		)
		// The session service bounds the call with its own RPC timeout.
		saveErr := x.Keys.Save(context.Background(), &AuthKey{
			AuthKeyId:   key.AuthKeyId,
			AuthKey:     key.AuthKey,
			AuthKeyType: st.HandshakeType,
			ServerSalt:  ServerSalt(&st),
		}, st.ExpiresIn)
		switch {
		case saveErr == nil:
			answer = mtproto.MakeTLDhGenOk(&mtproto.SetClient_DHParamsAnswer{
				Nonce:         st.Nonce,
				ServerNonce:   st.ServerNonce,
				NewNonceHash1: calcNewNonceHash(st.NewNonce, key.AuthKey, 0x01),
			}).To_SetClient_DHParamsAnswer()
		case errors.Is(saveErr, ErrKeyIdTaken):
			retry = true
			answer = mtproto.MakeTLDhGenRetry(&mtproto.SetClient_DHParamsAnswer{
				Nonce:         st.Nonce,
				ServerNonce:   st.ServerNonce,
				NewNonceHash2: calcNewNonceHash(st.NewNonce, key.AuthKey, 0x02),
			}).To_SetClient_DHParamsAnswer()
		default:
			answer = mtproto.MakeTLDhGenFail(&mtproto.SetClient_DHParamsAnswer{
				Nonce:         st.Nonce,
				ServerNonce:   st.ServerNonce,
				NewNonceHash3: calcNewNonceHash(st.NewNonce, key.AuthKey, 0x03),
			}).To_SetClient_DHParamsAnswer()
		}

		// After dh_gen_retry the client may send set_client_DH_params again,
		// with a new g_b and retry_id = auth_key_aux_hash of this key.
		pending := x.States.Finish(nonce, StateDHGen, x.Now(), func(st *HandshakeStateCtx) bool {
			if !retry {
				return false
			}
			st.State = StateDHParamsRes
			st.RetryId = key.AuxHash
			return true
		})
		if retry && !pending {
			// Restarted by a new req_pq with this nonce, or evicted, meanwhile:
			// answer anyway (see onReqDHParams).
			x.Fail(errHandshakeState)
			return
		}
		x.Reply(answer)
	})
	if err != nil {
		x.States.Drop(nonce)
		return err
	}
	return nil
}

// ReqDHParamsResult is the outcome of req_DH_params.
type ReqDHParamsResult struct {
	Answer        *mtproto.Server_DH_Params
	NewNonce      []byte
	A             []byte
	HandshakeType int
	ExpiresIn     int32
}

// ProcessReqDHParams checks req_DH_params against the handshake state st and
// computes server_DH_params_ok. The caller has checked p and q (Handle does).
//
// req_DH_params#d712e4be nonce:int128 server_nonce:int128 p:string q:string public_key_fingerprint:long encrypted_data:string = Server_DH_Params;
func (m *Server) ProcessReqDHParams(st *HandshakeStateCtx, req *mtproto.TLReq_DHParams, now int64) (*ReqDHParamsResult, error) {
	if !bytes.Equal(req.GetNonce(), st.Nonce) || !bytes.Equal(req.GetServerNonce(), st.ServerNonce) {
		return nil, errHandshakeState
	}
	rsa := m.Cryptor(req.GetPublicKeyFingerprint())
	if rsa == nil {
		return nil, fmt.Errorf("handshake: req_DH_params: unknown public key fingerprint")
	}

	/*
		RSA_PAD(data, server_public_key) (https://core.telegram.org/mtproto/auth_key):

		- data_with_padding := data + random_padding_bytes; — 192 bytes.
		- data_pad_reversed := BYTE_REVERSE(data_with_padding);
		- data_with_hash := data_pad_reversed + SHA256(temp_key + data_with_padding); — 224 bytes.
		- aes_encrypted := AES256_IGE(data_with_hash, temp_key, 0);
		- temp_key_xor := temp_key XOR SHA256(aes_encrypted); — 32 bytes.
		- key_aes_encrypted := temp_key_xor + aes_encrypted; — 256 bytes.
		- encrypted_data := RSA(key_aes_encrypted, server_pubkey); — exactly 256 bytes.

		The server undoes these steps and checks the hash, which an altered or
		forged encrypted_data fails.
	*/
	encryptedData := req.GetEncryptedData()
	if len(encryptedData) != rsaEncryptedDataSize {
		return nil, errHandshakeData
	}
	decrypted := rsa.Decrypt([]byte(encryptedData))
	if len(decrypted) > rsaEncryptedDataSize {
		return nil, errHandshakeData
	}
	// RSA yields a number: put back the leading zero bytes it lost.
	keyAesEncrypted := make([]byte, rsaEncryptedDataSize)
	copy(keyAesEncrypted[rsaEncryptedDataSize-len(decrypted):], decrypted)

	tempKey := keyAesEncrypted[:32]
	hash := crypto.Sha256Digest(keyAesEncrypted[32:])
	for i := 0; i < 32; i++ {
		tempKey[i] ^= hash[i]
	}
	dataWithHash, err := crypto.NewAES256IGECryptor(tempKey, zeroIV).Decrypt(keyAesEncrypted[32:])
	if err != nil || len(dataWithHash) != rsaPadDataSize+sha256.Size {
		return nil, errHandshakeData
	}
	dataWithPadding := dataWithHash[:rsaPadDataSize]
	slices.Reverse(dataWithPadding)
	h256 := sha256.New()
	h256.Write(tempKey)
	h256.Write(dataWithPadding)
	if subtle.ConstantTimeCompare(h256.Sum(nil), dataWithHash[rsaPadDataSize:]) != 1 {
		return nil, errRSAPadHash
	}

	o, _, err := decodeObjectOf(dataWithPadding, pqInnerDataConstructors)
	if err != nil {
		return nil, errHandshakeData
	}

	var (
		pqInnerData   *mtproto.P_QInnerData
		handshakeType int
		expiresIn     int32
	)
	switch innerData := o.(type) {
	case *mtproto.TLPQInnerData:
		handshakeType = mtproto.AuthKeyTypePerm
		pqInnerData = innerData.To_P_QInnerData()
	case *mtproto.TLPQInnerDataDc:
		handshakeType = mtproto.AuthKeyTypePerm
		pqInnerData = innerData.To_P_QInnerData()
	case *mtproto.TLPQInnerDataTemp:
		handshakeType = mtproto.AuthKeyTypeTemp
		expiresIn = innerData.GetExpiresIn()
		pqInnerData = innerData.To_P_QInnerData()
	case *mtproto.TLPQInnerDataTempDc:
		if innerData.GetDc() < 0 {
			handshakeType = mtproto.AuthKeyTypeMediaTemp
		} else {
			handshakeType = mtproto.AuthKeyTypeTemp
		}
		expiresIn = innerData.GetExpiresIn()
		pqInnerData = innerData.To_P_QInnerData()
	default:
		return nil, errHandshakeData
	}
	if handshakeType != mtproto.AuthKeyTypePerm && expiresIn <= 0 {
		// A temporary key must expire (https://core.telegram.org/api/pfs);
		// stored with expires_in 0 it would never stop working.
		return nil, errTempKeyExpiresIn
	}

	// p_q_inner_data repeats pq, p, q, nonce and server_nonce.
	if !st.hasPQ(pqInnerData.GetPq()) ||
		!st.hasFactors(pqInnerData.GetP(), pqInnerData.GetQ()) ||
		!bytes.Equal(pqInnerData.GetNonce(), st.Nonce) ||
		!bytes.Equal(pqInnerData.GetServerNonce(), st.ServerNonce) {
		return nil, fmt.Errorf("handshake: p_q_inner_data does not match req_DH_params")
	}
	newNonce := pqInnerData.GetNewNonce()
	if len(newNonce) != newNonceSize {
		return nil, errHandshakeData
	}

	// The server checks its own g_a too; one out of range comes up with
	// probability about 2^-63.
	var a []byte
	gA := new(big.Int)
	for {
		a = crypto.GenerateNonce(DHSecretSize)
		if gA.Exp(gBigIntDH2048G, new(big.Int).SetBytes(a), gBigIntDH2048P); isGoodDHValue(gA) {
			break
		}
	}

	// server_DH_inner_data#b5890dba nonce:int128 server_nonce:int128 g:int dh_prime:string g_a:string server_time:int = Server_DH_inner_data;
	serverDHInnerData := &mtproto.TLServer_DHInnerData{Data2: &mtproto.Server_DHInnerData{
		Nonce:       st.Nonce,
		ServerNonce: st.ServerNonce,
		G:           int32(m.dh2048g[0]),
		GA:          string(gA.Bytes()),
		DhPrime:     string(m.dh2048p),
		ServerTime:  int32(now),
	}}
	serverDHInnerDataBuf := func() []byte {
		x := mtproto.GetEncodeBuf()
		defer mtproto.PutEncodeBuf(x)
		_ = serverDHInnerData.Encode(x, 0)
		return append([]byte(nil), x.GetBuf()...)
	}()

	// answer_with_hash := SHA1(answer) + answer + (0-15 random bytes), encrypted
	// with tmp_aes_key and tmp_aes_iv derived from new_nonce and server_nonce.
	tmpAesKey, tmpAesIV := tmpAesKeyAndIV(newNonce, st.ServerNonce)
	answerLen := 20 + len(serverDHInnerDataBuf)
	if answerLen%16 != 0 {
		answerLen += 16 - answerLen%16
	}
	answerWithHash := make([]byte, answerLen)
	sha1Answer := sha1.Sum(serverDHInnerDataBuf) //nolint:gosec // MTProto: answer_with_hash = SHA1(answer) + answer
	copy(answerWithHash, sha1Answer[:])
	copy(answerWithHash[20:], serverDHInnerDataBuf)
	copy(answerWithHash[20+len(serverDHInnerDataBuf):], crypto.GenerateNonce(answerLen-20-len(serverDHInnerDataBuf)))
	encryptedAnswer, err := crypto.NewAES256IGECryptor(tmpAesKey, tmpAesIV).Encrypt(answerWithHash)
	if err != nil {
		return nil, err
	}

	return &ReqDHParamsResult{
		Answer: mtproto.MakeTLServer_DHParamsOk(&mtproto.Server_DH_Params{
			Nonce:           st.Nonce,
			ServerNonce:     st.ServerNonce,
			EncryptedAnswer: hack.String(encryptedAnswer),
		}).To_Server_DH_Params(),
		NewNonce:      newNonce,
		A:             a,
		HandshakeType: handshakeType,
		ExpiresIn:     expiresIn,
	}, nil
}

// tmpAesKeyAndIV derives the key and IV that encrypt server_DH_inner_data and
// client_DH_inner_data:
//
//	tmp_aes_key := SHA1(new_nonce + server_nonce) + substr(SHA1(server_nonce + new_nonce), 0, 12);
//	tmp_aes_iv  := substr(SHA1(server_nonce + new_nonce), 12, 8) + SHA1(new_nonce + new_nonce) + substr(new_nonce, 0, 4);
func tmpAesKeyAndIV(newNonce, serverNonce []byte) (key, iv []byte) {
	cat := func(a, b []byte) [20]byte {
		return sha1.Sum(append(append(make([]byte, 0, len(a)+len(b)), a...), b...)) //nolint:gosec // MTProto: tmp_aes_key/iv derivation
	}
	sha1A := cat(newNonce, serverNonce)
	sha1B := cat(serverNonce, newNonce)
	sha1C := cat(newNonce, newNonce)

	keyAndIV := make([]byte, 64)
	copy(keyAndIV, sha1A[:])
	copy(keyAndIV[20:], sha1B[:])
	copy(keyAndIV[40:], sha1C[:])
	copy(keyAndIV[60:], newNonce[:4])
	return keyAndIV[:32], keyAndIV[32:64]
}

// DHGenKey is the key set_client_DH_params computes.
type DHGenKey struct {
	AuthKeyId int64
	AuthKey   []byte
	// AuxHash is auth_key_aux_hash: the 64 higher-order bits of
	// SHA1(auth_key), as retry_id carries it.
	AuxHash int64
}

// ProcessSetClientDHParams checks set_client_DH_params against the handshake
// state st (which req_DH_params completed) and computes the auth key.
//
// set_client_DH_params#f5045f1f nonce:int128 server_nonce:int128 encrypted_data:string = Set_client_DH_params_answer;
func ProcessSetClientDHParams(st *HandshakeStateCtx, req *mtproto.TLSetClient_DHParams) (*DHGenKey, error) {
	if !bytes.Equal(req.GetNonce(), st.Nonce) || !bytes.Equal(req.GetServerNonce(), st.ServerNonce) {
		return nil, errHandshakeState
	}
	// The state must be the one req_DH_params left.
	if len(st.NewNonce) != newNonceSize || len(st.A) != DHSecretSize {
		return nil, errHandshakeState
	}

	// encrypted_data := AES256_ige_encrypt(SHA1(client_DH_inner_data) + client_DH_inner_data + (0-15 random bytes))
	encryptedData := []byte(req.GetEncryptedData())
	if len(encryptedData) < 32 || len(encryptedData)%16 != 0 {
		return nil, errHandshakeData
	}
	tmpAesKey, tmpAesIV := tmpAesKeyAndIV(st.NewNonce, st.ServerNonce)
	decrypted, err := crypto.NewAES256IGECryptor(tmpAesKey, tmpAesIV).Decrypt(encryptedData)
	if err != nil || len(decrypted) <= sha1.Size {
		return nil, errHandshakeData
	}

	o, n, err := decodeObjectOf(decrypted[sha1.Size:], clientDHInnerDataConstructors)
	if err != nil {
		return nil, errHandshakeData
	}
	// The hash covers exactly the object; at most 15 bytes of padding follow.
	innerData := decrypted[sha1.Size : sha1.Size+n]
	if len(decrypted)-sha1.Size-n > 15 {
		return nil, errHandshakeData
	}
	if sum := sha1.Sum(innerData); subtle.ConstantTimeCompare(sum[:], decrypted[:sha1.Size]) != 1 { //nolint:gosec // MTProto: SHA1(client_DH_inner_data)
		return nil, errClientDHInnerDataHash
	}
	inner, ok := o.(*mtproto.TLClient_DHInnerData)
	if !ok {
		return nil, errHandshakeData
	}
	// client_DH_inner_data#6643b654 nonce:int128 server_nonce:int128 retry_id:long g_b:string = Client_DH_Inner_Data;
	if !bytes.Equal(inner.GetNonce(), st.Nonce) || !bytes.Equal(inner.GetServerNonce(), st.ServerNonce) {
		return nil, fmt.Errorf("handshake: client_DH_inner_data does not match set_client_DH_params")
	}
	if inner.GetRetryId() != st.RetryId {
		return nil, errRetryId
	}
	gB := []byte(inner.GetGB())
	if len(gB) == 0 || len(gB) > DHSecretSize {
		return nil, errHandshakeData
	}
	gBNum := new(big.Int).SetBytes(gB)
	if !isGoodDHValue(gBNum) {
		return nil, errDHValue
	}

	// auth_key := g_b^a mod dh_prime, 256 bytes big-endian.
	authKeyNum := new(big.Int).Exp(gBNum, new(big.Int).SetBytes(st.A), gBigIntDH2048P)
	authKey := make([]byte, 256)
	authKeyNum.FillBytes(authKey)

	// auth_key_id: the 64 lower-order bits of SHA1(auth_key);
	// auth_key_aux_hash: the 64 higher-order bits.
	sha1Key := sha1.Sum(authKey) //nolint:gosec // MTProto: auth_key_id = low 64 bits of SHA1(auth_key)
	return &DHGenKey{
		AuthKeyId: int64(binary.LittleEndian.Uint64(sha1Key[12:20])),
		AuthKey:   authKey,
		AuxHash:   int64(binary.LittleEndian.Uint64(sha1Key[0:8])),
	}, nil
}

// ServerSalt returns the first server salt of the key a handshake creates
// (https://core.telegram.org/mtproto/auth_key, step 9):
// substr(new_nonce, 0, 8) XOR substr(server_nonce, 0, 8), little-endian.
func ServerSalt(st *HandshakeStateCtx) int64 {
	var salt int64
	for a := 7; a >= 0; a-- {
		salt <<= 8
		salt |= int64(st.NewNonce[a] ^ st.ServerNonce[a])
	}
	return salt
}

func calcNewNonceHash(newNonce, authKey []byte, b byte) []byte {
	// newNonce is 32 bytes, total buffer: 32 + 1 + 20 + 20 = 73
	var buf [73]byte
	copy(buf[:32], newNonce)
	buf[32] = b
	sha1D := sha1.Sum(authKey) //nolint:gosec // MTProto: auth_key_aux_hash is taken from SHA1(auth_key)
	copy(buf[33:53], sha1D[:])
	sha1E := sha1.Sum(buf[:41]) //nolint:gosec // MTProto new_nonce_hash: SHA1 of new_nonce, the byte and auth_key_aux_hash, 32 + 1 + 8 = 41 bytes
	copy(buf[53:], sha1E[:])
	result := make([]byte, 16)
	copy(result, buf[73-16:])
	return result
}

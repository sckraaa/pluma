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

package gnet

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"time"

	"github.com/teamgram/marmota/pkg/hack"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/config"
	sessionclient "github.com/teamgram/teamgram-server/app/interface/session/client"
	"github.com/teamgram/teamgram-server/app/interface/session/session"
	"github.com/teamgram/teamgram-server/pkg/serverkey"

	"github.com/panjf2000/gnet/v2"
	"github.com/zeromicro/go-zero/core/logx"
)

var (
	// TODO(@benqi): 直接指定了dh2048_p和dh2048_g!!!
	// andriod client 指定的good prime
	//
	// static const char *goodPrime = "
	//
	// c71caeb9c6b1c9048e6c522f
	// 70f13f73980d40238e3e21c1
	// 4934d037563d930f48198a0a
	// a7c14058229493d22530f4db
	// fa336f6e0ac925139543aed4
	// 4cce7c3720fd51f69458705a
	// c68cd4fe6b6b13abdc974651
	// 2969328454f18faf8c595f64
	// 2477fe96bb2a941d5bcd1d4a
	// c8cc49880708fa9b378e3c4f
	// 3a9060bee67cf9a4a4a69581
	// 1051907e162753b56b0f6b41
	// 0dba74d8a84b2a14b3144e0e
	// f1284754fd17ed950d5965b4
	// b9dd46582db1178d169c6bc4
	// 65b0d6ff9ca3928fef5b9ae4
	// e418fc15e83ebea0f87fa9ff
	// 5eed70050ded2849f47bf959
	// d956850ce929851f0d8115f6
	// 35b105ee2e4e15d04b2454bf
	// 6f4fadf034b10403119cd8e3
	// b92fcc5b";
	//
	dh2048P = []byte{
		0xc7, 0x1c, 0xae, 0xb9, 0xc6, 0xb1, 0xc9, 0x04, 0x8e, 0x6c, 0x52, 0x2f,
		0x70, 0xf1, 0x3f, 0x73, 0x98, 0x0d, 0x40, 0x23, 0x8e, 0x3e, 0x21, 0xc1,
		0x49, 0x34, 0xd0, 0x37, 0x56, 0x3d, 0x93, 0x0f, 0x48, 0x19, 0x8a, 0x0a,
		0xa7, 0xc1, 0x40, 0x58, 0x22, 0x94, 0x93, 0xd2, 0x25, 0x30, 0xf4, 0xdb,
		0xfa, 0x33, 0x6f, 0x6e, 0x0a, 0xc9, 0x25, 0x13, 0x95, 0x43, 0xae, 0xd4,
		0x4c, 0xce, 0x7c, 0x37, 0x20, 0xfd, 0x51, 0xf6, 0x94, 0x58, 0x70, 0x5a,
		0xc6, 0x8c, 0xd4, 0xfe, 0x6b, 0x6b, 0x13, 0xab, 0xdc, 0x97, 0x46, 0x51,
		0x29, 0x69, 0x32, 0x84, 0x54, 0xf1, 0x8f, 0xaf, 0x8c, 0x59, 0x5f, 0x64,
		0x24, 0x77, 0xfe, 0x96, 0xbb, 0x2a, 0x94, 0x1d, 0x5b, 0xcd, 0x1d, 0x4a,
		0xc8, 0xcc, 0x49, 0x88, 0x07, 0x08, 0xfa, 0x9b, 0x37, 0x8e, 0x3c, 0x4f,
		0x3a, 0x90, 0x60, 0xbe, 0xe6, 0x7c, 0xf9, 0xa4, 0xa4, 0xa6, 0x95, 0x81,
		0x10, 0x51, 0x90, 0x7e, 0x16, 0x27, 0x53, 0xb5, 0x6b, 0x0f, 0x6b, 0x41,
		0x0d, 0xba, 0x74, 0xd8, 0xa8, 0x4b, 0x2a, 0x14, 0xb3, 0x14, 0x4e, 0x0e,
		0xf1, 0x28, 0x47, 0x54, 0xfd, 0x17, 0xed, 0x95, 0x0d, 0x59, 0x65, 0xb4,
		0xb9, 0xdd, 0x46, 0x58, 0x2d, 0xb1, 0x17, 0x8d, 0x16, 0x9c, 0x6b, 0xc4,
		0x65, 0xb0, 0xd6, 0xff, 0x9c, 0xa3, 0x92, 0x8f, 0xef, 0x5b, 0x9a, 0xe4,
		0xe4, 0x18, 0xfc, 0x15, 0xe8, 0x3e, 0xbe, 0xa0, 0xf8, 0x7f, 0xa9, 0xff,
		0x5e, 0xed, 0x70, 0x05, 0x0d, 0xed, 0x28, 0x49, 0xf4, 0x7b, 0xf9, 0x59,
		0xd9, 0x56, 0x85, 0x0c, 0xe9, 0x29, 0x85, 0x1f, 0x0d, 0x81, 0x15, 0xf6,
		0x35, 0xb1, 0x05, 0xee, 0x2e, 0x4e, 0x15, 0xd0, 0x4b, 0x24, 0x54, 0xbf,
		0x6f, 0x4f, 0xad, 0xf0, 0x34, 0xb1, 0x04, 0x03, 0x11, 0x9c, 0xd8, 0xe3,
		0xb9, 0x2f, 0xcc, 0x5b,
	}

	dh2048G = []byte{0x03}

	zeroIV = []byte{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
)

var (
	gBigIntDH2048P *big.Int
	gBigIntDH2048G *big.Int
)

func init() {
	gBigIntDH2048P = new(big.Int).SetBytes(dh2048P)
	gBigIntDH2048G = new(big.Int).SetBytes(dh2048G)
}

type rsaKeyHelper struct {
	rsa            *crypto.RSACryptor
	keyFingerprint int64
}

type handshake struct {
	keyFingerprints []int64
	rsaList         []rsaKeyHelper
	// keyFingerprint  uint64
	dh2048p []byte
	dh2048g []byte
}

func (m *handshake) getKey(keyFingerprint int64) *crypto.RSACryptor {
	for _, v := range m.rsaList {
		if v.keyFingerprint == keyFingerprint {
			return v.rsa
		}
	}

	return nil
}

// LoadRSAKeys loads the server RSA keys listed in the config and verifies
// each of them (file permissions, key size, fingerprint), so that gnetway
// refuses to start rather than serve handshakes with a missing, exposed or
// mismatching key. Several keys may be listed to rotate without downtime.
func LoadRSAKeys(cList []config.RSAKey) ([]*serverkey.Key, error) {
	if len(cList) == 0 {
		return nil, fmt.Errorf("no server RSA key configured (RSAKey is empty)")
	}

	keys := make([]*serverkey.Key, 0, len(cList))
	seen := make(map[uint64]string, len(cList))
	for _, c := range cList {
		key, err := serverkey.Load(c.KeyFile, c.KeyFingerprint)
		if err != nil {
			return nil, err
		}
		if prev, ok := seen[key.Fingerprint]; ok {
			return nil, fmt.Errorf("server RSA key %s duplicates %s (fingerprint %d)", c.KeyFile, prev, key.Fingerprint)
		}
		seen[key.Fingerprint] = c.KeyFile
		keys = append(keys, key)
	}

	return keys, nil
}

func newHandshake(keys []*serverkey.Key) (*handshake, error) {
	h := &handshake{
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

// //////////////////////////////////////////////////////////////////////////////////////////////////
// Handshake messages (https://core.telegram.org/mtproto/auth_key) are
// untrusted input that arrives before any authentication. Each step checks the
// handshake state of its nonce (handshake_state.go) and the lengths of every
// field before using them, and the expensive steps (RSA, DH) run in the
// worker pool on a copy of the state, never on state the event loop may
// change at the same time.

const (
	// unencrypted message: auth_key_id:long msg_id:long message_data_length:int message_data
	unencryptedHeaderSize = 20
	rsaEncryptedDataSize  = 256
	rsaPadDataSize        = 192 // data_with_padding of RSA_PAD
	newNonceSize          = 32
	dhSecretSize          = 256
)

var (
	errHandshakeMessage      = errors.New("handshake: malformed message")
	errHandshakeData         = errors.New("handshake: invalid encrypted data")
	errHandshakePQ           = errors.New("handshake: req_DH_params: p and q are not the factors of pq")
	errRSAPadHash            = errors.New("handshake: req_DH_params: RSA_PAD hash mismatch")
	errClientDHInnerDataHash = errors.New("handshake: set_client_DH_params: SHA1 of client_DH_inner_data mismatch")
	errTempKeyExpiresIn      = errors.New("handshake: temporary key without a positive expires_in")
)

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

// handshakePayload serializes an answer as an unencrypted message.
func handshakePayload(obj mtproto.TLObject) []byte {
	x := mtproto.GetEncodeBuf()
	defer mtproto.PutEncodeBuf(x)
	_ = serializeToBuffer(x, mtproto.GenerateMessageId(), obj)
	return append([]byte(nil), x.GetBuf()...)
}

// handshakeIO is how a transport keeps handshake state and answers. reply
// and fail run on the event loop of connection connId; fail is called when a
// step computed in the worker pool fails.
type handshakeIO struct {
	connId int64
	states *handshakeStore
	reply  func(c gnet.Conn, answer mtproto.TLObject)
	fail   func(c gnet.Conn, err error)
}

// onHandshakeMessage processes an unencrypted message. req_pq is answered at
// once (the answer is returned); the DH steps run in the worker pool and
// answer through io later (pending). msgs_ack needs no answer. An error means
// the message is malformed or does not match the handshake state.
func (s *Server) onHandshakeMessage(io *handshakeIO, frame []byte) (answer mtproto.TLObject, pending bool, err error) {
	obj, err := parseHandshakeMessage(frame)
	if err != nil {
		return nil, false, err
	}

	now := s.CachedNow()
	switch req := obj.(type) {
	case *mtproto.TLReqPq:
		answer, err = s.startHandshake(io.states, req.GetNonce(), now)
		return answer, false, err
	case *mtproto.TLReqPqMulti:
		answer, err = s.startHandshake(io.states, req.GetNonce(), now)
		return answer, false, err
	case *mtproto.TLReq_DHParams:
		err = s.onReqDHParams(io, req, now)
		return nil, err == nil, err
	case *mtproto.TLSetClient_DHParams:
		err = s.onSetClientDHParams(io, req, now)
		return nil, err == nil, err
	case *mtproto.TLMsgsAck:
		return nil, false, nil
	default:
		return nil, false, errHandshakeMessage
	}
}

// startHandshake answers req_pq / req_pq_multi with a new pq (pq.go):
// resPQ#05162463 nonce:int128 server_nonce:int128 pq:string server_public_key_fingerprints:Vector<long> = ResPQ;
func (s *Server) startHandshake(states *handshakeStore, nonce []byte, now int64) (mtproto.TLObject, error) {
	if len(nonce) != 16 {
		return nil, errHandshakeMessage
	}

	pq, p, q := newPQ()
	resPQ := mtproto.MakeTLResPQ(&mtproto.ResPQ{
		Nonce:                       nonce,
		ServerNonce:                 crypto.GenerateNonce(16),
		Pq:                          putPQ(pq),
		ServerPublicKeyFingerprints: s.handshake.keyFingerprints,
	}).To_ResPQ()

	states.put(HandshakeStateCtx{
		State:       STATE_pq_res,
		Nonce:       resPQ.GetNonce(),
		ServerNonce: resPQ.GetServerNonce(),
		Pq:          pq,
		P:           p,
		Q:           q,
	}, now)

	return resPQ, nil
}

// onReqDHParams starts req_DH_params; it is answered by server_DH_params_ok
// computed in the worker pool.
func (s *Server) onReqDHParams(io *handshakeIO, req *mtproto.TLReq_DHParams, now int64) error {
	st, err := io.states.begin(req.GetNonce(), req.GetServerNonce(), STATE_pq_res, STATE_DH_params, now)
	if err != nil {
		return err
	}

	nonce := req.GetNonce()
	// The proof of work, before anything costly: p and q must be the factors
	// of the pq this handshake got.
	if !st.hasFactors(req.GetP(), req.GetQ()) {
		io.states.drop(nonce)
		return errHandshakePQ
	}
	err = s.submit(io.connId, func() {
		res, err := s.handshake.processReqDHParams(&st, req, time.Now().Unix())
		if err != nil {
			io.states.drop(nonce)
			s.trigger(io.connId, func(c gnet.Conn) { io.fail(c, err) })
			return
		}

		if !io.states.finish(nonce, STATE_DH_params, s.CachedNow(), func(st *HandshakeStateCtx) bool {
			st.NewNonce = res.newNonce
			st.A = res.a
			st.HandshakeType = res.handshakeType
			st.ExpiresIn = res.expiresIn
			st.State = STATE_DH_params_res
			return true
		}) {
			// Restarted by a new req_pq with this nonce, or evicted, meanwhile.
			// The request still needs an answer: over HTTP the connection
			// waits for it and parses nothing else until then.
			s.trigger(io.connId, func(c gnet.Conn) { io.fail(c, errHandshakeState) })
			return
		}
		s.trigger(io.connId, func(c gnet.Conn) { io.reply(c, res.answer) })
	})
	if err != nil {
		io.states.drop(nonce)
		return err
	}
	return nil
}

// onSetClientDHParams starts set_client_DH_params; it is answered by
// dh_gen_ok (the key is saved) or dh_gen_retry, computed in the worker pool.
func (s *Server) onSetClientDHParams(io *handshakeIO, req *mtproto.TLSetClient_DHParams, now int64) error {
	st, err := io.states.begin(req.GetNonce(), req.GetServerNonce(), STATE_DH_params_res, STATE_dh_gen, now)
	if err != nil {
		return err
	}

	nonce := req.GetNonce()
	err = s.submit(io.connId, func() {
		key, err := processSetClientDHParams(&st, req)
		if err != nil {
			io.states.drop(nonce)
			s.trigger(io.connId, func(c gnet.Conn) { io.fail(c, err) })
			return
		}

		var answer *mtproto.SetClient_DHParamsAnswer
		saved := s.saveAuthKey(&st, mtproto.NewAuthKeyInfo(key.authKeyId, key.authKey, st.HandshakeType))
		if saved {
			answer = mtproto.MakeTLDhGenOk(&mtproto.SetClient_DHParamsAnswer{
				Nonce:         st.Nonce,
				ServerNonce:   st.ServerNonce,
				NewNonceHash1: calcNewNonceHash(st.NewNonce, key.authKey, 0x01),
			}).To_SetClient_DHParamsAnswer()
		} else {
			// The key could not be stored: the client may retry with a new g_b.
			answer = mtproto.MakeTLDhGenRetry(&mtproto.SetClient_DHParamsAnswer{
				Nonce:         st.Nonce,
				ServerNonce:   st.ServerNonce,
				NewNonceHash2: calcNewNonceHash(st.NewNonce, key.authKey, 0x02),
			}).To_SetClient_DHParamsAnswer()
		}

		// dh_gen_ok ends the handshake; after dh_gen_retry the client may send
		// set_client_DH_params again.
		pending := io.states.finish(nonce, STATE_dh_gen, s.CachedNow(), func(st *HandshakeStateCtx) bool {
			if saved {
				return false
			}
			st.State = STATE_DH_params_res
			return true
		})
		if !pending && !saved {
			// Restarted by a new req_pq with this nonce, or evicted, meanwhile:
			// answer anyway (see onReqDHParams).
			s.trigger(io.connId, func(c gnet.Conn) { io.fail(c, errHandshakeState) })
			return
		}
		s.trigger(io.connId, func(c gnet.Conn) { io.reply(c, answer) })
	})
	if err != nil {
		io.states.drop(nonce)
		return err
	}
	return nil
}

type reqDHParamsResult struct {
	answer        *mtproto.Server_DH_Params
	newNonce      []byte
	a             []byte
	handshakeType int
	expiresIn     int32
}

// processReqDHParams checks req_DH_params against the handshake state st and
// computes server_DH_params_ok. onReqDHParams has checked p and q.
//
// req_DH_params#d712e4be nonce:int128 server_nonce:int128 p:string q:string public_key_fingerprint:long encrypted_data:string = Server_DH_Params;
func (h *handshake) processReqDHParams(st *HandshakeStateCtx, req *mtproto.TLReq_DHParams, now int64) (*reqDHParamsResult, error) {
	if !bytes.Equal(req.GetNonce(), st.Nonce) || !bytes.Equal(req.GetServerNonce(), st.ServerNonce) {
		return nil, errHandshakeState
	}
	rsa := h.getKey(req.GetPublicKeyFingerprint())
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

	a := crypto.GenerateNonce(dhSecretSize)
	gA := new(big.Int).Exp(gBigIntDH2048G, new(big.Int).SetBytes(a), gBigIntDH2048P)

	// server_DH_inner_data#b5890dba nonce:int128 server_nonce:int128 g:int dh_prime:string g_a:string server_time:int = Server_DH_inner_data;
	serverDHInnerData := &mtproto.TLServer_DHInnerData{Data2: &mtproto.Server_DHInnerData{
		Nonce:       st.Nonce,
		ServerNonce: st.ServerNonce,
		G:           int32(h.dh2048g[0]),
		GA:          string(gA.Bytes()),
		DhPrime:     string(h.dh2048p),
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

	return &reqDHParamsResult{
		answer: mtproto.MakeTLServer_DHParamsOk(&mtproto.Server_DH_Params{
			Nonce:           st.Nonce,
			ServerNonce:     st.ServerNonce,
			EncryptedAnswer: hack.String(encryptedAnswer),
		}).To_Server_DH_Params(),
		newNonce:      newNonce,
		a:             a,
		handshakeType: handshakeType,
		expiresIn:     expiresIn,
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

type dhGenKey struct {
	authKeyId int64
	authKey   []byte
}

// processSetClientDHParams checks set_client_DH_params against the handshake
// state st (which req_DH_params completed) and computes the auth key.
//
// set_client_DH_params#f5045f1f nonce:int128 server_nonce:int128 encrypted_data:string = Set_client_DH_params_answer;
func processSetClientDHParams(st *HandshakeStateCtx, req *mtproto.TLSetClient_DHParams) (*dhGenKey, error) {
	if !bytes.Equal(req.GetNonce(), st.Nonce) || !bytes.Equal(req.GetServerNonce(), st.ServerNonce) {
		return nil, errHandshakeState
	}
	// The state must be the one req_DH_params left.
	if len(st.NewNonce) != newNonceSize || len(st.A) != dhSecretSize {
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
	gB := []byte(inner.GetGB())
	if len(gB) == 0 || len(gB) > dhSecretSize {
		return nil, errHandshakeData
	}

	// auth_key := g_b^a mod dh_prime, 256 bytes big-endian.
	authKeyNum := new(big.Int).Exp(new(big.Int).SetBytes(gB), new(big.Int).SetBytes(st.A), gBigIntDH2048P)
	authKey := make([]byte, 256)
	authKeyNum.FillBytes(authKey)

	// auth_key_id: the 64 lower-order bits of SHA1(auth_key).
	sha1Key := sha1.Sum(authKey) //nolint:gosec // MTProto: auth_key_id = low 64 bits of SHA1(auth_key)
	return &dhGenKey{
		authKeyId: int64(binary.LittleEndian.Uint64(sha1Key[12:20])),
		authKey:   authKey,
	}, nil
}

// saveAuthKeyInfo stores a key created by a handshake (with its first server
// salt) through the session service and caches it.
func (s *Server) saveAuthKeyInfo(ctx *HandshakeStateCtx, key *mtproto.AuthKeyInfo) bool {
	var (
		salt = int64(0)
		now  = int32(time.Now().Unix())
	)

	for a := 7; a >= 0; a-- {
		salt <<= 8
		salt |= int64(ctx.NewNonce[a] ^ ctx.ServerNonce[a])
	}

	serverSalt := mtproto.MakeTLFutureSalt(&mtproto.FutureSalt{
		ValidSince: now,
		ValidUntil: now + 30*60,
		Salt:       salt,
	}).To_FutureSalt()

	keyInfo := &mtproto.AuthKeyInfo{
		AuthKeyId:          key.AuthKeyId,
		AuthKey:            key.AuthKey,
		AuthKeyType:        key.AuthKeyType,
		PermAuthKeyId:      key.PermAuthKeyId,
		TempAuthKeyId:      key.TempAuthKeyId,
		MediaTempAuthKeyId: key.MediaTempAuthKeyId,
	}

	// Fix by @wuyun9527, 2018-12-21
	var (
		rB *mtproto.Bool
	)
	err := s.svcCtx.Dao.ShardingSessionClient.InvokeByKey(
		strconv.FormatInt(key.AuthKeyId, 10),
		func(client sessionclient.SessionClient) (err error) {
			rB, err = client.SessionSetAuthKey(context.Background(), &session.TLSessionSetAuthKey{
				AuthKey:    keyInfo,
				FutureSalt: serverSalt,
				ExpiresIn:  ctx.ExpiresIn,
			})
			return err
		})
	if err != nil {
		logx.Errorf("saveAuthKeyInfo not successful - auth_key_id:%d, err:%v", key.AuthKeyId, err)
		return false
	} else if !mtproto.FromBool(rB) {
		logx.Errorf("saveAuthKeyInfo not successful - auth_key_id:%d, err:%v", key.AuthKeyId, err)
		return false
	} else {
		// A temporary key expires expires_in seconds after the handshake
		// (authsession stores the same deadline).
		var expiresAt int64
		if ctx.ExpiresIn > 0 {
			expiresAt = int64(now) + int64(ctx.ExpiresIn)
		}
		s.PutAuthKey(newCheckedAuthKeyUtil(
			&mtproto.AuthKeyInfo{
				AuthKeyId:          keyInfo.AuthKeyId,
				AuthKey:            keyInfo.AuthKey,
				AuthKeyType:        keyInfo.AuthKeyType,
				PermAuthKeyId:      keyInfo.PermAuthKeyId,
				TempAuthKeyId:      keyInfo.TempAuthKeyId,
				MediaTempAuthKeyId: keyInfo.MediaTempAuthKeyId},
			int64(now),
			expiresAt))
	}
	return true
}

func calcNewNonceHash(newNonce, authKey []byte, b byte) []byte {
	// newNonce is 32 bytes, total buffer: 32 + 1 + 20 + 20 = 73
	var buf [73]byte
	copy(buf[:32], newNonce)
	buf[32] = b
	sha1D := sha1.Sum(authKey)
	copy(buf[33:53], sha1D[:])
	sha1E := sha1.Sum(buf[:41]) // 32 + 1 + 8 = 41 (total - 12 when using first 53 bytes)
	copy(buf[53:], sha1E[:])
	result := make([]byte, 16)
	copy(result, buf[73-16:])
	return result
}

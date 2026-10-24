package mtclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // MTProto key exchange: answer_with_hash and data_with_hash are defined over SHA-1
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
	"github.com/teamgram/teamgram-server/pkg/serverkey"
)

// AuthKey is an authorization key created by the key exchange.
type AuthKey struct {
	ID  int64
	Key []byte // 256 bytes
	// Temp is set for a temporary key (PFS), which stops working at ExpiresAt
	// (unix time, as the client computed it from expires_in).
	Temp      bool
	ExpiresAt int64
	// ServerSalt is the first server salt: new_nonce[0:8] XOR server_nonce[0:8].
	ServerSalt int64
	// TimeOffset is server time minus local time at the exchange.
	TimeOffset time.Duration
}

// KeyOptions configures CreateAuthKey.
type KeyOptions struct {
	// DC is the data center number sent in p_q_inner_data_dc (default 2,
	// which official clients use against a single-DC server).
	DC int32
	// Temp creates a temporary key that expires after ExpiresIn seconds
	// (p_q_inner_data_temp_dc).
	Temp      bool
	ExpiresIn int32
}

// CreateAuthKey performs the key exchange (https://core.telegram.org/mtproto/auth_key)
// with the server whose RSA public key is pub, over t, which must be a fresh
// connection (no encrypted traffic yet).
func CreateAuthKey(ctx context.Context, t Transport, pub *rsa.PublicKey, opts KeyOptions) (*AuthKey, error) {
	if opts.DC == 0 {
		opts.DC = 2
	}
	if opts.Temp && opts.ExpiresIn <= 0 {
		return nil, errors.New("mtclient: a temporary key needs ExpiresIn > 0")
	}
	if deadline, ok := ctx.Deadline(); ok {
		if d, ok := t.(interface{ SetDeadline(time.Time) error }); ok {
			_ = d.SetDeadline(deadline)
			defer func() { _ = d.SetDeadline(time.Time{}) }()
		}
	}

	h := &handshake{t: t, pub: pub, opts: opts}
	key, err := h.run()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w (%w)", ctx.Err(), err)
		}
		return nil, err
	}
	return key, nil
}

type handshake struct {
	t         Transport
	pub       *rsa.PublicKey
	opts      KeyOptions
	lastMsgID int64
}

func (h *handshake) run() (*AuthKey, error) {
	fingerprint := int64(serverkey.Fingerprint(h.pub))
	nonce := crypto.GenerateNonce(16)

	// 1. req_pq_multi -> resPQ
	o, err := h.call(&mtproto.TLReqPqMulti{Constructor: mtproto.CRC32_req_pq_multi, Nonce: nonce})
	if err != nil {
		return nil, err
	}
	resPQ, ok := o.(*mtproto.TLResPQ)
	if !ok {
		return nil, fmt.Errorf("mtclient: req_pq_multi: got %T, want resPQ", o)
	}
	if !bytes.Equal(resPQ.GetNonce(), nonce) || len(resPQ.GetServerNonce()) != 16 {
		return nil, errors.New("mtclient: resPQ: nonce mismatch")
	}
	if !slices.Contains(resPQ.GetServerPublicKeyFingerprints(), fingerprint) {
		return nil, fmt.Errorf("mtclient: resPQ does not offer the key %d (offers %v)", fingerprint, resPQ.GetServerPublicKeyFingerprints())
	}
	serverNonce := resPQ.GetServerNonce()
	pqBytes := []byte(resPQ.GetPq())
	if len(pqBytes) == 0 || len(pqBytes) > 8 {
		return nil, fmt.Errorf("mtclient: resPQ: pq is %d bytes", len(pqBytes))
	}
	p, q, err := FactorizePQ(new(big.Int).SetBytes(pqBytes).Uint64())
	if err != nil {
		return nil, err
	}
	pStr := string(new(big.Int).SetUint64(p).Bytes())
	qStr := string(new(big.Int).SetUint64(q).Bytes())

	// 2. req_DH_params with p_q_inner_data_(temp_)dc -> server_DH_params_ok
	newNonce := crypto.GenerateNonce(32)
	inner := &mtproto.P_QInnerData{
		Pq:          string(pqBytes),
		P:           pStr,
		Q:           qStr,
		Nonce:       nonce,
		ServerNonce: serverNonce,
		NewNonce:    newNonce,
		Dc:          h.opts.DC,
		ExpiresIn:   h.opts.ExpiresIn,
	}
	x := mtproto.NewEncodeBuf(256)
	if h.opts.Temp {
		err = mtproto.MakeTLPQInnerDataTempDc(inner).Encode(x, 0)
	} else {
		err = mtproto.MakeTLPQInnerDataDc(inner).Encode(x, 0)
	}
	if err != nil {
		return nil, err
	}
	encryptedData, err := RSAPad(rand.Reader, h.pub, x.GetBuf())
	if err != nil {
		return nil, err
	}
	o, err = h.call(&mtproto.TLReq_DHParams{
		Constructor:          mtproto.CRC32_req_DH_params,
		Nonce:                nonce,
		ServerNonce:          serverNonce,
		P:                    pStr,
		Q:                    qStr,
		PublicKeyFingerprint: fingerprint,
		EncryptedData:        string(encryptedData),
	})
	if err != nil {
		return nil, err
	}
	dhParams, ok := o.(*mtproto.TLServer_DHParamsOk)
	if !ok {
		return nil, fmt.Errorf("mtclient: req_DH_params: got %T, want server_DH_params_ok", o)
	}
	if !bytes.Equal(dhParams.GetNonce(), nonce) || !bytes.Equal(dhParams.GetServerNonce(), serverNonce) {
		return nil, errors.New("mtclient: server_DH_params_ok: nonce mismatch")
	}

	tmpKey, tmpIV := TempAESKeyIV(newNonce, serverNonce)
	answer, err := crypto.NewAES256IGECryptor(tmpKey, tmpIV).Decrypt([]byte(dhParams.GetEncryptedAnswer()))
	if err != nil || len(answer) < 20+4 {
		return nil, errors.New("mtclient: server_DH_params_ok: encrypted_answer does not decrypt")
	}
	d := mtproto.NewDecodeBuf(answer[20:])
	dhInner, ok := d.Object().(*mtproto.TLServer_DHInnerData)
	if !ok || d.GetError() != nil {
		return nil, fmt.Errorf("mtclient: server_DH_inner_data: %v", d.GetError())
	}
	if sum := sha1.Sum(answer[20 : 20+d.GetOffset()]); !bytes.Equal(sum[:], answer[:20]) { //nolint:gosec // defined by the protocol
		return nil, errors.New("mtclient: server_DH_inner_data: SHA1 mismatch")
	}
	if !bytes.Equal(dhInner.GetNonce(), nonce) || !bytes.Equal(dhInner.GetServerNonce(), serverNonce) {
		return nil, errors.New("mtclient: server_DH_inner_data: nonce mismatch")
	}
	g := int64(dhInner.GetG())
	dhPrime := new(big.Int).SetBytes([]byte(dhInner.GetDhPrime()))
	gA := new(big.Int).SetBytes([]byte(dhInner.GetGA()))
	if err = checkDHPrime(g, dhPrime); err != nil {
		return nil, err
	}
	if err = checkDHValue(gA, dhPrime); err != nil {
		return nil, fmt.Errorf("g_a: %w", err)
	}
	timeOffset := time.Duration(int64(dhInner.GetServerTime())-time.Now().Unix()) * time.Second

	// 3. set_client_DH_params -> dh_gen_ok
	var b, gB *big.Int
	for {
		b = new(big.Int).SetBytes(crypto.GenerateNonce(256))
		gB = new(big.Int).Exp(big.NewInt(g), b, dhPrime)
		if checkDHValue(gB, dhPrime) == nil {
			break
		}
	}
	x = mtproto.NewEncodeBuf(512)
	if err = mtproto.MakeTLClient_DHInnerData(&mtproto.Client_DH_Inner_Data{
		Nonce:       nonce,
		ServerNonce: serverNonce,
		RetryId:     0,
		GB:          string(gB.Bytes()),
	}).Encode(x, 0); err != nil {
		return nil, err
	}
	data := x.GetBuf()
	sum := sha1.Sum(data) //nolint:gosec // defined by the protocol
	plain := append(append(make([]byte, 0, 20+len(data)+16), sum[:]...), data...)
	if pad := len(plain) % 16; pad != 0 {
		plain = append(plain, crypto.GenerateNonce(16-pad)...)
	}
	encrypted, err := crypto.NewAES256IGECryptor(tmpKey, tmpIV).Encrypt(plain)
	if err != nil {
		return nil, err
	}
	authKey := new(big.Int).Exp(gA, b, dhPrime).FillBytes(make([]byte, 256))

	o, err = h.call(&mtproto.TLSetClient_DHParams{
		Constructor:   mtproto.CRC32_set_client_DH_params,
		Nonce:         nonce,
		ServerNonce:   serverNonce,
		EncryptedData: string(encrypted),
	})
	if err != nil {
		return nil, err
	}
	switch r := o.(type) {
	case *mtproto.TLDhGenOk:
		if !bytes.Equal(r.GetNonce(), nonce) || !bytes.Equal(r.GetServerNonce(), serverNonce) {
			return nil, errors.New("mtclient: dh_gen_ok: nonce mismatch")
		}
		if !bytes.Equal(r.GetNewNonceHash1(), newNonceHash(newNonce, authKey, 1)) {
			return nil, errors.New("mtclient: dh_gen_ok: new_nonce_hash1 mismatch")
		}
	case *mtproto.TLDhGenRetry:
		// Only after an auth_key_id collision on the server; not retried.
		return nil, errors.New("mtclient: set_client_DH_params: dh_gen_retry")
	case *mtproto.TLDhGenFail:
		return nil, errors.New("mtclient: set_client_DH_params: dh_gen_fail")
	default:
		return nil, fmt.Errorf("mtclient: set_client_DH_params: got %T", o)
	}

	key := &AuthKey{
		ID:         AuthKeyID(authKey),
		Key:        authKey,
		Temp:       h.opts.Temp,
		ServerSalt: int64(binary.LittleEndian.Uint64(newNonce[:8]) ^ binary.LittleEndian.Uint64(serverNonce[:8])),
		TimeOffset: timeOffset,
	}
	if h.opts.Temp {
		key.ExpiresAt = time.Now().Unix() + int64(h.opts.ExpiresIn)
	}
	return key, nil
}

// call sends obj as an unencrypted message and returns the answer.
func (h *handshake) call(obj mtproto.TLObject) (mtproto.TLObject, error) {
	body, err := encodeObject(obj, 0)
	if err != nil {
		return nil, err
	}
	h.lastMsgID = nextMsgID(h.lastMsgID, 0)
	p := make([]byte, 20+len(body))
	binary.LittleEndian.PutUint64(p[8:], uint64(h.lastMsgID))
	binary.LittleEndian.PutUint32(p[16:], uint32(len(body)))
	copy(p[20:], body)
	if err = h.t.WritePacket(p); err != nil {
		return nil, err
	}

	p, err = h.t.ReadPacket()
	if err != nil {
		return nil, err
	}
	if len(p) < 20 || binary.LittleEndian.Uint64(p) != 0 {
		return nil, errors.New("mtclient: handshake: not an unencrypted message")
	}
	n := binary.LittleEndian.Uint32(p[16:])
	if uint64(n) > uint64(len(p)-20) {
		return nil, errors.New("mtclient: handshake: bad message length")
	}
	d := mtproto.NewDecodeBuf(p[20 : 20+n])
	o := d.Object()
	if err = d.GetError(); err != nil {
		return nil, fmt.Errorf("mtclient: handshake: %w", err)
	}
	return o, nil
}

// encodeObject serializes a TL object. The generated encoders write nothing
// for an object without a constructor, which is reported as an error.
func encodeObject(obj mtproto.TLObject, layer int32) ([]byte, error) {
	x := mtproto.NewEncodeBuf(512)
	if err := obj.Encode(x, layer); err != nil {
		return nil, err
	}
	b := x.GetBuf()
	if len(b) < 4 {
		return nil, fmt.Errorf("mtclient: %T encodes to %d bytes (constructor not set?)", obj, len(b))
	}
	return slices.Clone(b), nil
}

// nextMsgID returns a client msg_id (https://core.telegram.org/mtproto/description#message-identifier-msg-id):
// about (unixtime + offset) * 2^32, divisible by 4 and greater than last.
func nextMsgID(last int64, offset time.Duration) int64 {
	now := time.Now().Add(offset)
	id := now.Unix()<<32 | int64(uint64(now.Nanosecond())<<32/1e9)
	id &^= 3
	if id <= last {
		id = last + 4
	}
	return id
}

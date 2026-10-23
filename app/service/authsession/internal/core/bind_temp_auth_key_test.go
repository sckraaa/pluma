// Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
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

package core

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
)

const testNow = int64(1_800_000_000)

type testKey struct {
	id  int64
	key []byte
}

func newTestKey(t testing.TB) testKey {
	t.Helper()
	key := make([]byte, 256)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return testKey{id: crypto.NewAuthKey(0, key).AuthKeyId(), key: key}
}

// fakeTempAuthKeyStore keeps keys in memory and binds them the way
// dao.BindTempAuthKey does.
type fakeTempAuthKeyStore struct {
	keys      map[int64]*mtproto.AuthKeyInfo
	expiresAt map[int64]int64
	bindCalls int
}

func newFakeTempAuthKeyStore() *fakeTempAuthKeyStore {
	return &fakeTempAuthKeyStore{
		keys:      make(map[int64]*mtproto.AuthKeyInfo),
		expiresAt: make(map[int64]int64),
	}
}

func (s *fakeTempAuthKeyStore) add(k testKey, keyType int, expiresAt int64) {
	s.keys[k.id] = mtproto.NewAuthKeyInfo(k.id, k.key, keyType)
	s.expiresAt[k.id] = expiresAt
}

func (s *fakeTempAuthKeyStore) QueryAuthKeyWithExpiry(ctx context.Context, authKeyId int64) (*mtproto.AuthKeyInfo, int64, error) {
	k, ok := s.keys[authKeyId]
	if !ok {
		return nil, 0, mtproto.ErrAuthKeyUnregistered
	}
	return &mtproto.AuthKeyInfo{
		AuthKeyId:          k.AuthKeyId,
		AuthKey:            k.AuthKey,
		AuthKeyType:        k.AuthKeyType,
		PermAuthKeyId:      k.PermAuthKeyId,
		TempAuthKeyId:      k.TempAuthKeyId,
		MediaTempAuthKeyId: k.MediaTempAuthKeyId,
	}, s.expiresAt[authKeyId], nil
}

func (s *fakeTempAuthKeyStore) BindTempAuthKey(ctx context.Context, permAuthKeyId, tempAuthKeyId int64, expiresAt int64) error {
	s.bindCalls++
	temp, ok := s.keys[tempAuthKeyId]
	if !ok {
		return mtproto.ErrTempAuthKeyEmpty
	}
	if temp.PermAuthKeyId != 0 && temp.PermAuthKeyId != permAuthKeyId {
		return mtproto.ErrTempAuthKeyAlreadyBound
	}
	temp.PermAuthKeyId = permAuthKeyId
	if old := s.expiresAt[tempAuthKeyId]; old == 0 || expiresAt < old {
		s.expiresAt[tempAuthKeyId] = expiresAt
	}
	return nil
}

// bindMessage describes the plaintext of a binding message; valid() fills it
// the way official clients do.
type bindMessage struct {
	msgId       int64
	seqNo       uint32
	msgLen      uint32
	constructor uint32
	inner       bindAuthKeyInner
	extraPad    int // extra 16-byte blocks of padding (invalid in MTProto v1)
}

func encryptBindMessage(t testing.TB, encryptKey testKey, prefixKeyId int64, m bindMessage) []byte {
	t.Helper()

	plain := make([]byte, bindMessageHeaderSize+bindAuthKeyInnerSize)
	if _, err := rand.Read(plain[:16]); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(plain[16:], uint64(m.msgId))
	binary.LittleEndian.PutUint32(plain[24:], m.seqNo)
	binary.LittleEndian.PutUint32(plain[28:], m.msgLen)
	body := plain[bindMessageHeaderSize:]
	binary.LittleEndian.PutUint32(body, m.constructor)
	binary.LittleEndian.PutUint64(body[4:], uint64(m.inner.Nonce))
	binary.LittleEndian.PutUint64(body[12:], uint64(m.inner.TempAuthKeyId))
	binary.LittleEndian.PutUint64(body[20:], uint64(m.inner.PermAuthKeyId))
	binary.LittleEndian.PutUint64(body[28:], uint64(m.inner.TempSessionId))
	binary.LittleEndian.PutUint32(body[36:], uint32(m.inner.ExpiresAt))

	msgKey, encrypted, err := crypto.NewClientAuthKey(encryptKey.id, encryptKey.key).AesIgeEncryptV1(plain)
	if err != nil {
		t.Fatal(err)
	}
	if m.extraPad > 0 {
		// Re-encrypt with more padding, keeping msg_key over the same plaintext.
		padded := make([]byte, len(encrypted)+16*m.extraPad)
		copy(padded, plain)
		ck := crypto.NewClientAuthKey(encryptKey.id, encryptKey.key)
		_, encrypted, err = ck.AesIgeEncryptV1(padded)
		if err != nil {
			t.Fatal(err)
		}
	}

	out := make([]byte, 8, 8+16+len(encrypted))
	binary.LittleEndian.PutUint64(out, uint64(prefixKeyId))
	out = append(out, msgKey...)
	return append(out, encrypted...)
}

type bindFixture struct {
	store *fakeTempAuthKeyStore
	perm  testKey
	temp  testKey
	other testKey // another user's permanent key
	req   *bindTempAuthKeyRequest
	msg   bindMessage
}

func newBindFixture(t testing.TB) *bindFixture {
	t.Helper()
	f := &bindFixture{
		store: newFakeTempAuthKeyStore(),
		perm:  newTestKey(t),
		temp:  newTestKey(t),
		other: newTestKey(t),
	}
	f.store.add(f.perm, mtproto.AuthKeyTypePerm, 0)
	f.store.add(f.other, mtproto.AuthKeyTypePerm, 0)
	f.store.add(f.temp, mtproto.AuthKeyTypeTemp, testNow+86400+60)

	const (
		sessionId = int64(0x5e55)
		msgId     = int64(0x6000_0000_0000_0004)
		nonce     = int64(0x0123_4567_89ab_cdef)
	)
	expiresAt := int32(testNow + 86400)

	f.msg = bindMessage{
		msgId:       msgId,
		seqNo:       0,
		msgLen:      bindAuthKeyInnerSize,
		constructor: uint32(mtproto.CRC32_bind_auth_key_inner),
		inner: bindAuthKeyInner{
			Nonce:         nonce,
			TempAuthKeyId: f.temp.id,
			PermAuthKeyId: f.perm.id,
			TempSessionId: sessionId,
			ExpiresAt:     expiresAt,
		},
	}
	f.req = &bindTempAuthKeyRequest{
		TempAuthKeyId: f.temp.id,
		TempSessionId: sessionId,
		MsgId:         msgId,
		PermAuthKeyId: f.perm.id,
		Nonce:         nonce,
		ExpiresAt:     expiresAt,
	}
	f.req.EncryptedMessage = encryptBindMessage(t, f.perm, f.perm.id, f.msg)
	return f
}

func TestBindTempAuthKeyAcceptsValidBinding(t *testing.T) {
	f := newBindFixture(t)

	if err := bindTempAuthKey(context.Background(), f.store, f.req, testNow); err != nil {
		t.Fatalf("bindTempAuthKey: %v", err)
	}
	if got := f.store.keys[f.temp.id].PermAuthKeyId; got != f.perm.id {
		t.Fatalf("temp key bound to %d, want %d", got, f.perm.id)
	}
	if got := f.store.expiresAt[f.temp.id]; got != int64(f.req.ExpiresAt) {
		t.Fatalf("expires_at = %d, want %d", got, f.req.ExpiresAt)
	}

	// Repeating the same binding is accepted (clients resend after reconnects).
	if err := bindTempAuthKey(context.Background(), f.store, f.req, testNow); err != nil {
		t.Fatalf("repeated bindTempAuthKey: %v", err)
	}
}

func TestBindTempAuthKeyMediaTempKey(t *testing.T) {
	f := newBindFixture(t)
	f.store.keys[f.temp.id].AuthKeyType = mtproto.AuthKeyTypeMediaTemp

	if err := bindTempAuthKey(context.Background(), f.store, f.req, testNow); err != nil {
		t.Fatalf("bindTempAuthKey: %v", err)
	}
}

func TestBindTempAuthKeyRejects(t *testing.T) {
	cases := []struct {
		name string
		// mutate changes the fixture before the request is sent; it may
		// re-encrypt the binding message.
		mutate  func(t *testing.T, f *bindFixture)
		wantErr error
	}{
		{
			name: "outer nonce differs",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.Nonce++
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "inner temp_auth_key_id is another key",
			mutate: func(t *testing.T, f *bindFixture) {
				f.msg.inner.TempAuthKeyId++
				f.req.EncryptedMessage = encryptBindMessage(t, f.perm, f.perm.id, f.msg)
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "request arrived on another temp key",
			mutate: func(t *testing.T, f *bindFixture) {
				otherTemp := newTestKey(t)
				f.store.add(otherTemp, mtproto.AuthKeyTypeTemp, 0)
				f.req.TempAuthKeyId = otherTemp.id
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "inner perm_auth_key_id differs from outer",
			mutate: func(t *testing.T, f *bindFixture) {
				f.msg.inner.PermAuthKeyId = f.other.id
				f.req.EncryptedMessage = encryptBindMessage(t, f.perm, f.perm.id, f.msg)
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "inner temp_session_id differs from the request session",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.TempSessionId++
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "outer expires_at differs from inner",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.ExpiresAt++
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "binding message msg_id differs from the request",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.MsgId += 4
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "binding message seqno is not 0",
			mutate: func(t *testing.T, f *bindFixture) {
				f.msg.seqNo = 1
				f.req.EncryptedMessage = encryptBindMessage(t, f.perm, f.perm.id, f.msg)
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "binding message msg_len is not 40",
			mutate: func(t *testing.T, f *bindFixture) {
				f.msg.msgLen = 36
				f.req.EncryptedMessage = encryptBindMessage(t, f.perm, f.perm.id, f.msg)
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "binding message is not bind_auth_key_inner",
			mutate: func(t *testing.T, f *bindFixture) {
				f.msg.constructor = 0x83c95aec // p_q_inner_data
				f.req.EncryptedMessage = encryptBindMessage(t, f.perm, f.perm.id, f.msg)
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "binding message padded beyond 15 bytes",
			mutate: func(t *testing.T, f *bindFixture) {
				f.msg.extraPad = 1
				f.req.EncryptedMessage = encryptBindMessage(t, f.perm, f.perm.id, f.msg)
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "victim perm key named, binding encrypted with attacker's own key",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.EncryptedMessage = encryptBindMessage(t, f.other, f.perm.id, f.msg)
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "encrypted_message prefix names another key",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.EncryptedMessage = encryptBindMessage(t, f.perm, f.other.id, f.msg)
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "valid binding replayed for another perm key",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.PermAuthKeyId = f.other.id
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "ciphertext tampered",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.EncryptedMessage[len(f.req.EncryptedMessage)-20] ^= 0x01
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "encrypted_message truncated",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.EncryptedMessage = f.req.EncryptedMessage[:len(f.req.EncryptedMessage)-16]
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "encrypted_message empty",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.EncryptedMessage = nil
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "unknown perm key",
			mutate: func(t *testing.T, f *bindFixture) {
				delete(f.store.keys, f.perm.id)
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "perm_auth_key_id names a temporary key",
			mutate: func(t *testing.T, f *bindFixture) {
				f.store.keys[f.perm.id].AuthKeyType = mtproto.AuthKeyTypeTemp
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "perm_auth_key_id is 0",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.PermAuthKeyId = 0
			},
			wantErr: mtproto.ErrEncryptedMessageInvalid,
		},
		{
			name: "request sent with a permanent key",
			mutate: func(t *testing.T, f *bindFixture) {
				f.store.keys[f.temp.id].AuthKeyType = mtproto.AuthKeyTypePerm
			},
			wantErr: mtproto.ErrTempAuthKeyEmpty,
		},
		{
			name: "no temp key in metadata",
			mutate: func(t *testing.T, f *bindFixture) {
				f.req.TempAuthKeyId = 0
			},
			wantErr: mtproto.ErrTempAuthKeyEmpty,
		},
		{
			name: "temp key unknown or expired",
			mutate: func(t *testing.T, f *bindFixture) {
				delete(f.store.keys, f.temp.id)
			},
			wantErr: mtproto.ErrAuthKeyUnregistered,
		},
		{
			name: "expires_at in the past",
			mutate: func(t *testing.T, f *bindFixture) {
				f.msg.inner.ExpiresAt = int32(testNow - 1)
				f.req.ExpiresAt = f.msg.inner.ExpiresAt
				f.req.EncryptedMessage = encryptBindMessage(t, f.perm, f.perm.id, f.msg)
			},
			wantErr: errExpiresAtInvalid,
		},
		{
			name: "expires_at is now",
			mutate: func(t *testing.T, f *bindFixture) {
				f.msg.inner.ExpiresAt = int32(testNow)
				f.req.ExpiresAt = f.msg.inner.ExpiresAt
				f.req.EncryptedMessage = encryptBindMessage(t, f.perm, f.perm.id, f.msg)
			},
			wantErr: errExpiresAtInvalid,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newBindFixture(t)
			c.mutate(t, f)

			err := bindTempAuthKey(context.Background(), f.store, f.req, testNow)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("bindTempAuthKey error = %v, want %v", err, c.wantErr)
			}
			if f.store.bindCalls != 0 {
				t.Fatalf("binding stored %d times after a failed check", f.store.bindCalls)
			}
			if temp := f.store.keys[f.temp.id]; temp != nil && temp.PermAuthKeyId != 0 {
				t.Fatalf("temp key bound to %d after a failed check", temp.PermAuthKeyId)
			}
		})
	}
}

func TestBindTempAuthKeyRebindToAnotherPermKeyFails(t *testing.T) {
	f := newBindFixture(t)
	if err := bindTempAuthKey(context.Background(), f.store, f.req, testNow); err != nil {
		t.Fatalf("first bind: %v", err)
	}

	// A correct binding message for another permanent key (one the sender
	// really owns) must not move the already bound temporary key.
	f.msg.inner.PermAuthKeyId = f.other.id
	f.req.PermAuthKeyId = f.other.id
	f.req.EncryptedMessage = encryptBindMessage(t, f.other, f.other.id, f.msg)

	err := bindTempAuthKey(context.Background(), f.store, f.req, testNow)
	if !errors.Is(err, mtproto.ErrTempAuthKeyAlreadyBound) {
		t.Fatalf("rebind error = %v, want TEMP_AUTH_KEY_ALREADY_BOUND", err)
	}
	if got := f.store.keys[f.temp.id].PermAuthKeyId; got != f.perm.id {
		t.Fatalf("temp key bound to %d, want %d", got, f.perm.id)
	}
}

func TestBindTempAuthKeyStoreRaceAlreadyBound(t *testing.T) {
	// The pre-check passes, but the transaction finds the key bound by a
	// concurrent request: its error is returned as is.
	f := newBindFixture(t)
	store := &racingStore{fakeTempAuthKeyStore: f.store, boundTo: f.other.id}
	err := bindTempAuthKey(context.Background(), store, f.req, testNow)
	if !errors.Is(err, mtproto.ErrTempAuthKeyAlreadyBound) {
		t.Fatalf("error = %v, want TEMP_AUTH_KEY_ALREADY_BOUND", err)
	}
}

type racingStore struct {
	*fakeTempAuthKeyStore
	boundTo int64
}

func (s *racingStore) BindTempAuthKey(ctx context.Context, permAuthKeyId, tempAuthKeyId int64, expiresAt int64) error {
	s.keys[tempAuthKeyId].PermAuthKeyId = s.boundTo
	return s.fakeTempAuthKeyStore.BindTempAuthKey(ctx, permAuthKeyId, tempAuthKeyId, expiresAt)
}

func TestDecryptBindAuthKeyInner(t *testing.T) {
	f := newBindFixture(t)
	inner, err := decryptBindAuthKeyInner(crypto.NewAuthKey(f.perm.id, f.perm.key), f.req.EncryptedMessage, f.req.MsgId)
	if err != nil {
		t.Fatalf("decryptBindAuthKeyInner: %v", err)
	}
	if *inner != f.msg.inner {
		t.Fatalf("inner = %+v, want %+v", *inner, f.msg.inner)
	}
	if len(f.req.EncryptedMessage) != 8+16+80 {
		t.Fatalf("encrypted_message length = %d, want 104 (as official clients send)", len(f.req.EncryptedMessage))
	}
}

func FuzzDecryptBindAuthKeyInner(f *testing.F) {
	fx := newBindFixture(f)
	permKey := crypto.NewAuthKey(fx.perm.id, fx.perm.key)

	f.Add(fx.req.EncryptedMessage, fx.req.MsgId)
	f.Add([]byte{}, int64(0))
	f.Add(make([]byte, 104), int64(1))
	short := make([]byte, 40)
	binary.LittleEndian.PutUint64(short, uint64(fx.perm.id))
	f.Add(short, int64(0))

	f.Fuzz(func(t *testing.T, encryptedMessage []byte, msgId int64) {
		inner, err := decryptBindAuthKeyInner(permKey, encryptedMessage, msgId)
		if err == nil && inner == nil {
			t.Fatal("nil inner without error")
		}
	})
}

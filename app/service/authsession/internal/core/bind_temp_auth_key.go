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
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"

	"google.golang.org/grpc/status"
)

// errExpiresAtInvalid is the documented auth.bindTempAuthKey error for an
// expires_at that is not in the future.
var errExpiresAtInvalid = status.Error(mtproto.ErrBadRequest, "EXPIRES_AT_INVALID")

const (
	// bind_auth_key_inner#75a3f765 nonce:long temp_auth_key_id:long
	// perm_auth_key_id:long temp_session_id:long expires_at:int
	bindAuthKeyInnerSize = 4 + 8 + 8 + 8 + 8 + 4

	// MTProto v1 plaintext header of the binding message: random:int128 (in
	// place of salt and session_id), msg_id:long, seqno:int, msg_len:int.
	bindMessageHeaderSize = 16 + 8 + 4 + 4

	// encrypted_message prefix: perm auth_key_id:long, msg_key:int128.
	bindMessagePrefixSize = 8 + 16
)

// bindAuthKeyInner is the decoded bind_auth_key_inner.
type bindAuthKeyInner struct {
	Nonce         int64
	TempAuthKeyId int64
	PermAuthKeyId int64
	TempSessionId int64
	ExpiresAt     int32
}

// tempAuthKeyStore is what auth.bindTempAuthKey needs from storage (dao.Dao).
type tempAuthKeyStore interface {
	QueryAuthKeyWithExpiry(ctx context.Context, authKeyId int64) (*mtproto.AuthKeyInfo, int64, error)
	BindTempAuthKey(ctx context.Context, permAuthKeyId, tempAuthKeyId int64, expiresAt int64) error
}

// bindTempAuthKeyRequest is auth.bindTempAuthKey together with the outer
// message it arrived in, as the session saw it.
type bindTempAuthKeyRequest struct {
	// The connection the request arrived on (RPC metadata from the session):
	// its auth key, session and the request's msg_id.
	TempAuthKeyId int64
	TempSessionId int64
	MsgId         int64

	// auth.bindTempAuthKey parameters.
	PermAuthKeyId    int64
	Nonce            int64
	ExpiresAt        int32
	EncryptedMessage []byte
}

// bindTempAuthKey verifies auth.bindTempAuthKey
// (https://core.telegram.org/method/auth.bindTempAuthKey) and only then stores
// the binding. Nothing is written when any check fails.
//
// The binding message must be encrypted with the permanent key it names
// (MTProto v1), carry the msg_id of the request and seqno 0, and its
// bind_auth_key_inner must repeat the request's nonce, perm_auth_key_id and
// expires_at and name the temporary key and session the request was sent
// with. A request that only names a perm_auth_key_id proves nothing, so
// nothing about that key is trusted before these checks pass.
func bindTempAuthKey(ctx context.Context, store tempAuthKeyStore, req *bindTempAuthKeyRequest, now int64) error {
	if req.TempAuthKeyId == 0 {
		return mtproto.ErrTempAuthKeyEmpty
	}

	tempKey, _, err := store.QueryAuthKeyWithExpiry(ctx, req.TempAuthKeyId)
	if err != nil {
		if errors.Is(err, mtproto.ErrAuthKeyUnregistered) {
			// Unknown or already expired: the client has to create a new key.
			return mtproto.ErrAuthKeyUnregistered
		}
		return mtproto.ErrInternalServerError
	}
	if tempKey.GetAuthKeyType() != mtproto.AuthKeyTypeTemp && tempKey.GetAuthKeyType() != mtproto.AuthKeyTypeMediaTemp {
		return mtproto.ErrTempAuthKeyEmpty
	}

	if req.PermAuthKeyId == 0 {
		return mtproto.ErrEncryptedMessageInvalid
	}
	permKey, _, err := store.QueryAuthKeyWithExpiry(ctx, req.PermAuthKeyId)
	if err != nil {
		if errors.Is(err, mtproto.ErrAuthKeyUnregistered) {
			return mtproto.ErrEncryptedMessageInvalid
		}
		return mtproto.ErrInternalServerError
	}
	if permKey.GetAuthKeyType() != mtproto.AuthKeyTypePerm {
		return mtproto.ErrEncryptedMessageInvalid
	}

	inner, err := decryptBindAuthKeyInner(
		crypto.NewAuthKey(req.PermAuthKeyId, permKey.GetAuthKey()),
		req.EncryptedMessage,
		req.MsgId)
	if err != nil {
		return mtproto.ErrEncryptedMessageInvalid
	}

	if inner.Nonce != req.Nonce ||
		inner.TempAuthKeyId != req.TempAuthKeyId ||
		inner.PermAuthKeyId != req.PermAuthKeyId ||
		inner.TempSessionId != req.TempSessionId ||
		inner.ExpiresAt != req.ExpiresAt {
		return mtproto.ErrEncryptedMessageInvalid
	}

	if int64(req.ExpiresAt) <= now {
		return errExpiresAtInvalid
	}

	if bound := tempKey.GetPermAuthKeyId(); bound != 0 && bound != req.PermAuthKeyId {
		return mtproto.ErrTempAuthKeyAlreadyBound
	}

	return store.BindTempAuthKey(ctx, req.PermAuthKeyId, req.TempAuthKeyId, int64(req.ExpiresAt))
}

// decryptBindAuthKeyInner decrypts encrypted_message of auth.bindTempAuthKey
// with the permanent key and returns the bind_auth_key_inner it carries.
//
// encrypted_message = perm auth_key_id:long msg_key:int128 encrypted_data, where
// encrypted_data is MTProto v1 AES-IGE of
// random:int128 msg_id:long seqno:int msg_len:int bind_auth_key_inner padding
// (padding 0..15 bytes) and msg_key is the lower 128 bits of the SHA1 of the
// plaintext without padding. msg_id must be the msg_id of the request itself,
// seqno 0 and msg_len 40.
//
// The returned errors never include key material.
func decryptBindAuthKeyInner(permKey *crypto.AuthKey, encryptedMessage []byte, msgId int64) (*bindAuthKeyInner, error) {
	const plainSize = bindMessageHeaderSize + bindAuthKeyInnerSize

	if len(encryptedMessage) < bindMessagePrefixSize+plainSize {
		return nil, fmt.Errorf("encrypted_message too short: %d", len(encryptedMessage))
	}
	encryptedData := encryptedMessage[bindMessagePrefixSize:]
	if len(encryptedData)%16 != 0 || len(encryptedData)-plainSize >= 16 {
		return nil, fmt.Errorf("encrypted_message has invalid length: %d", len(encryptedMessage))
	}
	if int64(binary.LittleEndian.Uint64(encryptedMessage)) != permKey.AuthKeyId() {
		return nil, errors.New("encrypted_message is not encrypted with perm_auth_key_id")
	}

	plain, err := permKey.AesIgeDecryptV1(encryptedMessage[8:bindMessagePrefixSize], encryptedData)
	if err != nil {
		// The crypto error text includes key material: do not wrap it.
		return nil, errors.New("encrypted_message does not decrypt with perm_auth_key_id")
	}
	if len(plain) < plainSize {
		return nil, errors.New("binding message too short")
	}

	if v := int64(binary.LittleEndian.Uint64(plain[16:])); v != msgId {
		return nil, errors.New("binding message msg_id differs from the request msg_id")
	}
	if v := binary.LittleEndian.Uint32(plain[24:]); v != 0 {
		return nil, errors.New("binding message seqno is not 0")
	}
	if v := binary.LittleEndian.Uint32(plain[28:]); v != bindAuthKeyInnerSize {
		return nil, fmt.Errorf("binding message msg_len is %d", v)
	}

	body := plain[bindMessageHeaderSize:plainSize]
	if v := binary.LittleEndian.Uint32(body); v != uint32(mtproto.CRC32_bind_auth_key_inner) {
		return nil, fmt.Errorf("binding message is not bind_auth_key_inner: %#x", v)
	}

	return &bindAuthKeyInner{
		Nonce:         int64(binary.LittleEndian.Uint64(body[4:])),
		TempAuthKeyId: int64(binary.LittleEndian.Uint64(body[12:])),
		PermAuthKeyId: int64(binary.LittleEndian.Uint64(body[20:])),
		TempSessionId: int64(binary.LittleEndian.Uint64(body[28:])),
		ExpiresAt:     int32(binary.LittleEndian.Uint32(body[36:])),
	}, nil
}

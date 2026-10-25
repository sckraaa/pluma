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

package dao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/authkeys"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// auth_keys4 holds authKeyData with the key sealed (task 008); auth_keys3
	// entries held the plain key and are no longer read, nor are auth_keys2
	// (AuthKeyInfo) or the auth_keys_<id> hashes of old teamgram versions
	// (see authKeyDataFromRows).
	cacheAuthKeyV2Prefix = "auth_keys4"
)

func genCacheAuthKeyV2Key(id int64) string {
	return fmt.Sprintf("%s_%d", cacheAuthKeyV2Prefix, id)
}

// authKeyData is how an auth key is cached: AuthKeyInfo plus the expiry of a
// temporary key, for which AuthKeyInfo has no field. Body is the key as
// auth_keys stores it, sealed under AUTH_KEY_ENCRYPTION_KEY (pkg/authkeys):
// the cache holds no plain key either.
type authKeyData struct {
	AuthKeyId          int64  `json:"auth_key_id"`
	Body               string `json:"body"`
	AuthKeyType        int32  `json:"auth_key_type"`
	PermAuthKeyId      int64  `json:"perm_auth_key_id"`
	TempAuthKeyId      int64  `json:"temp_auth_key_id"`
	MediaTempAuthKeyId int64  `json:"media_temp_auth_key_id"`
	ExpiresAt          int64  `json:"expires_at"`
}

// toAuthKeyInfo returns the key with its plain bytes key.
func (k *authKeyData) toAuthKeyInfo(key []byte) *mtproto.AuthKeyInfo {
	return mtproto.MakeTLAuthKeyInfo(&mtproto.AuthKeyInfo{
		AuthKeyId:          k.AuthKeyId,
		AuthKey:            key,
		AuthKeyType:        k.AuthKeyType,
		PermAuthKeyId:      k.PermAuthKeyId,
		TempAuthKeyId:      k.TempAuthKeyId,
		MediaTempAuthKeyId: k.MediaTempAuthKeyId,
	}).To_AuthKeyInfo()
}

// unboundTemp reports a temporary key that auth.bindTempAuthKey has not bound
// to a permanent key yet.
func (k *authKeyData) unboundTemp() bool {
	return IsTempAuthKeyType(k.AuthKeyType) && k.PermAuthKeyId == 0
}

func (k *authKeyData) expired(now int64) bool {
	return IsTempAuthKeyType(k.AuthKeyType) && k.ExpiresAt != 0 && now >= k.ExpiresAt
}

// IsTempAuthKeyType reports whether keyType is one of the temporary key types
// (PFS, https://core.telegram.org/api/pfs).
func IsTempAuthKeyType(keyType int32) bool {
	return keyType == mtproto.AuthKeyTypeTemp || keyType == mtproto.AuthKeyTypeMediaTemp
}

func (d *Dao) QueryAuthKeyV2(ctx context.Context, authKeyId int64) (*mtproto.AuthKeyInfo, error) {
	keyInfo, _, err := d.QueryAuthKeyWithExpiry(ctx, authKeyId)
	return keyInfo, err
}

// QueryAuthKeyWithExpiry returns the key and the unix time a temporary key
// expires at (0: never). An expired temporary key is reported as
// AUTH_KEY_UNREGISTERED, the same as an unknown one, so the gateway answers
// with transport error -404 and the client creates a new temporary key.
func (d *Dao) QueryAuthKeyWithExpiry(ctx context.Context, authKeyId int64) (*mtproto.AuthKeyInfo, int64, error) {
	var (
		key   = genCacheAuthKeyV2Key(authKeyId)
		kData = &authKeyData{}
	)

	if err := d.CachedConn.GetCache(ctx, key, kData); err != nil || kData.AuthKeyId != authKeyId {
		kData, err = d.loadAuthKeyData(ctx, authKeyId)
		if err != nil {
			return nil, 0, err
		}

		// An unbound temporary key changes when auth.bindTempAuthKey succeeds.
		// It is never cached, so a read that races with the binding cannot
		// leave a stale unbound copy behind.
		if !kData.unboundTemp() {
			if err = d.CachedConn.SetCache(ctx, key, kData); err != nil {
				logx.WithContext(ctx).Errorf("queryAuthKey(%d) - set cache error: %v", authKeyId, err)
			}
		}
	}

	if kData.expired(time.Now().Unix()) {
		return nil, 0, mtproto.ErrAuthKeyUnregistered
	}

	plain, _, err := d.keys.Open(authKeyId, kData.Body)
	if err != nil {
		// Another AUTH_KEY_ENCRYPTION_KEY than the one the key was sealed
		// with, or a damaged row: not an unknown key, which would make the
		// client drop its authorization.
		logx.WithContext(ctx).Errorf("queryAuthKey(%d) - open the sealed key: %v", authKeyId, err)
		return nil, 0, mtproto.ErrInternalServerError
	}
	return kData.toAuthKeyInfo(plain), kData.ExpiresAt, nil
}

func (d *Dao) loadAuthKeyData(ctx context.Context, authKeyId int64) (*authKeyData, error) {
	do1, err := d.AuthKeyInfosDAO.SelectByAuthKeyId(ctx, authKeyId)
	if err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	do2, err := d.AuthKeysDAO.SelectByAuthKeyId(ctx, authKeyId)
	if err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	kData, err := authKeyDataFromRows(authKeyId, do1, do2)
	if err != nil {
		return nil, err
	}
	if !authkeys.IsSealed(kData.Body) {
		// A plain key of before task 008: sealed on its first read.
		if sealed, err := d.sealPlainKey(ctx, authKeyId, kData.Body); err != nil {
			logx.WithContext(ctx).Errorf("loadAuthKeyData(%d) - seal the plain key: %v", authKeyId, err)
		} else {
			kData.Body = sealed
		}
	}
	return kData, nil
}

// authKeyDataFromRows builds a key from its MySQL rows; a key without both
// rows is unknown. Old teamgram versions fell back to a Redis hash
// auth_keys_<id> and trusted its type and perm_auth_key_id; before task 001
// Redis was reachable without a password, so such a hash may be planted (a
// temporary key "bound" to someone's permanent key) and is never read.
func authKeyDataFromRows(authKeyId int64, info *dataobject.AuthKeyInfosDO, key *dataobject.AuthKeysDO) (*authKeyData, error) {
	if info == nil || key == nil {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	kData := &authKeyData{
		AuthKeyId:          authKeyId,
		AuthKeyType:        info.AuthKeyType,
		PermAuthKeyId:      info.PermAuthKeyId,
		TempAuthKeyId:      info.TempAuthKeyId,
		MediaTempAuthKeyId: info.MediaTempAuthKeyId,
		ExpiresAt:          info.ExpiresAt,
		Body:               key.Body,
	}
	return kData, nil
}

// errTempKeyWithoutExpiry: a temporary key must expire.
var errTempKeyWithoutExpiry = errors.New("temporary auth key without a positive expires_in")

// ErrAuthKeyIdTaken: a key with this auth_key_id exists already, the
// collision the key exchange answers with dh_gen_retry
// (https://core.telegram.org/mtproto/auth_key, step 9).
var ErrAuthKeyIdTaken = errors.New("auth_key_id is taken")

// SetAuthKeyV2 stores a key created by a handshake. expiresIn is the
// expires_in of p_q_inner_data_temp(_dc); a temporary key stops working
// expiresIn seconds from now, at most authkeys.MaxTempExpiresIn. A key whose auth_key_id is taken is not stored
// (ErrAuthKeyIdTaken): the unique index on auth_key_id decides.
func (d *Dao) SetAuthKeyV2(ctx context.Context, authKey *mtproto.AuthKeyInfo, expiresIn int32) (err error) {
	var expiresAt int64
	if IsTempAuthKeyType(authKey.AuthKeyType) {
		// expires_at 0 means "never": the handshake refuses such keys, and
		// they are not stored either.
		if expiresIn <= 0 {
			return errTempKeyWithoutExpiry
		}
		expiresAt = time.Now().Unix() + int64(authkeys.TempExpiresIn(expiresIn))
	}

	body, err := d.keys.Seal(authKey.AuthKeyId, authKey.AuthKey)
	if err != nil {
		return err
	}
	tR := sqlx.TxWrapper(ctx, d.DB, func(tx *sqlx.Tx, result *sqlx.StoreResult) {
		_, _, err = d.AuthKeysDAO.InsertTx(tx, &dataobject.AuthKeysDO{
			AuthKeyId: authKey.AuthKeyId,
			Body:      body,
		})
		if err != nil {
			result.Err = err
			return
		}
		_, _, err = d.AuthKeyInfosDAO.InsertTx(tx, &dataobject.AuthKeyInfosDO{
			AuthKeyId:          authKey.AuthKeyId,
			AuthKeyType:        authKey.AuthKeyType,
			PermAuthKeyId:      authKey.PermAuthKeyId,
			TempAuthKeyId:      authKey.TempAuthKeyId,
			MediaTempAuthKeyId: authKey.MediaTempAuthKeyId,
			ExpiresAt:          expiresAt,
			Deleted:            false,
		})
		if err != nil {
			result.Err = err
		}
	})

	if sqlx.IsDuplicate(tR.Err) {
		return ErrAuthKeyIdTaken
	}
	return tR.Err
}

// BindTempAuthKey records, after auth.bindTempAuthKey has been verified, that
// the temporary key tempAuthKeyId acts for the permanent key permAuthKeyId
// until expiresAt.
//
// A temporary key is bound to one permanent key for its whole life: binding it
// again to the same key succeeds (the client may repeat the request), binding
// it to another key fails with TEMP_AUTH_KEY_ALREADY_BOUND. expires_at never
// moves later than the value stored at the handshake. Both rows change in one
// transaction; the cached copies are dropped after it commits.
func (d *Dao) BindTempAuthKey(ctx context.Context, permAuthKeyId, tempAuthKeyId int64, expiresAt int64) error {
	// rejected is the client error a check inside the transaction found. It
	// does not leave the transaction as an error: go-zero's circuit breaker
	// counts every error a transaction returns as a database failure, so a
	// burst of rejected bindings (concurrent requests for one temporary key)
	// opened it and failed every query of authsession for a while. A rejected
	// binding commits having written nothing.
	var rejected error
	tR := sqlx.TxWrapper(ctx, d.DB, func(tx *sqlx.Tx, result *sqlx.StoreResult) {
		tempKey := &dataobject.AuthKeyInfosDO{}
		err := tx.QueryRowPartial(
			tempKey,
			"select auth_key_id, auth_key_type, perm_auth_key_id, expires_at from auth_key_infos where auth_key_id = ? limit 1 for update",
			tempAuthKeyId)
		if err != nil {
			if errors.Is(err, sqlx.ErrNotFound) {
				rejected = mtproto.ErrTempAuthKeyEmpty
			} else {
				result.Err = err
			}
			return
		}

		var permColumn string
		switch tempKey.AuthKeyType {
		case mtproto.AuthKeyTypeTemp:
			permColumn = "temp_auth_key_id"
		case mtproto.AuthKeyTypeMediaTemp:
			permColumn = "media_temp_auth_key_id"
		default:
			rejected = mtproto.ErrTempAuthKeyEmpty
			return
		}

		if tempKey.PermAuthKeyId != 0 && tempKey.PermAuthKeyId != permAuthKeyId {
			rejected = mtproto.ErrTempAuthKeyAlreadyBound
			return
		}

		if tempKey.ExpiresAt != 0 && tempKey.ExpiresAt < expiresAt {
			expiresAt = tempKey.ExpiresAt
		}

		if _, err = d.AuthKeyInfosDAO.UpdateCustomMapTx(
			tx,
			map[string]interface{}{
				"perm_auth_key_id": permAuthKeyId,
				"expires_at":       expiresAt,
			},
			tempAuthKeyId); err != nil {
			result.Err = err
			return
		}

		// Informational: the permanent key's latest temporary key of this type.
		if _, err = d.AuthKeyInfosDAO.UpdateCustomMapTx(
			tx,
			map[string]interface{}{
				permColumn: tempAuthKeyId,
			},
			permAuthKeyId); err != nil {
			result.Err = err
		}
	})
	if tR.Err != nil {
		logx.WithContext(ctx).Errorf("bindTempAuthKey(temp: %d, perm: %d) - error: %v", tempAuthKeyId, permAuthKeyId, tR.Err)
		return mtproto.ErrInternalServerError
	}
	if rejected != nil {
		return rejected
	}

	if err := d.CachedConn.DelCache(ctx, genCacheAuthKeyV2Key(tempAuthKeyId), genCacheAuthKeyV2Key(permAuthKeyId)); err != nil {
		logx.WithContext(ctx).Errorf("bindTempAuthKey(temp: %d, perm: %d) - del cache error: %v", tempAuthKeyId, permAuthKeyId, err)
		return mtproto.ErrInternalServerError
	}

	return nil
}

func (d *Dao) GetPermAuthKeyId(ctx context.Context, authKeyId int64) int64 {
	if k, err := d.QueryAuthKeyV2(ctx, authKeyId); err != nil || k == nil {
		return 0
	} else {
		return k.PermAuthKeyId
	}
}

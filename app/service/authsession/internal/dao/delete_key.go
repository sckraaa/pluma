// Copyright 2026 The Pluma Authors
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

package dao

import (
	"context"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
)

// Removal of auth keys (task 008): destroy_auth_key, auth.dropTempAuthKeys
// and the removal of expired temporary keys. A removed key is unknown to
// authsession at once (AUTH_KEY_UNREGISTERED); the gateway reads its keys
// again within a minute and then answers -404 for it.

// placeholders returns "?, ?, …" for n values.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func int64Args(ids []int64) []interface{} {
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// TempAuthKeysOf returns the temporary keys bound to the permanent key
// permAuthKeyId.
func (d *Dao) TempAuthKeysOf(ctx context.Context, permAuthKeyId int64) ([]int64, error) {
	var ids []int64
	err := d.DB.QueryRowsPartial(ctx, &ids,
		"SELECT auth_key_id FROM auth_key_infos WHERE perm_auth_key_id = ? AND auth_key_type IN (?, ?)",
		permAuthKeyId, mtproto.AuthKeyTypeTemp, mtproto.AuthKeyTypeMediaTemp)
	return ids, err
}

// ExpiredTempAuthKeys returns up to limit temporary keys that stopped
// working at now.
func (d *Dao) ExpiredTempAuthKeys(ctx context.Context, now int64, limit int) ([]int64, error) {
	var ids []int64
	err := d.DB.QueryRowsPartial(ctx, &ids,
		"SELECT auth_key_id FROM auth_key_infos WHERE auth_key_type IN (?, ?) AND expires_at > 0 AND expires_at <= ? ORDER BY expires_at LIMIT ?",
		mtproto.AuthKeyTypeTemp, mtproto.AuthKeyTypeMediaTemp, now, limit)
	return ids, err
}

// DeleteAuthKeys removes the keys ids for good in one transaction: their
// rows in auth_keys and auth_key_infos, and the references of permanent keys
// to them as their latest temporary keys. Their cached copies, and those of
// the permanent keys that referred to them, are dropped after the commit.
// It returns the ids of the keys that were there.
func (d *Dao) DeleteAuthKeys(ctx context.Context, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	in, args := placeholders(len(ids)), int64Args(ids)
	var (
		found []int64
		perms []int64
	)
	tR := sqlx.TxWrapper(ctx, d.DB, func(tx *sqlx.Tx, result *sqlx.StoreResult) {
		if err := tx.QueryRowsPartial(&found,
			"SELECT auth_key_id FROM auth_key_infos WHERE auth_key_id IN ("+in+") FOR UPDATE", args...); err != nil {
			result.Err = err
			return
		}
		if err := tx.QueryRowsPartial(&perms,
			"SELECT auth_key_id FROM auth_key_infos WHERE temp_auth_key_id IN ("+in+") OR media_temp_auth_key_id IN ("+in+") FOR UPDATE",
			append(append([]interface{}{}, args...), args...)...); err != nil {
			result.Err = err
			return
		}
		for _, q := range []string{
			"DELETE FROM auth_keys WHERE auth_key_id IN (" + in + ")",
			"DELETE FROM auth_key_infos WHERE auth_key_id IN (" + in + ")",
			"UPDATE auth_key_infos SET temp_auth_key_id = 0 WHERE temp_auth_key_id IN (" + in + ")",
			"UPDATE auth_key_infos SET media_temp_auth_key_id = 0 WHERE media_temp_auth_key_id IN (" + in + ")",
		} {
			if _, err := tx.Exec(q, args...); err != nil {
				result.Err = err
				return
			}
		}
	})
	if tR.Err != nil {
		logx.WithContext(ctx).Errorf("deleteAuthKeys(%d keys) - error: %v", len(ids), tR.Err)
		return nil, tR.Err
	}

	keys := make([]string, 0, len(ids)+len(perms))
	for _, id := range ids {
		keys = append(keys, genCacheAuthKeyV2Key(id))
	}
	for _, id := range perms {
		keys = append(keys, genCacheAuthKeyV2Key(id))
	}
	if err := d.CachedConn.DelCache(ctx, keys...); err != nil {
		logx.WithContext(ctx).Errorf("deleteAuthKeys(%d keys) - del cache error: %v", len(ids), err)
		return found, err
	}
	return found, nil
}

// DestroyAuthKey removes the key authKeyId (destroy_auth_key). A permanent
// key takes with it the temporary keys bound to it and its authorization:
// its user is unbound as by auth.logOut. It returns the ids of the removed
// keys; none when the key was not there.
func (d *Dao) DestroyAuthKey(ctx context.Context, authKeyId int64) ([]int64, error) {
	var types []int32
	if err := d.DB.QueryRowsPartial(ctx, &types,
		"SELECT auth_key_type FROM auth_key_infos WHERE auth_key_id = ?", authKeyId); err != nil {
		return nil, err
	}
	if len(types) == 0 {
		return nil, nil
	}
	ids := []int64{authKeyId}
	if !IsTempAuthKeyType(types[0]) {
		temps, err := d.TempAuthKeysOf(ctx, authKeyId)
		if err != nil {
			return nil, err
		}
		ids = append(ids, temps...)
		if userId := d.GetAuthKeyUserId(ctx, authKeyId); userId != 0 && !d.UnbindAuthUser(ctx, authKeyId, userId) {
			return nil, mtproto.ErrInternalServerError
		}
	}
	return d.DeleteAuthKeys(ctx, ids)
}

// DropTempAuthKeys removes the temporary keys bound to permAuthKeyId but
// those in except (auth.dropTempAuthKeys) and returns the removed ones.
func (d *Dao) DropTempAuthKeys(ctx context.Context, permAuthKeyId int64, except []int64) ([]int64, error) {
	temps, err := d.TempAuthKeysOf(ctx, permAuthKeyId)
	if err != nil {
		return nil, err
	}
	keep := make(map[int64]bool, len(except))
	for _, id := range except {
		keep[id] = true
	}
	drop := temps[:0]
	for _, id := range temps {
		if !keep[id] {
			drop = append(drop, id)
		}
	}
	return d.DeleteAuthKeys(ctx, drop)
}

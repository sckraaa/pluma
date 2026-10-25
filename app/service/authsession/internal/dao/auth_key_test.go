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

package dao

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/authkeys"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestAuthKeyDataState(t *testing.T) {
	const now = int64(1_800_000_000)

	cases := []struct {
		name        string
		k           authKeyData
		wantUnbound bool
		wantExpired bool
	}{
		{"perm key", authKeyData{AuthKeyType: mtproto.AuthKeyTypePerm, PermAuthKeyId: 1}, false, false},
		{"perm key with a stray expires_at", authKeyData{AuthKeyType: mtproto.AuthKeyTypePerm, ExpiresAt: now - 1}, false, false},
		{"unbound temp key", authKeyData{AuthKeyType: mtproto.AuthKeyTypeTemp, ExpiresAt: now + 1}, true, false},
		{"bound temp key", authKeyData{AuthKeyType: mtproto.AuthKeyTypeTemp, PermAuthKeyId: 1, ExpiresAt: now + 1}, false, false},
		{"temp key without expiry", authKeyData{AuthKeyType: mtproto.AuthKeyTypeTemp, PermAuthKeyId: 1}, false, false},
		{"temp key at expiry", authKeyData{AuthKeyType: mtproto.AuthKeyTypeTemp, PermAuthKeyId: 1, ExpiresAt: now}, false, true},
		{"media temp key expired", authKeyData{AuthKeyType: mtproto.AuthKeyTypeMediaTemp, ExpiresAt: now - 1}, true, true},
	}
	for _, c := range cases {
		if got := c.k.unboundTemp(); got != c.wantUnbound {
			t.Errorf("%s: unboundTemp = %v, want %v", c.name, got, c.wantUnbound)
		}
		if got := c.k.expired(now); got != c.wantExpired {
			t.Errorf("%s: expired = %v, want %v", c.name, got, c.wantExpired)
		}
	}
}

func TestQueryAuthKeyWithExpiryFromCache(t *testing.T) {
	// miniredis does not implement CLIENT SETINFO; DisableIdentity keeps the failing
	// connect-time command from tripping the go-zero redis breaker.
	mr := miniredis.RunT(t)
	d := &Dao{CachedConn: sqlc.NewNodeConn(nil, redis.MustNewRedis(redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true})), keys: testKeyCipher(t)}
	ctx := context.Background()
	now := time.Now().Unix()
	seal := func(id int64, key byte) string {
		body, err := d.keys.Seal(id, []byte{key})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}

	keys := []*authKeyData{
		{AuthKeyId: 1, Body: seal(1, 1), AuthKeyType: mtproto.AuthKeyTypePerm, PermAuthKeyId: 1},
		{AuthKeyId: 2, Body: seal(2, 2), AuthKeyType: mtproto.AuthKeyTypeTemp, PermAuthKeyId: 1, TempAuthKeyId: 2, ExpiresAt: now + 3600},
		{AuthKeyId: 3, Body: seal(3, 3), AuthKeyType: mtproto.AuthKeyTypeTemp, PermAuthKeyId: 1, TempAuthKeyId: 3, ExpiresAt: now - 1},
		{AuthKeyId: 4, Body: seal(5, 4), AuthKeyType: mtproto.AuthKeyTypePerm}, // sealed for another id
	}
	for _, k := range keys {
		if err := d.CachedConn.SetCache(ctx, genCacheAuthKeyV2Key(k.AuthKeyId), k); err != nil {
			t.Fatal(err)
		}
	}

	perm, expiresAt, err := d.QueryAuthKeyWithExpiry(ctx, 1)
	if err != nil || perm.GetPermAuthKeyId() != 1 || expiresAt != 0 || string(perm.GetAuthKey()) != "\x01" {
		t.Fatalf("perm key: %v, expiresAt %d, err %v", perm, expiresAt, err)
	}

	temp, expiresAt, err := d.QueryAuthKeyWithExpiry(ctx, 2)
	if err != nil || temp.GetPermAuthKeyId() != 1 || temp.GetAuthKeyType() != mtproto.AuthKeyTypeTemp || expiresAt != now+3600 {
		t.Fatalf("bound temp key: %v, expiresAt %d, err %v", temp, expiresAt, err)
	}

	if _, _, err = d.QueryAuthKeyWithExpiry(ctx, 3); !errors.Is(err, mtproto.ErrAuthKeyUnregistered) {
		t.Fatalf("expired temp key: err = %v, want AUTH_KEY_UNREGISTERED", err)
	}
	if _, err = d.QueryAuthKeyV2(ctx, 3); !errors.Is(err, mtproto.ErrAuthKeyUnregistered) {
		t.Fatalf("expired temp key via QueryAuthKeyV2: err = %v, want AUTH_KEY_UNREGISTERED", err)
	}
	// A body that does not open is an internal error, not an unknown key
	// (which would make the client drop its authorization).
	if _, err = d.QueryAuthKeyV2(ctx, 4); !errors.Is(err, mtproto.ErrInternalServerError) {
		t.Fatalf("key sealed for another id: err = %v, want INTERNAL_SERVER_ERROR", err)
	}
}

// testKeyCipher seals keys under a fixed test encryption key.
func testKeyCipher(t testing.TB) *authkeys.KeyCipher {
	t.Helper()
	c, err := authkeys.NewKeyCipher("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestAuthKeyDataFromRows: a key comes only from its MySQL rows. The Redis
// hash of old versions (possibly planted while Redis was exposed) is not a
// source, so a forged temporary key bound to someone's permanent key cannot
// come back from it.
func TestAuthKeyDataFromRows(t *testing.T) {
	info := &dataobject.AuthKeyInfosDO{AuthKeyId: 5, AuthKeyType: mtproto.AuthKeyTypeTemp, PermAuthKeyId: 9, ExpiresAt: 1_800_000_000}
	key := &dataobject.AuthKeysDO{AuthKeyId: 5, Body: base64.RawStdEncoding.EncodeToString([]byte{1, 2, 3})}

	k, err := authKeyDataFromRows(5, info, key)
	if err != nil || k.PermAuthKeyId != 9 || k.ExpiresAt != 1_800_000_000 || k.Body != key.Body {
		t.Fatalf("rows: %+v %v", k, err)
	}
	for _, c := range []struct {
		info *dataobject.AuthKeyInfosDO
		key  *dataobject.AuthKeysDO
	}{{nil, key}, {info, nil}, {nil, nil}} {
		if _, err := authKeyDataFromRows(5, c.info, c.key); !errors.Is(err, mtproto.ErrAuthKeyUnregistered) {
			t.Fatalf("missing row: %v", err)
		}
	}
}

func TestSetAuthKeyRequiresTempExpiry(t *testing.T) {
	d := &Dao{}
	for _, keyType := range []int{mtproto.AuthKeyTypeTemp, mtproto.AuthKeyTypeMediaTemp} {
		for _, expiresIn := range []int32{0, -5} {
			err := d.SetAuthKeyV2(context.Background(), mtproto.NewAuthKeyInfo(5, make([]byte, 256), keyType), expiresIn)
			if !errors.Is(err, errTempKeyWithoutExpiry) {
				t.Fatalf("type %d expires_in %d: %v", keyType, expiresIn, err)
			}
		}
	}
}

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

//go:build integration

package dao

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

// hasKey reports whether authsession still knows the key, with the cache.
func hasKey(t *testing.T, d *Dao, id int64) bool {
	t.Helper()
	_, err := d.QueryAuthKeyV2(context.Background(), id)
	switch {
	case err == nil:
		return true
	case errors.Is(err, mtproto.ErrAuthKeyUnregistered):
		return false
	default:
		t.Fatalf("query key %d: %v", id, err)
		return false
	}
}

func bindTemp(t *testing.T, d *Dao, perm, temp int64) {
	t.Helper()
	if err := d.BindTempAuthKey(context.Background(), perm, temp, time.Now().Unix()+3600); err != nil {
		t.Fatalf("bind %d to %d: %v", temp, perm, err)
	}
}

// destroy_auth_key of a permanent key takes its temporary keys and its user;
// of a temporary key, only that key.
func TestDestroyAuthKey(t *testing.T) {
	ctx := context.Background()
	d := newIntegrationDao(t, itFullDB)
	db := openSQL(t, itFullDB)
	perm := storeKey(t, d, mtproto.AuthKeyTypePerm, 0)
	temp1 := storeKey(t, d, mtproto.AuthKeyTypeTemp, 3600)
	temp2 := storeKey(t, d, mtproto.AuthKeyTypeTemp, 3600)
	bindTemp(t, d, perm, temp1)
	bindTemp(t, d, perm, temp2)
	const user = 777001
	if d.BindAuthKeyUser(ctx, perm, user) == 0 {
		t.Fatal("bind user")
	}
	for _, id := range []int64{perm, temp1, temp2} {
		if !hasKey(t, d, id) {
			t.Fatalf("key %d missing before", id)
		}
	}

	// A temporary key alone; its permanent key forgets it as the latest.
	removed, err := d.DestroyAuthKey(ctx, temp2)
	if err != nil || !slices.Equal(removed, []int64{temp2}) {
		t.Fatalf("destroy temp: %v, %v", removed, err)
	}
	if hasKey(t, d, temp2) || !hasKey(t, d, temp1) || !hasKey(t, d, perm) {
		t.Fatal("destroying a temporary key touched other keys")
	}
	if r := readKeyRow(t, db, perm); r.temp == temp2 {
		t.Fatalf("the permanent key still refers to the destroyed key: %+v", r)
	}

	removed, err = d.DestroyAuthKey(ctx, perm)
	slices.Sort(removed)
	want := []int64{perm, temp1}
	slices.Sort(want)
	if err != nil || !slices.Equal(removed, want) {
		t.Fatalf("destroy perm: %v, %v; want %v", removed, err, want)
	}
	if hasKey(t, d, perm) || hasKey(t, d, temp1) {
		t.Fatal("keys of a destroyed permanent key are still served")
	}
	if uid := d.GetAuthKeyUserId(ctx, perm); uid != 0 {
		t.Fatalf("the user is still bound to the destroyed key: %d", uid)
	}

	// Again: nothing is there.
	if removed, err = d.DestroyAuthKey(ctx, perm); err != nil || len(removed) != 0 {
		t.Fatalf("destroy again: %v, %v", removed, err)
	}
}

func TestDropTempAuthKeys(t *testing.T) {
	ctx := context.Background()
	d := newIntegrationDao(t, itFullDB)
	perm := storeKey(t, d, mtproto.AuthKeyTypePerm, 0)
	keep := storeKey(t, d, mtproto.AuthKeyTypeTemp, 3600)
	drop := storeKey(t, d, mtproto.AuthKeyTypeTemp, 3600)
	media := storeKey(t, d, mtproto.AuthKeyTypeMediaTemp, 3600)
	other := storeKey(t, d, mtproto.AuthKeyTypeTemp, 3600) // unbound, someone else's
	for _, id := range []int64{keep, drop, media} {
		bindTemp(t, d, perm, id)
	}

	removed, err := d.DropTempAuthKeys(ctx, perm, []int64{keep})
	slices.Sort(removed)
	want := []int64{drop, media}
	slices.Sort(want)
	if err != nil || !slices.Equal(removed, want) {
		t.Fatalf("dropped %v, %v; want %v", removed, err, want)
	}
	if !hasKey(t, d, perm) || !hasKey(t, d, keep) || !hasKey(t, d, other) || hasKey(t, d, drop) || hasKey(t, d, media) {
		t.Fatal("dropTempAuthKeys removed the wrong keys")
	}
}

// Expired temporary keys are listed for removal; running ones and
// permanent keys are not.
func TestExpiredTempAuthKeys(t *testing.T) {
	ctx := context.Background()
	d := newIntegrationDao(t, itFullDB)
	db := openSQL(t, itFullDB)
	expired := storeKey(t, d, mtproto.AuthKeyTypeTemp, 3600)
	running := storeKey(t, d, mtproto.AuthKeyTypeTemp, 3600)
	perm := storeKey(t, d, mtproto.AuthKeyTypePerm, 0)
	if _, err := db.Exec("UPDATE auth_key_infos SET expires_at = ? WHERE auth_key_id = ?", time.Now().Unix()-1, expired); err != nil {
		t.Fatal(err)
	}

	ids, err := d.ExpiredTempAuthKeys(ctx, time.Now().Unix(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ids, expired) || slices.Contains(ids, running) || slices.Contains(ids, perm) {
		t.Fatalf("expired keys %v: want %d, not %d or %d", ids, expired, running, perm)
	}
	if _, err = d.DeleteAuthKeys(ctx, ids); err != nil {
		t.Fatal(err)
	}
	if ids, _ = d.ExpiredTempAuthKeys(ctx, time.Now().Unix(), 1000); slices.Contains(ids, expired) {
		t.Fatal("still listed after the removal")
	}
	if !hasKey(t, d, running) || !hasKey(t, d, perm) {
		t.Fatal("the removal took a running key")
	}
}

// Keys are sealed at rest: in auth_keys and in the cache. A plain key of
// before task 008 is still read and is sealed on its first read or by
// SealPlainAuthKeys.
func TestAuthKeysSealedAtRest(t *testing.T) {
	ctx := context.Background()
	d := newIntegrationDao(t, itFullDB)
	db := openSQL(t, itFullDB)
	body := func(id int64) string {
		t.Helper()
		var b string
		if err := db.QueryRow("SELECT body FROM auth_keys WHERE auth_key_id = ?", id).Scan(&b); err != nil {
			t.Fatal(err)
		}
		return b
	}

	id := storeKey(t, d, mtproto.AuthKeyTypePerm, 0)
	key, err := d.QueryAuthKeyV2(ctx, id)
	if err != nil || len(key.GetAuthKey()) != 256 {
		t.Fatalf("read: %v", err)
	}
	stored := body(id)
	plain := base64.RawStdEncoding.EncodeToString(key.GetAuthKey())
	if !strings.HasPrefix(stored, "v1:") || strings.Contains(stored, plain[:32]) {
		t.Fatalf("stored body is not sealed: %.16q", stored)
	}
	raw, err := redis.MustNewRedis(itRedis.RedisConf()).GetCtx(ctx, genCacheAuthKeyV2Key(id))
	if err != nil || raw == "" {
		t.Fatalf("cache: %q, %v", raw, err)
	}
	if strings.Contains(raw, plain[:32]) || !strings.Contains(raw, "v1:") {
		t.Fatal("the cache holds the plain key")
	}

	// Plain keys of before task 008.
	legacy := []int64{newKeyID(t), newKeyID(t)}
	legacyKeys := map[int64][]byte{}
	for _, lid := range legacy {
		k := make([]byte, 256)
		_, _ = rand.Read(k)
		legacyKeys[lid] = k
		if _, err = db.Exec("INSERT INTO auth_keys (id, auth_key_id, body) VALUES (?, ?, ?)", lid, lid, base64.RawStdEncoding.EncodeToString(k)); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("INSERT INTO auth_key_infos (id, auth_key_id, auth_key_type) VALUES (?, ?, ?)", lid, lid, mtproto.AuthKeyTypePerm); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.QueryAuthKeyV2(ctx, legacy[0])
	if err != nil || !bytes.Equal(got.GetAuthKey(), legacyKeys[legacy[0]]) {
		t.Fatalf("plain key: %v", err)
	}
	if !strings.HasPrefix(body(legacy[0]), "v1:") {
		t.Fatal("a plain key read is not sealed")
	}
	if strings.HasPrefix(body(legacy[1]), "v1:") {
		t.Fatal("an unread plain key sealed already")
	}
	if _, err = d.SealPlainAuthKeys(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body(legacy[1]), "v1:") {
		t.Fatal("SealPlainAuthKeys left a plain key")
	}
	if got, err = d.QueryAuthKeyV2(ctx, legacy[1]); err != nil || !bytes.Equal(got.GetAuthKey(), legacyKeys[legacy[1]]) {
		t.Fatalf("sealed plain key: %v", err)
	}
}

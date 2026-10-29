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

package twofa

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

// Redis layout. Every operation touches a single key (Lua scripts
// included), so it works with a go-zero kv cluster as well as with a single
// node.
//
//	twofa_srp_<srp_id>          hash {user_id, b, B, fp}: one SRP check, SRPTTL, taken once
//	twofa_new_algo_<user_id>    hex(salt1 prefix | salt2) of new_algo, NewAlgoTTL from the last account.getPassword
//	twofa_login_<perm key id>   hash {user_id, phone}: a login waiting for the password, LoginTTL
//
// fp is the fingerprint of the password the srp_id was issued for.
const (
	srpKeyPrefix     = "twofa_srp_"
	newAlgoKeyPrefix = "twofa_new_algo_"
	loginKeyPrefix   = "twofa_login_"
)

func srpKey(srpId int64) string       { return srpKeyPrefix + strconv.FormatInt(srpId, 10) }
func newAlgoKey(userId int64) string  { return newAlgoKeyPrefix + strconv.FormatInt(userId, 10) }
func loginKey(authKeyId int64) string { return loginKeyPrefix + strconv.FormatInt(authKeyId, 10) }

const (
	// KEYS[1] srp; ARGV user_id, b, B, fp, ttl seconds.
	putSRPScript = `
redis.call('DEL', KEYS[1])
redis.call('HSET', KEYS[1], 'user_id', ARGV[1], 'b', ARGV[2], 'B', ARGV[3], 'fp', ARGV[4])
redis.call('EXPIRE', KEYS[1], ARGV[5])
return 1`

	// KEYS[1] srp. Returns {user_id, b, B, fp} and deletes the key: an
	// srp_id checks one proof, right or wrong. {} if there is no such key.
	takeSRPScript = `
local v = redis.call('HMGET', KEYS[1], 'user_id', 'b', 'B', 'fp')
if not v[1] then return {} end
redis.call('DEL', KEYS[1])
return v`

	// KEYS[1] new algo; ARGV value if there is none, ttl seconds. Returns
	// the value, extending its life.
	newAlgoScript = `
local v = redis.call('GET', KEYS[1])
if not v then
  v = ARGV[1]
  redis.call('SET', KEYS[1], v)
end
redis.call('EXPIRE', KEYS[1], ARGV[2])
return v`

	// KEYS[1] login; ARGV user_id, phone, lang_code, ttl seconds.
	putLoginScript = `
redis.call('DEL', KEYS[1])
redis.call('HSET', KEYS[1], 'user_id', ARGV[1], 'phone', ARGV[2], 'lang_code', ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return 1`
)

// srpSession is what an srp_id stands for: the server's ephemeral values of
// one SRP exchange with userId about the password with fingerprint fp.
type srpSession struct {
	userId int64
	b, B   []byte
	fp     []byte
}

// redisState keeps the short-lived state of the 2FA flows in Redis, shared
// by every bff instance.
type redisState struct {
	kv kv.Store
}

func (r *redisState) putSRP(ctx context.Context, srpId int64, s srpSession, ttl time.Duration) error {
	_, err := r.kv.EvalCtx(ctx, putSRPScript, srpKey(srpId),
		s.userId, hex.EncodeToString(s.b), hex.EncodeToString(s.B), hex.EncodeToString(s.fp), int64(ttl/time.Second))
	return err
}

// takeSRP returns the session of srpId and forgets it; ok is false if there
// is none (never issued, used or expired).
func (r *redisState) takeSRP(ctx context.Context, srpId int64) (s srpSession, ok bool, err error) {
	reply, err := r.kv.EvalCtx(ctx, takeSRPScript, srpKey(srpId))
	if err != nil {
		return s, false, err
	}
	a, isList := reply.([]any)
	if !isList {
		return s, false, fmt.Errorf("twofa: srp session: unexpected reply %T", reply)
	}
	if len(a) == 0 {
		return s, false, nil
	}
	if len(a) != 4 {
		return s, false, fmt.Errorf("twofa: srp session: %d fields", len(a))
	}
	fields := make([]string, 4)
	for i, x := range a {
		if fields[i], isList = x.(string); !isList {
			return s, false, errors.New("twofa: srp session: incomplete")
		}
	}
	if s.userId, err = strconv.ParseInt(fields[0], 10, 64); err != nil {
		return s, false, err
	}
	if s.b, err = hex.DecodeString(fields[1]); err != nil {
		return s, false, err
	}
	if s.B, err = hex.DecodeString(fields[2]); err != nil {
		return s, false, err
	}
	if s.fp, err = hex.DecodeString(fields[3]); err != nil {
		return s, false, err
	}
	return s, true, nil
}

// newAlgo returns the salt1 prefix and salt2 of userId's new_algo, storing
// fresh (the given ones) if there are none, and keeps them ttl more.
func (r *redisState) newAlgo(ctx context.Context, userId int64, fresh []byte, ttl time.Duration) ([]byte, error) {
	reply, err := r.kv.EvalCtx(ctx, newAlgoScript, newAlgoKey(userId), hex.EncodeToString(fresh), int64(ttl/time.Second))
	if err != nil {
		return nil, err
	}
	s, ok := reply.(string)
	if !ok {
		return nil, fmt.Errorf("twofa: new algo: unexpected reply %T", reply)
	}
	return decodeNewAlgo(s)
}

// issuedNewAlgo returns the new_algo salts issued to userId, or nil if none
// is live.
func (r *redisState) issuedNewAlgo(ctx context.Context, userId int64) ([]byte, error) {
	s, err := r.kv.GetCtx(ctx, newAlgoKey(userId))
	if err != nil || s == "" {
		return nil, err
	}
	return decodeNewAlgo(s)
}

func decodeNewAlgo(s string) ([]byte, error) {
	v, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(v) != newSalt1PrefixLen+salt2Len {
		return nil, fmt.Errorf("twofa: new algo of %d bytes", len(v))
	}
	return v, nil
}

// dropNewAlgo forgets userId's new_algo: the next account.getPassword issues
// new salts.
func (r *redisState) dropNewAlgo(ctx context.Context, userId int64) error {
	_, err := r.kv.DelCtx(ctx, newAlgoKey(userId))
	return err
}

func (r *redisState) putLogin(ctx context.Context, authKeyId int64, l Login, ttl time.Duration) error {
	_, err := r.kv.EvalCtx(ctx, putLoginScript, loginKey(authKeyId), l.UserId, l.Phone, l.LangCode, int64(ttl/time.Second))
	return err
}

func (r *redisState) login(ctx context.Context, authKeyId int64) (*Login, error) {
	m, err := r.kv.HgetallCtx(ctx, loginKey(authKeyId))
	if err != nil || len(m) == 0 {
		return nil, err
	}
	userId, err := strconv.ParseInt(m["user_id"], 10, 64)
	if err != nil || userId == 0 {
		return nil, fmt.Errorf("twofa: login of key %d without a user", authKeyId)
	}
	return &Login{UserId: userId, Phone: m["phone"], LangCode: m["lang_code"]}, nil
}

func (r *redisState) dropLogin(ctx context.Context, authKeyId int64) error {
	_, err := r.kv.DelCtx(ctx, loginKey(authKeyId))
	return err
}

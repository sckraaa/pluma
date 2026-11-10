package logincode

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

// Redis layout. Every operation touches a single key (Lua scripts included),
// so it works with a go-zero kv cluster as well as with a single node.
//
//	login_code_<phone_code_hash>     hash {phone, hmac, expires_at, admin_hmac, admin_expires_at, attempts, created_at, ip}
//	login_code_requests_<phone>      hash {phone_code_hash: created_at} of the pending requests
//
// Both keys live TransactionTTL (the requests hash from its latest request;
// older entries are pruned). The plaintext code is never stored.
const (
	secretKeyPrefix   = "login_code_"
	requestsKeyPrefix = "login_code_requests_"
)

func secretKey(phoneCodeHash string) string { return secretKeyPrefix + phoneCodeHash }
func requestsKey(phone string) string       { return requestsKeyPrefix + phone }

// secret is the stored state of one request and its codes. A request has
// two slots: the code of the app or of the person's Telegram (Hmac), and the
// administrator's (AdminHmac). Either one is accepted, so a code the
// administrator gave out stays valid when the person asks Telegram again,
// and the other way round.
type secret struct {
	Phone          string
	Hmac           string
	ExpiresAt      int64 // unix seconds
	AdminHmac      string
	AdminExpiresAt int64 // unix seconds
	Attempts       int64
	CreatedAt      int64 // unix seconds, when the code was requested
	ClientIp       string
}

// live reports whether the slot holds a code that can still be matched at
// unix time now.
func live(hmac string, expiresAt, now int64) bool { return hmac != "" && now <= expiresAt }

const (
	// KEYS[1] secret; ARGV phone, hmac, expires_at, created_at, ip, ttl seconds.
	putScript = `
redis.call('DEL', KEYS[1])
redis.call('HSET', KEYS[1], 'phone', ARGV[1], 'hmac', ARGV[2], 'expires_at', ARGV[3], 'attempts', 0, 'created_at', ARGV[4], 'ip', ARGV[5])
redis.call('EXPIRE', KEYS[1], ARGV[6])
return 1`

	// KEYS[1] secret; ARGV now (unix seconds). Counts one check of the code
	// and returns {attempts, hmac, expires_at, phone, admin_hmac,
	// admin_expires_at}, or {-1} if there is no such request. The counter is
	// incremented before the caller compares the code, so concurrent requests
	// never get more than MaxAttempts comparisons. Only a check that can
	// match is counted: a request without a live code in either slot
	// (waiting for the administrator, or its codes expired) uses up nothing.
	attemptScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then return {-1, '', '0', '', '', '0'} end
local v = redis.call('HMGET', KEYS[1], 'hmac', 'expires_at', 'phone', 'attempts', 'admin_hmac', 'admin_expires_at')
local now = tonumber(ARGV[1])
local n = tonumber(v[4] or '0') or 0
local function live(h, e) return h and h ~= '' and (tonumber(e or '0') or 0) >= now end
if live(v[1], v[2]) or live(v[5], v[6]) then
  n = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
end
return {n, v[1] or '', v[2] or '0', v[3] or '', v[5] or '', v[6] or '0'}`

	// KEYS[1] secret; ARGV slot field, its expiry field, hmac, expires_at.
	// Replaces the code in one slot of a pending request; the attempt counter
	// and the other slot are kept.
	replaceScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
redis.call('HSET', KEYS[1], ARGV[1], ARGV[3], ARGV[2], ARGV[4])
return 1`

	// KEYS[1] requests; ARGV phone_code_hash, created_at, ttl seconds. Adds a
	// pending request and drops entries older than ttl (their codes are gone).
	addRequestScript = `
local now = tonumber(ARGV[2])
local all = redis.call('HGETALL', KEYS[1])
for i = 1, #all, 2 do
  if (tonumber(all[i + 1]) or 0) + tonumber(ARGV[3]) <= now then redis.call('HDEL', KEYS[1], all[i]) end
end
redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
return 1`
)

// Store keeps issued login codes (as HMACs) in Redis.
type Store struct {
	kv kv.Store
}

// NewStore returns a Store on kv (the KV config of bff).
func NewStore(kv kv.Store) *Store {
	return &Store{kv: kv}
}

func (s *Store) put(ctx context.Context, phoneCodeHash string, v secret, ttl time.Duration) error {
	_, err := s.kv.EvalCtx(ctx, putScript, secretKey(phoneCodeHash),
		v.Phone, v.Hmac, v.ExpiresAt, v.CreatedAt, v.ClientIp, int64(ttl/time.Second))
	return err
}

// attempt counts one check of the code of phoneCodeHash at unix time now and
// returns the stored state with the updated counter; ok is false if there is
// no such request.
func (s *Store) attempt(ctx context.Context, phoneCodeHash string, now int64) (v secret, ok bool, err error) {
	r, err := s.kv.EvalCtx(ctx, attemptScript, secretKey(phoneCodeHash), now)
	if err != nil {
		return v, false, err
	}
	a, isList := r.([]any)
	if !isList || len(a) != 6 {
		return v, false, fmt.Errorf("login code attempt: unexpected reply %T", r)
	}
	if v.Attempts, err = toInt64(a[0]); err != nil {
		return v, false, err
	}
	if v.Attempts < 0 {
		return v, false, nil
	}
	v.Hmac, _ = a[1].(string)
	if v.ExpiresAt, err = toInt64(a[2]); err != nil {
		return v, false, err
	}
	v.Phone, _ = a[3].(string)
	v.AdminHmac, _ = a[4].(string)
	if v.AdminExpiresAt, err = toInt64(a[5]); err != nil {
		return v, false, err
	}
	return v, true, nil
}

// replace puts a new code of the app or of the person's Telegram into a
// pending request; ok is false if there is no such request.
func (s *Store) replace(ctx context.Context, phoneCodeHash, hmac string, expiresAt int64) (bool, error) {
	return s.replaceSlot(ctx, phoneCodeHash, "hmac", "expires_at", hmac, expiresAt)
}

// replaceAdmin puts the administrator's code into a pending request.
func (s *Store) replaceAdmin(ctx context.Context, phoneCodeHash, hmac string, expiresAt int64) (bool, error) {
	return s.replaceSlot(ctx, phoneCodeHash, "admin_hmac", "admin_expires_at", hmac, expiresAt)
}

func (s *Store) replaceSlot(ctx context.Context, phoneCodeHash, field, expiresField, hmac string, expiresAt int64) (bool, error) {
	r, err := s.kv.EvalCtx(ctx, replaceScript, secretKey(phoneCodeHash), field, expiresField, hmac, expiresAt)
	if err != nil {
		return false, err
	}
	n, err := toInt64(r)
	return n == 1, err
}

// get returns the stored state of the code of phoneCodeHash, or ok = false.
func (s *Store) get(ctx context.Context, phoneCodeHash string) (v secret, ok bool, err error) {
	m, err := s.kv.HgetallCtx(ctx, secretKey(phoneCodeHash))
	if err != nil || len(m) == 0 {
		return v, false, err
	}
	v.Phone = m["phone"]
	v.Hmac = m["hmac"]
	v.AdminHmac = m["admin_hmac"]
	v.ClientIp = m["ip"]
	v.ExpiresAt, _ = strconv.ParseInt(m["expires_at"], 10, 64)
	v.AdminExpiresAt, _ = strconv.ParseInt(m["admin_expires_at"], 10, 64)
	v.Attempts, _ = strconv.ParseInt(m["attempts"], 10, 64)
	v.CreatedAt, _ = strconv.ParseInt(m["created_at"], 10, 64)
	return v, true, nil
}

func (s *Store) delete(ctx context.Context, phoneCodeHash string) error {
	_, err := s.kv.DelCtx(ctx, secretKey(phoneCodeHash))
	return err
}

func (s *Store) addRequest(ctx context.Context, phone, phoneCodeHash string, createdAt int64, ttl time.Duration) error {
	_, err := s.kv.EvalCtx(ctx, addRequestScript, requestsKey(phone), phoneCodeHash, createdAt, int64(ttl/time.Second))
	return err
}

// requests returns the phone_code_hash of every request of phone that may
// still be pending (some may have been used or expired since).
func (s *Store) requests(ctx context.Context, phone string) ([]string, error) {
	m, err := s.kv.HgetallCtx(ctx, requestsKey(phone))
	if err != nil {
		return nil, err
	}
	hashes := make([]string, 0, len(m))
	for h := range m {
		hashes = append(hashes, h)
	}
	return hashes, nil
}

func (s *Store) removeRequest(ctx context.Context, phone, phoneCodeHash string) error {
	_, err := s.kv.HdelCtx(ctx, requestsKey(phone), phoneCodeHash)
	return err
}

// toInt64 converts an integer from a Redis reply (an integer or a decimal string).
func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case string:
		return strconv.ParseInt(x, 10, 64)
	case nil:
		return 0, errors.New("redis: nil reply")
	default:
		return 0, fmt.Errorf("redis: unexpected reply %T", v)
	}
}

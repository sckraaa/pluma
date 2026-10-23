//go:build integration

package logincode

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// The Lua scripts of the store and the limiter run on the production Redis
// here; the unit tests run them on miniredis, whose Lua and command
// semantics are an imitation (e.g. HMGET of a missing field).

var itRedis *testinfra.Redis

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itRedis, err = testinfra.StartRedis(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itRedis.Terminate(ctx)
	os.Exit(code)
}

// itPhoneSeq and itIPSeq make phones and client addresses unique across the
// tests sharing the server, also when they run again (go test -count=N): the
// limits count per phone and per address.
var itPhoneSeq, itIPSeq atomic.Int64

func itPhone() string {
	return fmt.Sprintf("1201555%04d", 1000+itPhoneSeq.Add(1))
}

// itIP returns an address of the documentation ranges (RFC 5737).
func itIP() string {
	n := itIPSeq.Add(1) - 1
	nets := []string{"192.0.2", "198.51.100", "203.0.113"}
	return fmt.Sprintf("%s.%d", nets[n/254%3], n%254+1)
}

// itService returns a Service on the real Redis with phones allowed.
func itService(t *testing.T, allowed ...string) (*Service, *redis.Redis) {
	t.Helper()
	allow := fakeAllowlist{}
	for _, p := range allowed {
		allow[p] = true
	}
	s, err := NewService(Config{HmacKey: testKey}, allow, kv.NewStore(itRedis.KvConf()))
	if err != nil {
		t.Fatal(err)
	}
	return s, redis.MustNewRedis(itRedis.RedisConf())
}

func TestRedisIssueAndVerify(t *testing.T) {
	ctx := context.Background()
	phone := itPhone()
	s, rds := itService(t, phone)
	hash := "h" + phone

	ip := itIP()
	code, err := s.Issue(ctx, phone, hash, ip, true)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := rds.HgetallCtx(ctx, secretKey(hash))
	if err != nil {
		t.Fatal(err)
	}
	if fields["phone"] != phone || fields["hmac"] == "" || fields["attempts"] != "0" || fields["ip"] != ip {
		t.Fatalf("stored request %v", fields)
	}
	for k, v := range fields {
		if strings.Contains(v, code) {
			t.Fatalf("plaintext code stored in field %s", k)
		}
	}
	for _, key := range []string{secretKey(hash), requestsKey(phone)} {
		ttl, err := rds.TtlCtx(ctx, key)
		if err != nil || ttl < int(TransactionTTL/time.Second)-5 || ttl > int(TransactionTTL/time.Second) {
			t.Fatalf("TTL of %s = %d, %v; want about %s", key, ttl, err, TransactionTTL)
		}
	}

	if got := rpcMessage(s.Verify(ctx, phone, hash, wrongCode(code), ip)); got != "PHONE_CODE_INVALID" {
		t.Fatalf("wrong code: %s", got)
	}
	if n, _ := rds.HgetCtx(ctx, secretKey(hash), "attempts"); n != "1" {
		t.Fatalf("attempts after a wrong code = %s", n)
	}
	if err = s.Verify(ctx, phone, hash, code, ip); err != nil {
		t.Fatalf("right code: %v", err)
	}
	if ok, _ := rds.ExistsCtx(ctx, secretKey(hash)); ok {
		t.Fatal("used request kept")
	}
	if got := rpcMessage(s.Verify(ctx, phone, hash, code, ip)); got != "PHONE_CODE_EXPIRED" {
		t.Fatalf("second use of the code: %s", got)
	}
}

// TestRedisConcurrentAttempts: every concurrent check gets its own attempt
// number from the script, so no more than MaxAttempts codes are compared.
func TestRedisConcurrentAttempts(t *testing.T) {
	ctx := context.Background()
	phone := itPhone()
	s, _ := itService(t, phone)
	hash := "h" + phone
	if _, err := s.Issue(ctx, phone, hash, "", true); err != nil {
		t.Fatal(err)
	}
	now := s.now().Unix()

	const n = 50
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = map[int64]bool{}
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, ok, err := s.store.attempt(ctx, hash, now)
			if err != nil || !ok {
				t.Errorf("attempt: %v %v", ok, err)
				return
			}
			mu.Lock()
			seen[v.Attempts] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	for i := int64(1); i <= n; i++ {
		if !seen[i] {
			t.Fatalf("attempt numbers are not 1..%d: %v", n, seen)
		}
	}
}

// TestRedisAdministratorPath: a request without a code (the user has no
// other session) counts no checks until the administrator issues one.
func TestRedisAdministratorPath(t *testing.T) {
	ctx := context.Background()
	phone := itPhone()
	s, rds := itService(t, phone)
	hash := "h" + phone
	ip := itIP()
	if _, err := s.Issue(ctx, phone, hash, ip, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2*MaxAttempts; i++ {
		if got := rpcMessage(s.Verify(ctx, phone, hash, "123456", ip)); got != "PHONE_CODE_INVALID" {
			t.Fatalf("check before the code is issued: %s", got)
		}
	}
	if n, _ := rds.HgetCtx(ctx, secretKey(hash), "attempts"); n != "0" {
		t.Fatalf("attempts = %s: checks of a request without a code were counted", n)
	}

	pending, err := s.Pending(ctx, phone)
	if err != nil || len(pending) != 1 || pending[0].HasCode || pending[0].ClientIp != ip {
		t.Fatalf("Pending: %+v %v", pending, err)
	}
	r, err := s.Reissue(ctx, phone, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Verify(ctx, phone, hash, r.Code, ip); err != nil {
		t.Fatalf("code issued by the administrator: %v", err)
	}
	if pending, err = s.Pending(ctx, phone); err != nil || len(pending) != 0 {
		t.Fatalf("Pending after login: %+v %v", pending, err)
	}
}

// TestRedisRequestsPruned: the requests hash drops entries older than
// TransactionTTL when a new request comes.
func TestRedisRequestsPruned(t *testing.T) {
	ctx := context.Background()
	phone := itPhone()
	s, rds := itService(t, phone)
	now := time.Now().Unix()
	if err := s.store.addRequest(ctx, phone, "old", now-int64(TransactionTTL/time.Second)-1, TransactionTTL); err != nil {
		t.Fatal(err)
	}
	if err := s.store.addRequest(ctx, phone, "new", now, TransactionTTL); err != nil {
		t.Fatal(err)
	}
	fields, err := rds.HgetallCtx(ctx, requestsKey(phone))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["old"]; ok || fields["new"] == "" {
		t.Fatalf("requests %v: want only the new one", fields)
	}
}

func TestRedisLimiter(t *testing.T) {
	ctx := context.Background()
	s, rds := itService(t)
	rule := Rule{Name: "it_test", Limit: 3, Window: time.Hour}
	subject := "subject-" + itPhone()

	for i := 0; i < 3; i++ {
		if err := s.limiter.Hit(ctx, rule, subject); err != nil {
			t.Fatalf("hit %d: %v", i+1, err)
		}
	}
	err := s.limiter.Hit(ctx, rule, subject)
	if got := rpcMessage(err); !strings.HasPrefix(got, "FLOOD_WAIT_") {
		t.Fatalf("over the limit: %v", err)
	}
	if ttl, _ := rds.TtlCtx(ctx, floodKey(rule, subject)); ttl <= 0 || ttl > 3600 {
		t.Fatalf("counter TTL %d: the window starts at the first hit", ttl)
	}

	s.limiter.Refund(ctx, rule, subject)
	s.limiter.Refund(ctx, rule, subject)
	if v, _ := rds.GetCtx(ctx, floodKey(rule, subject)); v != "2" {
		t.Fatalf("counter after two refunds = %q, want 2", v)
	}
	if err = s.limiter.Reset(ctx, rule, subject); err != nil {
		t.Fatal(err)
	}
	if ok, _ := rds.ExistsCtx(ctx, floodKey(rule, subject)); ok {
		t.Fatal("Reset kept the counter")
	}
	// A refund never creates a counter.
	s.limiter.Refund(ctx, rule, subject)
	if ok, _ := rds.ExistsCtx(ctx, floodKey(rule, subject)); ok {
		t.Fatal("Refund created a counter")
	}
}

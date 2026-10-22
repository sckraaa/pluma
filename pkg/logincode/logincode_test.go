package logincode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/grpc/status"
)

const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type fakeAllowlist map[string]bool

func (f fakeAllowlist) Contains(_ context.Context, phone string) (bool, error) {
	return f[phone], nil
}

type brokenAllowlist struct{}

func (brokenAllowlist) Contains(context.Context, string) (bool, error) {
	return false, errors.New("mysql down")
}

func newTestKV(t *testing.T) (*miniredis.Miniredis, kv.Store) {
	t.Helper()
	// miniredis does not implement CLIENT SETINFO; DisableIdentity keeps the failing
	// connect-time command from tripping the go-zero redis breaker.
	mr := miniredis.RunT(t)
	return mr, kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
}

// testService returns a Service whose clock the test controls.
func testService(t *testing.T, allowed ...string) (*Service, *miniredis.Miniredis, *time.Time) {
	t.Helper()
	mr, store := newTestKV(t)
	allow := fakeAllowlist{}
	for _, p := range allowed {
		allow[p] = true
	}
	s, err := NewService(Config{HmacKey: testKey}, allow, store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	return s, mr, &now
}

// rpcMessage returns the TL error message carried by err ("" for nil).
func rpcMessage(err error) string {
	if err == nil {
		return ""
	}
	if st, ok := status.FromError(err); ok {
		return st.Message()
	}
	return err.Error()
}

func TestGenerateFormatAndDistribution(t *testing.T) {
	const n = 50000
	var (
		seen   = make(map[string]int, n)
		counts [Length][10]int
	)
	for i := 0; i < n; i++ {
		code, err := Generate()
		if err != nil {
			t.Fatal(err)
		}
		if !wellFormed(code) {
			t.Fatalf("code %q is not %d digits", code, Length)
		}
		seen[code]++
		for p := 0; p < Length; p++ {
			counts[p][code[p]-'0']++
		}
	}
	// Every digit in every position should appear ~n/10 times; ±15% is far
	// outside random noise (sd ≈ 1.3%) but catches a biased generator.
	for p := range counts {
		for d, c := range counts[p] {
			if c < n/10*85/100 || c > n/10*115/100 {
				t.Errorf("digit %d at position %d appeared %d times out of %d", d, p, c, n)
			}
		}
	}
	// 50k draws from 10^6 values: ~48.8k distinct expected.
	if len(seen) < 45000 {
		t.Errorf("only %d distinct codes out of %d", len(seen), n)
	}
}

func TestGenerateKeepsLeadingZeros(t *testing.T) {
	code, err := generate(bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if code != strings.Repeat("0", Length) {
		t.Fatalf("got %q, want %d zeros", code, Length)
	}
	if _, err = generate(bytes.NewReader(nil)); err == nil {
		t.Fatal("expected an error from an empty random source")
	}
}

func TestWellFormed(t *testing.T) {
	for code, want := range map[string]bool{
		"123456":  true,
		"000000":  true,
		"12345":   false,
		"1234567": false,
		"12345a":  false,
		" 12345":  false,
		"１２３４５６":  false, // full-width digits
		"":        false,
	} {
		if got := wellFormed(code); got != want {
			t.Errorf("wellFormed(%q) = %v, want %v", code, got, want)
		}
	}
}

func TestHasher(t *testing.T) {
	if _, err := NewHasher(""); err == nil {
		t.Fatal("empty key accepted")
	}
	if _, err := NewHasher(strings.Repeat("a", MinHmacKeyLength-1)); err == nil {
		t.Fatal("short key accepted")
	}
	h, err := NewHasher(testKey)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := NewHasher(strings.Repeat("b", MinHmacKeyLength))

	sum := h.Sum("hash1", "79990000001", "123456")
	if strings.Contains(sum, "123456") || len(sum) != 64 {
		t.Fatalf("unexpected sum %q", sum)
	}
	tests := []struct {
		name              string
		hasher            *Hasher
		hash, phone, code string
		sum               string
		want              bool
	}{
		{"match", h, "hash1", "79990000001", "123456", sum, true},
		{"wrong code", h, "hash1", "79990000001", "123457", sum, false},
		{"other phone", h, "hash1", "79990000002", "123456", sum, false},
		{"other request", h, "hash2", "79990000001", "123456", sum, false},
		{"other key", other, "hash1", "79990000001", "123456", sum, false},
		{"field boundary", h, "hash17", "9990000001", "123456", sum, false},
		{"not hex", h, "hash1", "79990000001", "123456", "zz", false},
		{"empty sum", h, "hash1", "79990000001", "123456", "", false},
	}
	for _, tt := range tests {
		if got := tt.hasher.Verify(tt.hash, tt.phone, tt.code, tt.sum); got != tt.want {
			t.Errorf("%s: Verify = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestNewServiceRequiresKey(t *testing.T) {
	_, store := newTestKV(t)
	if _, err := NewService(Config{}, fakeAllowlist{}, store); err == nil {
		t.Fatal("service started without an HMAC key")
	}
}

func TestIssueStoresOnlyHmac(t *testing.T) {
	s, mr, _ := testService(t, "79990000001")
	ctx := context.Background()
	code, err := s.Issue(ctx, "79990000001", "h1", "10.0.0.1", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range mr.Keys() {
		var v string
		if mr.Type(k) == "hash" {
			fields, _ := mr.HKeys(k)
			for _, f := range fields {
				v += f + "=" + mr.HGet(k, f) + ";"
			}
		} else {
			v, _ = mr.Get(k)
		}
		if strings.Contains(v, code) {
			t.Fatalf("plaintext code stored in %s", k)
		}
	}
	if ttl := mr.TTL(secretKey("h1")); ttl != TransactionTTL {
		t.Fatalf("secret TTL %v, want %v", ttl, TransactionTTL)
	}
	if got := mr.HGet(requestsKey("79990000001"), "h1"); got == "" {
		t.Fatal("request not registered as pending")
	}
	if ttl := mr.TTL(requestsKey("79990000001")); ttl != TransactionTTL {
		t.Fatalf("requests TTL %v, want %v", ttl, TransactionTTL)
	}
}

// wrongCode returns a well-formed code other than code.
func wrongCode(code string) string {
	if code == strings.Repeat("0", Length) {
		return strings.Repeat("1", Length)
	}
	return strings.Repeat("0", Length)
}

// clearFlood removes every flood counter, for tests about something else.
func clearFlood(mr *miniredis.Miniredis) {
	for _, k := range mr.Keys() {
		if strings.HasPrefix(k, floodKeyPrefix) {
			mr.Del(k)
		}
	}
}

func TestVerify(t *testing.T) {
	ctx := context.Background()
	const phone = "79990000001"

	t.Run("correct code once", func(t *testing.T) {
		s, mr, _ := testService(t, phone)
		code, _ := s.Issue(ctx, phone, "h1", "", true)
		if err := s.Verify(ctx, phone, "h1", code, ""); err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if mr.Exists(secretKey("h1")) || mr.HGet(requestsKey(phone), "h1") != "" {
			t.Fatal("used code not removed")
		}
		if got := rpcMessage(s.Verify(ctx, phone, "h1", code, "")); got != "PHONE_CODE_EXPIRED" {
			t.Fatalf("second use: %s", got)
		}
	})

	t.Run("wrong codes then expired", func(t *testing.T) {
		s, mr, _ := testService(t, phone)
		code, _ := s.Issue(ctx, phone, "h1", "", true)
		for i := 1; i <= MaxAttempts; i++ {
			if got := rpcMessage(s.Verify(ctx, phone, "h1", wrongCode(code), "")); got != "PHONE_CODE_INVALID" {
				t.Fatalf("attempt %d: %s", i, got)
			}
			clearFlood(mr) // this test is about the per-request counter
		}
		// Out of attempts: even the right code is refused.
		if got := rpcMessage(s.Verify(ctx, phone, "h1", code, "")); got != "PHONE_CODE_EXPIRED" {
			t.Fatalf("after %d wrong attempts: %s", MaxAttempts, got)
		}
	})

	t.Run("expired code is not counted", func(t *testing.T) {
		s, mr, now := testService(t, phone)
		code, _ := s.Issue(ctx, phone, "h1", "", true)
		*now = now.Add(CodeTTL + time.Second)
		for i := 0; i < 2*MaxAttempts; i++ {
			if got := rpcMessage(s.Verify(ctx, phone, "h1", code, "")); got != "PHONE_CODE_EXPIRED" {
				t.Fatalf("got %s", got)
			}
		}
		if got := mr.HGet(secretKey("h1"), "attempts"); got != "0" {
			t.Fatalf("attempts = %s, want 0", got)
		}
		if mr.Exists(floodKey(CheckCodePerPhone, phone)) {
			t.Fatal("checks of an expired code counted against the phone")
		}
	})

	t.Run("malformed and other-phone codes count as wrong", func(t *testing.T) {
		s, mr, _ := testService(t, phone, "79990000002")
		code, _ := s.Issue(ctx, phone, "h1", "", true)
		for _, c := range []string{"", code[1:], code + "0", " " + code[1:]} {
			if got := rpcMessage(s.Verify(ctx, phone, "h1", c, "")); got != "PHONE_CODE_INVALID" {
				t.Fatalf("code %q: %s", c, got)
			}
		}
		if got := rpcMessage(s.Verify(ctx, "79990000002", "h1", code, "")); got != "PHONE_CODE_INVALID" {
			t.Fatalf("other phone: %s", got)
		}
		// Five wrong checks used the request up.
		if mr.Exists(secretKey("h1")) || mr.HGet(requestsKey(phone), "h1") != "" {
			t.Fatal("request kept after the last attempt")
		}
		if got := rpcMessage(s.Verify(ctx, phone, "h1", code, "")); got != "PHONE_CODE_EXPIRED" {
			t.Fatalf("right code after the last attempt: %s", got)
		}
	})

	t.Run("unknown request", func(t *testing.T) {
		s, _, _ := testService(t, phone)
		if got := rpcMessage(s.Verify(ctx, phone, "nope", "123456", "")); got != "PHONE_CODE_EXPIRED" {
			t.Fatalf("got %s", got)
		}
	})
}

func TestConcurrentAttemptsAreCountedAtomically(t *testing.T) {
	s, _, _ := testService(t, "79990000001")
	ctx := context.Background()
	now := s.now().Unix()
	if _, err := s.Issue(ctx, "79990000001", "h1", "", true); err != nil {
		t.Fatal(err)
	}
	// Verify compares a code only when its attempt number is <= MaxAttempts,
	// so the guarantee rests on every concurrent check getting its own number.
	const n = 40
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = map[int64]bool{}
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, ok, err := s.store.attempt(ctx, "h1", now)
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
	code, _ := s.Issue(ctx, "79990000001", "h2", "", true)
	for i := 0; i <= MaxAttempts; i++ {
		_, _, _ = s.store.attempt(ctx, "h2", now)
	}
	if got := rpcMessage(s.Verify(ctx, "79990000001", "h2", code, "")); got != "PHONE_CODE_EXPIRED" {
		t.Fatalf("right code past the attempt limit: %s", got)
	}
}

func TestPendingRequestWaitsForAdministrator(t *testing.T) {
	s, mr, now := testService(t, "79990000001")
	ctx := context.Background()

	code, err := s.Issue(ctx, "79990000001", "h1", "10.0.0.7", false)
	if err != nil || code != "" {
		t.Fatalf("Issue without code = %q, %v", code, err)
	}
	// No code exists yet: nothing matches and nothing is counted.
	for _, c := range []string{"000000", "123456", "999999"} {
		if got := rpcMessage(s.Verify(ctx, "79990000001", "h1", c, "")); got != "PHONE_CODE_INVALID" {
			t.Fatalf("before reissue: %s", got)
		}
	}
	if got := mr.HGet(secretKey("h1"), "attempts"); got != "0" {
		t.Fatalf("attempts = %s, want 0", got)
	}
	if mr.Exists(floodKey(CheckCodePerPhone, "79990000001")) {
		t.Fatal("checks of a request without a code counted against the phone")
	}

	r, err := s.Reissue(ctx, "79990000001", "")
	if err != nil {
		t.Fatal(err)
	}
	if !wellFormed(r.Code) || r.ClientIp != "10.0.0.7" || !r.RequestedAt.Equal(*now) || !r.ExpiresAt.Equal(now.Add(CodeTTL)) {
		t.Fatalf("unexpected reissue %+v", r)
	}
	// A second reissue replaces the code.
	r2, err := s.Reissue(ctx, "79990000001", "")
	if err != nil {
		t.Fatal(err)
	}
	if r2.Code != r.Code {
		if got := rpcMessage(s.Verify(ctx, "79990000001", "h1", r.Code, "")); got != "PHONE_CODE_INVALID" {
			t.Fatalf("replaced code: %s", got)
		}
	}
	if err := s.Verify(ctx, "79990000001", "h1", r2.Code, ""); err != nil {
		t.Fatalf("reissued code: %v", err)
	}
	if _, err := s.Reissue(ctx, "79990000001", ""); !errors.Is(err, ErrNoPendingRequest) {
		t.Fatalf("reissue after use: %v", err)
	}
}

// TestReissueChoosesRequest: anyone who knows the phone can create a request,
// so the administrator chooses which one gets the code.
func TestReissueChoosesRequest(t *testing.T) {
	s, _, now := testService(t, "79990000001")
	ctx := context.Background()
	if _, err := s.Reissue(ctx, "79990000001", ""); !errors.Is(err, ErrNoPendingRequest) {
		t.Fatalf("no request: %v", err)
	}
	if _, err := s.Reissue(ctx, "79990000009", ""); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("not allowed: %v", err)
	}
	_, _ = s.Issue(ctx, "79990000001", "aaaaaaaa11", "198.51.100.1", false)
	*now = now.Add(time.Minute)
	_, _ = s.Issue(ctx, "79990000001", "aaaaaaaa22", "203.0.113.9", false)

	list, err := s.Pending(ctx, "79990000001")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ClientIp != "198.51.100.1" || list[1].ClientIp != "203.0.113.9" ||
		list[0].Id != "aaaaaaaa1" || list[1].Id != "aaaaaaaa2" {
		t.Fatalf("pending = %+v", list)
	}
	if _, err := s.Reissue(ctx, "79990000001", ""); !errors.Is(err, ErrAmbiguousRequest) {
		t.Fatalf("two requests, none chosen: %v", err)
	}
	if _, err := s.Reissue(ctx, "79990000001", "aaaaaaaa"); !errors.Is(err, ErrAmbiguousRequest) {
		t.Fatalf("ambiguous id: %v", err)
	}
	if _, err := s.Reissue(ctx, "79990000001", "bbbb"); !errors.Is(err, ErrNoPendingRequest) {
		t.Fatalf("unknown id: %v", err)
	}
	r, err := s.Reissue(ctx, "79990000001", list[0].Id)
	if err != nil {
		t.Fatal(err)
	}
	if r.ClientIp != "198.51.100.1" {
		t.Fatalf("reissued %+v", r)
	}
	if got := rpcMessage(s.Verify(ctx, "79990000001", "aaaaaaaa22", r.Code, "")); got != "PHONE_CODE_INVALID" {
		t.Fatalf("code works for the other request: %s", got)
	}
	if err := s.Verify(ctx, "79990000001", "aaaaaaaa11", r.Code, ""); err != nil {
		t.Fatalf("chosen request: %v", err)
	}

	// A used or cancelled request is not pending any more.
	_, _ = s.Issue(ctx, "79990000001", "h3", "", false)
	s.Cancel(ctx, "79990000001", "h3")
	list, _ = s.Pending(ctx, "79990000001")
	if len(list) != 1 || list[0].phoneCodeHash != "aaaaaaaa22" {
		t.Fatalf("pending after use and cancel = %+v", list)
	}
	r, err = s.Reissue(ctx, "79990000001", "")
	if err != nil || r.ClientIp != "203.0.113.9" {
		t.Fatalf("single request left: %+v %v", r, err)
	}
}

// TestReissueRefusesUselessCodes: the administrator never gets a code that
// cannot work or a wrong validity time.
func TestReissueRefusesUselessCodes(t *testing.T) {
	ctx := context.Background()
	const phone = "79990000001"

	t.Run("validity clamped to the request", func(t *testing.T) {
		s, _, now := testService(t, phone)
		start := *now
		_, _ = s.Issue(ctx, phone, "h1", "", false)
		*now = start.Add(TransactionTTL - 5*time.Minute)
		r, err := s.Reissue(ctx, phone, "")
		if err != nil {
			t.Fatal(err)
		}
		if !r.ExpiresAt.Equal(start.Add(TransactionTTL)) {
			t.Fatalf("expires at %v, request ends at %v", r.ExpiresAt, start.Add(TransactionTTL))
		}
		*now = start.Add(TransactionTTL - ReissueMinTime + time.Second)
		if _, err := s.Reissue(ctx, phone, ""); !errors.Is(err, ErrRequestEnding) {
			t.Fatalf("request about to end: %v", err)
		}
	})

	t.Run("checks of an expired code leave the request usable", func(t *testing.T) {
		s, _, now := testService(t, phone)
		code, _ := s.Issue(ctx, phone, "h1", "", true)
		*now = now.Add(CodeTTL + time.Second)
		for i := 0; i < 2*MaxAttempts; i++ {
			_ = s.Verify(ctx, phone, "h1", code, "")
		}
		r, err := s.Reissue(ctx, phone, "")
		if err != nil {
			t.Fatal(err)
		}
		if r.Attempts != 0 {
			t.Fatalf("attempts = %d", r.Attempts)
		}
		if err := s.Verify(ctx, phone, "h1", r.Code, ""); err != nil {
			t.Fatalf("reissued code: %v", err)
		}
	})

	t.Run("no checks left", func(t *testing.T) {
		s, mr, _ := testService(t, phone)
		_, _ = s.Issue(ctx, phone, "h1", "", false)
		mr.HSet(secretKey("h1"), "attempts", fmt.Sprint(MaxAttempts))
		if _, err := s.Reissue(ctx, phone, ""); !errors.Is(err, ErrRequestUsedUp) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestCheckSendCode(t *testing.T) {
	ctx := context.Background()

	t.Run("allowlist", func(t *testing.T) {
		s, _, _ := testService(t, "79990000001")
		if err := s.CheckSendCode(ctx, "79990000001", 1, "10.0.0.1"); err != nil {
			t.Fatalf("allowed phone: %v", err)
		}
		if got := rpcMessage(s.CheckSendCode(ctx, "79990000002", 1, "10.0.0.1")); got != "PHONE_NUMBER_INVALID" {
			t.Fatalf("other phone: %s", got)
		}
	})

	t.Run("allowlist failure is an internal error", func(t *testing.T) {
		_, store := newTestKV(t)
		s, _ := NewService(Config{HmacKey: testKey}, brokenAllowlist{}, store)
		if got := rpcMessage(s.CheckAllowed(ctx, "79990000001")); got != "INTERNAL_SERVER_ERROR" {
			t.Fatalf("got %s", got)
		}
	})

	t.Run("per phone and address", func(t *testing.T) {
		s, _, _ := testService(t, "79990000001")
		for i := 0; i < int(SendCodePerPhoneIP.Limit); i++ {
			// A new auth key each time: only the phone limits apply.
			if err := s.CheckSendCode(ctx, "79990000001", int64(i+1), "10.0.0.1"); err != nil {
				t.Fatalf("request %d: %v", i+1, err)
			}
		}
		err := s.CheckSendCode(ctx, "79990000001", 99, "10.0.0.1")
		if got := rpcMessage(err); !strings.HasPrefix(got, "FLOOD_WAIT_") {
			t.Fatalf("over the limit: %s", got)
		}
		if st, _ := status.FromError(err); st.Code() != mtproto.ErrFlood {
			t.Fatalf("code %d, want 420", st.Code())
		}
		// The owner, elsewhere, still has its own budget.
		if err := s.CheckSendCode(ctx, "79990000001", 100, "192.0.2.50"); err != nil {
			t.Fatalf("other address: %v", err)
		}
	})

	t.Run("per phone in total", func(t *testing.T) {
		s, _, _ := testService(t, "79990000001")
		for i := 0; i < int(SendCodePerPhone.Limit); i++ {
			ip := fmt.Sprintf("10.0.%d.1", i/int(SendCodePerPhoneIP.Limit))
			if err := s.CheckSendCode(ctx, "79990000001", int64(i+1), ip); err != nil {
				t.Fatalf("request %d: %v", i+1, err)
			}
		}
		if got := rpcMessage(s.CheckSendCode(ctx, "79990000001", 999, "10.1.0.1")); !strings.HasPrefix(got, "FLOOD_WAIT_") {
			t.Fatalf("over the total: %s", got)
		}
		if err := s.Unblock(ctx, "79990000001"); err != nil {
			t.Fatal(err)
		}
		if err := s.CheckSendCode(ctx, "79990000001", 1000, "10.1.0.1"); err != nil {
			t.Fatalf("after unblock: %v", err)
		}
	})

	t.Run("probes per auth key and per address", func(t *testing.T) {
		s, _, _ := testService(t)
		for i := 0; i < int(SendCodePerAuthKey.Limit); i++ {
			if got := rpcMessage(s.CheckSendCode(ctx, fmt.Sprintf("7999000%04d", i), 7, "")); got != "PHONE_NUMBER_INVALID" {
				t.Fatalf("probe %d: %s", i, got)
			}
		}
		if got := rpcMessage(s.CheckSendCode(ctx, "79990009999", 7, "")); !strings.HasPrefix(got, "FLOOD_WAIT_") {
			t.Fatalf("auth key over the limit: %s", got)
		}
		for i := 0; i < int(PhoneProbePerIP.Limit); i++ {
			_ = s.CheckSendCode(ctx, fmt.Sprintf("7999100%04d", i), int64(100+i), "10.9.9.9")
		}
		if got := rpcMessage(s.CheckSendCode(ctx, "79991009999", 500, "10.9.9.9")); !strings.HasPrefix(got, "FLOOD_WAIT_") {
			t.Fatalf("address over the probe limit: %s", got)
		}
	})

	// The whole group behind one NAT logs in within an hour: allowed phones
	// are not probes.
	t.Run("members behind one address", func(t *testing.T) {
		var phones []string
		for i := 0; i < 100; i++ {
			phones = append(phones, fmt.Sprintf("7999200%04d", i))
		}
		s, _, _ := testService(t, phones...)
		for i, p := range phones {
			for j := 0; j < 2; j++ { // sendCode and resendCode
				if err := s.CheckSendCode(ctx, p, int64(i+1), "203.0.113.5"); err != nil {
					t.Fatalf("member %d request %d: %v", i, j, err)
				}
			}
		}
		if got := rpcMessage(s.CheckSendCode(ctx, phones[0], 999, "203.0.113.5")); !strings.HasPrefix(got, "FLOOD_WAIT_") {
			t.Fatalf("address over the total: %s", got)
		}
	})
}

func TestCheckPhoneLimitsLookups(t *testing.T) {
	ctx := context.Background()
	s, _, _ := testService(t, "79990000001")
	if err := s.CheckPhone(ctx, "79990000001", 1, "10.0.0.1"); err != nil {
		t.Fatalf("allowed phone: %v", err)
	}
	if got := rpcMessage(s.CheckPhone(ctx, "79990000002", 1, "10.0.0.1")); got != "PHONE_NUMBER_INVALID" {
		t.Fatalf("other phone: %s", got)
	}
	// Probing the allowlist from one auth key runs into FLOOD_WAIT.
	for i := 2; i < int(PhoneLookupPerAuthKey.Limit); i++ {
		_ = s.CheckPhone(ctx, fmt.Sprintf("7999000%04d", i), 1, "")
	}
	if got := rpcMessage(s.CheckPhone(ctx, "79990000001", 1, "")); !strings.HasPrefix(got, "FLOOD_WAIT_") {
		t.Fatalf("auth key over the limit: %s", got)
	}
	// From one address, lookups of allowed phones are free; misses are not.
	for i := 0; i < 3*int(PhoneProbePerIP.Limit); i++ {
		if err := s.CheckPhone(ctx, "79990000001", int64(100+i), "10.2.2.2"); err != nil {
			t.Fatalf("lookup %d of an allowed phone: %v", i, err)
		}
	}
	for i := 0; i < int(PhoneProbePerIP.Limit); i++ {
		_ = s.CheckPhone(ctx, fmt.Sprintf("7999400%04d", i), int64(500+i), "10.2.2.2")
	}
	if got := rpcMessage(s.CheckPhone(ctx, "79990000001", 999, "10.2.2.2")); !strings.HasPrefix(got, "FLOOD_WAIT_") {
		t.Fatalf("address over the probe limit: %s", got)
	}
	if err := s.UnblockIP(ctx, "10.2.2.2"); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckPhone(ctx, "79990000001", 1000, "10.2.2.2"); err != nil {
		t.Fatalf("after unblock: %v", err)
	}
}

func TestVerifyFloodLimits(t *testing.T) {
	ctx := context.Background()
	const phone = "79990000001"

	t.Run("per phone: wrong codes only", func(t *testing.T) {
		s, mr, _ := testService(t, phone)
		// Correct codes are refunded: logging in often never locks the owner out.
		for i := 0; i < 3*int(CheckCodePerPhone.Limit); i++ {
			hash := fmt.Sprintf("ok%d", i)
			code, _ := s.Issue(ctx, phone, hash, "", true)
			if err := s.Verify(ctx, phone, hash, code, "10.0.0.1"); err != nil {
				t.Fatalf("login %d: %v", i, err)
			}
		}
		// Each wrong code counts, across requests and addresses.
		for i := 0; i < int(CheckCodePerPhone.Limit); i++ {
			hash := fmt.Sprintf("h%d", i)
			code, _ := s.Issue(ctx, phone, hash, "", true)
			if got := rpcMessage(s.Verify(ctx, phone, hash, wrongCode(code), fmt.Sprintf("10.0.0.%d", i))); got != "PHONE_CODE_INVALID" {
				t.Fatalf("check %d: %s", i, got)
			}
		}
		code, _ := s.Issue(ctx, phone, "last", "", true)
		err := s.Verify(ctx, phone, "last", code, "10.0.1.1")
		if got := rpcMessage(err); got != fmt.Sprintf("FLOOD_WAIT_%d", int(CheckCodePerPhone.Window/time.Second)) {
			t.Fatalf("over the hourly limit: %s", got)
		}

		// The hourly window passes; the daily one allows 5 more wrong codes.
		mr.FastForward(CheckCodePerPhone.Window)
		for i := 0; i < int(CheckCodePerPhoneDay.Limit-CheckCodePerPhone.Limit); i++ {
			hash := fmt.Sprintf("d%d", i)
			c, _ := s.Issue(ctx, phone, hash, "", true)
			_ = s.Verify(ctx, phone, hash, wrongCode(c), "")
		}
		code, _ = s.Issue(ctx, phone, "last2", "", true)
		if got := rpcMessage(s.Verify(ctx, phone, "last2", code, "")); !strings.HasPrefix(got, "FLOOD_WAIT_") {
			t.Fatalf("over the daily limit: %s", got)
		}

		// Days pass; the monthly limit stops the guessing for good.
		for day := 0; ; day++ {
			mr.FastForward(CheckCodePerPhoneDay.Window)
			hash := fmt.Sprintf("m%d", day)
			c, _ := s.Issue(ctx, phone, hash, "", true)
			got := rpcMessage(s.Verify(ctx, phone, hash, wrongCode(c), ""))
			if strings.HasPrefix(got, "FLOOD_WAIT_") {
				v, _ := mr.Get(floodKey(CheckCodePerPhoneMonth, phone))
				if n, _ := strconv.Atoi(v); int64(n) != CheckCodePerPhoneMonth.Limit+1 {
					t.Fatalf("monthly counter %d at the flood wait", n)
				}
				break
			}
			if day > 40 {
				t.Fatal("no monthly limit")
			}
			clearDaily(mr, phone)
		}
		if err := s.Unblock(ctx, phone); err != nil {
			t.Fatal(err)
		}
		code, _ = s.Issue(ctx, phone, "after", "", true)
		if err := s.Verify(ctx, phone, "after", code, ""); err != nil {
			t.Fatalf("after unblock: %v", err)
		}
	})

	t.Run("requests without a live code do not count per phone", func(t *testing.T) {
		s, mr, _ := testService(t, phone)
		_, _ = s.Issue(ctx, phone, "admin", "", false)
		for i := 0; i < 3*int(CheckCodePerPhoneDay.Limit); i++ {
			_ = s.Verify(ctx, phone, "admin", "123456", "")
			_ = s.Verify(ctx, phone, "unknown", "123456", "")
		}
		for _, k := range mr.Keys() {
			if strings.HasPrefix(k, floodKeyPrefix+"check_code_phone") {
				t.Fatalf("counted against the phone: %s", k)
			}
		}
	})

	t.Run("per address: failures only", func(t *testing.T) {
		s, _, _ := testService(t)
		for i := 0; i < int(CheckCodePerIP.Limit)-1; i++ {
			_ = s.Verify(ctx, fmt.Sprintf("7999000%04d", i), "h", "000000", "10.1.1.1")
		}
		// Members behind the address log in fine.
		for i := 0; i < 50; i++ {
			p := fmt.Sprintf("7999300%04d", i)
			s.allowlist.(fakeAllowlist)[p] = true
			code, _ := s.Issue(ctx, p, "ok"+p, "", true)
			if err := s.Verify(ctx, p, "ok"+p, code, "10.1.1.1"); err != nil {
				t.Fatalf("member %d: %v", i, err)
			}
		}
		_ = s.Verify(ctx, "79991111110", "h", "000000", "10.1.1.1")
		if got := rpcMessage(s.Verify(ctx, "79991111111", "h", "000000", "10.1.1.1")); !strings.HasPrefix(got, "FLOOD_WAIT_") {
			t.Fatalf("address over the limit: %s", got)
		}
	})

	t.Run("IPv6 addresses count per /64", func(t *testing.T) {
		s, _, _ := testService(t)
		for i := 0; i < int(CheckCodePerIP.Limit); i++ {
			_ = s.Verify(ctx, "79990000001", "h", "000000", fmt.Sprintf("2001:db8:1:2::%x", i+1))
		}
		if got := rpcMessage(s.Verify(ctx, "79990000001", "h", "000000", "2001:db8:1:2:ffff::1")); !strings.HasPrefix(got, "FLOOD_WAIT_") {
			t.Fatalf("same /64: %s", got)
		}
		if got := rpcMessage(s.Verify(ctx, "79990000001", "h", "000000", "2001:db8:1:3::1")); got != "PHONE_CODE_EXPIRED" {
			t.Fatalf("other /64: %s", got)
		}
	})
}

// clearDaily drops the hourly and daily check counters of phone.
func clearDaily(mr *miniredis.Miniredis, phone string) {
	mr.Del(floodKey(CheckCodePerPhone, phone))
	mr.Del(floodKey(CheckCodePerPhoneDay, phone))
}

func TestIpSubject(t *testing.T) {
	for in, want := range map[string]string{
		"":                          "",
		"203.0.113.7":               "203.0.113.7",
		" 203.0.113.7 ":             "203.0.113.7",
		"::ffff:203.0.113.7":        "203.0.113.7",
		"203.0.113.7:4433":          "203.0.113.7",
		"2001:db8:1:2:3:4:5:6":      "2001:db8:1:2::/64",
		"[2001:db8:1:2::9]:443":     "2001:db8:1:2::/64",
		"fe80::1%eth0":              "fe80::/64",
		"not-an-ip":                 "not-an-ip",
		"2001:db8:1:2:ffff::1":      "2001:db8:1:2::/64",
		"2001:0db8:0001:0002::0001": "2001:db8:1:2::/64",
	} {
		if got := ipSubject(in); got != want {
			t.Errorf("ipSubject(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLimiterRefundAndReset(t *testing.T) {
	ctx := context.Background()
	mr, store := newTestKV(t)
	l := NewLimiter(store)
	rule := Rule{Name: "t", Limit: 2, Window: time.Hour}
	for i := 0; i < 2; i++ {
		if err := l.Hit(ctx, rule, "x"); err != nil {
			t.Fatal(err)
		}
	}
	l.Refund(ctx, rule, "x")
	if err := l.Hit(ctx, rule, "x"); err != nil {
		t.Fatalf("after refund: %v", err)
	}
	if err := l.Hit(ctx, rule, "x"); err == nil {
		t.Fatal("over the limit")
	}
	// A refund never creates a counter (or one without a TTL).
	l.Refund(ctx, rule, "y")
	if mr.Exists(floodKey(rule, "y")) {
		t.Fatal("refund created a counter")
	}
	if err := l.Reset(ctx, rule, "x"); err != nil {
		t.Fatal(err)
	}
	if err := l.Hit(ctx, rule, "x"); err != nil {
		t.Fatalf("after reset: %v", err)
	}
	if over, n := l.Over(ctx, Rule{Name: "a", Limit: 1, Window: time.Hour}, "all"); over || n != 1 {
		t.Fatalf("first: %v %d", over, n)
	}
	if over, _ := l.Over(ctx, Rule{Name: "a", Limit: 1, Window: time.Hour}, "all"); !over {
		t.Fatal("second: not over")
	}
}

func TestLimiterFailsClosed(t *testing.T) {
	mr, store := newTestKV(t)
	l := NewLimiter(store)
	mr.Close()
	if got := rpcMessage(l.Hit(context.Background(), SendCodePerPhone, "79990000001")); got != "INTERNAL_SERVER_ERROR" {
		t.Fatalf("got %s", got)
	}
	if err := l.Hit(context.Background(), SendCodePerPhone, ""); err != nil {
		t.Fatalf("empty subject: %v", err)
	}
}

// FuzzCheckCode feeds client-supplied codes (auth.signIn phone_code) to the
// format check and the HMAC comparison: no panic, and only the issued code
// matches.
func FuzzCheckCode(f *testing.F) {
	h, _ := NewHasher(testKey)
	const issued = "048213"
	sum := h.Sum("hash", "79990000001", issued)
	for _, seed := range []string{issued, "", "04821", "0482130", "\x00\x00\x00\x00\x00\x00", "٠١٢٣٤٥", "048213\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, code string) {
		ok := wellFormed(code) && h.Verify("hash", "79990000001", code, sum)
		if ok != (code == issued) {
			t.Fatalf("code %q: match = %v", code, ok)
		}
	})
}

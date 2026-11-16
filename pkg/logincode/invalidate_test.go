package logincode

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestCandidates(t *testing.T) {
	for text, want := range map[string][]string{
		"":               nil,
		"no digits here": nil,
		"123456":         {"123456"},
		"Login code: 123456. Do not give this code to anyone": {"123456"},
		"my code is 12-34-56":       {"123456"},
		"123-456 and 654321":        {"123456", "654321"},
		"12345":                     nil, // five digits
		"1234567":                   nil, // seven digits
		"a long number 12345678901": nil, // no code inside a longer number
		"2026-11-16":                nil, // eight digits with dashes
		"123456 again 123456":       {"123456"},
		"x123456y":                  {"123456"},
		"-123456-":                  {"123456"},
		"123456.78":                 {"123456"},
		"phone +7 999 123-45-67":    nil,
		"код: 123456,\n000000":      {"123456", "000000"},
		"１２３４５６ full-width digits":  nil,
	} {
		if got := Candidates(text); !slices.Equal(got, want) {
			t.Errorf("Candidates(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestCandidatesAreBounded(t *testing.T) {
	var b strings.Builder
	for i := range 3 * MaxInvalidateCodes {
		fmt.Fprintf(&b, "%06d ", i)
	}
	got := Candidates(b.String())
	if len(got) != MaxInvalidateCodes || got[0] != "000000" || got[MaxInvalidateCodes-1] != fmt.Sprintf("%06d", MaxInvalidateCodes-1) {
		t.Fatalf("%d candidates, first %q, last %q", len(got), got[0], got[len(got)-1])
	}
}

func TestNormalizeCodes(t *testing.T) {
	got := normalizeCodes([]string{"12-34-56", "123456", "12345", "1234567", "abcdef", "", "000-000"})
	if want := []string{"123456", "000000"}; !slices.Equal(got, want) {
		t.Fatalf("normalizeCodes = %q, want %q", got, want)
	}
	many := make([]string, 0, 2*MaxInvalidateCodes)
	for i := range 2 * MaxInvalidateCodes {
		many = append(many, fmt.Sprintf("%06d", i))
	}
	if got := normalizeCodes(many); len(got) != MaxInvalidateCodes {
		t.Fatalf("%d codes kept, want %d", len(got), MaxInvalidateCodes)
	}
}

func TestNewInvalidator(t *testing.T) {
	_, store := newTestKV(t)
	if _, err := NewInvalidator(Config{HmacKey: "short"}, store); err == nil {
		t.Fatal("a short HMAC key was accepted")
	}
	if _, err := NewInvalidator(Config{HmacKey: testKey}, nil); err == nil {
		t.Fatal("no kv store was accepted")
	}
	// No allowlist is needed: the invalidator issues nothing.
	if _, err := NewInvalidator(Config{HmacKey: testKey}, store); err != nil {
		t.Fatal(err)
	}
}

// TestInvalidate: a code the person showed drops its request, whichever
// slot it is in, and nothing else: not the requests of another phone, not
// a request whose code did not match, and no attempt or limit is spent.
func TestInvalidate(t *testing.T) {
	const (
		phone = "79990000001"
		other = "79990000002"
	)
	ctx := context.Background()

	t.Run("code of the app", func(t *testing.T) {
		s, mr, _ := testService(t, phone, other)
		code, err := s.Issue(ctx, phone, "h1", "10.0.0.7", true)
		if err != nil {
			t.Fatal(err)
		}
		keep := code
		for keep == code || keep == wrongCode(code) {
			if keep, err = s.Issue(ctx, phone, "h2", "10.0.0.7", true); err != nil {
				t.Fatal(err)
			}
		}
		// The same digits for another phone are another request.
		if _, err := s.Issue(ctx, other, "h3", "10.0.0.7", true); err != nil {
			t.Fatal(err)
		}
		mr.HSet(secretKey("h3"), "hmac", s.hasher.Sum("h3", other, code))

		dashed := code[:2] + "-" + code[2:4] + "-" + code[4:]
		n, err := s.Invalidate(ctx, phone, []string{wrongCode(code), dashed, "not a code"})
		if err != nil || n != 1 {
			t.Fatalf("Invalidate = %d, %v; want 1 request dropped", n, err)
		}
		if got := rpcMessage(s.Verify(ctx, phone, "h1", code, "")); got != "PHONE_CODE_EXPIRED" {
			t.Fatalf("the shown code: %s, want PHONE_CODE_EXPIRED", got)
		}
		if got := mr.HGet(secretKey("h2"), "attempts"); got != "0" {
			t.Fatalf("attempts of the other request = %s, want 0", got)
		}
		if err := s.Verify(ctx, phone, "h2", keep, ""); err != nil {
			t.Fatalf("the code of the other request: %v", err)
		}
		if err := s.Verify(ctx, other, "h3", code, ""); err != nil {
			t.Fatalf("the code of another phone: %v", err)
		}
	})

	t.Run("code of the administrator", func(t *testing.T) {
		s, mr, _ := testService(t, phone)
		if _, err := s.Issue(ctx, phone, "h1", "10.0.0.7", true); err != nil {
			t.Fatal(err)
		}
		r, err := s.Reissue(ctx, phone, "")
		if err != nil {
			t.Fatal(err)
		}
		n, err := s.Invalidate(ctx, phone, []string{r.Code})
		if err != nil || n != 1 {
			t.Fatalf("Invalidate = %d, %v; want 1", n, err)
		}
		if mr.Exists(secretKey("h1")) {
			t.Fatal("the request of the administrator's code is kept")
		}
		if pending, err := s.Pending(ctx, phone); err != nil || len(pending) != 0 {
			t.Fatalf("Pending = %+v, %v; want none", pending, err)
		}
	})

	t.Run("no match spends nothing", func(t *testing.T) {
		s, mr, _ := testService(t, phone)
		code, err := s.Issue(ctx, phone, "h1", "10.0.0.7", true)
		if err != nil {
			t.Fatal(err)
		}
		n, err := s.Invalidate(ctx, phone, []string{wrongCode(code)})
		if err != nil || n != 0 {
			t.Fatalf("Invalidate = %d, %v; want 0", n, err)
		}
		if got := mr.HGet(secretKey("h1"), "attempts"); got != "0" {
			t.Fatalf("attempts = %s, want 0", got)
		}
		for _, k := range mr.Keys() {
			if strings.HasPrefix(k, floodKeyPrefix) {
				t.Fatalf("a limit was counted: %s", k)
			}
		}
		if err := s.Verify(ctx, phone, "h1", code, ""); err != nil {
			t.Fatalf("the code after a miss: %v", err)
		}
	})

	t.Run("expired code", func(t *testing.T) {
		s, mr, now := testService(t, phone)
		code, err := s.Issue(ctx, phone, "h1", "10.0.0.7", true)
		if err != nil {
			t.Fatal(err)
		}
		*now = now.Add(CodeTTL + time.Second)
		if n, err := s.Invalidate(ctx, phone, []string{code}); err != nil || n != 0 {
			t.Fatalf("Invalidate = %d, %v; want 0", n, err)
		}
		// The request still waits for the administrator.
		if !mr.Exists(secretKey("h1")) {
			t.Fatal("the request of an expired code was dropped")
		}
	})

	t.Run("nothing to look up", func(t *testing.T) {
		s, mr, _ := testService(t, phone)
		mr.Close() // any lookup would fail
		for _, codes := range [][]string{nil, {"12345", "abc"}} {
			if n, err := s.Invalidate(ctx, phone, codes); err != nil || n != 0 {
				t.Fatalf("Invalidate(%q) = %d, %v", codes, n, err)
			}
		}
		if n, err := s.Invalidate(ctx, "", []string{"123456"}); err != nil || n != 0 {
			t.Fatalf("Invalidate without a phone = %d, %v", n, err)
		}
		if _, err := s.Invalidate(ctx, phone, []string{"123456"}); err == nil {
			t.Fatal("a failed lookup was not reported")
		}
	})

	t.Run("narrow constructor", func(t *testing.T) {
		s, mr, _ := testService(t, phone)
		code, err := s.Issue(ctx, phone, "h1", "10.0.0.7", true)
		if err != nil {
			t.Fatal(err)
		}
		v, err := NewInvalidator(Config{HmacKey: testKey}, kv.NewStore(kv.KvConf{cache.NodeConf{
			RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
			Weight:    100,
		}}))
		if err != nil {
			t.Fatal(err)
		}
		if n, err := v.Invalidate(ctx, phone, []string{code}); err != nil || n != 1 {
			t.Fatalf("Invalidate = %d, %v; want 1", n, err)
		}
	})
}

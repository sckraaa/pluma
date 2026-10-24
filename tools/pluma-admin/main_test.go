package main

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/phoneallowlist"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const testHmacKey = "0123456789abcdef0123456789abcdef"

type memAllowlist map[string]string

func (m memAllowlist) Contains(_ context.Context, phone string) (bool, error) {
	_, ok := m[phone]
	return ok, nil
}

func (m memAllowlist) Add(_ context.Context, phone, note string) (bool, error) {
	if _, ok := m[phone]; ok {
		return false, nil
	}
	m[phone] = note
	return true, nil
}

func (m memAllowlist) Remove(_ context.Context, phone string) (bool, error) {
	_, ok := m[phone]
	delete(m, phone)
	return ok, nil
}

func (m memAllowlist) List(context.Context) ([]phoneallowlist.Entry, error) {
	var entries []phoneallowlist.Entry
	for p, n := range m {
		entries = append(entries, phoneallowlist.Entry{Phone: p, Note: n})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Phone < entries[j].Phone })
	return entries, nil
}

type harness struct {
	list  memAllowlist
	codes *logincode.Service
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	// miniredis does not implement CLIENT SETINFO; DisableIdentity keeps the failing
	// connect-time command from tripping the go-zero redis breaker.
	mr := miniredis.RunT(t)
	store := kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
	h := &harness{list: memAllowlist{}}
	var err error
	if h.codes, err = logincode.NewService(logincode.Config{HmacKey: testHmacKey}, h.list, store); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) run(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut, func(string) (allowlist, codeIssuer, error) {
		return h.list, h.codes, nil
	})
	return code, out.String(), errOut.String()
}

func TestAllowlistCommands(t *testing.T) {
	h := newHarness(t)

	if code, out, _ := h.run("allowlist", "add", "+7 999 123-45-67", "Alice", "Smith"); code != 0 || !strings.Contains(out, "+79991234567 added") {
		t.Fatalf("add: %d %q", code, out)
	}
	if h.list["79991234567"] != "Alice Smith" {
		t.Fatalf("stored %v", h.list)
	}
	if code, out, _ := h.run("allowlist", "add", "79991234567"); code != 0 || !strings.Contains(out, "already") {
		t.Fatalf("add again: %d %q", code, out)
	}
	if code, out, _ := h.run("allowlist", "list"); code != 0 || !strings.Contains(out, "+79991234567") || !strings.Contains(out, "1 phone(s)") {
		t.Fatalf("list: %d %q", code, out)
	}
	if code, _, _ := h.run("allowlist", "remove", "+79991234567"); code != 0 || len(h.list) != 0 {
		t.Fatalf("remove: %d %v", code, h.list)
	}
	if code, _, errOut := h.run("allowlist", "remove", "+79991234567"); code != 1 || !strings.Contains(errOut, "not on the allowlist") {
		t.Fatalf("remove missing: %d %q", code, errOut)
	}
}

func TestLoginCodeIssuesUsableCode(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.list["79991234567"] = ""

	if code, _, errOut := h.run("login-code", "+79991234567"); code != 1 || !strings.Contains(errOut, "no pending login code request") {
		t.Fatalf("without a request: %d %q", code, errOut)
	}

	// The app asked for a code and got "sent by SMS": nothing was delivered.
	if _, err := h.codes.Issue(ctx, "79991234567", "hash1", "198.51.100.7", false); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := h.run("login-code", "+7 999 123 45 67")
	if code != 0 {
		t.Fatalf("login-code: %d %q", code, errOut)
	}
	m := regexp.MustCompile(`Login code for \+79991234567: (\d{6})\n`).FindStringSubmatch(out)
	if m == nil || !strings.Contains(out, "198.51.100.7") {
		t.Fatalf("unexpected output %q", out)
	}
	if err := h.codes.Verify(ctx, "79991234567", "hash1", m[1], ""); err != nil {
		t.Fatalf("printed code does not work: %v", err)
	}
}

// TestLoginCodeChoosesRequest: with several pending requests (anyone who knows
// the phone can make one) the tool lists them instead of picking the latest.
func TestLoginCodeChoosesRequest(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.list["79991234567"] = ""
	if _, err := h.codes.Issue(ctx, "79991234567", "aaaaaaaa11111111", "198.51.100.7", false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.codes.Issue(ctx, "79991234567", "bbbbbbbb22222222", "203.0.113.66", false); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := h.run("login-code", "+79991234567")
	if code != 1 || !strings.Contains(errOut, "several pending") ||
		!strings.Contains(out, "aaaaaaaa") || !strings.Contains(out, "198.51.100.7") ||
		!strings.Contains(out, "bbbbbbbb") || !strings.Contains(out, "203.0.113.66") || strings.Contains(out, "Login code for") {
		t.Fatalf("ambiguous: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := h.run("login-code", "+79991234567", "cccccccc"); code != 1 || !strings.Contains(errOut, "no pending") {
		t.Fatalf("unknown id: %d %q", code, errOut)
	}

	code, out, errOut = h.run("login-code", "+79991234567", "aaaaaaaa")
	if code != 0 {
		t.Fatalf("chosen: %d %q", code, errOut)
	}
	m := regexp.MustCompile(`Login code for \+79991234567: (\d{6})\n`).FindStringSubmatch(out)
	if m == nil || !strings.Contains(out, "198.51.100.7") {
		t.Fatalf("unexpected output %q", out)
	}
	if err := h.codes.Verify(ctx, "79991234567", "bbbbbbbb22222222", m[1], ""); err == nil {
		t.Fatal("code works for the other request")
	}
	if err := h.codes.Verify(ctx, "79991234567", "aaaaaaaa11111111", m[1], ""); err != nil {
		t.Fatalf("printed code does not work: %v", err)
	}
}

func TestLoginUnblock(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.list["79991234567"] = ""
	// Wrong codes lock the phone out of logins for hours.
	for i := 0; i < int(logincode.CheckCodePerPhone.Limit); i++ {
		hash := fmt.Sprintf("h%d", i)
		code, _ := h.codes.Issue(ctx, "79991234567", hash, "", true)
		_ = h.codes.Verify(ctx, "79991234567", hash, wrong(code), "")
	}
	code, _ := h.codes.Issue(ctx, "79991234567", "last", "", true)
	if err := h.codes.Verify(ctx, "79991234567", "last", code, ""); err == nil || !strings.Contains(err.Error(), "FLOOD_WAIT") {
		t.Fatalf("not locked: %v", err)
	}
	if code, out, errOut := h.run("login-unblock", "+7 999 123 45 67"); code != 0 || !strings.Contains(out, "+79991234567") {
		t.Fatalf("unblock: %d %q %q", code, out, errOut)
	}
	if err := h.codes.Verify(ctx, "79991234567", "last", code, ""); err != nil {
		t.Fatalf("after unblock: %v", err)
	}

	for i := 0; i < int(logincode.PhoneProbePerIP.Limit)+1; i++ {
		_ = h.codes.CheckPhone(ctx, fmt.Sprintf("7999000%04d", i), int64(i+1), "2001:db8::5")
	}
	if err := h.codes.CheckPhone(ctx, "79991234567", 999, "2001:db8::5"); err == nil {
		t.Fatal("address not locked")
	}
	if code, out, errOut := h.run("login-unblock", "2001:db8::7"); code != 0 || !strings.Contains(out, "2001:db8::7") {
		t.Fatalf("unblock ip: %d %q %q", code, out, errOut)
	}
	if err := h.codes.CheckPhone(ctx, "79991234567", 1000, "2001:db8::5"); err != nil {
		t.Fatalf("after unblock: %v", err)
	}
}

// wrong returns a well-formed code other than code.
func wrong(code string) string {
	if code == "000000" {
		return "111111"
	}
	return "000000"
}

func TestLoginCodeRefusesPhoneOffAllowlist(t *testing.T) {
	h := newHarness(t)
	if code, _, errOut := h.run("login-code", "+79991234567"); code != 1 || !strings.Contains(errOut, "not on the allowlist") {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestUsage(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		nil,
		{"allowlist"},
		{"allowlist", "add"},
		{"allowlist", "remove", "1", "2"},
		{"login-code"},
		{"login-code", "1", "2", "3"},
		{"login-unblock"},
		{"login-unblock", "1", "2"},
		{"reset-everything"},
		{"-bogus-flag"},
		{"state-raise"},
		{"state-raise", "-by", "0"},
		{"state-raise", "-by", "10", "extra"},
		{"state-export-legacy"},
		{"verify-pts"},
		{"verify-pts", "-after"},
		{"outbox"},
		{"outbox", "purge"},
		{"outbox", "replay"},
		{"outbox", "discard"},
		{"outbox", "discard", "-id", "0"},
		{"outbox", "list", "extra"},
		{"password"},
		{"password", "reset"},
		{"password", "remove", "+12015550101"},
		{"password", "reset", "+12015550101", "extra"},
	} {
		if code, _, _ := h.run(args...); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
	for _, args := range [][]string{
		{"allowlist", "add", "12"},
		{"login-code", "not-a-phone"},
		{"login-unblock", "not-a-phone"},
		{"password", "reset", "not-a-phone"},
	} {
		if code, _, errOut := h.run(args...); code != 1 || !strings.Contains(errOut, "invalid phone number") {
			t.Errorf("%q: exit %d %q", args, code, errOut)
		}
	}
	if code, _, errOut := h.run("verify-pts", "alice"); code != 1 || !strings.Contains(errOut, "invalid user id") {
		t.Errorf("verify-pts alice: exit %d %q", code, errOut)
	}
	if code, _, errOut := h.run("outbox", "list", "-state", "lost"); code != 1 || !strings.Contains(errOut, "unknown event state") {
		t.Errorf("outbox list -state lost: exit %d %q", code, errOut)
	}
}

// TestLoadsOutboxConfig: the outbox commands find the push topic of bff.
func TestLoadsOutboxConfig(t *testing.T) {
	t.Setenv("MYSQL_PASSWORD", "pw")
	t.Setenv("REDIS_PASSWORD", "rpw")
	t.Setenv("LOGIN_CODE_HMAC_KEY", testHmacKey)
	for _, f := range []string{"../../teamgramd/etc/bff.yaml", "../../teamgramd/etc2/bff.yaml"} {
		var c outboxConfig
		if err := conf.Load(f, &c); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if c.SyncClient.Topic != "Sync-T" {
			t.Errorf("%s: push topic %q", f, c.SyncClient.Topic)
		}
	}
}

// TestLoadsPasswordConfig: password reset finds MySQL and Redis of bff.
func TestLoadsPasswordConfig(t *testing.T) {
	t.Setenv("MYSQL_PASSWORD", "pw")
	t.Setenv("REDIS_PASSWORD", "rpw")
	t.Setenv("LOGIN_CODE_HMAC_KEY", testHmacKey)
	for _, f := range []string{"../../teamgramd/etc/bff.yaml", "../../teamgramd/etc2/bff.yaml"} {
		var c passwordConfig
		if err := conf.Load(f, &c); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !strings.Contains(c.Mysql.DSN, ":pw@") || len(c.KV) == 0 || c.KV[0].Pass != "rpw" {
			t.Fatalf("%s: unexpected config", f)
		}
	}
}

// TestLoadsBffConfig checks that the tool reads the real bff configs.
func TestLoadsBffConfig(t *testing.T) {
	t.Setenv("MYSQL_PASSWORD", "pw")
	t.Setenv("REDIS_PASSWORD", "rpw")
	t.Setenv("LOGIN_CODE_HMAC_KEY", testHmacKey)
	for _, f := range []string{"../../teamgramd/etc/bff.yaml", "../../teamgramd/etc2/bff.yaml"} {
		var c config
		if err := conf.Load(f, &c); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !strings.Contains(c.Mysql.DSN, ":pw@") || len(c.KV) == 0 || c.KV[0].Pass != "rpw" || c.LoginCode.HmacKey != testHmacKey {
			t.Fatalf("%s: unexpected config %+v", f, c)
		}
	}
}

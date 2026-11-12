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
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/mail"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// fakeSender is the mail provider of the tests: it answers the planned
// errors first, then accepts.
type fakeSender struct {
	mu    sync.Mutex
	calls []mail.Message // every Send
	sent  []mail.Message // the accepted ones
	errs  []error
}

func (f *fakeSender) Send(_ context.Context, m mail.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, m)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return "", err
	}
	f.sent = append(f.sent, m)
	return "msg-" + strconv.Itoa(len(f.sent)), nil
}

func (f *fakeSender) fail(errs ...error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs = append(f.errs, errs...)
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

var codeLine = regexp.MustCompile(`(?m)^(\d{6})$`)

// lastCode is the code of the last accepted email, which must be to address.
func (f *fakeSender) lastCode(t *testing.T, address string) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		t.Fatal("no email was sent")
	}
	m := f.sent[len(f.sent)-1]
	if m.To != address {
		t.Fatalf("the email went to %q, want %q", m.To, address)
	}
	c := codeLine.FindStringSubmatch(m.Text)
	if c == nil {
		t.Fatalf("no code in %q", m.Text)
	}
	return c[1]
}

var (
	retryable = &mail.Error{Code: "rate_limit_exceeded", Status: 429, Retryable: true}
	permanent = &mail.Error{Code: "invalid_api_key", Status: 403}
)

const testHmacKey = "0123456789abcdef0123456789abcdef"

type mailEnv struct {
	*testEnv
	mail   *fakeSender
	pauses []time.Duration
}

func newMailEnv(t *testing.T) *mailEnv {
	t.Helper()
	e := &mailEnv{testEnv: newTestEnv(t), mail: &fakeSender{}}
	if err := e.s.SetMailSender(e.mail, testHmacKey, 0); err != nil {
		t.Fatal(err)
	}
	e.s.sleep = func(_ context.Context, d time.Duration) error {
		e.pauses = append(e.pauses, d)
		return nil
	}
	return e
}

// setWithEmail sets password with a recovery email for userId, which has
// none: the answer is EMAIL_UNCONFIRMED_6.
func (e *mailEnv) setWithEmail(t *testing.T, userId int64, password, email string) {
	t.Helper()
	s := newSettings(t, e.password(t, userId), password, "")
	s.Email = wrapperspb.String(email)
	mustError(t, e.s.UpdateIn(context.Background(), userId, keyOne, "en", mtclient.NoPassword(), s), "EMAIL_UNCONFIRMED_6")
}

// confirmed sets password with the confirmed recovery email for userId.
func (e *mailEnv) confirmed(t *testing.T, userId int64, password, email string) {
	t.Helper()
	e.setWithEmail(t, userId, password, email)
	if err := e.s.ConfirmEmail(context.Background(), userId, keyOne, e.mail.lastCode(t, email)); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	e.advance(time.Minute) // past the limit of one email a minute
}

func emailOnly(email string) *mtproto.Account_PasswordInputSettings {
	return mtproto.MakeTLAccountPasswordInputSettings(&mtproto.Account_PasswordInputSettings{
		Email: wrapperspb.String(email),
	}).To_Account_PasswordInputSettings()
}

func TestNormalizeEmail(t *testing.T) {
	for in, want := range map[string]string{
		"alice@example.com":       "alice@example.com",
		"  Alice.B@Example.COM  ": "Alice.B@example.com",
		"a+tag@mail.example.org":  "a+tag@mail.example.org",
	} {
		if got, ok := normalizeEmail(in); !ok || got != want {
			t.Errorf("%q: %q %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{
		"", "alice", "alice@", "@example.com", "alice@example", "alice@.example.com", "alice@example.com.",
		"alice@exa..mple.com", "Alice <alice@example.com>", "<alice@example.com>", "al ice@example.com",
		"алиса@example.com", "alice@exämple.com", "a\x00b@example.com", "a@b.c, d@e.f",
		strings.Repeat("a", 250) + "@b.cd",
	} {
		if got, ok := normalizeEmail(in); ok {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestEmailPattern(t *testing.T) {
	for in, want := range map[string]string{
		"alice@gmail.com": "al***@gmail.com",
		"abcd@x.io":       "ab***@x.io",
		"abc@x.io":        "a***@x.io",
		"a@x.io":          "a***@x.io",
	} {
		got := emailPattern(in)
		if got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
		// Android hides what is between the stars; tdesktop and Web K take
		// a string with a space for a placeholder.
		if !strings.Contains(got, "*") || strings.ContainsAny(got, " \t") {
			t.Errorf("%q: pattern %q", in, got)
		}
	}
}

// The HMAC of a code binds the purpose, the user, the address and, for a
// recovery, the password it was issued under.
func TestEmailCodeHash(t *testing.T) {
	h := &emailHasher{key: []byte(testHmacKey)}
	base := h.sum(PurposeRecovery, 1, "a@b.cd", 100, "123456")
	for name, other := range map[string]string{
		"purpose":  h.sum(PurposeSetup, 1, "a@b.cd", 100, "123456"),
		"user":     h.sum(PurposeRecovery, 2, "a@b.cd", 100, "123456"),
		"address":  h.sum(PurposeRecovery, 1, "a@b.ce", 100, "123456"),
		"password": h.sum(PurposeRecovery, 1, "a@b.cd", 101, "123456"),
		"code":     h.sum(PurposeRecovery, 1, "a@b.cd", 100, "123457"),
		"key":      (&emailHasher{key: []byte(testHmacKey + "x")}).sum(PurposeRecovery, 1, "a@b.cd", 100, "123456"),
	} {
		if other == base {
			t.Errorf("the hash does not depend on the %s", name)
		}
	}
	if h.sum(PurposeSetup, 1, "a@b.cd", 100, "1") != h.sum(PurposeSetup, 1, "a@b.cd", 200, "1") {
		t.Error("a setup code depends on the password")
	}
	if base != h.sum(PurposeRecovery, 1, "a@b.cd", 100, "123456") || len(base) != 64 || strings.Contains(base, "123456") {
		t.Errorf("hash %q", base)
	}
}

func TestSetUpRecoveryEmail(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.setWithEmail(t, alice, "pw", "Alice@Example.com")

	// The first password of the request waits for the address: no password
	// yet, and the pattern of the address.
	pw := e.password(t, alice)
	if pw.GetHasPassword() || pw.GetCurrentAlgo() != nil || pw.GetHasRecovery() || pw.GetEmailUnconfirmedPattern().GetValue() != "Al***@example.com" || pw.GetLoginEmailPattern() != nil {
		t.Fatalf("account.password after the email: %v", pw)
	}
	if e.mail.count() != 1 {
		t.Fatalf("%d emails", e.mail.count())
	}
	m := e.mail.sent[0]
	if m.Tag != "email_confirm" || !strings.HasPrefix(m.IdempotencyKey, "pluma/setup/1001/") || m.Subject != langpack.MailText("PlumaMailConfirmSubject", "en") {
		t.Fatalf("email %+v", m)
	}
	code := e.mail.lastCode(t, "Alice@example.com")

	mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, ""), "CODE_EMPTY")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, wrong), "CODE_INVALID")
	if err := e.s.ConfirmEmail(ctx, alice, keyOne, " "+code+" "); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, code), "EMAIL_HASH_EXPIRED")

	pw = e.password(t, alice)
	if !pw.GetHasPassword() || !pw.GetHasRecovery() || pw.GetEmailUnconfirmedPattern() != nil {
		t.Fatalf("account.password after the confirmation: %v", pw)
	}
	settings, err := e.s.Settings(ctx, alice, keyOne, e.proof(t, alice, "pw"))
	if err != nil || settings.GetEmail().GetValue() != "Alice@example.com" {
		t.Fatalf("account.passwordSettings: %v %v", settings, err)
	}

	// The same address again: nothing to confirm, no email.
	if err = e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw"), emailOnly("Alice@EXAMPLE.com")); err != nil {
		t.Fatalf("the confirmed address again: %v", err)
	}
	// A change of the password keeps the email.
	if err = e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw"), newSettings(t, pw, "pw2", "")); err != nil {
		t.Fatal(err)
	}
	if !e.password(t, alice).GetHasRecovery() || e.mail.count() != 1 {
		t.Fatalf("after a change of the password: %v, %d emails", e.password(t, alice), e.mail.count())
	}

	// Another address waits; the confirmed one stays until it is confirmed.
	e.advance(time.Minute)
	mustError(t, e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw2"), emailOnly("new@example.org")), "EMAIL_UNCONFIRMED_6")
	pw = e.password(t, alice)
	if !pw.GetHasRecovery() || pw.GetEmailUnconfirmedPattern().GetValue() != "n***@example.org" {
		t.Fatalf("a second address: %v", pw)
	}
	if err = e.s.ConfirmEmail(ctx, alice, keyOne, e.mail.lastCode(t, "new@example.org")); err != nil {
		t.Fatal(err)
	}
	if settings, _ = e.s.Settings(ctx, alice, keyOne, e.proof(t, alice, "pw2")); settings.GetEmail().GetValue() != "new@example.org" {
		t.Fatalf("the email after the change: %v", settings)
	}

	// An empty email (the client's "skip") removes it.
	if err = e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw2"), emailOnly("")); err != nil {
		t.Fatal(err)
	}
	if pw = e.password(t, alice); pw.GetHasRecovery() || pw.GetEmailUnconfirmedPattern() != nil {
		t.Fatalf("after the removal: %v", pw)
	}
}

// The provider did not take the email: the password of the request waits
// for the address all the same and the answer is EMAIL_UNCONFIRMED_6,
// without a live code; "send again" sends one.
func TestSetUpEmailWhenTheProviderFails(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.mail.fail(permanent)
	e.setWithEmail(t, alice, "pw", "alice@example.com")
	pw := e.password(t, alice)
	if pw.GetEmailUnconfirmedPattern() == nil || e.mail.count() != 0 {
		t.Fatalf("after a refused email: %v, %d emails", pw, e.mail.count())
	}
	mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, "123456"), "EMAIL_HASH_EXPIRED")

	// The refused email gave the count of the minute back.
	if err := e.s.ResendEmail(ctx, alice, keyOne, "ru"); err != nil {
		t.Fatalf("resend: %v", err)
	}
	if e.mail.sent[0].Subject != langpack.MailText("PlumaMailConfirmSubject", "ru") {
		t.Fatalf("subject %q", e.mail.sent[0].Subject)
	}
	if err := e.s.ConfirmEmail(ctx, alice, keyOne, e.mail.lastCode(t, "alice@example.com")); err != nil {
		t.Fatal(err)
	}

	// A provider that keeps failing: the resend is FLOOD_WAIT_60.
	e.advance(time.Minute)
	if err := e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw"), emailOnly("b@example.com")); !errorIs(err, "EMAIL_UNCONFIRMED_6") {
		t.Fatal(err)
	}
	e.advance(time.Minute)
	e.mail.fail(retryable, retryable, retryable)
	mustError(t, e.s.ResendEmail(ctx, alice, keyOne, "en"), "FLOOD_WAIT_60")
}

func TestDeliverRetries(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.setPassword(t, alice, "pw", "")

	// Two retryable errors, then the email goes, under one key.
	e.mail.fail(retryable, retryable)
	if err := e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw"), emailOnly("a@example.com")); !errorIs(err, "EMAIL_UNCONFIRMED_6") {
		t.Fatal(err)
	}
	if len(e.mail.calls) != 3 || e.mail.count() != 1 {
		t.Fatalf("%d attempts, %d sent", len(e.mail.calls), e.mail.count())
	}
	for _, m := range e.mail.calls {
		if m.IdempotencyKey != e.mail.calls[0].IdempotencyKey {
			t.Fatalf("idempotency keys %q and %q", m.IdempotencyKey, e.mail.calls[0].IdempotencyKey)
		}
	}
	if len(e.pauses) != 2 || e.pauses[0] != 500*time.Millisecond || e.pauses[1] != 1500*time.Millisecond {
		t.Fatalf("pauses %v", e.pauses)
	}

	// A permanent error is not repeated.
	e.advance(time.Minute)
	e.mail.calls, e.pauses = nil, nil
	e.mail.fail(permanent)
	mustError(t, e.s.ResendEmail(ctx, alice, keyOne, "en"), "FLOOD_WAIT_60")
	if len(e.mail.calls) != 1 || len(e.pauses) != 0 {
		t.Fatalf("a permanent error: %d attempts", len(e.mail.calls))
	}

	// A new code gets a new key.
	if err := e.s.ResendEmail(ctx, alice, keyOne, "en"); err != nil {
		t.Fatal(err)
	}
	if e.mail.sent[1].IdempotencyKey == e.mail.sent[0].IdempotencyKey {
		t.Fatal("two codes under one idempotency key")
	}
}

func TestRecoveryEmailNeedsAPassword(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	mustError(t, e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), emailOnly("a@example.com")), "EMAIL_INVALID")
	e.setPassword(t, alice, "pw", "")
	remove := removeSettings()
	remove.Email = wrapperspb.String("a@example.com")
	mustError(t, e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw"), remove), "EMAIL_INVALID")
	mustError(t, e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw"), emailOnly("not an address")), "EMAIL_INVALID")
	if e.mail.count() != 0 {
		t.Fatal("an email was sent")
	}

	// Removing the password removes its email.
	e.advance(time.Minute)
	if err := e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw"), emailOnly("a@example.com")); !errorIs(err, "EMAIL_UNCONFIRMED_6") {
		t.Fatal(err)
	}
	if err := e.s.ConfirmEmail(ctx, alice, keyOne, e.mail.lastCode(t, "a@example.com")); err != nil {
		t.Fatal(err)
	}
	if err := e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw"), removeSettings()); err != nil {
		t.Fatal(err)
	}
	e.setPassword(t, alice, "again", "")
	if e.password(t, alice).GetHasRecovery() {
		t.Fatal("the email outlived its password")
	}
}

func TestMailOff(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	e.setPassword(t, alice, "pw", "")
	// An email left from when mail was on.
	p, _ := e.store.Get(ctx, alice)
	next := *p
	next.Email, next.EmailUnconfirmed = "a@example.com", "b@example.com"
	if ok, err := e.store.Replace(ctx, p, &next); err != nil || !ok {
		t.Fatal(ok, err)
	}
	pw := e.password(t, alice)
	if pw.GetHasRecovery() || pw.GetEmailUnconfirmedPattern() != nil {
		t.Fatalf("account.password without mail: %v", pw)
	}
	if _, err := e.s.RequestRecovery(ctx, alice, keyOne, "en"); !errorIs(err, "PASSWORD_RECOVERY_NA") {
		t.Fatalf("auth.requestPasswordRecovery without mail: %v", err)
	}
	mustError(t, e.s.CheckRecovery(ctx, alice, keyOne, "123456"), "PASSWORD_RECOVERY_EXPIRED")
	mustError(t, e.s.ResendEmail(ctx, alice, keyOne, "en"), "EMAIL_HASH_EXPIRED")
	mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, "123456"), "EMAIL_HASH_EXPIRED")
	if e.s.MailEnabled() {
		t.Fatal("mail is on")
	}
	// A key without a sender address stops the start.
	if err := e.s.EnableMail(mail.Config{ApiKey: "re_x"}, testHmacKey); !errors.Is(err, mail.ErrMailMisconfigured) {
		t.Fatalf("EnableMail without From: %v", err)
	}
	if err := e.s.EnableMail(mail.Config{From: "a@b.cd"}, ""); err != nil || e.s.MailEnabled() {
		t.Fatalf("EnableMail without a key: %v", err)
	}
	if err := e.s.SetMailSender(&fakeSender{}, "short", 0); err == nil {
		t.Fatal("a short HMAC key")
	}
}

func TestEmailCodeBurns(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.setWithEmail(t, alice, "pw", "a@example.com")
	code := e.mail.lastCode(t, "a@example.com")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 0; i < EmailCodeMaxAttempts; i++ {
		mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, wrong), "CODE_INVALID")
	}
	mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, code), "EMAIL_HASH_EXPIRED")

	// The code expires.
	e.advance(time.Minute)
	if err := e.s.ResendEmail(ctx, alice, keyOne, "en"); err != nil {
		t.Fatal(err)
	}
	code = e.mail.lastCode(t, "a@example.com")
	e.advance(EmailCodeTTL + time.Second)
	mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, code), "EMAIL_HASH_EXPIRED")
}

func TestResendAndCancel(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.setWithEmail(t, alice, "pw", "a@example.com")
	old := e.mail.lastCode(t, "a@example.com")

	mustFloodWait(t, e.s.ResendEmail(ctx, alice, keyOne, "en"))
	e.advance(time.Minute)
	if err := e.s.ResendEmail(ctx, alice, keyOne, "en"); err != nil {
		t.Fatal(err)
	}
	code := e.mail.lastCode(t, "a@example.com")
	if code != old {
		mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, old), "CODE_INVALID")
	}
	// Five an hour per user and purpose.
	for i := 2; i < int(EmailSendPerUserHour.Limit); i++ {
		e.advance(time.Minute)
		if err := e.s.ResendEmail(ctx, alice, keyOne, "en"); err != nil {
			t.Fatalf("email %d: %v", i+1, err)
		}
	}
	e.advance(time.Minute)
	mustFloodWait(t, e.s.ResendEmail(ctx, alice, keyOne, "en"))

	// Cancel: the waiting address and its code go, and the first password
	// that waited for it; a confirmed address stays.
	if err := e.s.CancelEmail(ctx, alice); err != nil {
		t.Fatal(err)
	}
	if pw := e.password(t, alice); pw.GetEmailUnconfirmedPattern() != nil || pw.GetHasPassword() {
		t.Fatalf("after the cancel: %v", pw)
	}
	if p, _ := e.store.Get(ctx, alice); p != nil {
		t.Fatalf("the waiting password outlived the cancel: %+v", p)
	}
	mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, e.mail.lastCode(t, "a@example.com")), "EMAIL_HASH_EXPIRED")
	mustError(t, e.s.ResendEmail(ctx, alice, keyOne, "en"), "EMAIL_HASH_EXPIRED")
	if err := e.s.CancelEmail(ctx, alice); err != nil {
		t.Fatalf("a cancel of nothing: %v", err)
	}

	e2 := newMailEnv(t)
	e2.confirmed(t, bob, "pw", "bob@example.com")
	mustError(t, e2.s.Update(ctx, bob, keyOne, e2.proof(t, bob, "pw"), emailOnly("other@example.com")), "EMAIL_UNCONFIRMED_6")
	if err := e2.s.CancelEmail(ctx, bob); err != nil {
		t.Fatal(err)
	}
	if pw := e2.password(t, bob); !pw.GetHasPassword() || !pw.GetHasRecovery() || pw.GetEmailUnconfirmedPattern() != nil {
		t.Fatalf("the confirmed email after a cancel: %v", pw)
	}
}

func TestRecoverWithNewPassword(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.confirmed(t, alice, "forgotten", "a@example.com")
	if _, err := e.s.Reset(ctx, alice); err != nil {
		t.Fatal(err)
	}

	pattern, err := e.s.RequestRecovery(ctx, alice, keyTwo, "de")
	if err != nil || pattern != "a***@example.com" {
		t.Fatalf("auth.requestPasswordRecovery: %q %v", pattern, err)
	}
	m := e.mail.sent[len(e.mail.sent)-1]
	if m.Tag != "recovery_code" || m.Subject != langpack.MailText("PlumaMailRecoverySubject", "de") || !strings.HasPrefix(m.IdempotencyKey, "pluma/recovery/1001/") {
		t.Fatalf("email %+v", m)
	}
	code := e.mail.lastCode(t, "a@example.com")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}

	mustError(t, e.s.CheckRecovery(ctx, alice, keyTwo, ""), "CODE_EMPTY")
	mustError(t, e.s.CheckRecovery(ctx, alice, keyTwo, wrong), "CODE_INVALID")
	if err = e.s.CheckRecovery(ctx, alice, keyTwo, code); err != nil {
		t.Fatalf("check: %v", err)
	}
	// The check does not use the code up; bad settings do not either.
	bad := newSettings(t, e.password(t, alice), "new", "")
	bad.NewAlgo.Salt2 = []byte("not issued")
	if _, err = e.s.Recover(ctx, alice, keyTwo, "en", code, bad); !errorIs(err, "NEW_SALT_INVALID") {
		t.Fatalf("bad settings: %v", err)
	}
	if _, err = e.s.Recover(ctx, alice, keyTwo, "en", wrong, nil); !errorIs(err, "CODE_INVALID") {
		t.Fatalf("a wrong code: %v", err)
	}
	has, err := e.s.Recover(ctx, alice, keyTwo, "en", code, newSettings(t, e.password(t, alice), "new", "the new one"))
	if err != nil || !has {
		t.Fatalf("auth.recoverPassword: %v %v", has, err)
	}
	if _, err = e.s.Recover(ctx, alice, keyTwo, "en", code, nil); !errorIs(err, "PASSWORD_RECOVERY_EXPIRED") {
		t.Fatalf("the code again: %v", err)
	}

	pw := e.password(t, alice)
	if !pw.GetHasPassword() || pw.GetHint().GetValue() != "the new one" || !pw.GetHasRecovery() || pw.GetPendingResetDate() != nil {
		t.Fatalf("after the recovery: %v", pw)
	}
	if err = e.s.Check(ctx, alice, keyTwo, e.proof(t, alice, "new")); err != nil {
		t.Fatalf("the new password: %v", err)
	}

	// A recovery with another address: it waits for its code.
	e.advance(time.Minute)
	if _, err = e.s.RequestRecovery(ctx, alice, keyTwo, "en"); err != nil {
		t.Fatal(err)
	}
	s := newSettings(t, e.password(t, alice), "newer", "")
	s.Email = wrapperspb.String("b@example.com")
	if has, err = e.s.Recover(ctx, alice, keyTwo, "en", e.mail.lastCode(t, "a@example.com"), s); err != nil || !has {
		t.Fatalf("a recovery with another email: %v %v", has, err)
	}
	if pw = e.password(t, alice); !pw.GetHasRecovery() || pw.GetEmailUnconfirmedPattern().GetValue() != "b***@example.com" {
		t.Fatalf("the email after a recovery with another: %v", pw)
	}
	if err = e.s.ConfirmEmail(ctx, alice, keyTwo, e.mail.lastCode(t, "b@example.com")); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverRemovesThePassword(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.confirmed(t, alice, "forgotten", "a@example.com")
	mustError(t, func() error { _, err := e.s.RequestRecovery(ctx, bob, keyTwo, "en"); return err }(), "PASSWORD_EMPTY")
	if _, err := e.s.RequestRecovery(ctx, alice, keyTwo, "en"); err != nil {
		t.Fatal(err)
	}
	has, err := e.s.Recover(ctx, alice, keyTwo, "en", e.mail.lastCode(t, "a@example.com"), nil)
	if err != nil || has {
		t.Fatalf("auth.recoverPassword without settings: %v %v", has, err)
	}
	if pw := e.password(t, alice); pw.GetHasPassword() || pw.GetHasRecovery() {
		t.Fatalf("after the recovery: %v", pw)
	}
	if _, err = e.s.RequestRecovery(ctx, alice, keyTwo, "en"); !errorIs(err, "PASSWORD_EMPTY") {
		t.Fatalf("a recovery without a password: %v", err)
	}
	// Without a recovery email: PASSWORD_RECOVERY_NA.
	e.setPassword(t, alice, "pw", "")
	if _, err = e.s.RequestRecovery(ctx, alice, keyTwo, "en"); !errorIs(err, "PASSWORD_RECOVERY_NA") {
		t.Fatalf("no recovery email: %v", err)
	}
}

// A recovery code is bound to the password it was issued under: a change
// of the password meanwhile makes it useless.
func TestRecoveryCodeIsBoundToThePassword(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.confirmed(t, alice, "pw", "a@example.com")
	if _, err := e.s.RequestRecovery(ctx, alice, keyTwo, "en"); err != nil {
		t.Fatal(err)
	}
	code := e.mail.lastCode(t, "a@example.com")
	e.advance(time.Second)
	if err := e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw"), newSettings(t, e.password(t, alice), "pw2", "")); err != nil {
		t.Fatal(err)
	}
	mustError(t, e.s.CheckRecovery(ctx, alice, keyTwo, code), "CODE_INVALID")
}

// A refused email answers FLOOD_WAIT_60 and gives the user's count of the
// minute back; the other counts stay (TestFailedSendsStayCounted), and the
// server's emails of a day are counted all the same.
func TestRecoveryEmailLimits(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.confirmed(t, alice, "pw", "a@example.com")
	e.mail.fail(permanent)
	if _, err := e.s.RequestRecovery(ctx, alice, keyTwo, "en"); !errorIs(err, "FLOOD_WAIT_60") {
		t.Fatalf("a refused email: %v", err)
	}
	if _, err := e.s.RequestRecovery(ctx, alice, keyTwo, "en"); err != nil {
		t.Fatalf("after a refused email: %v", err)
	}
	if _, err := e.s.RequestRecovery(ctx, alice, keyTwo, "en"); err == nil {
		t.Fatal("two emails within a minute")
	}

	// The server's day: 3 so far (confirmation, refused, sent).
	if err := e.s.SetMailSender(e.mail, testHmacKey, 4); err != nil {
		t.Fatal(err)
	}
	e.confirmed(t, bob, "pw", "b@example.com") // the 4th
	_, err := e.s.RequestRecovery(ctx, bob, keyTwo, "en")
	mustFloodWait(t, err)
	midnight := time.Date(e.now.UTC().Year(), e.now.UTC().Month(), e.now.UTC().Day()+1, 0, 0, 0, 0, time.UTC)
	if want := "FLOOD_WAIT_" + strconv.Itoa(int(midnight.Sub(*e.now)/time.Second)); !errorIs(err, want) {
		t.Fatalf("past the server's day: %v, want %s", err, want)
	}
}

// Codes typed in are limited per user and per key.
func TestEmailCodeEntryLimits(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.confirmed(t, alice, "pw", "a@example.com")
	if _, err := e.s.RequestRecovery(ctx, alice, keyTwo, "en"); err != nil {
		t.Fatal(err)
	}
	code := e.mail.lastCode(t, "a@example.com")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	// The code burns at 5 wrong ones; the rest are EXPIRED and not counted.
	for i := 0; i < EmailCodeMaxAttempts; i++ {
		mustError(t, e.s.CheckRecovery(ctx, alice, keyTwo, wrong), "CODE_INVALID")
	}
	for i := 0; i < 3; i++ {
		e.advance(time.Minute)
		if _, err := e.s.RequestRecovery(ctx, alice, keyTwo, "en"); err != nil {
			t.Fatal(err)
		}
		mustError(t, e.s.CheckRecovery(ctx, alice, keyTwo, wrong), "CODE_INVALID")
	}
	// 8 counted on keyTwo; two more, then FLOOD_WAIT even for the right code.
	mustError(t, e.s.CheckRecovery(ctx, alice, keyTwo, wrong), "CODE_INVALID")
	mustError(t, e.s.CheckRecovery(ctx, alice, keyTwo, wrong), "CODE_INVALID")
	mustFloodWait(t, e.s.CheckRecovery(ctx, alice, keyTwo, e.mail.lastCode(t, "a@example.com")))
}

func TestComposeEmail(t *testing.T) {
	m := composeEmail(PurposeRecovery, "ru-RU", "042917")
	if m.Subject != langpack.MailText("PlumaMailRecoverySubject", "ru") || m.Tag != "recovery_code" {
		t.Fatalf("message %+v", m)
	}
	want := langpack.MailText("PlumaMailRecoveryBody", "ru") + "\n\n042917\n\n" +
		langpack.MailText("PlumaMailValidFor", "ru") + "\n" + langpack.MailText("PlumaMailIgnore", "ru") + "\n"
	if m.Text != want {
		t.Fatalf("text %q, want %q", m.Text, want)
	}
	if !strings.Contains(m.HTML, `lang="ru-ru" dir="ltr"`) || !strings.Contains(m.HTML, ">042917<") || strings.Count(m.HTML, "<table") != 1 || len(m.HTML) > 102*1024 {
		t.Fatalf("html %s", m.HTML)
	}
	if ar := composeEmail(PurposeSetup, "ar", "1"); !strings.Contains(ar.HTML, `dir="rtl"`) || ar.Tag != "email_confirm" {
		t.Fatalf("arabic %s", ar.HTML)
	}
	if odd := composeEmail(PurposeSetup, `"><script>`, "1"); strings.Contains(odd.HTML, "<script>") || !strings.Contains(odd.HTML, `lang="en"`) {
		t.Fatalf("a strange lang_code: %s", odd.HTML)
	}
	// Every language: an escaped body, the 30 minutes of EmailCodeTTL.
	if EmailCodeTTL != 30*time.Minute {
		t.Fatal("the emails say 30 minutes: change mail.json with EmailCodeTTL")
	}
}

// A first password set with an address is not in force until the address
// is confirmed, and the clients give it up from the code screen with an
// empty email and no password to check (Android, iOS) or with
// account.cancelPasswordEmail (tdesktop).
func TestFirstPasswordAwaitsItsEmail(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.setWithEmail(t, alice, "pw", "a@example.com")

	// No password yet: not asked at a login, not checked, not needed for a
	// deletion, and recovery has nothing to recover.
	if has, err := e.s.HasPassword(ctx, alice); err != nil || has {
		t.Fatalf("HasPassword before the confirmation: %v %v", has, err)
	}
	if err := e.s.Check(ctx, alice, keyOne, mtclient.NoPassword()); !errors.Is(err, ErrNoPassword) {
		t.Fatalf("Check before the confirmation: %v", err)
	}
	if _, err := e.s.CheckDeletion(ctx, alice, keyOne, mtclient.NoPassword()); err != nil {
		t.Fatalf("CheckDeletion before the confirmation: %v", err)
	}
	if _, err := e.s.RequestRecovery(ctx, alice, keyOne, "en"); !errors.Is(err, ErrNoPassword) {
		t.Fatalf("auth.requestPasswordRecovery before the confirmation: %v", err)
	}

	// Android "abort" (TwoStepVerificationSetupActivity.java:1837-1839,
	// 1874) and iOS openResetPendingEmail: no password, email "".
	if err := e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), emailOnly("")); err != nil {
		t.Fatalf("the setup given up: %v", err)
	}
	if pw := e.password(t, alice); pw.GetHasPassword() || pw.GetEmailUnconfirmedPattern() != nil {
		t.Fatalf("after the setup was given up: %v", pw)
	}
	if p, _ := e.store.Get(ctx, alice); p != nil {
		t.Fatalf("the waiting password is still stored: %+v", p)
	}
	mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, e.mail.lastCode(t, "a@example.com")), "EMAIL_HASH_EXPIRED")
	// Nothing left to give up.
	mustError(t, e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), emailOnly("")), "NEW_SETTINGS_EMPTY")

	// tdesktop "abort": account.cancelPasswordEmail.
	e.advance(time.Minute)
	e.setWithEmail(t, alice, "pw", "a@example.com")
	if err := e.s.CancelEmail(ctx, alice); err != nil {
		t.Fatal(err)
	}
	if p, _ := e.store.Get(ctx, alice); p != nil {
		t.Fatalf("the waiting password outlived the cancel: %+v", p)
	}
}

// iOS changes the address on the code screen by setting the first password
// again with no password to check (TwoFactorAuthDataInputScreen.swift:225,
// 719-733): the new one waits instead, and its confirmation puts it in
// force.
func TestFirstPasswordChangesItsEmail(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.setWithEmail(t, alice, "pw", "a@example.com")
	old := e.mail.lastCode(t, "a@example.com")

	e.advance(time.Minute)
	e.setWithEmail(t, alice, "pw2", "b@example.com")
	if pw := e.password(t, alice); pw.GetHasPassword() || pw.GetEmailUnconfirmedPattern().GetValue() != "b***@example.com" {
		t.Fatalf("after the change of the address: %v", pw)
	}
	code := e.mail.lastCode(t, "b@example.com")
	if code != old {
		mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, old), "CODE_INVALID")
	}
	if err := e.s.ConfirmEmail(ctx, alice, keyOne, code); err != nil {
		t.Fatal(err)
	}
	pw := e.password(t, alice)
	if !pw.GetHasPassword() || !pw.GetHasRecovery() || pw.GetEmailUnconfirmedPattern() != nil {
		t.Fatalf("after the confirmation: %v", pw)
	}
	if err := e.s.Check(ctx, alice, keyOne, e.proof(t, alice, "pw2")); err != nil {
		t.Fatalf("the password of the second request: %v", err)
	}
	p, _ := e.store.Get(ctx, alice)
	if p.SetAt != e.now.Unix() || p.EmailSetAt != e.now.Unix() || p.Email != "b@example.com" {
		t.Fatalf("the row after the confirmation: %+v", p)
	}
	// Now a password is in force: the empty check is refused.
	mustError(t, e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), emailOnly("")), "PASSWORD_HASH_INVALID")
}

// A first password without an address replaces one that waits for its
// address, and is in force at once.
func TestFirstPasswordWithoutEmailReplacesTheWaitingOne(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.setWithEmail(t, alice, "pw", "a@example.com")
	code := e.mail.lastCode(t, "a@example.com")
	e.setPassword(t, alice, "plain", "")
	pw := e.password(t, alice)
	if !pw.GetHasPassword() || pw.GetEmailUnconfirmedPattern() != nil || pw.GetHasRecovery() {
		t.Fatalf("after a first password without an address: %v", pw)
	}
	mustError(t, e.s.ConfirmEmail(ctx, alice, keyOne, code), "EMAIL_HASH_EXPIRED")
	if err := e.s.Check(ctx, alice, keyOne, e.proof(t, alice, "plain")); err != nil {
		t.Fatal(err)
	}
}

// Sends the provider did not take stay counted, but for the minute's: a
// client repeating the request while the provider fails cannot use up the
// server's emails of the day.
func TestFailedSendsStayCounted(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	e.confirmed(t, alice, "pw", "a@example.com")
	for i := 0; i < int(EmailSendPerUserHour.Limit); i++ {
		e.mail.fail(permanent)
		if _, err := e.s.RequestRecovery(ctx, alice, keyTwo, "en"); !errorIs(err, "FLOOD_WAIT_60") {
			t.Fatalf("failed email %d: %v", i+1, err)
		}
	}
	// The sixth in the hour is over the user's limit: the provider is not
	// called.
	calls := len(e.mail.calls)
	_, err := e.s.RequestRecovery(ctx, alice, keyTwo, "en")
	mustFloodWait(t, err)
	if errorIs(err, "FLOOD_WAIT_60") || len(e.mail.calls) != calls {
		t.Fatalf("past the limit: %v, %d calls", err, len(e.mail.calls)-calls)
	}
}

// hookStore runs before once, before the next conditional write.
type hookStore struct {
	*MemStore
	before func()
}

func (h *hookStore) fire() {
	if f := h.before; f != nil {
		h.before = nil
		f()
	}
}

func (h *hookStore) Replace(ctx context.Context, prev, next *Password) (bool, error) {
	h.fire()
	return h.MemStore.Replace(ctx, prev, next)
}

func (h *hookStore) RemoveIf(ctx context.Context, prev *Password) (bool, error) {
	h.fire()
	return h.MemStore.RemoveIf(ctx, prev)
}

func (h *hookStore) SwapEmail(ctx context.Context, userId, setAt int64, prev, next Emails, emailSetAt int64) (bool, error) {
	h.fire()
	return h.MemStore.SwapEmail(ctx, userId, setAt, prev, next, emailSetAt)
}

// A recovery does not overwrite a change of the password made after it read
// the password: the code is spent, the newer password stays.
func TestRecoveryLosesToANewerChange(t *testing.T) {
	ctx := context.Background()
	for _, withSettings := range []bool{false, true} {
		e := newMailEnv(t)
		h := &hookStore{MemStore: e.store}
		e.s.store = h
		e.confirmed(t, alice, "pw", "a@example.com")
		if _, err := e.s.RequestRecovery(ctx, alice, keyTwo, "en"); err != nil {
			t.Fatal(err)
		}
		code := e.mail.lastCode(t, "a@example.com")
		var settings *mtproto.Account_PasswordInputSettings
		if withSettings {
			settings = newSettings(t, e.password(t, alice), "recovered", "")
		}
		h.before = func() {
			// After Recover checked its settings: each account.getPassword
			// issues another new_algo.
			e.advance(time.Second)
			in := e.proof(t, alice, "pw")
			change := newSettings(t, e.password(t, alice), "changed", "")
			if err := e.s.Update(ctx, alice, keyOne, in, change); err != nil {
				t.Errorf("the change meanwhile: %v", err)
			}
		}
		if _, err := e.s.Recover(ctx, alice, keyTwo, "en", code, settings); !errorIs(err, "PASSWORD_RECOVERY_EXPIRED") {
			t.Fatalf("a recovery (settings %v) after a change: %v", withSettings, err)
		}
		if err := e.s.Check(ctx, alice, keyOne, e.proof(t, alice, "changed")); err != nil {
			t.Fatalf("the newer password (settings %v): %v", withSettings, err)
		}
	}
}

// A change of the password with an address does not revert a confirmation
// of the recovery email made after it read the password.
func TestUpdateLosesToANewerConfirmation(t *testing.T) {
	ctx := context.Background()
	e := newMailEnv(t)
	h := &hookStore{MemStore: e.store}
	e.s.store = h
	e.setPassword(t, alice, "pw", "")
	mustError(t, e.s.Update(ctx, alice, keyOne, e.proof(t, alice, "pw"), emailOnly("a@example.com")), "EMAIL_UNCONFIRMED_6")
	code := e.mail.lastCode(t, "a@example.com")
	e.advance(time.Minute)

	in := e.proof(t, alice, "pw")
	s := newSettings(t, e.password(t, alice), "pw2", "")
	s.Email = wrapperspb.String("b@example.com")
	h.before = func() {
		if err := e.s.ConfirmEmail(ctx, alice, keyOne, code); err != nil {
			t.Errorf("the confirmation meanwhile: %v", err)
		}
	}
	mustError(t, e.s.Update(ctx, alice, keyOne, in, s), "SRP_PASSWORD_CHANGED")
	p, _ := e.store.Get(ctx, alice)
	if p.Email != "a@example.com" || p.EmailUnconfirmed != "" {
		t.Fatalf("the confirmation was reverted: %+v", p)
	}
	if err := e.s.Check(ctx, alice, keyOne, e.proof(t, alice, "pw")); err != nil {
		t.Fatalf("the password: %v", err)
	}
}

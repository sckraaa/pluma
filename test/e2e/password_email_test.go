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

//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The recovery email of the cloud password (task 091, ADR 0058): the stand
// sends its emails to test/e2e/fakemail, which the tests read the codes
// from.

var fakeMailAddr = os.Getenv("PLUMA_E2E_FAKEMAIL")

type sentEmail struct {
	Id             string `json:"id"`
	From           string `json:"from"`
	Subject        string `json:"subject"`
	Text           string `json:"text"`
	HTML           string `json:"html"`
	Tag            string `json:"tag"`
	IdempotencyKey string `json:"idempotency_key"`
}

func needFakeMail(t *testing.T) {
	t.Helper()
	if fakeMailAddr == "" {
		t.Skip("PLUMA_E2E_FAKEMAIL is not set: the stand has no mail provider")
	}
}

// emailsTo are the emails fakemail took for address, oldest first.
func emailsTo(t *testing.T, address string) []sentEmail {
	t.Helper()
	resp, err := http.Get("http://" + fakeMailAddr + "/e2e/emails?to=" + url.QueryEscape(address))
	if err != nil {
		t.Fatalf("fakemail: %v", err)
	}
	defer resp.Body.Close()
	var emails []sentEmail
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		t.Fatalf("fakemail: %v", err)
	}
	return emails
}

var emailCodeRe = regexp.MustCompile(`(?m)^(\d{6})$`)

// emailCode waits for the n-th email to address (1 is the first) and
// returns its code.
func emailCode(t *testing.T, address string, n int) string {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		if emails := emailsTo(t, address); len(emails) >= n {
			m := emailCodeRe.FindStringSubmatch(emails[n-1].Text)
			if m == nil {
				t.Fatalf("no code in the email %q", emails[n-1].Text)
			}
			return m[1]
		}
		if time.Now().After(deadline) {
			t.Fatalf("no email %d to %s", n, address)
		}
	}
}

// failEmails makes fakemail answer the next count emails to address with
// status and name.
func failEmails(t *testing.T, address string, status int, name string, count int) {
	t.Helper()
	q := url.Values{"to": {address}, "status": {strconv.Itoa(status)}, "name": {name}, "count": {strconv.Itoa(count)}}
	resp, err := http.Post("http://"+fakeMailAddr+"/e2e/fail?"+q.Encode(), "", nil)
	if err != nil {
		t.Fatalf("fakemail: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("fakemail: %s", resp.Status)
	}
}

// otherCode is a well-formed code that is not code.
func otherCode(code string) string {
	if code == "000000" {
		return "111111"
	}
	return "000000"
}

func invokeBool(ctx context.Context, c *mtclient.Client, req mtproto.TLObject) error {
	o, err := c.Invoke(ctx, req)
	if err != nil {
		return err
	}
	if _, ok := o.(*mtproto.TLBoolTrue); !ok {
		return fmt.Errorf("returned %T, want boolTrue", o)
	}
	return nil
}

func confirmPasswordEmail(ctx context.Context, c *mtclient.Client, code string) error {
	return invokeBool(ctx, c, &mtproto.TLAccountConfirmPasswordEmail{
		Constructor: c.Constructor(mtproto.Predicate_account_confirmPasswordEmail),
		Code:        code,
	})
}

func requestPasswordRecovery(ctx context.Context, c *mtclient.Client) (string, error) {
	o, err := c.Invoke(ctx, &mtproto.TLAuthRequestPasswordRecovery{
		Constructor: c.Constructor(mtproto.Predicate_auth_requestPasswordRecovery),
	})
	if err != nil {
		return "", err
	}
	r, ok := o.(*mtproto.TLAuthPasswordRecovery)
	if !ok {
		return "", fmt.Errorf("auth.requestPasswordRecovery returned %T", o)
	}
	return r.GetEmailPattern(), nil
}

func checkRecoveryPassword(ctx context.Context, c *mtclient.Client, code string) error {
	return invokeBool(ctx, c, &mtproto.TLAuthCheckRecoveryPassword{
		Constructor: c.Constructor(mtproto.Predicate_auth_checkRecoveryPassword),
		Code:        code,
	})
}

// recoverPassword sends auth.recoverPassword with code and, if password is
// not empty, the settings of a new password.
func recoverPassword(ctx context.Context, t *testing.T, c *mtclient.Client, code, password string) (*mtproto.TLAuthAuthorization, error) {
	t.Helper()
	req := &mtproto.TLAuthRecoverPassword{
		Constructor: c.Constructor(mtproto.Predicate_auth_recoverPassword),
		Code:        code,
	}
	if password != "" {
		algo, err := mtclient.NewPasswordAlgo(getPassword(ctx, t, c).GetNewAlgo())
		if err != nil {
			t.Fatal(err)
		}
		hash, err := mtclient.PasswordHash(algo, password)
		if err != nil {
			t.Fatal(err)
		}
		req.NewSettings = mtproto.MakeTLAccountPasswordInputSettings(&mtproto.Account_PasswordInputSettings{
			NewAlgo:         algo,
			NewPasswordHash: hash,
			Hint:            wrapperspb.String(""),
		}).To_Account_PasswordInputSettings()
	}
	o, err := c.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	a, ok := o.(*mtproto.TLAuthAuthorization)
	if !ok {
		return nil, fmt.Errorf("auth.recoverPassword returned %T", o)
	}
	return a, nil
}

func floodWaitSeconds(err error) int {
	var rpcErr *mtclient.RPCError
	if !errors.As(err, &rpcErr) {
		return -1
	}
	s, ok := strings.CutPrefix(rpcErr.Message, "FLOOD_WAIT_")
	if !ok {
		return -1
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

// withRecoveryEmail sets password on u with a confirmed recovery email and
// returns the address.
func withRecoveryEmail(ctx context.Context, t *testing.T, u *user, password string) string {
	t.Helper()
	address := fmt.Sprintf("u%d@example.com", u.self.GetId())
	n := len(emailsTo(t, address))
	if err := updatePassword(ctx, t, u.c, "", password, "", address); !rpcErrorIs(err, "EMAIL_UNCONFIRMED_6") {
		t.Fatalf("a password with a recovery email: %v, want EMAIL_UNCONFIRMED_6", err)
	}
	if err := confirmPasswordEmail(ctx, u.c, emailCode(t, address, n+1)); err != nil {
		t.Fatalf("account.confirmPasswordEmail: %v", err)
	}
	return address
}

// waitServiceTexts waits until u's chat with the service account has n
// messages starting with prefix.
func waitServiceTexts(ctx context.Context, t *testing.T, u *user, prefix string, n int) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(300 * time.Millisecond) {
		texts := serviceTexts(ctx, t, u, prefix)
		if countPrefix(texts, prefix) >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d messages %q in the chat with %d, want %d", countPrefix(texts, prefix), prefix, serviceUserId, n)
		}
	}
}

// The first password with a recovery email: the answer is EMAIL_UNCONFIRMED_6
// (what every client expects), account.password shows no password yet and
// the pattern of the waiting address, and the code from the email confirms
// the address and puts the password in force.
func TestPasswordEmailSetup(t *testing.T) {
	needFakeMail(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	alice := loginIn(ctx, t, testPhones(t, 1)[0], "AliceMail", "ru")
	resetPasswordAtEnd(t, alice)
	getState(ctx, t, alice)
	address := fmt.Sprintf("Alice.%d@Example.COM", alice.self.GetId())
	stored := fmt.Sprintf("Alice.%d@example.com", alice.self.GetId())

	if err := updatePassword(ctx, t, alice.c, "", "alice secret", "", address); !rpcErrorIs(err, "EMAIL_UNCONFIRMED_6") {
		t.Fatalf("a password with a recovery email: %v, want EMAIL_UNCONFIRMED_6", err)
	}
	pw := getPassword(ctx, t, alice.c)
	if pw.GetHasPassword() || pw.GetCurrentAlgo() != nil || pw.GetHasRecovery() || pw.GetEmailUnconfirmedPattern().GetValue() != "Al***@example.com" {
		t.Fatalf("account.getPassword with a waiting email: %v", pw)
	}
	emails := emailsTo(t, stored)
	if len(emails) != 1 || emails[0].Tag != "email_confirm" || !strings.HasPrefix(emails[0].IdempotencyKey, "pluma/setup/") ||
		!strings.HasPrefix(emails[0].Subject, "Pluma: подтвердите") || !strings.Contains(emails[0].HTML, `lang="ru"`) {
		t.Fatalf("the email: %+v", emails)
	}
	code := emailCode(t, stored, 1)
	if err := confirmPasswordEmail(ctx, alice.c, otherCode(code)); !rpcErrorIs(err, "CODE_INVALID") {
		t.Fatalf("a wrong code: %v, want CODE_INVALID", err)
	}
	if err := confirmPasswordEmail(ctx, alice.c, code); err != nil {
		t.Fatalf("account.confirmPasswordEmail: %v", err)
	}
	if err := confirmPasswordEmail(ctx, alice.c, code); !rpcErrorIs(err, "EMAIL_HASH_EXPIRED") {
		t.Fatalf("the code again: %v, want EMAIL_HASH_EXPIRED", err)
	}
	pw = getPassword(ctx, t, alice.c)
	if !pw.GetHasPassword() || !pw.GetHasRecovery() || pw.GetEmailUnconfirmedPattern() != nil || pw.GetLoginEmailPattern() != nil {
		t.Fatalf("account.getPassword after the confirmation: %v", pw)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLAccountGetPasswordSettings{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_getPasswordSettings),
		Password:    passwordProof(ctx, t, alice.c, "alice secret"),
	})
	settings, ok := o.(*mtproto.TLAccountPasswordSettings)
	if err != nil || !ok || settings.GetEmail().GetValue() != stored {
		t.Fatalf("account.getPasswordSettings: %v %v", o, err)
	}
	// One message, when the password came into force with the email.
	const changed = "Two-Step Verification settings changed."
	waitServiceTexts(ctx, t, alice, changed, 1)
	if n := countPrefix(serviceTexts(ctx, t, alice, changed), changed); n != 1 {
		t.Fatalf("%d messages on the change, want 1", n)
	}

	// Not an address: EMAIL_INVALID, and the password stays.
	if err = updatePassword(ctx, t, alice.c, "alice secret", "alice secret", "", "not an address"); !rpcErrorIs(err, "EMAIL_INVALID") {
		t.Fatalf("not an address: %v, want EMAIL_INVALID", err)
	}
}

// account.resendPasswordEmail sends a new code (the old one stops working)
// not sooner than a minute after the last; account.cancelPasswordEmail
// drops the waiting address, and the first password that waited for it
// (tdesktop's "abort").
func TestPasswordEmailResendCancel(t *testing.T) {
	needFakeMail(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	bob := login(ctx, t, testPhones(t, 1)[0], "BobMail")
	resetPasswordAtEnd(t, bob)
	address := fmt.Sprintf("bob.%d@example.com", bob.self.GetId())
	if err := updatePassword(ctx, t, bob.c, "", "bob secret", "", address); !rpcErrorIs(err, "EMAIL_UNCONFIRMED_6") {
		t.Fatalf("a password with a recovery email: %v", err)
	}
	first := emailCode(t, address, 1)

	resend := &mtproto.TLAccountResendPasswordEmail{Constructor: bob.c.Constructor(mtproto.Predicate_account_resendPasswordEmail)}
	err := invokeBool(ctx, bob.c, resend)
	wait := floodWaitSeconds(err)
	if wait <= 0 || wait > 60 {
		t.Fatalf("a resend at once: %v, want FLOOD_WAIT_X up to 60", err)
	}
	time.Sleep(time.Duration(wait+1) * time.Second)
	if err = invokeBool(ctx, bob.c, resend); err != nil {
		t.Fatalf("account.resendPasswordEmail: %v", err)
	}
	second := emailCode(t, address, 2)
	if second != first {
		if err = confirmPasswordEmail(ctx, bob.c, first); !rpcErrorIs(err, "CODE_INVALID") {
			t.Fatalf("the first code after a resend: %v, want CODE_INVALID", err)
		}
	}

	if err = invokeBool(ctx, bob.c, &mtproto.TLAccountCancelPasswordEmail{Constructor: bob.c.Constructor(mtproto.Predicate_account_cancelPasswordEmail)}); err != nil {
		t.Fatalf("account.cancelPasswordEmail: %v", err)
	}
	// The first password waited for the address: it goes with it.
	if pw := getPassword(ctx, t, bob.c); pw.GetEmailUnconfirmedPattern() != nil || pw.GetHasRecovery() || pw.GetHasPassword() {
		t.Fatalf("account.getPassword after the cancel: %v", pw)
	}
	if err = confirmPasswordEmail(ctx, bob.c, second); !rpcErrorIs(err, "EMAIL_HASH_EXPIRED") {
		t.Fatalf("a code after the cancel: %v, want EMAIL_HASH_EXPIRED", err)
	}
	if err = invokeBool(ctx, bob.c, resend); !rpcErrorIs(err, "EMAIL_HASH_EXPIRED") {
		t.Fatalf("a resend after the cancel: %v, want EMAIL_HASH_EXPIRED", err)
	}
}

// A login that gave the phone code and stops at the password recovers it by
// email and logs in with the new one; the other session stays and is told.
func TestPasswordRecoveryLoggedOut(t *testing.T) {
	needFakeMail(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	carol := login(ctx, t, testPhones(t, 1)[0], "CarolMail")
	resetPasswordAtEnd(t, carol)
	getState(ctx, t, carol)
	address := withRecoveryEmail(ctx, t, carol, "forgotten")
	sent := len(emailsTo(t, address))

	device := signInStopsAtPassword(ctx, t, carol, "e2e CarolMail new device")
	if !getPassword(ctx, t, device).GetHasRecovery() {
		t.Fatal("has_recovery at the login")
	}
	pattern, err := requestPasswordRecovery(ctx, device)
	if err != nil || pattern != "u"+strconv.FormatInt(carol.self.GetId(), 10)[:1]+"***@example.com" {
		t.Fatalf("auth.requestPasswordRecovery: %q %v", pattern, err)
	}
	code := emailCode(t, address, sent+1)
	if e := emailsTo(t, address)[sent]; e.Tag != "recovery_code" || !strings.HasPrefix(e.IdempotencyKey, "pluma/recovery/") {
		t.Fatalf("the recovery email: %+v", e)
	}
	if err = checkRecoveryPassword(ctx, device, otherCode(code)); !rpcErrorIs(err, "CODE_INVALID") {
		t.Fatalf("a wrong code: %v, want CODE_INVALID", err)
	}
	if err = checkRecoveryPassword(ctx, device, code); err != nil {
		t.Fatalf("auth.checkRecoveryPassword: %v", err)
	}
	auth, err := recoverPassword(ctx, t, device, code, "remembered")
	if err != nil || auth.GetUser().GetId() != carol.self.GetId() {
		t.Fatalf("auth.recoverPassword: %v %v", auth, err)
	}
	if _, err = recoverPassword(ctx, t, device, code, ""); err == nil {
		t.Fatal("the code was used twice")
	}
	getState(ctx, t, &user{c: device, phone: carol.phone, self: auth.GetUser()})

	// The first session is alive, and both are told.
	getState(ctx, t, carol)
	waitServiceTexts(ctx, t, carol, "Cloud password reset.", 1)
	if pw := getPassword(ctx, t, carol.c); !pw.GetHasPassword() || !pw.GetHasRecovery() {
		t.Fatalf("account.getPassword after the recovery: %v", pw)
	}

	// The new password logs in, the old one does not.
	third := signInStopsAtPassword(ctx, t, carol, "e2e CarolMail third")
	if _, err = checkPassword(ctx, third, passwordProof(ctx, t, third, "forgotten")); !rpcErrorIs(err, "PASSWORD_HASH_INVALID") {
		t.Fatalf("the old password: %v", err)
	}
	if _, err = checkPassword(ctx, third, passwordProof(ctx, t, third, "remembered")); err != nil {
		t.Fatalf("the new password: %v", err)
	}
}

// A logged-in session recovers by email without new settings: the password
// and its email go.
func TestPasswordRecoveryLoggedIn(t *testing.T) {
	needFakeMail(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dave := login(ctx, t, testPhones(t, 1)[0], "DaveMail")
	resetPasswordAtEnd(t, dave)
	address := withRecoveryEmail(ctx, t, dave, "dave secret")
	sent := len(emailsTo(t, address))
	if _, err := requestPasswordRecovery(ctx, dave.c); err != nil {
		t.Fatalf("auth.requestPasswordRecovery: %v", err)
	}
	auth, err := recoverPassword(ctx, t, dave.c, emailCode(t, address, sent+1), "")
	if err != nil || auth.GetUser().GetId() != dave.self.GetId() {
		t.Fatalf("auth.recoverPassword: %v %v", auth, err)
	}
	if pw := getPassword(ctx, t, dave.c); pw.GetHasPassword() || pw.GetHasRecovery() {
		t.Fatalf("account.getPassword after the recovery: %v", pw)
	}
	login(ctx, t, dave.phone, "DaveMail")
}

// What is refused: a key with no login, an account without a recovery
// email, and an email the provider did not take (FLOOD_WAIT_60, nothing
// sent, the user's count of the minute given back).
func TestPasswordRecoveryRefusals(t *testing.T) {
	needFakeMail(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	stranger, err := mtclient.Dial(ctx, config(t, "e2e stranger"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	closeChecked(t, "stranger", stranger)
	if _, err = requestPasswordRecovery(ctx, stranger); !rpcErrorIs(err, "AUTH_KEY_UNREGISTERED") {
		t.Fatalf("a key with no login: %v, want AUTH_KEY_UNREGISTERED", err)
	}

	eve := login(ctx, t, testPhones(t, 1)[0], "EveMail")
	resetPasswordAtEnd(t, eve)
	if _, err = requestPasswordRecovery(ctx, eve.c); !rpcErrorIs(err, "PASSWORD_EMPTY") {
		t.Fatalf("no password: %v, want PASSWORD_EMPTY", err)
	}
	if err = updatePassword(ctx, t, eve.c, "", "eve secret", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = requestPasswordRecovery(ctx, eve.c); !rpcErrorIs(err, "PASSWORD_RECOVERY_NA") {
		t.Fatalf("no recovery email: %v, want PASSWORD_RECOVERY_NA", err)
	}
	if pw := getPassword(ctx, t, eve.c); pw.GetHasRecovery() {
		t.Fatalf("has_recovery without an email: %v", pw)
	}

	// A provider that fails, then one that refuses the key.
	address := fmt.Sprintf("eve.%d@example.com", eve.self.GetId())
	if err = updatePassword(ctx, t, eve.c, "eve secret", "eve secret", "", address); !rpcErrorIs(err, "EMAIL_UNCONFIRMED_6") {
		t.Fatal(err)
	}
	if err = confirmPasswordEmail(ctx, eve.c, emailCode(t, address, 1)); err != nil {
		t.Fatal(err)
	}
	failEmails(t, address, http.StatusServiceUnavailable, "service_unavailable", 3)
	if _, err = requestPasswordRecovery(ctx, eve.c); !rpcErrorIs(err, "FLOOD_WAIT_60") {
		t.Fatalf("a failing provider: %v, want FLOOD_WAIT_60", err)
	}
	if err = checkRecoveryPassword(ctx, eve.c, "123456"); !rpcErrorIs(err, "PASSWORD_RECOVERY_EXPIRED") {
		t.Fatalf("a code after a failed email: %v, want PASSWORD_RECOVERY_EXPIRED", err)
	}
	failEmails(t, address, http.StatusUnauthorized, "invalid_api_key", 1)
	if _, err = requestPasswordRecovery(ctx, eve.c); !rpcErrorIs(err, "FLOOD_WAIT_60") {
		t.Fatalf("a refused key: %v, want FLOOD_WAIT_60", err)
	}
	if n := len(emailsTo(t, address)); n != 1 {
		t.Fatalf("%d emails, want the confirmation only", n)
	}
	// The refused emails gave back the count of the minute.
	if _, err = requestPasswordRecovery(ctx, eve.c); err != nil {
		t.Fatalf("after the refused emails: %v", err)
	}
	if err = checkRecoveryPassword(ctx, eve.c, emailCode(t, address, 2)); err != nil {
		t.Fatalf("auth.checkRecoveryPassword: %v", err)
	}
}

// The code screen of a first setup, as Android and iOS leave it: Android's
// "abort" sends no password and an empty email
// (TwoStepVerificationSetupActivity.java:1837-1839, 1874), iOS changes the
// address by setting the password again with no password to check
// (TwoFactorAuthDataInputScreen.swift:225, TwoStepVerification.swift:
// 121-139). Both work because the first password is not in force yet.
func TestPasswordEmailFirstSetupCodeScreen(t *testing.T) {
	needFakeMail(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	frank := login(ctx, t, testPhones(t, 1)[0], "FrankMail")
	resetPasswordAtEnd(t, frank)
	first := fmt.Sprintf("frank.%d@example.com", frank.self.GetId())
	second := fmt.Sprintf("frank2.%d@example.com", frank.self.GetId())

	// Android: abort.
	if err := updatePassword(ctx, t, frank.c, "", "frank secret", "", first); !rpcErrorIs(err, "EMAIL_UNCONFIRMED_6") {
		t.Fatalf("a first password with a recovery email: %v", err)
	}
	abort := &mtproto.TLAccountUpdatePasswordSettings{
		Constructor: frank.c.Constructor(mtproto.Predicate_account_updatePasswordSettings),
		Password:    mtclient.NoPassword(),
		NewSettings: mtproto.MakeTLAccountPasswordInputSettings(&mtproto.Account_PasswordInputSettings{
			Email: wrapperspb.String(""),
		}).To_Account_PasswordInputSettings(),
	}
	if err := invokeBool(ctx, frank.c, abort); err != nil {
		t.Fatalf("Android's abort: %v", err)
	}
	if pw := getPassword(ctx, t, frank.c); pw.GetHasPassword() || pw.GetEmailUnconfirmedPattern() != nil {
		t.Fatalf("account.getPassword after the abort: %v", pw)
	}
	if err := confirmPasswordEmail(ctx, frank.c, emailCode(t, first, 1)); !rpcErrorIs(err, "EMAIL_HASH_EXPIRED") {
		t.Fatalf("the code after the abort: %v, want EMAIL_HASH_EXPIRED", err)
	}

	// iOS: another address on the code screen.
	time.Sleep(61 * time.Second) // one email a minute
	if err := updatePassword(ctx, t, frank.c, "", "frank secret", "", first); !rpcErrorIs(err, "EMAIL_UNCONFIRMED_6") {
		t.Fatalf("a first password with a recovery email: %v", err)
	}
	// Another address gets its email at once: the minute is per address.
	if err := updatePassword(ctx, t, frank.c, "", "frank other", "", second); !rpcErrorIs(err, "EMAIL_UNCONFIRMED_6") {
		t.Fatalf("iOS's change of the address: %v", err)
	}
	pw := getPassword(ctx, t, frank.c)
	if pw.GetHasPassword() || pw.GetEmailUnconfirmedPattern().GetValue() != "fr***@example.com" {
		t.Fatalf("account.getPassword after the change of the address: %v", pw)
	}
	if err := confirmPasswordEmail(ctx, frank.c, emailCode(t, second, 1)); err != nil {
		t.Fatalf("account.confirmPasswordEmail: %v", err)
	}
	if pw = getPassword(ctx, t, frank.c); !pw.GetHasPassword() || !pw.GetHasRecovery() {
		t.Fatalf("account.getPassword after the confirmation: %v", pw)
	}
	o, err := frank.c.Invoke(ctx, &mtproto.TLAccountGetPasswordSettings{
		Constructor: frank.c.Constructor(mtproto.Predicate_account_getPasswordSettings),
		Password:    passwordProof(ctx, t, frank.c, "frank other"),
	})
	if settings, ok := o.(*mtproto.TLAccountPasswordSettings); err != nil || !ok || settings.GetEmail().GetValue() != second {
		t.Fatalf("account.getPasswordSettings: %v %v", o, err)
	}
}

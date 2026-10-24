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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The 2FA password (task 008): set on one device, a login on another
// stops at SESSION_PASSWORD_NEEDED until auth.checkPassword, wrong
// passwords end in FLOOD_WAIT, the password changes and goes away.

func getPassword(ctx context.Context, t *testing.T, c *mtclient.Client) *mtproto.Account_Password {
	t.Helper()
	o, err := c.Invoke(ctx, &mtproto.TLAccountGetPassword{Constructor: c.Constructor(mtproto.Predicate_account_getPassword)})
	if err != nil {
		t.Fatalf("account.getPassword: %v", err)
	}
	r, ok := o.(*mtproto.TLAccountPassword)
	if !ok {
		t.Fatalf("account.getPassword returned %T", o)
	}
	return r.To_Account_Password()
}

// passwordProof is the proof of password for a fresh srp_id.
func passwordProof(ctx context.Context, t *testing.T, c *mtclient.Client, password string) *mtproto.InputCheckPasswordSRP {
	t.Helper()
	in, err := mtclient.CheckPassword(getPassword(ctx, t, c), password)
	if err != nil {
		t.Fatalf("SRP: %v", err)
	}
	return in
}

// updatePassword sets password (empty: removes it) on c, given the current
// one (empty: none is set), with an optional recovery email.
func updatePassword(ctx context.Context, t *testing.T, c *mtclient.Client, current, password, hint, email string) error {
	t.Helper()
	pw := getPassword(ctx, t, c)
	in := mtclient.NoPassword()
	if current != "" {
		var err error
		if in, err = mtclient.CheckPassword(pw, current); err != nil {
			t.Fatalf("SRP: %v", err)
		}
	}
	settings := &mtproto.Account_PasswordInputSettings{
		NewAlgo:         mtproto.MakeTLPasswordKdfAlgoUnknown(nil).To_PasswordKdfAlgo(),
		NewPasswordHash: []byte{},
		Hint:            wrapperspb.String(""),
	}
	if password != "" {
		algo, err := mtclient.NewPasswordAlgo(pw.GetNewAlgo())
		if err != nil {
			t.Fatal(err)
		}
		if settings.NewPasswordHash, err = mtclient.PasswordHash(algo, password); err != nil {
			t.Fatal(err)
		}
		settings.NewAlgo = algo
		settings.Hint = wrapperspb.String(hint)
	}
	if email != "" {
		settings.Email = wrapperspb.String(email)
	}
	_, err := c.Invoke(ctx, &mtproto.TLAccountUpdatePasswordSettings{
		Constructor: c.Constructor(mtproto.Predicate_account_updatePasswordSettings),
		Password:    in,
		NewSettings: mtproto.MakeTLAccountPasswordInputSettings(settings).To_Account_PasswordInputSettings(),
	})
	return err
}

func checkPassword(ctx context.Context, c *mtclient.Client, in *mtproto.InputCheckPasswordSRP) (*mtproto.TLAuthAuthorization, error) {
	o, err := c.Invoke(ctx, &mtproto.TLAuthCheckPassword{
		Constructor: c.Constructor(mtproto.Predicate_auth_checkPassword),
		Password:    in,
	})
	if err != nil {
		return nil, err
	}
	a, _ := o.(*mtproto.TLAuthAuthorization)
	return a, nil
}

// signInStopsAtPassword connects a new device of u and signs in with a code
// from the administrator: the login must stop at SESSION_PASSWORD_NEEDED.
func signInStopsAtPassword(ctx context.Context, t *testing.T, u *user, device string) *mtclient.Client {
	t.Helper()
	c, err := mtclient.Dial(ctx, config(t, device))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	closeChecked(t, device, c)
	o, err := c.Invoke(ctx, &mtproto.TLAuthSendCode{
		Constructor: c.Constructor(mtproto.Predicate_auth_sendCode),
		PhoneNumber: u.phone,
		ApiId:       1,
		ApiHash:     "e2e",
		Settings:    &mtproto.CodeSettings{Constructor: c.Constructor(mtproto.Predicate_codeSettings)},
	})
	if err != nil {
		t.Fatalf("auth.sendCode: %v", err)
	}
	sent, ok := o.(*mtproto.TLAuthSentCode)
	if !ok {
		t.Fatalf("auth.sendCode returned %T", o)
	}
	m := loginCodeRe.FindStringSubmatch(admin(t, "login-code", u.phone))
	if m == nil {
		t.Fatal("pluma-admin login-code printed no code")
	}
	_, err = c.Invoke(ctx, &mtproto.TLAuthSignIn{
		Constructor:          c.Constructor(mtproto.Predicate_auth_signIn),
		PhoneNumber:          u.phone,
		PhoneCodeHash:        sent.GetPhoneCodeHash(),
		PhoneCode_FLAGSTRING: mtproto.MakeFlagsString(m[1]),
	})
	if !rpcErrorIs(err, "SESSION_PASSWORD_NEEDED") {
		t.Fatalf("auth.signIn with a 2FA password: %v, want SESSION_PASSWORD_NEEDED", err)
	}
	return c
}

func isFloodWait(err error) bool {
	var rpcErr *mtclient.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == 420 && strings.HasPrefix(rpcErr.Message, "FLOOD_WAIT_")
}

// resetPasswordAtEnd removes u's password when the test ends, whatever
// happens: other tests log in to the same phones without one.
func resetPasswordAtEnd(t *testing.T, u *user) {
	t.Cleanup(func() { admin(t, "password", "reset", u.phone) })
}

func TestTwoFa(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	phones := testPhones(t, 1)
	alice := login(ctx, t, phones[0], "Alice2fa")
	resetPasswordAtEnd(t, alice)
	getState(ctx, t, alice)

	pw := getPassword(ctx, t, alice.c)
	if pw.GetHasPassword() || pw.GetHasRecovery() || pw.GetNewAlgo().GetPredicateName() != mtproto.Predicate_passwordKdfAlgoModPow {
		t.Fatalf("account.getPassword without a password: %v", pw)
	}

	// No mail on the server: a recovery email is refused, skipping it works.
	if err := updatePassword(ctx, t, alice.c, "", "first secret", "first", "alice@example.com"); !rpcErrorIs(err, "EMAIL_INVALID") {
		t.Fatalf("a password with a recovery email: %v, want EMAIL_INVALID", err)
	}
	if err := updatePassword(ctx, t, alice.c, "", "first secret", "first", ""); err != nil {
		t.Fatalf("set the password: %v", err)
	}
	if pw = getPassword(ctx, t, alice.c); !pw.GetHasPassword() || pw.GetHint().GetValue() != "first" || len(pw.GetSrp_B()) != 256 {
		t.Fatalf("account.getPassword with a password: %v", pw)
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountGetPasswordSettings{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_getPasswordSettings),
		Password:    passwordProof(ctx, t, alice.c, "first secret"),
	}); err != nil {
		t.Fatalf("account.getPasswordSettings: %v", err)
	}

	// A second device stops at the password.
	second := signInStopsAtPassword(ctx, t, alice, "e2e Alice2fa second")
	if _, err := second.Invoke(ctx, &mtproto.TLUpdatesGetState{Constructor: second.Constructor(mtproto.Predicate_updates_getState)}); !rpcErrorIs(err, "SESSION_PASSWORD_NEEDED") {
		t.Fatalf("updates.getState before the password: %v", err)
	}
	if _, err := second.Invoke(ctx, &mtproto.TLAccountGetPasswordSettings{
		Constructor: second.Constructor(mtproto.Predicate_account_getPasswordSettings),
		Password:    mtclient.NoPassword(),
	}); !rpcErrorIs(err, "SESSION_PASSWORD_NEEDED") {
		t.Fatalf("account.getPasswordSettings before the password: %v", err)
	}
	if hint := getPassword(ctx, t, second).GetHint().GetValue(); hint != "first" {
		t.Fatalf("the hint at the login: %q", hint)
	}
	if _, err := checkPassword(ctx, second, passwordProof(ctx, t, second, "wrong secret")); !rpcErrorIs(err, "PASSWORD_HASH_INVALID") {
		t.Fatalf("a wrong password: %v, want PASSWORD_HASH_INVALID", err)
	}
	stale := passwordProof(ctx, t, second, "first secret")
	auth, err := checkPassword(ctx, second, stale)
	if err != nil || auth.GetUser().GetId() != alice.self.GetId() {
		t.Fatalf("auth.checkPassword: %v %v", auth, err)
	}
	if _, err = checkPassword(ctx, second, stale); !rpcErrorIs(err, "SRP_ID_INVALID") {
		t.Fatalf("an srp_id used twice: %v, want SRP_ID_INVALID", err)
	}
	secondUser := &user{c: second, phone: alice.phone, self: auth.GetUser()}
	getState(ctx, t, secondUser)

	// Change: the old password no longer logs in.
	if err = updatePassword(ctx, t, alice.c, "wrong secret", "second secret", "", ""); !rpcErrorIs(err, "PASSWORD_HASH_INVALID") {
		t.Fatalf("a change with a wrong current password: %v", err)
	}
	if err = updatePassword(ctx, t, alice.c, "first secret", "second secret", "", ""); err != nil {
		t.Fatalf("change the password: %v", err)
	}
	third := signInStopsAtPassword(ctx, t, alice, "e2e Alice2fa third")
	if _, err = checkPassword(ctx, third, passwordProof(ctx, t, third, "first secret")); !rpcErrorIs(err, "PASSWORD_HASH_INVALID") {
		t.Fatalf("the old password: %v", err)
	}
	if _, err = checkPassword(ctx, third, passwordProof(ctx, t, third, "second secret")); err != nil {
		t.Fatalf("the new password: %v", err)
	}

	// A reset from a logged-in session waits a week; declined, it is gone.
	o, err := alice.c.Invoke(ctx, &mtproto.TLAccountResetPassword{Constructor: alice.c.Constructor(mtproto.Predicate_account_resetPassword)})
	wait, ok := o.(*mtproto.TLAccountResetPasswordRequestedWait)
	if err != nil || !ok || int64(wait.GetUntilDate()) < time.Now().Add(7*24*time.Hour-time.Hour).Unix() {
		t.Fatalf("account.resetPassword: %T %v", o, err)
	}
	if pending := getPassword(ctx, t, alice.c).GetPendingResetDate(); pending.GetValue() != wait.GetUntilDate() {
		t.Fatalf("pending_reset_date %v, want %d", pending, wait.GetUntilDate())
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLAccountDeclinePasswordReset{Constructor: alice.c.Constructor(mtproto.Predicate_account_declinePasswordReset)}); err != nil {
		t.Fatalf("account.declinePasswordReset: %v", err)
	}
	if getPassword(ctx, t, alice.c).GetPendingResetDate() != nil {
		t.Fatal("pending_reset_date after the decline")
	}

	// Remove: a new device logs in with the code only.
	if err = updatePassword(ctx, t, alice.c, "second secret", "", "", ""); err != nil {
		t.Fatalf("remove the password: %v", err)
	}
	if getPassword(ctx, t, alice.c).GetHasPassword() {
		t.Fatal("the password is still set")
	}
	login(ctx, t, alice.phone, "Alice2fa")
}

func TestTwoFaFloodWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	phones := testPhones(t, 1)
	bob := login(ctx, t, phones[0], "Bob2fa")
	resetPasswordAtEnd(t, bob)
	if err := updatePassword(ctx, t, bob.c, "", "bob secret", "", ""); err != nil {
		t.Fatal(err)
	}

	guesser := signInStopsAtPassword(ctx, t, bob, "e2e Bob2fa guesser")
	wrong := 0
	for ; ; wrong++ {
		_, err := checkPassword(ctx, guesser, passwordProof(ctx, t, guesser, "guess"))
		if rpcErrorIs(err, "PASSWORD_HASH_INVALID") {
			if wrong >= 5 {
				t.Fatalf("%d wrong passwords without FLOOD_WAIT", wrong+1)
			}
			continue
		}
		if !isFloodWait(err) {
			t.Fatalf("guess %d: %v, want PASSWORD_HASH_INVALID or FLOOD_WAIT_X", wrong+1, err)
		}
		break
	}
	if wrong != 5 {
		t.Fatalf("FLOOD_WAIT after %d wrong passwords, want 5", wrong)
	}
	// Past the limit the right password waits too.
	if _, err := checkPassword(ctx, guesser, passwordProof(ctx, t, guesser, "bob secret")); !isFloodWait(err) {
		t.Fatalf("the right password past the limit: %v", err)
	}

	// The administrator removes the password (pluma-admin password reset).
	if out := admin(t, "password", "reset", bob.phone); !strings.Contains(out, "is removed") {
		t.Fatalf("pluma-admin password reset: %s", out)
	}
	login(ctx, t, bob.phone, "Bob2fa")
}

func TestTwoFaQRLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 1)
	carol := login(ctx, t, phones[0], "Carol2fa")
	resetPasswordAtEnd(t, carol)
	if err := updatePassword(ctx, t, carol.c, "", "carol secret", "", ""); err != nil {
		t.Fatal(err)
	}

	tv, err := mtclient.Dial(ctx, config(t, "e2e Carol2fa qr"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	closeChecked(t, "qr", tv)
	export := func() (mtproto.TLObject, error) {
		return tv.Invoke(ctx, &mtproto.TLAuthExportLoginToken{
			Constructor: tv.Constructor(mtproto.Predicate_auth_exportLoginToken),
			ApiId:       1,
			ApiHash:     "e2e",
		})
	}
	o, err := export()
	token, ok := o.(*mtproto.TLAuthLoginToken)
	if err != nil || !ok {
		t.Fatalf("auth.exportLoginToken: %T %v", o, err)
	}
	o, err = carol.c.Invoke(ctx, &mtproto.TLAuthAcceptLoginToken{
		Constructor: carol.c.Constructor(mtproto.Predicate_auth_acceptLoginToken),
		Token:       token.GetToken(),
	})
	if err != nil {
		t.Fatalf("auth.acceptLoginToken: %v", err)
	}
	if a, ok := o.(*mtproto.TLAuthorization); !ok || !a.GetPasswordPending() {
		t.Fatalf("auth.acceptLoginToken returned %T without password_pending", o)
	}
	if _, err = export(); !rpcErrorIs(err, "SESSION_PASSWORD_NEEDED") {
		t.Fatalf("auth.exportLoginToken after the accept: %v, want SESSION_PASSWORD_NEEDED", err)
	}
	auth, err := checkPassword(ctx, tv, passwordProof(ctx, t, tv, "carol secret"))
	if err != nil || auth.GetUser().GetId() != carol.self.GetId() {
		t.Fatalf("auth.checkPassword: %v %v", auth, err)
	}
	getState(ctx, t, &user{c: tv, phone: carol.phone, self: auth.GetUser()})
}

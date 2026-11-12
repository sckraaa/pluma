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
	"bytes"
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/logincode"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Password returns account.password of userId: whether a password is set
// and, if it is, its algorithm, hint and a fresh srp_id with srp_B for one
// check; the algorithms for a new password; the pending reset; and, with
// mail on, whether a recovery email is set (has_recovery) and the pattern
// of the one waiting for its code (email_unconfirmed_pattern, which iOS
// needs to show the code screen: TwoStepVerification.swift:210-212).
// A first password waiting for its recovery email is not one yet: the
// answer has no password and the pattern of the address, the state the
// clients show as "not set, an email waits" (iOS TwoStepVerification.swift:
// 8-10, 20-24; Web K tabs.ts:1164). login_email_pattern is never set: Pluma
// has no login email.
func (s *Service) Password(ctx context.Context, userId int64) (*mtproto.Account_Password, error) {
	row, err := s.row(ctx, userId)
	if err != nil {
		return nil, err
	}
	p := row
	if p != nil && p.AwaitsEmail {
		p = nil
	}
	fresh, err := s.randomBytes(newSalt1PrefixLen + salt2Len)
	if err != nil {
		return nil, errInternal(ctx, "random", err)
	}
	salts, err := s.state.newAlgo(ctx, userId, fresh, NewAlgoTTL)
	if err != nil {
		return nil, errInternal(ctx, "new algo of user "+formatKeyId(userId), err)
	}
	secureSalt, err := s.randomBytes(secureSaltPrefixLen)
	if err != nil {
		return nil, errInternal(ctx, "random", err)
	}
	secureRandom, err := s.randomBytes(secureRandomLen)
	if err != nil {
		return nil, errInternal(ctx, "random", err)
	}

	r := &mtproto.Account_Password{
		HasRecovery:     p != nil && s.mail != nil && p.Email != "",
		HasSecureValues: false,
		HasPassword:     p != nil,
		NewAlgo:         makeAlgo(salts[:newSalt1PrefixLen], salts[newSalt1PrefixLen:]),
		// Telegram Passport is not served: its settings are ignored.
		NewSecureAlgo: mtproto.MakeTLSecurePasswordKdfAlgoPBKDF2(&mtproto.SecurePasswordKdfAlgo{
			Salt: secureSalt,
		}).To_SecurePasswordKdfAlgo(),
		SecureRandom: secureRandom,
	}
	if p != nil {
		srpId, B, err := s.newSRP(ctx, p)
		if err != nil {
			return nil, err
		}
		r.CurrentAlgo = makeAlgo(p.Salt1, p.Salt2)
		r.Srp_B = B
		r.SrpId = wrapperspb.Int64(srpId)
		if p.Hint != "" {
			r.Hint = wrapperspb.String(p.Hint)
		}
		if p.PendingResetDate != 0 {
			r.PendingResetDate = wrapperspb.Int32(int32(p.PendingResetDate))
		}
	}
	if row != nil && s.mail != nil && row.EmailUnconfirmed != "" {
		r.EmailUnconfirmedPattern = wrapperspb.String(emailPattern(row.EmailUnconfirmed))
	}
	return mtproto.MakeTLAccountPassword(r).To_Account_Password(), nil
}

// newSRP issues an srp_id for one check of p and returns it with srp_B.
func (s *Service) newSRP(ctx context.Context, p *Password) (int64, []byte, error) {
	srpId, err := s.newSRPId()
	if err != nil {
		return 0, nil, errInternal(ctx, "random", err)
	}
	b, B := newSRPB(p)
	if err = s.state.putSRP(ctx, srpId, srpSession{userId: p.UserId, b: b, B: B, fp: fingerprint(p)}, SRPTTL); err != nil {
		return 0, nil, errInternal(ctx, "store srp_id of user "+formatKeyId(p.UserId), err)
	}
	return srpId, B, nil
}

// Check verifies the password of userId given as in (InputCheckPasswordSRP)
// from the permanent key authKeyId, under the limits of both. It returns
// nil for the right password and otherwise:
//
//   - ErrNoPassword: userId has no password;
//   - PASSWORD_HASH_INVALID: no password given, or a wrong one;
//   - SRP_ID_INVALID: an srp_id that is not userId's, already used or expired;
//   - SRP_PASSWORD_CHANGED: the password changed since the srp_id was issued;
//   - FLOOD_WAIT_X: too many wrong passwords.
//
// The right password also cancels a pending deletion of the account
// (account.deleteAccount without the password): its owner is here.
func (s *Service) Check(ctx context.Context, userId, authKeyId int64, in *mtproto.InputCheckPasswordSRP) error {
	p, err := s.get(ctx, userId)
	if err != nil {
		return err
	}
	if p == nil {
		return ErrNoPassword
	}
	return s.check(ctx, p, authKeyId, in)
}

// TransferFreshWait is how long a password must have been set and a session
// logged in before they may transfer the ownership of a channel
// (core.telegram.org/method/channels.editCreator: PASSWORD_TOO_FRESH_%d,
// SESSION_TOO_FRESH_%d, "less than 24 hours ago").
const TransferFreshWait = int64(24 * 60 * 60)

// CheckTransfer is Check for a transfer of ownership: before the password
// is compared the account must have one set for TransferFreshWait and the
// session (sessionCreated, unix seconds; 0 when not known) must be as old,
// or the answer is PASSWORD_TOO_FRESH_X or SESSION_TOO_FRESH_X with the
// seconds left. They come before the hash because the clients probe with
// an empty password and show these as the reasons the transfer cannot be
// done yet (iOS ChannelOwnershipTransfer.swift:36-53, Android
// ChatRightsEditActivity.java:1148).
func (s *Service) CheckTransfer(ctx context.Context, userId, authKeyId int64, in *mtproto.InputCheckPasswordSRP, sessionCreated int64) error {
	p, err := s.get(ctx, userId)
	if err != nil {
		return err
	}
	if p == nil {
		return ErrNoPassword
	}
	now := s.now().Unix()
	if wait := p.SetAt + TransferFreshWait - now; wait > 0 {
		return mtproto.NewErrPasswordTooFreshX(int32(wait))
	}
	if wait := sessionCreated + TransferFreshWait - now; sessionCreated != 0 && wait > 0 {
		return mtproto.NewErrSessionTooFreshX(int32(wait))
	}
	return s.check(ctx, p, authKeyId, in)
}

func (s *Service) check(ctx context.Context, p *Password, authKeyId int64, in *mtproto.InputCheckPasswordSRP) error {
	if passwordEmpty(in) {
		return mtproto.ErrPasswordHashInvalid
	}
	sess, ok, err := s.state.takeSRP(ctx, in.GetSrpId())
	switch {
	case err != nil:
		return errInternal(ctx, "srp_id of user "+formatKeyId(p.UserId), err)
	case !ok || sess.userId != p.UserId:
		logx.WithContext(ctx).Infof("twofa: user %d: unknown srp_id", p.UserId)
		return mtproto.ErrSrpIdInvalid
	case !bytes.Equal(sess.fp, fingerprint(p)):
		logx.WithContext(ctx).Infof("twofa: user %d: the password changed since the srp_id", p.UserId)
		return mtproto.ErrSrpPasswordChanged
	}

	counted, err := s.countCheck(ctx, p.UserId, authKeyId)
	if err != nil {
		return err
	}
	if !verifySRP(p, sess.b, sess.B, in.GetA(), in.GetM1()) {
		logx.WithContext(ctx).Infof("twofa: user %d: wrong password", p.UserId)
		return mtproto.ErrPasswordHashInvalid
	}
	s.refund(ctx, counted)

	if p.PendingDeleteDate != 0 {
		if cancelled, err := s.store.CancelDelete(ctx, p.UserId); err != nil {
			logx.WithContext(ctx).Errorf("twofa: cancel the deletion of user %d: %v", p.UserId, err)
		} else if cancelled {
			logx.WithContext(ctx).Infof("twofa: user %d gave the password: the deletion of the account is cancelled", p.UserId)
		}
	}
	return nil
}

// counted is one count of a check under a limit.
type counted struct {
	rule    logincode.Rule
	subject string
}

// countCheck counts one comparison for userId and authKeyId under every
// limit. If one is exceeded, the counts already made are taken back (no
// comparison happens) and FLOOD_WAIT_X is returned.
func (s *Service) countCheck(ctx context.Context, userId, authKeyId int64) ([]counted, error) {
	user, key := formatKeyId(userId), formatKeyId(authKeyId)
	all := []counted{
		{CheckPerUser, user},
		{CheckPerUserDay, user},
		{CheckPerAuthKey, key},
		{CheckPerAuthKeyDay, key},
	}
	for i, c := range all {
		if err := s.limiter.Hit(ctx, c.rule, c.subject); err != nil {
			s.refund(ctx, all[:i])
			return nil, err
		}
	}
	return all, nil
}

func (s *Service) refund(ctx context.Context, cs []counted) {
	for _, c := range cs {
		s.limiter.Refund(ctx, c.rule, c.subject)
	}
}

// passwordEmpty reports whether in gives no password (inputCheckPasswordEmpty
// or nothing).
func passwordEmpty(in *mtproto.InputCheckPasswordSRP) bool {
	return in == nil || in.GetPredicateName() == mtproto.Predicate_inputCheckPasswordEmpty || in.GetSrpId() == 0
}

// Settings returns account.passwordSettings of userId after checking its
// password: the confirmed recovery email, whole, which only the owner of the
// password sees; there is no Telegram Passport.
func (s *Service) Settings(ctx context.Context, userId, authKeyId int64, in *mtproto.InputCheckPasswordSRP) (*mtproto.Account_PasswordSettings, error) {
	p, err := s.get(ctx, userId)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNoPassword
	}
	if err = s.check(ctx, p, authKeyId, in); err != nil {
		return nil, err
	}
	r := &mtproto.Account_PasswordSettings{}
	if s.mail != nil && p.Email != "" {
		r.Email = wrapperspb.String(p.Email)
	}
	return mtproto.MakeTLAccountPasswordSettings(r).To_Account_PasswordSettings(), nil
}

// Update applies account.updatePasswordSettings of userId: the current
// password in (if one is set) and the new settings. A new_algo with
// new_password_hash sets or changes the password. An empty hash removes
// it, with passwordKdfAlgoUnknown (Android, tdesktop) or with the new_algo
// of account.getPassword (Web K); Telegram accepts both.
//
// The recovery email (flags.1, ADR 0058, p. 4) is written with the password,
// in the same statement:
//
//   - an empty one removes both the confirmed and the waiting address (the
//     client's "skip");
//   - the confirmed one keeps it and drops a waiting one;
//   - another one waits for its code, which is sent, and the answer is
//     EMAIL_UNCONFIRMED_6 with everything else of the request applied, as
//     the clients expect (Android TwoStepVerificationSetupActivity.java:
//     1985-1997, iOS TwoStepVerification.swift:202-213, tdesktop
//     api_cloud_password.cpp:244-252), even when the email did not go out:
//     "send again" repeats it;
//   - without mail on the server, or without a password left, an address
//     is EMAIL_INVALID.
//
// A first password given with an address is not in force until the address
// is confirmed (Store.AwaitsEmail): the clients keep "no password" on the
// code screen and give the setup up from there with no password to check,
// an empty email and no new_algo (Android TwoStepVerificationSetupActivity.
// java:1529-1562, 1809, 1837-1839, 1874; iOS TwoStepVerificationUnlock
// Controller.swift:774, SetupTwoStepVerificationControllerNode.swift:342),
// which removes that password. Until then a new first password replaces it.
//
// Every write is conditional on the row read at the start: a change made
// meanwhile answers SRP_PASSWORD_CHANGED, and the client reads
// account.getPassword again.
//
// Telegram Passport settings are ignored. Every new setting is checked
// before the password, so a request refused for them does not use up its
// srp_id.
func (s *Service) Update(ctx context.Context, userId, authKeyId int64, in *mtproto.InputCheckPasswordSRP, settings *mtproto.Account_PasswordInputSettings) error {
	return s.UpdateIn(ctx, userId, authKeyId, "", in, settings)
}

// UpdateIn is Update with lang, the language of the session, for the email
// with the code.
func (s *Service) UpdateIn(ctx context.Context, userId, authKeyId int64, lang string, in *mtproto.InputCheckPasswordSRP, settings *mtproto.Account_PasswordInputSettings) error {
	row, err := s.row(ctx, userId)
	if err != nil {
		return err
	}
	p := row // in force
	if p != nil && p.AwaitsEmail {
		p = nil
	}
	if settings == nil {
		return mtproto.ErrNewSettingsInvalid
	}
	address, emailGiven, err := s.newEmail(ctx, userId, settings)
	if err != nil {
		return err
	}

	algo := settings.GetNewAlgo()
	var (
		next    *Password
		removes bool
	)
	switch {
	case algo == nil:
		// No change of the password (an email or Passport change only).
		if p == nil {
			switch {
			case address != "":
				return mtproto.ErrEmailInvalid
			case emailGiven && row != nil:
				// The setup of a first password is given up.
				return s.dropAwaiting(ctx, row)
			}
			return mtproto.ErrNewSettingsEmpty
		}
	case algo.GetPredicateName() == mtproto.Predicate_passwordKdfAlgoUnknown && len(settings.GetNewPasswordHash()) != 0:
		return mtproto.ErrNewSettingsInvalid
	case algo.GetPredicateName() == mtproto.Predicate_passwordKdfAlgoUnknown,
		algo.GetPredicateName() == mtproto.Predicate_passwordKdfAlgoModPow && len(settings.GetNewPasswordHash()) == 0:
		if p == nil {
			if row != nil && address == "" {
				return s.dropAwaiting(ctx, row)
			}
			return mtproto.ErrNewSettingsEmpty
		}
		removes = true
	case algo.GetPredicateName() == mtproto.Predicate_passwordKdfAlgoModPow:
		if next, err = s.newPassword(ctx, userId, algo, settings); err != nil {
			return err
		}
	default:
		return mtproto.ErrNewSettingsInvalid
	}
	if removes && address != "" {
		// A recovery email needs a password.
		return mtproto.ErrEmailInvalid
	}

	if p != nil {
		if err = s.check(ctx, p, authKeyId, in); err != nil {
			return err
		}
	}

	was := row.emails() // stored, with the code of its waiting address
	prev := p.emails()  // of the password in force
	emails := prev
	if emailGiven {
		emails = emailsAfter(prev, address)
	}
	switch {
	case removes:
		removed, err := s.store.RemoveIf(ctx, p)
		if err != nil {
			return errInternal(ctx, "remove the password of user "+formatKeyId(userId), err)
		}
		if !removed {
			logx.WithContext(ctx).Infof("twofa: user %d: the password settings changed meanwhile", userId)
			return mtproto.ErrSrpPasswordChanged
		}
		s.dropCodes(ctx, userId)
		logx.WithContext(ctx).Infof("twofa: user %d removed the password", userId)
	case next != nil:
		next.Email, next.EmailUnconfirmed, next.EmailSetAt = emails.Email, emails.Unconfirmed, emailSetAt(p, emails, s.now())
		next.AwaitsEmail = p == nil && emails.Unconfirmed != ""
		replaced, err := s.store.Replace(ctx, row, next)
		if err != nil {
			return errInternal(ctx, "set the password of user "+formatKeyId(userId), err)
		}
		if !replaced {
			logx.WithContext(ctx).Infof("twofa: user %d: the password settings changed meanwhile", userId)
			return mtproto.ErrSrpPasswordChanged
		}
		if next.AwaitsEmail {
			logx.WithContext(ctx).Infof("twofa: user %d set a first password: in force once its recovery email is confirmed", userId)
		} else {
			logx.WithContext(ctx).Infof("twofa: user %d set a password (had one: %v)", userId, p != nil)
		}
	case emails != prev:
		swapped, err := s.store.SwapEmail(ctx, userId, p.SetAt, prev, emails, emailSetAt(p, emails, s.now()))
		if err != nil {
			return errInternal(ctx, "set the recovery email of user "+formatKeyId(userId), err)
		}
		if !swapped {
			// The password or its email changed under the request: the
			// client reads account.getPassword again.
			logx.WithContext(ctx).Infof("twofa: user %d: the password settings changed meanwhile", userId)
			return mtproto.ErrSrpPasswordChanged
		}
	}
	if algo != nil {
		// The next password gets new salts.
		s.dropNewAlgo(ctx, userId)
	}
	if removes {
		return nil
	}
	if emails.Unconfirmed != was.Unconfirmed {
		s.dropCode(ctx, PurposeSetup, userId)
	}
	if !emailGiven {
		return nil
	}
	confirmed, waiting := emails.Email != "", emails.Unconfirmed != ""
	logx.WithContext(ctx).Infof("twofa: user %d: recovery email confirmed %v, waiting %v", userId, confirmed, waiting)
	if emails.Unconfirmed == "" {
		return nil
	}
	// The settings are applied whatever happens to the email: the answer
	// is EMAIL_UNCONFIRMED_6, and account.resendPasswordEmail sends it again.
	if err = s.sendCode(ctx, emailRequest{
		purpose: PurposeSetup, userId: userId, authKeyId: authKeyId, address: emails.Unconfirmed, lang: lang,
	}); err != nil {
		logx.WithContext(ctx).Infof("twofa: user %d: the confirmation email waits for a resend: %v", userId, err)
	}
	return mtproto.NewErrEmailUnconfirmedX(EmailCodeLength)
}

// dropAwaiting removes row, a first password waiting for its recovery
// email, and the code of the address: the setup is given up. If the row
// changed meanwhile the answer is SRP_PASSWORD_CHANGED.
func (s *Service) dropAwaiting(ctx context.Context, row *Password) error {
	removed, err := s.store.RemoveIf(ctx, row)
	if err != nil {
		return errInternal(ctx, "remove the waiting password of user "+formatKeyId(row.UserId), err)
	}
	if !removed {
		logx.WithContext(ctx).Infof("twofa: user %d: the password settings changed meanwhile", row.UserId)
		return mtproto.ErrSrpPasswordChanged
	}
	s.dropCode(ctx, PurposeSetup, row.UserId)
	logx.WithContext(ctx).Infof("twofa: user %d gave up the first password waiting for its recovery email", row.UserId)
	return nil
}

// newEmail is the recovery email of settings: given is false without
// flags.1; an empty address removes the email. An address is
// EMAIL_INVALID if it is not a bare address or there is no mail on the
// server.
func (s *Service) newEmail(ctx context.Context, userId int64, settings *mtproto.Account_PasswordInputSettings) (address string, given bool, err error) {
	email := settings.GetEmail()
	if email == nil {
		return "", false, nil
	}
	raw := strings.TrimSpace(email.GetValue())
	if raw == "" {
		return "", true, nil
	}
	if s.mail == nil {
		logx.WithContext(ctx).Infof("twofa: user %d: a recovery email without mail on the server", userId)
		return "", false, mtproto.ErrEmailInvalid
	}
	address, ok := normalizeEmail(raw)
	if !ok {
		logx.WithContext(ctx).Infof("twofa: user %d: a recovery email that is not an address", userId)
		return "", false, mtproto.ErrEmailInvalid
	}
	return address, true, nil
}

// emailsAfter is the recovery email after address (normalized; empty: none)
// is given for one with prev.
func emailsAfter(prev Emails, address string) Emails {
	switch address {
	case "":
		return Emails{}
	case prev.Email:
		return Emails{Email: prev.Email}
	default:
		return Emails{Email: prev.Email, Unconfirmed: address}
	}
}

// emailSetAt is email_set_at for emails replacing those of p.
func emailSetAt(p *Password, emails Emails, now time.Time) int64 {
	switch {
	case emails.Email == "":
		return 0
	case p != nil && p.Email == emails.Email:
		return p.EmailSetAt
	default:
		return now.Unix()
	}
}

// newPassword checks a new password of userId: new_algo must be the one
// account.getPassword issued (NEW_SALT_INVALID otherwise) in the SRP group,
// new_password_hash a verifier of the group and the hint not too long
// (NEW_SETTINGS_INVALID otherwise).
func (s *Service) newPassword(ctx context.Context, userId int64, algo *mtproto.PasswordKdfAlgo, settings *mtproto.Account_PasswordInputSettings) (*Password, error) {
	if algo.GetG() != srpG || !bytes.Equal(algo.GetP(), srpP) {
		return nil, mtproto.ErrNewSettingsInvalid
	}
	issued, err := s.state.issuedNewAlgo(ctx, userId)
	if err != nil {
		return nil, errInternal(ctx, "new algo of user "+formatKeyId(userId), err)
	}
	salt1, salt2 := algo.GetSalt1(), algo.GetSalt2()
	if issued == nil ||
		len(salt1) < minSalt1Len || len(salt1) > maxSaltLen ||
		!srpUtil(issued[:newSalt1PrefixLen], nil).CheckNewSalt1(salt1) ||
		!bytes.Equal(salt2, issued[newSalt1PrefixLen:]) {
		logx.WithContext(ctx).Infof("twofa: user %d: new_algo was not issued", userId)
		return nil, mtproto.ErrNewSaltInvalid
	}
	if !validVerifier(settings.GetNewPasswordHash()) {
		return nil, mtproto.ErrNewSettingsInvalid
	}
	hint := settings.GetHint().GetValue()
	if !utf8.ValidString(hint) || utf8.RuneCountInString(hint) > MaxHintLength {
		return nil, mtproto.ErrNewSettingsInvalid
	}
	return &Password{
		UserId:   userId,
		Salt1:    bytes.Clone(salt1),
		Salt2:    bytes.Clone(salt2),
		Verifier: padSRPValue(settings.GetNewPasswordHash()),
		Hint:     hint,
		SetAt:    s.now().Unix(),
	}, nil
}

// Reset applies account.resetPassword of the logged-in userId, as Telegram
// does without a recovery email: the first request answers
// account.resetPasswordRequestedWait until ResetWait from now, a request
// after that date removes the password (account.resetPasswordOk), one
// before it answers the same wait. A user without a password gets
// account.resetPasswordOk.
func (s *Service) Reset(ctx context.Context, userId int64) (*mtproto.Account_ResetPasswordResult, error) {
	p, err := s.get(ctx, userId)
	if err != nil {
		return nil, err
	}
	now := s.now().Unix()
	ok := mtproto.MakeTLAccountResetPasswordOk(nil).To_Account_ResetPasswordResult()
	if p == nil {
		return ok, nil
	}
	if p.PendingResetDate == 0 {
		date := now + int64(ResetWait.Seconds())
		started, err := s.store.StartReset(ctx, userId, date)
		if err != nil {
			return nil, errInternal(ctx, "start the reset of user "+formatKeyId(userId), err)
		}
		if !started {
			// A concurrent request started it, or the password is gone.
			return s.Reset(ctx, userId)
		}
		logx.WithContext(ctx).Infof("twofa: user %d asked to reset the password", userId)
		return requestedWait(date), nil
	}
	if now < p.PendingResetDate {
		return requestedWait(p.PendingResetDate), nil
	}
	removed, err := s.store.FinishReset(ctx, userId, now)
	if err != nil {
		return nil, errInternal(ctx, "reset the password of user "+formatKeyId(userId), err)
	}
	if !removed {
		// Declined or changed meanwhile.
		return s.Reset(ctx, userId)
	}
	s.dropNewAlgo(ctx, userId)
	logx.WithContext(ctx).Infof("twofa: user %d: the password is reset", userId)
	return ok, nil
}

func requestedWait(date int64) *mtproto.Account_ResetPasswordResult {
	return mtproto.MakeTLAccountResetPasswordRequestedWait(&mtproto.Account_ResetPasswordResult{
		UntilDate: int32(date),
	}).To_Account_ResetPasswordResult()
}

// DeclineReset applies account.declinePasswordReset: it cancels the pending
// reset of userId's password (RESET_REQUEST_MISSING if there is none).
func (s *Service) DeclineReset(ctx context.Context, userId int64) error {
	if userId == 0 {
		return mtproto.ErrAuthKeyUnregistered
	}
	cancelled, err := s.store.CancelReset(ctx, userId)
	if err != nil {
		return errInternal(ctx, "decline the reset of user "+formatKeyId(userId), err)
	}
	if !cancelled {
		return mtproto.ErrResetRequestMissing
	}
	logx.WithContext(ctx).Infof("twofa: user %d declined the password reset", userId)
	return nil
}

// CheckDeletion decides whether account.deleteAccount from authKeyId may
// delete userId's account now, as Telegram does for an account protected by
// a password:
//
//   - without a password: yes;
//   - with the password given in in: if it is right (the errors of Check);
//   - without it (the login of someone who forgot it, or a session that
//     does not give it): only DeleteWait after the first such request;
//     until then 2FA_CONFIRM_WAIT_X with the seconds left, and started
//     tells that this request began the wait. The right password, given
//     anywhere meanwhile, cancels it.
//
// After the deletion the caller calls Forget.
func (s *Service) CheckDeletion(ctx context.Context, userId, authKeyId int64, in *mtproto.InputCheckPasswordSRP) (started bool, err error) {
	p, err := s.get(ctx, userId)
	if err != nil || p == nil {
		return false, err
	}
	if !passwordEmpty(in) {
		return false, s.check(ctx, p, authKeyId, in)
	}
	now := s.now().Unix()
	if p.PendingDeleteDate == 0 {
		date := now + int64(DeleteWait.Seconds())
		if started, err = s.store.StartDelete(ctx, userId, date); err != nil {
			return false, errInternal(ctx, "start the deletion of user "+formatKeyId(userId), err)
		}
		if !started {
			return s.CheckDeletion(ctx, userId, authKeyId, in)
		}
		logx.WithContext(ctx).Infof("twofa: deletion of user %d without the password: waits until %d", userId, date)
		return true, mtproto.NewErr2faConfirmWaitX(int32(date - now))
	}
	if now < p.PendingDeleteDate {
		return false, mtproto.NewErr2faConfirmWaitX(int32(p.PendingDeleteDate - now))
	}
	return false, nil
}

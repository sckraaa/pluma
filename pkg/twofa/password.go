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
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/logincode"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Password returns account.password of userId: whether a password is set
// and, if it is, its algorithm, hint and a fresh srp_id with srp_B for one
// check; the algorithms for a new password; and the pending reset.
func (s *Service) Password(ctx context.Context, userId int64) (*mtproto.Account_Password, error) {
	p, err := s.get(ctx, userId)
	if err != nil {
		return nil, err
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
		HasRecovery:     false,
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
// password: there is no recovery email and no Telegram Passport.
func (s *Service) Settings(ctx context.Context, userId, authKeyId int64, in *mtproto.InputCheckPasswordSRP) (*mtproto.Account_PasswordSettings, error) {
	if err := s.Check(ctx, userId, authKeyId, in); err != nil {
		return nil, err
	}
	return mtproto.MakeTLAccountPasswordSettings(&mtproto.Account_PasswordSettings{}).To_Account_PasswordSettings(), nil
}

// Update applies account.updatePasswordSettings of userId: the current
// password in (if one is set) and the new settings. A new_algo with
// new_password_hash sets or changes the password, passwordKdfAlgoUnknown
// with an empty hash removes it.
//
// A recovery email cannot be set: there is no mail on the server. An empty
// one (what clients send when the user skips it) is ignored, any other is
// EMAIL_INVALID, the answer that lets the user skip it. Telegram Passport
// settings are ignored.
//
// Every new setting is checked before the password, so a request refused
// for them does not use up its srp_id.
func (s *Service) Update(ctx context.Context, userId, authKeyId int64, in *mtproto.InputCheckPasswordSRP, settings *mtproto.Account_PasswordInputSettings) error {
	p, err := s.get(ctx, userId)
	if err != nil {
		return err
	}
	if settings == nil {
		return mtproto.ErrNewSettingsInvalid
	}
	if email := settings.GetEmail(); email != nil && strings.TrimSpace(email.GetValue()) != "" {
		logx.WithContext(ctx).Infof("twofa: user %d: a recovery email is not supported", userId)
		return mtproto.ErrEmailInvalid
	}

	algo := settings.GetNewAlgo()
	var next *Password
	switch {
	case algo == nil:
		// No change of the password (an email or Passport change only).
		if p == nil {
			return mtproto.ErrNewSettingsEmpty
		}
	case algo.GetPredicateName() == mtproto.Predicate_passwordKdfAlgoUnknown:
		if len(settings.GetNewPasswordHash()) != 0 {
			return mtproto.ErrNewSettingsInvalid
		}
		if p == nil {
			return mtproto.ErrNewSettingsEmpty
		}
	case algo.GetPredicateName() == mtproto.Predicate_passwordKdfAlgoModPow:
		if next, err = s.newPassword(ctx, userId, algo, settings); err != nil {
			return err
		}
	default:
		return mtproto.ErrNewSettingsInvalid
	}

	if p != nil {
		if err = s.check(ctx, p, authKeyId, in); err != nil {
			return err
		}
	}

	switch {
	case algo == nil:
		return nil
	case next == nil:
		if _, err = s.store.Delete(ctx, userId); err != nil {
			return errInternal(ctx, "remove the password of user "+formatKeyId(userId), err)
		}
		logx.WithContext(ctx).Infof("twofa: user %d removed the password", userId)
	default:
		if err = s.store.Put(ctx, next); err != nil {
			return errInternal(ctx, "set the password of user "+formatKeyId(userId), err)
		}
		logx.WithContext(ctx).Infof("twofa: user %d set a password (had one: %v)", userId, p != nil)
	}
	// The next password gets new salts.
	s.dropNewAlgo(ctx, userId)
	return nil
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

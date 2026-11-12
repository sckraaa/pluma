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
	"strings"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

// The recovery email of the password (task 091, ADR 0058, p. 4): its
// confirmation, and the recovery of a forgotten password with a code sent
// to it (https://core.telegram.org/api/srp, "Email verification" and
// "Password recovery"). Setting it is part of Update.

// ConfirmEmail applies account.confirmPasswordEmail of userId: the code
// sent to the waiting address makes it the recovery email. Errors as the
// clients read them (iOS ConfirmTwoStepRecoveryEmail.swift:16-28, tdesktop
// settings_cloud_password_email_confirm.cpp:285-294):
//
//   - CODE_EMPTY: no code;
//   - EMAIL_HASH_EXPIRED: no address waits, or its code expired or burnt;
//   - CODE_INVALID: a wrong code (EmailCodeMaxAttempts of them burn it);
//   - FLOOD_WAIT_X: too many codes typed in.
//
// A first password waiting for this address comes into force with it, as
// the clients expect (Android TwoStepVerificationSetupActivity.java:
// 1529-1562 sets has_password only now).
func (s *Service) ConfirmEmail(ctx context.Context, userId, authKeyId int64, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return mtproto.ErrCodeEmpty
	}
	p, err := s.row(ctx, userId)
	if err != nil {
		return err
	}
	if s.mail == nil || p == nil || p.EmailUnconfirmed == "" {
		return mtproto.ErrEmailHashExpired
	}
	r, err := s.checkCode(ctx, PurposeSetup, p, authKeyId, p.EmailUnconfirmed, code, true)
	switch {
	case err != nil:
		return err
	case r == codeWrong:
		return mtproto.ErrCodeInvalid
	case r == codeNone:
		return mtproto.ErrEmailHashExpired
	}
	var swapped bool
	if p.AwaitsEmail {
		next := *p
		now := s.now().Unix()
		next.Email, next.EmailUnconfirmed, next.EmailSetAt = p.EmailUnconfirmed, "", now
		next.AwaitsEmail, next.SetAt = false, now
		swapped, err = s.store.Replace(ctx, p, &next)
	} else {
		prev := p.emails()
		next := Emails{Email: p.EmailUnconfirmed}
		swapped, err = s.store.SwapEmail(ctx, userId, p.SetAt, prev, next, emailSetAt(p, next, s.now()))
	}
	if err != nil {
		return errInternal(ctx, "confirm the recovery email of user "+formatKeyId(userId), err)
	}
	if !swapped {
		logx.WithContext(ctx).Infof("twofa: user %d: the recovery email changed while it was confirmed", userId)
		return mtproto.ErrEmailHashExpired
	}
	logx.WithContext(ctx).Infof("twofa: user %d confirmed the recovery email (the password comes into force: %v)", userId, p.AwaitsEmail)
	return nil
}

// ResendEmail applies account.resendPasswordEmail of userId: a new code to
// the waiting address, in lang. EMAIL_HASH_EXPIRED if none waits,
// FLOOD_WAIT_X past a limit, and FLOOD_WAIT_60 if the provider did not take
// the email (the clients read it and wait; an internal error is retried by
// tgnet itself).
func (s *Service) ResendEmail(ctx context.Context, userId, authKeyId int64, lang string) error {
	p, err := s.row(ctx, userId)
	if err != nil {
		return err
	}
	if s.mail == nil || p == nil || p.EmailUnconfirmed == "" {
		return mtproto.ErrEmailHashExpired
	}
	err = s.sendCode(ctx, emailRequest{
		purpose: PurposeSetup, userId: userId, authKeyId: authKeyId, address: p.EmailUnconfirmed, lang: lang,
	})
	return sendAnswer(err)
}

// CancelEmail applies account.cancelPasswordEmail of userId: the waiting
// address and its code go; a confirmed recovery email stays (ADR 0058, p. 4,
// a decision to confirm). A first password waiting for the address goes with
// it: tdesktop calls this to abort the setup (settings_cloud_password_email_
// confirm.cpp:89-93, api_cloud_password.cpp:109-126). Done also when nothing
// waits.
func (s *Service) CancelEmail(ctx context.Context, userId int64) error {
	p, err := s.row(ctx, userId)
	if err != nil {
		return err
	}
	if p == nil || p.EmailUnconfirmed == "" {
		return nil
	}
	if p.AwaitsEmail {
		if err = s.dropAwaiting(ctx, p); errors.Is(err, mtproto.ErrSrpPasswordChanged) {
			return s.CancelEmail(ctx, userId)
		}
		return err
	}
	prev := p.emails()
	next := Emails{Email: p.Email}
	swapped, err := s.store.SwapEmail(ctx, userId, p.SetAt, prev, next, p.EmailSetAt)
	if err != nil {
		return errInternal(ctx, "cancel the recovery email of user "+formatKeyId(userId), err)
	}
	if !swapped {
		// Changed meanwhile: cancel what waits now.
		return s.CancelEmail(ctx, userId)
	}
	s.dropCode(ctx, PurposeSetup, userId)
	logx.WithContext(ctx).Infof("twofa: user %d cancelled the waiting recovery email", userId)
	return nil
}

// RequestRecovery applies auth.requestPasswordRecovery for userId (logged
// in on authKeyId, or logging in after the phone code): a recovery code to
// the recovery email, in lang. It returns the pattern of the address
// (auth.passwordRecovery.email_pattern), or:
//
//   - PASSWORD_EMPTY: userId has no password;
//   - PASSWORD_RECOVERY_NA: no recovery email, or no mail on the server
//     (the clients then offer the reset: Web K PasswordCard.tsx:73-100, iOS
//     AuthorizationSequenceController.swift:965-993);
//   - FLOOD_WAIT_X: past a limit; FLOOD_WAIT_60 if the provider did not take
//     the email.
func (s *Service) RequestRecovery(ctx context.Context, userId, authKeyId int64, lang string) (string, error) {
	p, err := s.get(ctx, userId)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "", ErrNoPassword
	}
	if s.mail == nil || p.Email == "" {
		return "", mtproto.ErrPasswordRecoveryNa
	}
	err = s.sendCode(ctx, emailRequest{
		purpose: PurposeRecovery, userId: userId, authKeyId: authKeyId, address: p.Email, setAt: p.SetAt, lang: lang,
	})
	if err = sendAnswer(err); err != nil {
		return "", err
	}
	return emailPattern(p.Email), nil
}

// CheckRecovery applies auth.checkRecoveryPassword: whether code is the
// live recovery code of userId, without using it up. CODE_EMPTY,
// PASSWORD_RECOVERY_EXPIRED (no live code), CODE_INVALID, FLOOD_WAIT_X.
func (s *Service) CheckRecovery(ctx context.Context, userId, authKeyId int64, code string) error {
	p, err := s.recoveryPassword(ctx, userId, code)
	if err != nil {
		return err
	}
	return s.checkRecoveryCode(ctx, p, authKeyId, strings.TrimSpace(code), false)
}

// Recover applies auth.recoverPassword of userId: the recovery code is
// taken (two requests cannot use one), then without new settings the
// password and its recovery email are removed, and with them the new
// password is set as Update sets one; the recovery email stays unless the
// settings give another, which then waits for its code (sent here; the
// answer cannot be EMAIL_UNCONFIRMED_6, the client sees the pattern in
// account.getPassword). A pending reset is dropped either way. hasPassword
// tells whether a password is left. The settings are checked before the
// code is taken: NEW_SETTINGS_INVALID, NEW_SALT_INVALID, EMAIL_INVALID. The
// change is conditional on the password the code was issued under: if it
// changed after the read, the taken code had no effect,
// PASSWORD_RECOVERY_EXPIRED.
func (s *Service) Recover(ctx context.Context, userId, authKeyId int64, lang, code string, settings *mtproto.Account_PasswordInputSettings) (hasPassword bool, err error) {
	p, err := s.recoveryPassword(ctx, userId, code)
	if err != nil {
		return false, err
	}

	var (
		next       *Password
		address    string
		emailGiven bool
	)
	if settings != nil {
		algo := settings.GetNewAlgo()
		switch {
		case algo == nil:
			return false, mtproto.ErrNewSettingsInvalid
		case algo.GetPredicateName() == mtproto.Predicate_passwordKdfAlgoUnknown && len(settings.GetNewPasswordHash()) != 0:
			return false, mtproto.ErrNewSettingsInvalid
		case algo.GetPredicateName() == mtproto.Predicate_passwordKdfAlgoUnknown,
			algo.GetPredicateName() == mtproto.Predicate_passwordKdfAlgoModPow && len(settings.GetNewPasswordHash()) == 0:
			// The password goes.
		case algo.GetPredicateName() == mtproto.Predicate_passwordKdfAlgoModPow:
			if next, err = s.newPassword(ctx, userId, algo, settings); err != nil {
				return false, err
			}
		default:
			return false, mtproto.ErrNewSettingsInvalid
		}
		if address, emailGiven, err = s.newEmail(ctx, userId, settings); err != nil {
			return false, err
		}
		if next == nil && address != "" {
			return false, mtproto.ErrEmailInvalid
		}
	}

	if err = s.checkRecoveryCode(ctx, p, authKeyId, strings.TrimSpace(code), true); err != nil {
		return false, err
	}

	if next == nil {
		removed, err := s.store.RemoveIf(ctx, p)
		if err != nil {
			return false, errInternal(ctx, "remove the password of user "+formatKeyId(userId), err)
		}
		if !removed {
			logx.WithContext(ctx).Infof("twofa: user %d: the password changed during the recovery", userId)
			return false, mtproto.ErrPasswordRecoveryExpired
		}
		s.dropNewAlgo(ctx, userId)
		s.dropCodes(ctx, userId)
		logx.WithContext(ctx).Infof("twofa: user %d recovered the password by email: removed", userId)
		return false, nil
	}

	prev := p.emails()
	emails := prev
	if emailGiven {
		emails = emailsAfter(prev, address)
	}
	next.Email, next.EmailUnconfirmed, next.EmailSetAt = emails.Email, emails.Unconfirmed, emailSetAt(p, emails, s.now())
	replaced, err := s.store.Replace(ctx, p, next)
	if err != nil {
		return false, errInternal(ctx, "set the password of user "+formatKeyId(userId), err)
	}
	if !replaced {
		logx.WithContext(ctx).Infof("twofa: user %d: the password changed during the recovery", userId)
		return false, mtproto.ErrPasswordRecoveryExpired
	}
	s.dropNewAlgo(ctx, userId)
	logx.WithContext(ctx).Infof("twofa: user %d recovered the password by email: a new one is set", userId)

	if emails.Unconfirmed != prev.Unconfirmed {
		s.dropCode(ctx, PurposeSetup, userId)
		if emails.Unconfirmed != "" {
			if err := s.sendCode(ctx, emailRequest{
				purpose: PurposeSetup, userId: userId, authKeyId: authKeyId, address: emails.Unconfirmed, lang: lang,
			}); err != nil {
				logx.WithContext(ctx).Infof("twofa: user %d: the confirmation email waits for a resend: %v", userId, err)
			}
		}
	}
	return true, nil
}

// recoveryPassword is the password of userId a recovery code can be typed
// for: CODE_EMPTY without a code, PASSWORD_RECOVERY_EXPIRED when no code
// can be live (no password, no recovery email, no mail on the server).
func (s *Service) recoveryPassword(ctx context.Context, userId int64, code string) (*Password, error) {
	if strings.TrimSpace(code) == "" {
		return nil, mtproto.ErrCodeEmpty
	}
	p, err := s.get(ctx, userId)
	if err != nil {
		return nil, err
	}
	if s.mail == nil || p == nil || p.Email == "" {
		return nil, mtproto.ErrPasswordRecoveryExpired
	}
	return p, nil
}

func (s *Service) checkRecoveryCode(ctx context.Context, p *Password, authKeyId int64, code string, take bool) error {
	r, err := s.checkCode(ctx, PurposeRecovery, p, authKeyId, p.Email, code, take)
	switch {
	case err != nil:
		return err
	case r == codeWrong:
		return mtproto.ErrCodeInvalid
	case r == codeNone:
		return mtproto.ErrPasswordRecoveryExpired
	}
	return nil
}

// sendAnswer is the answer to a request whose whole point is an email:
// a send the provider did not take is FLOOD_WAIT_60, which every client
// reads as "wait" (iOS Authorization.swift:1331-1345), where an internal
// error would be retried by tgnet at once.
func sendAnswer(err error) error {
	if errors.Is(err, errSendFailed) {
		return mtproto.NewErrFloodWaitX(60)
	}
	return err
}

// EmailUnconfirmed reports whether err is the EMAIL_UNCONFIRMED_X of
// UpdateIn: the request is applied and a recovery email waits for its code.
func EmailUnconfirmed(err error) bool {
	st, ok := status.FromError(err)
	return err != nil && ok && strings.HasPrefix(st.Message(), "EMAIL_UNCONFIRMED_")
}

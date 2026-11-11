// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package logic

import (
	"context"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
	"github.com/teamgram/teamgram-server/app/bff/authorization/model"
	"github.com/teamgram/teamgram-server/pkg/logincode"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
)

// PhoneCodeStore keeps login requests (model.PhoneCodeTransaction) by auth
// key and phone. *dao.Dao implements it on Redis.
type PhoneCodeStore interface {
	GetCachePhoneCode(ctx context.Context, authKeyId int64, phoneNumber string) (*model.PhoneCodeTransaction, error)
	PutCachePhoneCode(ctx context.Context, authKeyId int64, phoneNumber string, codeData *model.PhoneCodeTransaction) error
	DeleteCachePhoneCode(ctx context.Context, authKeyId int64, phoneNumber string) error
}

// CodeDelivery sends login codes to the Telegram app of a registered user,
// as Telegram does with auth.sentCodeTypeApp.
type CodeDelivery interface {
	// HasOtherAuthorizations reports whether userId has an authorized session
	// other than authKeyId (the one asking for the code).
	HasOtherAuthorizations(ctx context.Context, userId, authKeyId int64) (bool, error)
	// DeliverToApp sends code to userId as a message from the service
	// notifications account (777000).
	DeliverToApp(ctx context.Context, userId int64, code string)
}

// AuthLogic runs the login requests of auth.sendCode, auth.resendCode,
// auth.cancelCode, auth.signIn and auth.signUp. The code policy (allowlist,
// flood limits, code storage and checks) is pkg/logincode.
type AuthLogic struct {
	store PhoneCodeStore
	Codes *logincode.Service
	now   func() time.Time
}

func NewAuthSignLogic(store PhoneCodeStore, codes *logincode.Service) *AuthLogic {
	return &AuthLogic{
		store: store,
		Codes: codes,
		now:   time.Now,
	}
}

// DoAuthSendCode starts a new login request of phoneNumber for authKeyId,
// replacing an earlier one of the same auth key. userId is the account of the
// phone, 0 if it is not registered. The phone must have passed
// Codes.CheckSendCode.
//
// The code goes to the user's other authorized sessions when there are any
// (auth.sentCodeTypeApp). Otherwise to the person's own Telegram account, if
// the sender is deployed and takes it: auth.sentCodeTypeFragmentSms, whose
// screen the apps show as "Check Telegram" with a button to the chat of the
// account that wrote it (task 076, ADR 0044). Failing that the answer is
// auth.sentCodeTypeSms, the screen of a code that did not reach Telegram:
// the administrator issues the code (pluma-admin login-code), and "Send
// again" tries Telegram anew (DoAuthReSendCode).
func (m *AuthLogic) DoAuthSendCode(ctx context.Context,
	authKeyId int64,
	sessionId int64,
	phoneNumber string,
	userId int64,
	clientIp string,
	delivery CodeDelivery) (*model.PhoneCodeTransaction, error) {
	old, err := m.store.GetCachePhoneCode(ctx, authKeyId, phoneNumber)
	if err != nil {
		logx.WithContext(ctx).Errorf("getCachePhoneCode - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if old != nil {
		m.Codes.Cancel(ctx, phoneNumber, old.PhoneCodeHash)
	}

	toApp := false
	if userId != 0 && delivery != nil {
		if toApp, err = delivery.HasOtherAuthorizations(ctx, userId, authKeyId); err != nil {
			// Fall back to the administrator: the code is still never exposed.
			logx.WithContext(ctx).Errorf("auth.sendCode - list authorizations of user %d: %v", userId, err)
			toApp = false
		}
	}

	codeData := &model.PhoneCodeTransaction{
		AuthKeyId:             authKeyId,
		SessionId:             sessionId,
		PhoneNumber:           phoneNumber,
		PhoneNumberRegistered: userId != 0,
		PhoneCodeHash:         crypto.GenerateStringNonce(16),
		PhoneCodeExpired:      int32(m.now().Add(logincode.TransactionTTL).Unix()),
		SentCodeType:          model.SentCodeTypeSms,
		NextCodeType:          model.CodeTypeNone,
		State:                 model.CodeStateSent,
	}
	if toApp {
		codeData.SentCodeType = model.SentCodeTypeApp
		// "Didn't get the code?": a delivery through Telegram
		// (DoAuthReSendCode). The type stays codeTypeSms, not
		// codeTypeFragmentSms: the patched apps take a next type of SMS on
		// this screen for "Didn't get the code?", while for a Fragment one
		// Android and Web K would offer upstream's "Get a code via Fragment"
		// (ADR 0044).
		codeData.NextCodeType = model.CodeTypeSms
	}

	code, err := m.Codes.Issue(ctx, phoneNumber, codeData.PhoneCodeHash, clientIp, toApp)
	if err != nil {
		return nil, err
	}
	if !toApp {
		m.tryTelegram(ctx, codeData, clientIp)
	}
	if err = m.store.PutCachePhoneCode(ctx, authKeyId, phoneNumber, codeData); err != nil {
		m.Codes.Cancel(ctx, phoneNumber, codeData.PhoneCodeHash)
		return nil, mtproto.ErrInternalServerError
	}
	if toApp {
		delivery.DeliverToApp(ctx, userId, code)
	}
	return codeData, nil
}

// maxTelegramAttempts bounds the deliveries of one login request through
// the person's Telegram: the first one (auth.sendCode, or "Didn't get the
// code?" of a code in the app) and the "Send again" of the screen of a code
// that did not reach it. The account's own limits per phone stay.
const maxTelegramAttempts = 4

// tryTelegram offers the code of the request to the person's Telegram and
// sets what the apps show: "Check Telegram" (FragmentSms) if the sender took
// it, else the screen of a code that did not reach Telegram (Sms), with
// "Send again" while another attempt is possible.
func (m *AuthLogic) tryTelegram(ctx context.Context, codeData *model.PhoneCodeTransaction, clientIp string) {
	if m.Codes.TelegramAvailable(codeData.PhoneNumber) {
		codeData.TelegramAttempts++
		if account, ok := m.Codes.DeliverByTelegram(ctx, codeData.PhoneNumber, codeData.PhoneCodeHash, clientIp); ok {
			codeData.SentCodeType = model.SentCodeTypeFragmentSms
			codeData.TelegramAccount = account
			// "Didn't get the code?" leads to the screen below.
			codeData.NextCodeType = model.CodeTypeSms
			return
		}
	}
	m.toFallback(codeData)
}

// toFallback sets the screen of a code that did not reach Telegram: the
// administrator's code is entered there, and "Send again" (next_type sms)
// tries Telegram once more while it can.
func (m *AuthLogic) toFallback(codeData *model.PhoneCodeTransaction) {
	codeData.SentCodeType = model.SentCodeTypeSms
	codeData.NextCodeType = model.CodeTypeNone
	if m.Codes.TelegramAvailable(codeData.PhoneNumber) && codeData.TelegramAttempts < maxTelegramAttempts {
		codeData.NextCodeType = model.CodeTypeSms
	}
}

// DoAuthReSendCode moves a request that offered the next_type on, as
// auth.resendCode sends the code by the next_type of the previous answer
// ("Resend the login code via another medium", the type "determined by the
// return value of the previous auth.sendCode/auth.resendCode":
// core.telegram.org/method/auth.resendCode, core.telegram.org/api/auth):
//   - from a code in the app ("Didn't get the code?") to a delivery through
//     the person's Telegram at once: "Check Telegram", or the screen of a
//     code that did not reach Telegram if it failed;
//   - from a code in Telegram ("Didn't get the code?") to the screen of a
//     code that did not reach Telegram, without a new delivery: the code
//     sent stays valid, and the administrator can issue one;
//   - from that screen ("Send again", after the person allowed Telegram to
//     find them by number) to a new delivery through Telegram.
//
// The phone must have passed Codes.CheckPhone. A delivery counts under the
// code request limits (Codes.ReserveSend) before it is made; a move of the
// screen does not.
func (m *AuthLogic) DoAuthReSendCode(ctx context.Context,
	authKeyId int64,
	phoneNumber, phoneCodeHash, clientIp string) (*model.PhoneCodeTransaction, error) {
	codeData, err := m.getPhoneCode(ctx, authKeyId, phoneNumber, phoneCodeHash)
	if err != nil {
		return nil, err
	}
	if codeData.State != model.CodeStateSent || m.expired(codeData) {
		return nil, mtproto.ErrPhoneCodeExpired
	}
	if codeData.NextCodeType == model.CodeTypeNone {
		// 406: every delivery option was already used.
		return nil, mtproto.ErrSendCodeUnavailable
	}

	switch {
	case codeData.SentCodeType == model.SentCodeTypeFragmentSms:
		m.toFallback(codeData)
	case m.Codes.TelegramAvailable(phoneNumber):
		if err = m.Codes.ReserveSend(ctx, phoneNumber, authKeyId, clientIp); err != nil {
			return nil, err
		}
		m.tryTelegram(ctx, codeData, clientIp)
	default:
		// No way through Telegram: the administrator's screen.
		m.toFallback(codeData)
	}
	if err = m.store.PutCachePhoneCode(ctx, authKeyId, phoneNumber, codeData); err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	return codeData, nil
}

// DoAuthReportMissingCode records that the person did not get the code of
// the request phoneCodeHash (auth.reportMissingCode, which apps of before
// task 076 send from "Didn't get the code?"). It is counted for the
// administrator: the code itself is issued from the panel.
func (m *AuthLogic) DoAuthReportMissingCode(ctx context.Context, authKeyId int64, phoneNumber, phoneCodeHash string) error {
	codeData, err := m.getPhoneCode(ctx, authKeyId, phoneNumber, phoneCodeHash)
	if err != nil {
		return err
	}
	metricMissingCode.Inc(codeTypeName(codeData.SentCodeType))
	logx.WithContext(ctx).Infof("login code: the person reports a missing code (%s)", codeTypeName(codeData.SentCodeType))
	return nil
}

var metricMissingCode = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "login_code",
	Name:      "missing_total",
	Help:      "Codes the person reported as not received (auth.reportMissingCode), by the way the code went.",
	Labels:    []string{"delivery"},
})

// codeTypeName names the way a code went, for metrics and logs.
func codeTypeName(sentCodeType int) string {
	switch sentCodeType {
	case model.SentCodeTypeApp:
		return "app"
	case model.SentCodeTypeFragmentSms:
		return "telegram"
	default:
		return "administrator"
	}
}

// DoAuthCancelCode drops the request phoneCodeHash of authKeyId, if it is
// the current one.
func (m *AuthLogic) DoAuthCancelCode(ctx context.Context, authKeyId int64, phoneNumber, phoneCodeHash string) error {
	codeData, err := m.store.GetCachePhoneCode(ctx, authKeyId, phoneNumber)
	if err != nil {
		logx.WithContext(ctx).Errorf("getCachePhoneCode - error: %v", err)
		return mtproto.ErrInternalServerError
	}
	if codeData == nil || codeData.PhoneCodeHash != phoneCodeHash {
		return nil
	}
	m.Codes.Cancel(ctx, phoneNumber, phoneCodeHash)
	if err = m.DeletePhoneCode(ctx, authKeyId, phoneNumber); err != nil {
		return mtproto.ErrInternalServerError
	}
	return nil
}

// DoAuthSignIn checks phoneCode for the request and records the result:
// CodeStateOk for a registered phone (the caller logs in and deletes the
// request), CodeStateSignIn for a new one (auth.signUp comes next).
func (m *AuthLogic) DoAuthSignIn(ctx context.Context,
	authKeyId int64,
	phoneNumber,
	phoneCode,
	phoneCodeHash,
	clientIp string) (*model.PhoneCodeTransaction, error) {
	codeData, err := m.getPhoneCode(ctx, authKeyId, phoneNumber, phoneCodeHash)
	if err != nil {
		return nil, err
	}
	if m.expired(codeData) {
		return nil, mtproto.ErrPhoneCodeExpired
	}
	switch codeData.State {
	case model.CodeStateSent:
	case model.CodeStateSignIn:
		// A repeated auth.signIn of this auth key, which already proved the
		// code: the account still has to be created with auth.signUp.
		if !codeData.PhoneNumberRegistered {
			return codeData, nil
		}
		return nil, mtproto.ErrPhoneCodeExpired
	default:
		return nil, mtproto.ErrPhoneCodeExpired
	}

	if err = m.Codes.Verify(ctx, phoneNumber, phoneCodeHash, phoneCode, clientIp); err != nil {
		return nil, err
	}

	if codeData.PhoneNumberRegistered {
		codeData.State = model.CodeStateOk
	} else {
		codeData.State = model.CodeStateSignIn
	}
	if err = m.store.PutCachePhoneCode(ctx, authKeyId, phoneNumber, codeData); err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	return codeData, nil
}

// DoAuthSignUp returns the request if its code was accepted by auth.signIn
// for a phone without an account; it is the only way to auth.signUp.
func (m *AuthLogic) DoAuthSignUp(ctx context.Context, authKeyId int64, phoneNumber string, phoneCodeHash string) (*model.PhoneCodeTransaction, error) {
	codeData, err := m.getPhoneCode(ctx, authKeyId, phoneNumber, phoneCodeHash)
	if err != nil {
		return nil, err
	}
	if codeData.State != model.CodeStateSignIn || codeData.PhoneNumberRegistered {
		logx.WithContext(ctx).Errorf("auth.signUp - invalid code state(%d), registered: %v", codeData.State, codeData.PhoneNumberRegistered)
		return nil, mtproto.ErrInputRequestInvalid
	}
	if m.expired(codeData) {
		return nil, mtproto.ErrPhoneCodeExpired
	}
	return codeData, nil
}

// DeletePhoneCode deletes the request of authKeyId and phoneNumber.
func (m *AuthLogic) DeletePhoneCode(ctx context.Context, authKeyId int64, phoneNumber string) error {
	return m.store.DeleteCachePhoneCode(ctx, authKeyId, phoneNumber)
}

// getPhoneCode returns the current request of authKeyId and phoneNumber if
// its phone_code_hash is phoneCodeHash.
func (m *AuthLogic) getPhoneCode(ctx context.Context,
	authKeyId int64,
	phoneNumber, phoneCodeHash string) (*model.PhoneCodeTransaction, error) {
	codeData, err := m.store.GetCachePhoneCode(ctx, authKeyId, phoneNumber)
	switch {
	case err != nil:
		logx.WithContext(ctx).Errorf("getPhoneCode by {authKeyId: %d} error - %v", authKeyId, err)
		return nil, mtproto.ErrPhoneCodeExpired
	case codeData == nil:
		return nil, mtproto.ErrPhoneCodeExpired
	case codeData.PhoneCodeHash != phoneCodeHash:
		logx.WithContext(ctx).Errorf("getPhoneCode by {authKeyId: %d} error - invalid phoneCodeHash", authKeyId)
		return nil, mtproto.ErrPhoneCodeInvalid
	}
	return codeData, nil
}

func (m *AuthLogic) expired(codeData *model.PhoneCodeTransaction) bool {
	return m.now().Unix() > int64(codeData.PhoneCodeExpired)
}

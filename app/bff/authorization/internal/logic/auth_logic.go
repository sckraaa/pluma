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
// (auth.sentCodeTypeApp). Otherwise the answer is auth.sentCodeTypeSms and
// nothing is sent: the administrator issues the code (pluma-admin login-code).
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
		// auth.resendCode falls back to the administrator ("send as SMS").
		codeData.NextCodeType = model.CodeTypeSms
	}

	code, err := m.Codes.Issue(ctx, phoneNumber, codeData.PhoneCodeHash, clientIp, toApp)
	if err != nil {
		return nil, err
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

// DoAuthReSendCode switches a request delivered in the app to the
// administrator path (auth.sentCodeTypeSms, no next type). The phone must have
// passed Codes.CheckSendCode.
func (m *AuthLogic) DoAuthReSendCode(ctx context.Context,
	authKeyId int64,
	phoneNumber, phoneCodeHash string) (*model.PhoneCodeTransaction, error) {
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

	codeData.SentCodeType = model.SentCodeTypeSms
	codeData.NextCodeType = model.CodeTypeNone
	if err = m.store.PutCachePhoneCode(ctx, authKeyId, phoneNumber, codeData); err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	return codeData, nil
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

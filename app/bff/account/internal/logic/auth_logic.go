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

// PhoneCodeStore keeps phone change requests (model.PhoneCodeTransaction) by
// auth key and new phone. *dao.Dao implements it on Redis.
type PhoneCodeStore interface {
	GetCachePhoneCode(ctx context.Context, authKeyId int64, phoneNumber string) (*model.PhoneCodeTransaction, error)
	PutCachePhoneCode(ctx context.Context, authKeyId int64, phoneNumber string, codeData *model.PhoneCodeTransaction) error
	DeleteCachePhoneCode(ctx context.Context, authKeyId int64, phoneNumber string) error
}

// AuthLogic runs phone change requests (account.sendChangePhoneCode,
// account.changePhone) with the login code policy of pkg/logincode.
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

// DoSendChangePhoneCode starts a change to phoneNumber, which must have
// passed Codes.CheckSendCode. The new number has no sessions to deliver a
// code to, so the answer is auth.sentCodeTypeSms and the administrator
// issues the code (pluma-admin login-code <new phone>).
func (m *AuthLogic) DoSendChangePhoneCode(ctx context.Context,
	authKeyId int64,
	sessionId int64,
	phoneNumber string,
	clientIp string) (*model.PhoneCodeTransaction, error) {
	old, err := m.store.GetCachePhoneCode(ctx, authKeyId, phoneNumber)
	if err != nil {
		logx.WithContext(ctx).Errorf("getCachePhoneCode - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if old != nil {
		m.Codes.Cancel(ctx, phoneNumber, old.PhoneCodeHash)
	}

	codeData := &model.PhoneCodeTransaction{
		AuthKeyId:        authKeyId,
		SessionId:        sessionId,
		PhoneNumber:      phoneNumber,
		PhoneCodeHash:    crypto.GenerateStringNonce(16),
		PhoneCodeExpired: int32(m.now().Add(logincode.TransactionTTL).Unix()),
		SentCodeType:     model.SentCodeTypeSms,
		NextCodeType:     model.CodeTypeNone,
		State:            model.CodeStateSent,
	}
	if _, err = m.Codes.Issue(ctx, phoneNumber, codeData.PhoneCodeHash, clientIp, false); err != nil {
		return nil, err
	}
	if err = m.store.PutCachePhoneCode(ctx, authKeyId, phoneNumber, codeData); err != nil {
		m.Codes.Cancel(ctx, phoneNumber, codeData.PhoneCodeHash)
		return nil, mtproto.ErrInternalServerError
	}
	return codeData, nil
}

// DoChangePhone checks phoneCode for the change request phoneCodeHash of
// authKeyId to phoneNumber. On success the request is used up and the caller
// changes the phone.
func (m *AuthLogic) DoChangePhone(ctx context.Context,
	authKeyId int64,
	phoneNumber,
	phoneCode,
	phoneCodeHash,
	clientIp string) error {
	codeData, err := m.store.GetCachePhoneCode(ctx, authKeyId, phoneNumber)
	switch {
	case err != nil:
		logx.WithContext(ctx).Errorf("getCachePhoneCode - error: %v", err)
		return mtproto.ErrPhoneCodeExpired
	case codeData == nil:
		return mtproto.ErrPhoneCodeExpired
	case codeData.PhoneCodeHash != phoneCodeHash:
		return mtproto.ErrPhoneCodeInvalid
	case codeData.State != model.CodeStateSent,
		m.now().Unix() > int64(codeData.PhoneCodeExpired):
		return mtproto.ErrPhoneCodeExpired
	}

	if err = m.Codes.Verify(ctx, phoneNumber, phoneCodeHash, phoneCode, clientIp); err != nil {
		return err
	}
	if err = m.store.DeleteCachePhoneCode(ctx, authKeyId, phoneNumber); err != nil {
		logx.WithContext(ctx).Errorf("deleteCachePhoneCode - error: %v", err)
	}
	return nil
}

// AllowSignUp lets the login request of authKeyId for phoneNumber, whose
// code auth.signIn accepted, go on to auth.signUp: its account was deleted
// since (the reset of an account whose 2FA password is forgotten), and
// official clients sign up again with the same phone_code_hash. A request in
// another state is left as it is.
func (m *AuthLogic) AllowSignUp(ctx context.Context, authKeyId int64, phoneNumber string) error {
	codeData, err := m.store.GetCachePhoneCode(ctx, authKeyId, phoneNumber)
	if err != nil || codeData == nil || codeData.State != model.CodeStateOk {
		return err
	}
	codeData.State = model.CodeStateSignIn
	codeData.PhoneNumberRegistered = false
	return m.store.PutCachePhoneCode(ctx, authKeyId, phoneNumber, codeData)
}

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

package core

import (
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
	"github.com/teamgram/teamgram-server/app/bff/qrcode/internal/model"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/twofa"
)

const (
	qrCodeTimeout = 60 // salt timeout
)

// AuthExportLoginToken
// auth.exportLoginToken#b7e085fe api_id:int api_hash:string except_ids:Vector<long> = auth.LoginToken;
func (c *QrCodeCore) AuthExportLoginToken(in *mtproto.TLAuthExportLoginToken) (*mtproto.Auth_LoginToken, error) {
	// TODO:
	// 1. check api_id, api_hash
	// 2. check except_ids

	qrCode, err := c.svcCtx.Dao.GetCacheQRLoginCode(c.ctx, c.MD.PermAuthKeyId)
	if err != nil {
		c.Logger.Errorf("getQRCode - error: %v", err)
		return nil, err
	} else if qrCode == nil {
		qrCode = &model.QRCodeTransaction{
			PermAuthKeyId: c.MD.PermAuthKeyId,
			AuthKeyId:     c.MD.AuthId,
			SessionId:     c.MD.SessionId,
			ServerId:      c.MD.ServerId,
			ApiId:         in.ApiId,
			ApiHash:       in.ApiHash,
			CodeHash:      crypto.GenerateStringNonce(16),
			ExpireAt:      time.Now().Unix() + qrCodeTimeout,
			UserId:        0,
			State:         model.QRCodeStateNew,
		}
		// Never the transaction itself: code_hash is the login token.
		c.Logger.Infof("putQRCode - new, perm_auth_key_id: %d", qrCode.PermAuthKeyId)
		if err = c.svcCtx.Dao.PutCacheQRLoginCode(c.ctx, c.MD.PermAuthKeyId, qrCode, qrCodeTimeout+2); err != nil {
			c.Logger.Errorf("putQRCode - error: %v", err)
			return nil, err
		}
	} else {
		c.Logger.Infof("putQRCode - found, perm_auth_key_id: %d, state: %d", qrCode.PermAuthKeyId, qrCode.State)
	}

	var (
		rQRLoginToken *mtproto.Auth_LoginToken
	)

	switch qrCode.State {
	case model.QRCodeStateAccepted, model.QRCodeStateSuccess:
		user, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{
			Id: qrCode.UserId,
		})
		if err != nil {
			c.Logger.Errorf("auth.exportLoginToken - error: %v", err)
			return nil, err
		}
		// A user with a 2FA password: auth.acceptLoginToken left the key
		// unbound and waiting for auth.checkPassword, as Telegram does
		// (https://core.telegram.org/api/qr-login). A key that gave the
		// password meanwhile is logged in.
		if c.MD.UserId != user.Id() {
			hasPassword, err := c.svcCtx.Twofa.HasPassword(c.ctx, user.Id())
			if err != nil {
				return nil, err
			}
			if hasPassword {
				if err = c.waitForPassword(qrCode.PermAuthKeyId, user.Id(), ""); err != nil {
					return nil, err
				}
				c.Logger.Infof("auth.exportLoginToken - user %d, next step auth.checkPassword", user.Id())
				return nil, mtproto.ErrSessionPasswordNeeded
			}
		}

		rQRLoginToken = mtproto.MakeTLAuthLoginTokenSuccess(&mtproto.Auth_LoginToken{
			Authorization: mtproto.MakeTLAuthAuthorization(&mtproto.Auth_Authorization{
				SetupPasswordRequired: false,
				OtherwiseReloginDays:  nil,
				TmpSessions:           nil,
				FutureAuthToken:       nil,
				User:                  user.ToSelfUser(),
			}).To_Auth_Authorization(),
		}).To_Auth_LoginToken()
	default:
		rQRLoginToken = mtproto.MakeTLAuthLoginToken(&mtproto.Auth_LoginToken{
			Expires: int32(qrCode.ExpireAt),
			Token:   qrCode.Token(),
		}).To_Auth_LoginToken()
	}

	return rQRLoginToken, nil
}

// waitForPassword makes the QR login of userId on the permanent key
// authKeyId wait for the 2FA password, unless it already does. langCode is
// the language of the session that accepted the code, for the New login
// message of the login (task 019); empty keeps the one already saved.
func (c *QrCodeCore) waitForPassword(authKeyId, userId int64, langCode string) error {
	l, err := c.svcCtx.Twofa.WaitingLogin(c.ctx, authKeyId)
	if err != nil {
		return err
	}
	if l != nil && l.UserId == userId && (langCode == "" || l.LangCode == langCode) {
		return nil
	}
	return c.svcCtx.Twofa.StartLogin(c.ctx, authKeyId, twofa.Login{UserId: userId, LangCode: langCode})
}

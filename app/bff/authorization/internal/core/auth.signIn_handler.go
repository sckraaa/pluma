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
	"fmt"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/authorization/internal/logic"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/code/conf"
	"github.com/teamgram/teamgram-server/pkg/twofa"
)

/*
 1. PHONE_NUMBER_UNOCCUPIED ==> setPage(5, true, params, false);
 2. SESSION_PASSWORD_NEEDED ==> invoke rpc: TL_account_getPassword
 3. error:
	if (error.text.contains("PHONE_NUMBER_INVALID")) {
		needShowAlert(LocaleController.getString("AppName", R.string.AppName), LocaleController.getString("InvalidPhoneNumber", R.string.InvalidPhoneNumber));
	} else if (error.text.contains("PHONE_CODE_EMPTY") || error.text.contains("PHONE_CODE_INVALID")) {
		needShowAlert(LocaleController.getString("AppName", R.string.AppName), LocaleController.getString("InvalidCode", R.string.InvalidCode));
	} else if (error.text.contains("PHONE_CODE_EXPIRED")) {
		onBackPressed();
		setPage(0, true, null, true);
		needShowAlert(LocaleController.getString("AppName", R.string.AppName), LocaleController.getString("CodeExpired", R.string.CodeExpired));
	} else if (error.text.startsWith("FLOOD_WAIT")) {
		needShowAlert(LocaleController.getString("AppName", R.string.AppName), LocaleController.getString("FloodWait", R.string.FloodWait));
	} else {
		needShowAlert(LocaleController.getString("AppName", R.string.AppName), LocaleController.getString("ErrorOccurred", R.string.ErrorOccurred) + "\n" + error.text);
	}
*/

// AuthSignIn
// auth.signIn#bcd51581 phone_number:string phone_code_hash:string phone_code:string = auth.Authorization;
func (c *AuthorizationCore) AuthSignIn(in *mtproto.TLAuthSignIn) (*mtproto.Auth_Authorization, error) {
	var (
		phoneCode     = in.GetPhoneCode_STRING()
		phoneCodeHash = in.PhoneCodeHash
	)

	if phoneCode == "" {
		phoneCode = in.GetPhoneCode_FLAGSTRING().GetValue()
	}

	if phoneCode == "" || phoneCodeHash == "" {
		err := mtproto.ErrPhoneCodeEmpty
		c.Logger.Errorf("auth.signIn - error: %v", err)
		return nil, err
	}

	// 3. check number
	// client phone number format: "+86 111 1111 1111"
	_, phoneNumber, err := checkPhoneNumberInvalid(in.PhoneNumber)
	if err != nil {
		c.Logger.Errorf("check phone_number error - %v", err)
		err = mtproto.ErrPhoneNumberInvalid
		return nil, err
	}

	// Closed registration: PHONE_NUMBER_INVALID whether or not an account exists.
	if err = c.svcCtx.AuthLogic.Codes.CheckPhone(c.ctx, phoneNumber, c.MD.PermAuthKeyId, c.MD.ClientAddr); err != nil {
		c.Logger.Errorf("auth.signIn - error: %v", err)
		return nil, err
	}

	// Checks the code with the attempt and flood limits of pkg/logincode.
	codeData, err2 := c.svcCtx.AuthLogic.DoAuthSignIn(c.ctx,
		c.MD.PermAuthKeyId,
		phoneNumber,
		phoneCode,
		phoneCodeHash,
		c.MD.ClientAddr)
	if err2 != nil {
		c.Logger.Errorf("auth.signIn - error: %v", err2)
		err = err2
		return nil, err
	}

	if c.svcCtx.Plugin != nil {
		c.svcCtx.Plugin.OnAuthAction(c.ctx,
			c.MD.PermAuthKeyId,
			c.MD.ClientMsgId,
			c.MD.ClientAddr,
			in.PhoneNumber,
			logic.GetActionType(in),
			"auth.signIn")
	}

	// signIn successful, check phoneRegistered.
	if !codeData.PhoneNumberRegistered {
		if c.MD.Layer >= 104 {
			//  not register, next step: auth.singIn
			return mtproto.MakeTLAuthAuthorizationSignUpRequired(&mtproto.Auth_Authorization{
				// TermsOfService: model.MakeTermOfService(),
				TermsOfService: nil,
			}).To_Auth_Authorization(), nil
		} else {
			c.Logger.Errorf("auth.signIn - not registered, next step auth.signIn, %v", err)
			err = mtproto.ErrPhoneNumberUnoccupied
			return nil, err
		}
	}

	// TODO(@benqi): err handle
	// do signIn...
	var (
		user *mtproto.ImmutableUser
	)

	user, err = c.svcCtx.Dao.UserClient.UserGetImmutableUserByPhone(c.ctx, &userpb.TLUserGetImmutableUserByPhone{
		Phone: phoneNumber,
	})
	if err != nil {
		c.Logger.Errorf("user by phone - error: %v", err)
		return nil, err
	} else if user == nil {
		c.Logger.Errorf("user by phone - not found")
		err = mtproto.ErrInternalServerError
		return nil, err
	}

	// A user with a 2FA password logs in only with it: the key stays
	// unbound, the login waits for auth.checkPassword (the code request is
	// kept for the reset of the account, which signs up again with it).
	hasPassword, err := c.svcCtx.Twofa.HasPassword(c.ctx, user.Id())
	if err != nil {
		c.Logger.Errorf("auth.signIn - user %d: %v", user.Id(), err)
		return nil, err
	}
	if hasPassword {
		if err = c.svcCtx.Twofa.StartLogin(c.ctx, c.MD.PermAuthKeyId, twofa.Login{UserId: user.Id(), Phone: phoneNumber}); err != nil {
			return nil, err
		}
		c.Logger.Infof("auth.signIn - user %d, next step auth.checkPassword", user.Id())
		return nil, mtproto.ErrSessionPasswordNeeded
	}

	return c.logIn(user, phoneNumber)
}

// logIn binds the key to user and returns its auth.authorization: the end of
// auth.signIn, or of auth.checkPassword after it. phoneNumber is the phone of
// the code request to drop, empty for the other logins; the user's other
// devices are told of the login.
func (c *AuthorizationCore) logIn(user *mtproto.ImmutableUser, phoneNumber string) (*mtproto.Auth_Authorization, error) {
	if _, err := c.svcCtx.Dao.AuthsessionClient.AuthsessionBindAuthKeyUser(c.ctx, &authsession.TLAuthsessionBindAuthKeyUser{
		AuthKeyId: c.MD.PermAuthKeyId,
		UserId:    user.Id(),
	}); err != nil {
		c.Logger.Errorf("bind the key to user %d - error: %v", user.Id(), err)
		return nil, err
	}
	c.svcCtx.Twofa.EndLogin(c.ctx, c.MD.PermAuthKeyId)

	selfUser := user.ToSelfUser()
	if phoneNumber == "" {
		return mtproto.MakeTLAuthAuthorization(&mtproto.Auth_Authorization{
			User: selfUser,
		}).To_Auth_Authorization(), nil
	}

	_ = c.svcCtx.AuthLogic.DeletePhoneCode(c.ctx, c.MD.PermAuthKeyId, phoneNumber)

	region, _ := c.svcCtx.Dao.GetCountryAndRegionByIp(c.MD.ClientAddr)

	var (
		now     = time.Now()
		signInN *mtproto.Update
	)

	if len(c.svcCtx.Config.SignInServiceNotification) == 0 {
		signInN = mtproto.MakeSignInServiceNotification(selfUser, c.MD.PermAuthKeyId, c.MD.Client, region, c.MD.ClientAddr)
	} else {
		signInN = mtproto.MakeTLUpdateServiceNotification(&mtproto.Update{
			Popup:          false,
			InboxDate:      mtproto.MakeFlagsInt32(int32(now.Unix())),
			Type:           fmt.Sprintf("auth%d_%d", c.MD.PermAuthKeyId, now.Unix()),
			Message_STRING: "",
			Media:          mtproto.MakeTLMessageMediaEmpty(nil).To_MessageMedia(),
			Entities:       nil,
		}).To_Update()

		builder := conf.ToMessageBuildHelper(
			c.svcCtx.Config.SignInServiceNotification,
			map[string]interface{}{
				"name":     mtproto.GetUserName(selfUser),
				"now":      now.UTC(),
				"client":   c.MD.Client,
				"region":   region,
				"clientIp": c.MD.ClientAddr,
			})
		signInN.Message_STRING, signInN.Entities = mtproto.MakeTextAndMessageEntities(builder)
	}

	_, _ = c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(
		c.ctx,
		&sync.TLSyncUpdatesNotMe{
			UserId:        user.Id(),
			PermAuthKeyId: c.MD.PermAuthKeyId,
			Updates:       mtproto.MakeUpdatesByUpdates(signInN),
		})

	return mtproto.MakeTLAuthAuthorization(&mtproto.Auth_Authorization{
		SetupPasswordRequired: false,
		OtherwiseReloginDays:  nil,
		TmpSessions:           nil,
		FutureAuthToken:       nil,
		User:                  selfUser,
	}).To_Auth_Authorization(), nil
}

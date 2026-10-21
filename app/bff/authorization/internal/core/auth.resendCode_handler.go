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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/authorization/internal/logic"
)

/*
 Android client auth.resendCode#3ef1a9bf, handler error
	if (error.text != null) {
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
		} else if (error.code != -1000) {
			needShowAlert(LocaleController.getString("AppName", R.string.AppName), LocaleController.getString("ErrorOccurred", R.string.ErrorOccurred) + "\n" + error.text);
		}
	}
*/

// AuthResendCode
// auth.resendCode#3ef1a9bf phone_number:string phone_code_hash:string = auth.SentCode;
func (c *AuthorizationCore) AuthResendCode(in *mtproto.TLAuthResendCode) (*mtproto.Auth_SentCode, error) {
	// 2. check phone_code_hash
	if in.PhoneCodeHash == "" {
		err := mtproto.ErrPhoneCodeHashEmpty
		c.Logger.Errorf("auth.resendCode - error: %v", err)
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

	// A resend is a code request: same allowlist and flood limits as auth.sendCode.
	if err = c.svcCtx.AuthLogic.Codes.CheckSendCode(c.ctx, phoneNumber, c.MD.PermAuthKeyId, c.MD.ClientAddr); err != nil {
		c.Logger.Errorf("auth.resendCode - error: %v", err)
		return nil, err
	}

	// 4. MIGRATE datacenter
	// 	303	NETWORK_MIGRATE_X	重复查询到数据中心X
	// 	303	PHONE_MIGRATE_X	重复查询到数据中心X
	//
	// TODO(@benqi): MIGRATE datacenter
	// android client:
	//  migrateErrors.push_back("NETWORK_MIGRATE_");
	//  migrateErrors.push_back("PHONE_MIGRATE_");
	//  migrateErrors.push_back("USER_MIGRATE_");
	//
	// https://core.telegram.org/api/datacenter
	// The auth.sendCode method is the basic entry point when registering a new user or authorizing an existing user.
	//   95% of all redirection cases to a different DC will occure when invoking this method.
	//
	// The client does not yet know which DC it will be associated with; therefore,
	//   it establishes an encrypted connection to a random address and sends its query to that address.
	// Having received a phone_number from a client,
	// 	 we can find out whether or not it is registered in the system.
	//   If it is, then, if necessary, instead of sending a text message,
	//   we request that it establish a connection with a different DC first (PHONE_MIGRATE_X error).
	// If we do not yet have a user with this number, we examine its IP-address.
	//   We can use it to identify the closest DC.
	//   Again, if necessary, we redirect the user to a different DC (NETWORK_MIGRATE_X error).
	//
	// if userDO == nil {
	//	// phone registered
	//	// TODO(@benqi): 由phoneNumber和ip优选
	// } else {
	//	// TODO(@benqi): 由userId优选
	// }

	// 5. Check INPUT_REQUEST_TOO_LONG
	// TODO(@benqi):
	// 	400	INPUT_REQUEST_TOO_LONG	The request is too big

	// 5. banned phone number
	if c.svcCtx.Plugin != nil {
		banned, _ := c.svcCtx.Plugin.CheckPhoneNumberBanned(c.ctx, phoneNumber)
		if banned {
			c.Logger.Errorf("phone number banned: %v", err)
			err = mtproto.ErrPhoneNumberBanned
			return nil, err
		}
	}

	// 400	PHONE_NUMBER_FLOOD	You asked for the code too many times.
	// phone number flood
	// TODO(@benqi): PHONE_NUMBER_FLOOD
	// <string name="PhoneNumberFlood">Sorry, you have deleted and re-created your account too many times recently.
	//    Please wait for a few days before signing up again.</string>
	//

	// The in-app code falls back to the administrator ("send as SMS"):
	// nothing is sent from here, pluma-admin login-code issues the code.
	codeData, err2 := c.svcCtx.AuthLogic.DoAuthReSendCode(c.ctx,
		c.MD.PermAuthKeyId,
		phoneNumber,
		in.PhoneCodeHash)
	if err2 != nil {
		c.Logger.Errorf("auth.resendCode - error: %v", err2)
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
			"auth.resendCode")
	}

	return codeData.ToAuthSentCode(), nil
}

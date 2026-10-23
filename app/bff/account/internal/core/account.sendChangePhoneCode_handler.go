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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/phonenumber"

	"google.golang.org/grpc/status"
)

/*
   } else if (request instanceof TLRPC.TL_account_sendChangePhoneCode) {
       if (error.text.contains("PHONE_NUMBER_INVALID")) {
           showSimpleAlert(fragment, LocaleController.getString("InvalidPhoneNumber", R.string.InvalidPhoneNumber));
       } else if (error.text.contains("PHONE_CODE_EMPTY") || error.text.contains("PHONE_CODE_INVALID")) {
           showSimpleAlert(fragment, LocaleController.getString("InvalidCode", R.string.InvalidCode));
       } else if (error.text.contains("PHONE_CODE_EXPIRED")) {
           showSimpleAlert(fragment, LocaleController.getString("CodeExpired", R.string.CodeExpired));
       } else if (error.text.startsWith("FLOOD_WAIT")) {
           showSimpleAlert(fragment, LocaleController.getString("FloodWait", R.string.FloodWait));
       } else if (error.text.startsWith("PHONE_NUMBER_OCCUPIED")) {
           showSimpleAlert(fragment, LocaleController.formatString("ChangePhoneNumberOccupied", R.string.ChangePhoneNumberOccupied, args[0]));
       } else {
           showSimpleAlert(fragment, LocaleController.getString("ErrorOccurred", R.string.ErrorOccurred));
       }
*/

// AccountSendChangePhoneCode
// account.sendChangePhoneCode#82574ae5 phone_number:string settings:CodeSettings = auth.SentCode;
func (c *AccountCore) AccountSendChangePhoneCode(in *mtproto.TLAccountSendChangePhoneCode) (*mtproto.Auth_SentCode, error) {
	// ## Possible errors
	// Code	Type	Description
	// 406	FRESH_CHANGE_PHONE_FORBIDDEN	You can't change phone number right after logging in, please wait at least 24 hours.
	// 400	PHONE_NUMBER_BANNED	The provided phone number is banned from telegram.
	// 406	PHONE_NUMBER_INVALID	The phone number is invalid.
	// 400	PHONE_NUMBER_OCCUPIED	The phone number is already in use.

	// 3. check number

	// client phone number format: "+86 111 1111 1111"
	_, phoneNumber, err := phonenumber.CheckPhoneNumberInvalid(in.PhoneNumber)
	if err != nil {
		c.Logger.Errorf("check phone_number error - %v", err)
		err = mtproto.ErrPhoneNumberInvalid
		return nil, err
	}

	// The new number must be on the allowlist; same flood limits as auth.sendCode.
	if err = c.svcCtx.AuthLogic.Codes.CheckSendCode(c.ctx, phoneNumber, c.MD.PermAuthKeyId, c.MD.ClientAddr); err != nil {
		c.Logger.Errorf("account.sendChangePhoneCode - error: %v", err)
		return nil, err
	}

	// 5. banned phone number
	if c.svcCtx.Plugin != nil {
		banned, _ := c.svcCtx.Plugin.CheckPhoneNumberBanned(c.ctx, phoneNumber)
		if banned {
			c.Logger.Errorf("account.sendChangePhoneCode - phone banned")
			return nil, mtproto.ErrPhoneNumberBanned
		}
	}

	if err = c.checkPhoneNumberFree(phoneNumber); err != nil {
		return nil, err
	}

	// The code is issued by the administrator: pluma-admin login-code <new phone>.
	codeData, err := c.svcCtx.AuthLogic.DoSendChangePhoneCode(
		c.ctx,
		c.MD.PermAuthKeyId,
		c.MD.SessionId,
		phoneNumber,
		c.MD.ClientAddr)
	if err != nil {
		c.Logger.Errorf("account.sendChangePhoneCode - error: %v", err)
		return nil, err
	}

	return codeData.ToAuthSentCode(), nil
}

// checkPhoneNumberFree returns PHONE_NUMBER_OCCUPIED if phoneNumber belongs
// to an account.
func (c *AccountCore) checkPhoneNumberFree(phoneNumber string) error {
	user, err := c.svcCtx.Dao.UserClient.UserGetImmutableUserByPhone(c.ctx, &userpb.TLUserGetImmutableUserByPhone{
		Phone: phoneNumber,
	})
	if err != nil {
		if _, ok := status.FromError(err); ok {
			// TODO: check if the error is mtproto.ErrPhoneNumberUnoccupied
			return nil
		}
		c.Logger.Errorf("checkPhoneNumberExist error: %v", err)
		return err
	}
	c.Logger.Errorf("checkPhoneNumberExist - occupied by user %d", user.Id())
	return mtproto.ErrPhoneNumberOccupied
}

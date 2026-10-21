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
)

// AccountChangePhone
// account.changePhone#70c32edb phone_number:string phone_code_hash:string phone_code:string = User;
func (c *AccountCore) AccountChangePhone(in *mtproto.TLAccountChangePhone) (*mtproto.User, error) {
	// ## Possible errors
	// Code	Type	Description
	// 400	PHONE_CODE_EMPTY	phone_code is missing.
	// 400	PHONE_CODE_EXPIRED	The phone code you provided has expired.
	// 406	PHONE_NUMBER_INVALID	The phone number is invalid.
	// 400	PHONE_NUMBER_OCCUPIED	The phone number is already in use.

	var (
		phoneCode     = in.GetPhoneCode()
		phoneCodeHash = in.GetPhoneCodeHash()
	)

	if phoneCode == "" || phoneCodeHash == "" {
		err := mtproto.ErrPhoneCodeEmpty
		c.Logger.Errorf("account.changePhone - error: %v", err)
		return nil, err
	}

	// 3. check number

	// client phone number format: "+86 111 1111 1111"
	_, phoneNumber, err := phonenumber.CheckPhoneNumberInvalid(in.PhoneNumber)
	if err != nil {
		c.Logger.Errorf("check phone_number error - %v", err)
		err = mtproto.ErrPhoneNumberInvalid
		return nil, err
	}

	// The new number must still be on the allowlist.
	if err = c.svcCtx.AuthLogic.Codes.CheckPhone(c.ctx, phoneNumber, c.MD.PermAuthKeyId, c.MD.ClientAddr); err != nil {
		c.Logger.Errorf("account.changePhone - error: %v", err)
		return nil, err
	}

	// 5. banned phone number
	if c.svcCtx.Plugin != nil {
		banned, _ := c.svcCtx.Plugin.CheckPhoneNumberBanned(c.ctx, phoneNumber)
		if banned {
			c.Logger.Errorf("account.changePhone - phone banned")
			return nil, mtproto.ErrPhoneNumberBanned
		}
	}

	if err = c.checkPhoneNumberFree(phoneNumber); err != nil {
		return nil, err
	}

	// The code proves the new number: nothing changes without it.
	if err = c.svcCtx.AuthLogic.DoChangePhone(c.ctx,
		c.MD.PermAuthKeyId,
		phoneNumber,
		phoneCode,
		phoneCodeHash,
		c.MD.ClientAddr); err != nil {
		c.Logger.Errorf("account.changePhone - error: %v", err)
		return nil, err
	}

	user, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{
		Id: c.MD.UserId,
	})
	if err != nil || user == nil {
		c.Logger.Errorf("account.changePhone - getImmutableUser(%d) error: %v", c.MD.UserId, err)
		return nil, mtproto.ErrInternalServerError
	}

	_, err = c.svcCtx.Dao.UserClient.UserChangePhone(c.ctx, &userpb.TLUserChangePhone{
		UserId: c.MD.UserId,
		Phone:  phoneNumber,
	})
	if err != nil {
		c.Logger.Errorf("account.changePhone - error: %v", err)
		return nil, err
	}

	user.User.Phone = phoneNumber
	return user.ToSelfUser(), nil
}

// Copyright 2024 Teamgram Authors
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
)

// AuthReportMissingCode
// auth.reportMissingCode#cb9deff6 phone_number:string phone_code_hash:string mnc:string = Bool;
//
// Apps of before task 076 send it from "Didn't get the code?"; the report is
// counted for the administrator, who issues the code from the panel.
func (c *AuthorizationCore) AuthReportMissingCode(in *mtproto.TLAuthReportMissingCode) (*mtproto.Bool, error) {
	if in.PhoneCodeHash == "" {
		err := mtproto.ErrPhoneCodeHashEmpty
		c.Logger.Errorf("auth.reportMissingCode - error: %v", err)
		return nil, err
	}
	_, phoneNumber, err := checkPhoneNumberInvalid(in.GetPhoneNumber())
	if err != nil {
		c.Logger.Errorf("auth.reportMissingCode - error: %v", err)
		return nil, mtproto.ErrPhoneNumberInvalid
	}
	if err = c.svcCtx.AuthLogic.Codes.CheckPhone(c.ctx, phoneNumber, c.MD.PermAuthKeyId, c.MD.ClientAddr); err != nil {
		c.Logger.Errorf("auth.reportMissingCode - error: %v", err)
		return nil, err
	}
	if err = c.svcCtx.AuthLogic.DoAuthReportMissingCode(c.ctx, c.MD.PermAuthKeyId, phoneNumber, in.PhoneCodeHash); err != nil {
		c.Logger.Errorf("auth.reportMissingCode - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

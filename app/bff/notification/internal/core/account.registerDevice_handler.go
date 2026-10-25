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
	"errors"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/pushdevices"

	"google.golang.org/grpc/status"
)

// errTokenWasInvalidated answers the registration of a token the provider
// rejected: iOS then asks APNs for a new one.
var errTokenWasInvalidated = status.Error(mtproto.ErrBadRequest, "TOKEN_WAS_INVALIDATED")

// AccountRegisterDevice
// account.registerDevice#ec86017a flags:# no_muted:flags.0?true token_type:int token:string app_sandbox:Bool secret:bytes other_uids:Vector<long> = Bool;
//
// Registers the device of this authorization for push notifications (task
// 009, ADR 0017). A new token of the same type replaces the old one; the
// clients register again every few hours. Token types: APNs, FCM, VoIP and
// web push; the first three need the 256-byte secret the pushes are
// encrypted with, as the official clients always send it.
func (c *NotificationCore) AccountRegisterDevice(in *mtproto.TLAccountRegisterDevice) (*mtproto.Bool, error) {
	if c.MD.UserId == 0 || c.MD.PermAuthKeyId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	d := &pushdevices.Device{
		UserId:     c.MD.UserId,
		AuthKeyId:  c.MD.PermAuthKeyId,
		TokenType:  in.GetTokenType(),
		Token:      in.GetToken(),
		AppSandbox: mtproto.FromBool(in.GetAppSandbox()),
		NoMuted:    in.GetNoMuted(),
		Secret:     in.GetSecret(),
		OtherUids:  in.GetOtherUids(),
		LangCode:   c.MD.GetLangCode(),
	}
	if err := checkDevice(d); err != nil {
		c.Logger.Infof("account.registerDevice - user %d, token type %d: %v", c.MD.UserId, d.TokenType, err)
		return nil, err
	}
	switch err := c.svcCtx.PushDevices.Register(c.ctx, d); {
	case errors.Is(err, pushdevices.ErrTokenInvalidated):
		c.Logger.Infof("account.registerDevice - user %d, token type %d: the token was invalidated", c.MD.UserId, d.TokenType)
		return nil, errTokenWasInvalidated
	case err != nil:
		c.Logger.Errorf("account.registerDevice - user %d: %v", c.MD.UserId, err)
		return nil, mtproto.ErrInternalServerError
	}
	return mtproto.BoolTrue, nil
}

// checkDevice checks a registration as Telegram does: TOKEN_TYPE_INVALID,
// TOKEN_EMPTY or TOKEN_INVALID.
func checkDevice(d *pushdevices.Device) error {
	switch d.TokenType {
	case pushdevices.TokenAPNs, pushdevices.TokenFCM, pushdevices.TokenVoIP:
		if len(d.Secret) != pushdevices.SecretLength {
			return mtproto.ErrTokenInvalid
		}
	case pushdevices.TokenWebPush:
		if len(d.Secret) != 0 && len(d.Secret) != pushdevices.SecretLength {
			return mtproto.ErrTokenInvalid
		}
	default:
		return mtproto.ErrTokenTypeInvalid
	}
	switch {
	case d.Token == "":
		return mtproto.ErrTokenEmpty
	case len(d.Token) > pushdevices.MaxTokenLength || !utf8.ValidString(d.Token) || len(d.OtherUids) > pushdevices.MaxOtherUids:
		return mtproto.ErrTokenInvalid
	}
	if len(d.LangCode) > 32 {
		d.LangCode = ""
	}
	return nil
}

// Copyright 2022 Teamgram Authors
//  All rights reserved.
//
// Author: Benqi (wubenqi@gmail.com)
//

package bff_proxy_client

import (
	"context"
	"reflect"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/logx"
)

func (c *BFFProxyClient) TryReturnFakeRpcResult(ctx context.Context, object mtproto.TLObject) (mtproto.TLObject, error) {
	rt := reflect.TypeOf(object)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	switch rt.Name() {
	// tos
	case "TLHelpAcceptTermsOfService":
		return mtproto.BoolTrue, nil
	case "TLHelpGetTermsOfServiceUpdate":
		return mtproto.MakeTLHelpTermsOfServiceUpdateEmpty(&mtproto.Help_TermsOfServiceUpdate{
			Expires: int32(time.Now().Unix() + 3600),
		}).To_Help_TermsOfServiceUpdate(), nil

	// phone
	case "TLPhoneGetCallConfig":
		return mtproto.MakeTLDataJSON(&mtproto.DataJSON{
			Data: "{}",
		}).To_DataJSON(), nil

	case "TLAccountGetAuthorizations":
		return mtproto.MakeTLAccountAuthorizations(&mtproto.Account_Authorizations{
			AuthorizationTtlDays: 0,
			Authorizations:       []*mtproto.Authorization{},
		}).To_Account_Authorizations(), nil

	case "TLAccountGetWebAuthorizations":
		return mtproto.MakeTLAccountWebAuthorizations(&mtproto.Account_WebAuthorizations{
			Authorizations: []*mtproto.WebAuthorization{},
			Users:          []*mtproto.User{},
		}).To_Account_WebAuthorizations(), nil
	}

	logx.WithContext(ctx).Errorf("%s blocked, License key from https://teamgram.net required to unlock enterprise features.", rt.Name())
	return nil, mtproto.ErrEnterpriseIsBlocked
}

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

package model

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/logincode"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// PhoneCodeTransaction is the state of one login (or phone change) request,
// identified by its phone_code_hash. The code itself is not here: only its
// HMAC is stored, by pkg/logincode.
// TODO(@benqi): Add phone region
type PhoneCodeTransaction struct {
	AuthKeyId             int64  `json:"auth_key_id"`
	SessionId             int64  `json:"session_id"`
	PhoneNumber           string `json:"phone_number"`
	PhoneNumberRegistered bool   `json:"phone_number_registered"`
	PhoneCodeHash         string `json:"phone_code_hash"`
	PhoneCodeExpired      int32  `json:"phone_code_expired"` // end of the request, unix seconds
	SentCodeType          int    `json:"sent_code_type"`
	FlashCallPattern      string `json:"flash_call_pattern"`
	NextCodeType          int    `json:"next_code_type"`
	State                 int    `json:"state"`
}

// ToAuthSentCode returns the auth.sentCode for the request.
func (m *PhoneCodeTransaction) ToAuthSentCode() *mtproto.Auth_SentCode {
	authSentCode := mtproto.MakeTLAuthSentCode(&mtproto.Auth_SentCode{
		Type:          makeAuthSentCodeType(m.SentCodeType, logincode.Length, m.FlashCallPattern),
		PhoneCodeHash: m.PhoneCodeHash,
		NextType:      makeAuthCodeType(m.NextCodeType),
		Timeout:       nil,
	}).To_Auth_SentCode()
	// timeout only means something when auth.resendCode can switch to
	// next_type; the in-app code (sentCodeTypeApp) offers that at any time.
	if authSentCode.NextType != nil && m.SentCodeType != SentCodeTypeApp {
		authSentCode.Timeout = &wrapperspb.Int32Value{Value: 60}
	}
	return authSentCode
}

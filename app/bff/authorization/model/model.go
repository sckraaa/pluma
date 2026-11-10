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
	// TelegramTried: the code of this request was already offered to the
	// user's Telegram account (logincode.Service.DeliverByTelegram). Kept for
	// the change of phone; a login counts TelegramAttempts.
	TelegramTried bool `json:"telegram_tried,omitempty"`
	// TelegramAttempts counts the deliveries of this login request through
	// the person's Telegram (task 076).
	TelegramAttempts int `json:"telegram_attempts,omitempty"`
	// TelegramAccount is the username of the Telegram account that wrote the
	// code: the login screen opens the chat with it.
	TelegramAccount string `json:"telegram_account,omitempty"`
}

// fallbackTimeout is how long the apps count down before "Send again" on
// the screen of a code that did not reach Telegram: time to change the
// privacy setting in Telegram.
const fallbackTimeout = 30

// ToAuthSentCode returns the auth.sentCode for the request.
func (m *PhoneCodeTransaction) ToAuthSentCode() *mtproto.Auth_SentCode {
	authSentCode := mtproto.MakeTLAuthSentCode(&mtproto.Auth_SentCode{
		Type:          makeAuthSentCodeType(m.SentCodeType, logincode.Length, m.FlashCallPattern, m.telegramChatURL()),
		PhoneCodeHash: m.PhoneCodeHash,
		NextType:      makeAuthCodeType(m.NextCodeType),
		Timeout:       nil,
	}).To_Auth_SentCode()
	// timeout only means something when auth.resendCode can switch to
	// next_type. A code in the app or in Telegram offers that at once ("Didn't
	// get the code?"); the screen of a code that did not reach Telegram counts
	// down before "Send again".
	if authSentCode.NextType != nil && m.SentCodeType == SentCodeTypeSms {
		authSentCode.Timeout = &wrapperspb.Int32Value{Value: fallbackTimeout}
	}
	return authSentCode
}

// telegramChatURL is the link of the login screen's button to the chat with
// the account that wrote the code, "" without one (the apps then show no
// button). The username is checked by logincode (DeliverByTelegram).
func (m *PhoneCodeTransaction) telegramChatURL() string {
	if m.TelegramAccount == "" {
		return ""
	}
	return "tg://resolve?domain=" + m.TelegramAccount
}

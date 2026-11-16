// Copyright 2026 The Pluma Authors
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

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// AccountUpdateBusinessGreetingMessage
// account.updateBusinessGreetingMessage#66cdafc4 flags:# message:flags.0?InputBusinessGreetingMessage = Bool;
//
// Sets the greeting of the account: the shortcut sent to the recipients on
// their first message, or after no_activity_days without messages in the
// chat (core.telegram.org/api/business#greeting-messages). Without message
// it turns the greeting off, as tdesktop does
// (data/business/data_business_info.cpp:224-233). no_activity_days is 7,
// 14, 21 or 28 (core.telegram.org/constructor/inputBusinessGreetingMessage),
// else NO_ACTIVITY_DAYS_INVALID (not published by Telegram; the server's
// name).
func (c *BusinessCore) AccountUpdateBusinessGreetingMessage(in *mtproto.TLAccountUpdateBusinessGreetingMessage) (*mtproto.Bool, error) {
	const method = "account.updateBusinessGreetingMessage"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	m := in.GetMessage()
	if m == nil {
		if err := c.saveAutoReply(method, business.KindGreeting, nil); err != nil {
			return nil, err
		}
		return mtproto.BoolTrue, nil
	}
	if !business.ValidNoActivityDays(m.GetNoActivityDays()) {
		err := mtproto.ErrNoActivityDaysInvalid
		c.Logger.Errorf("%s - %d days: %v", method, m.GetNoActivityDays(), err)
		return nil, err
	}
	recipients, err := c.autoReplyRecipients(method, m.GetRecipients())
	if err != nil {
		return nil, err
	}
	if err = c.saveAutoReply(method, business.KindGreeting, &business.AutoReply{
		UserID:         c.MD.UserId,
		Kind:           business.KindGreeting,
		ShortcutID:     m.GetShortcutId(),
		Recipients:     recipients,
		NoActivityDays: m.GetNoActivityDays(),
	}); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

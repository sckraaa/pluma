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

// AccountUpdateBusinessAwayMessage
// account.updateBusinessAwayMessage#a26a7fa5 flags:# message:flags.0?InputBusinessAwayMessage = Bool;
//
// Sets the away message of the account: the shortcut sent to the
// recipients who write while the schedule holds — always, outside the
// work hours, or between two dates (core.telegram.org/api/business#away-messages)
// — and, with offline_only, only while the account has not been online for
// ten minutes (core.telegram.org/constructor/inputBusinessAwayMessage).
// Without message it turns the away message off, as tdesktop does
// (data/business/data_business_info.cpp:182-190). A custom schedule that
// does not end after it starts is INPUT_REQUEST_INVALID (Telegram
// publishes no error for it; the server's choice).
func (c *BusinessCore) AccountUpdateBusinessAwayMessage(in *mtproto.TLAccountUpdateBusinessAwayMessage) (*mtproto.Bool, error) {
	const method = "account.updateBusinessAwayMessage"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	m := in.GetMessage()
	if m == nil {
		if err := c.saveAutoReply(method, business.KindAway, nil); err != nil {
			return nil, err
		}
		return mtproto.BoolTrue, nil
	}
	schedule, start, end, ok := business.AwayScheduleFromTL(m.GetSchedule())
	if !ok {
		err := mtproto.ErrInputRequestInvalid
		c.Logger.Errorf("%s - schedule %s: %v", method, m.GetSchedule().GetPredicateName(), err)
		return nil, err
	}
	recipients, err := c.autoReplyRecipients(method, m.GetRecipients())
	if err != nil {
		return nil, err
	}
	if err = c.saveAutoReply(method, business.KindAway, &business.AutoReply{
		UserID:        c.MD.UserId,
		Kind:          business.KindAway,
		ShortcutID:    m.GetShortcutId(),
		Recipients:    recipients,
		Schedule:      schedule,
		ScheduleStart: start,
		ScheduleEnd:   end,
		OfflineOnly:   m.GetOfflineOnly(),
	}); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

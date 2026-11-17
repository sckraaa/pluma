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
	"errors"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// The automatic replies of Telegram Business (task 105, part 5; ADR 0067):
// the greeting and the away message an account sets up. The settings are
// kept in a box transaction of the account, as its profile is, and its
// other devices get updateUser about it: the clients read the settings
// from their own userFull only (tdesktop data/data_user.cpp:1066-1071), so
// a device reads it again. The service msg queues and sends the replies.

// autoReplyRecipients are the recipients of in, with the users resolved
// (recipientUsers). Choosing only the selected chats, a choice of no
// category and no user is BUSINESS_RECIPIENTS_EMPTY: the name
// core.telegram.org/method/account.updateConnectedBot gives the same
// mistake of the recipients of the connected bot (not documented for the
// automatic replies). The clients never send one: they turn an empty
// choice into "all but" (tdesktop BusinessRecipients::MakeValid,
// data/business/data_business_common.cpp:72-77).
func (c *BusinessCore) autoReplyRecipients(method string, in *mtproto.InputBusinessRecipients) (business.Recipients, error) {
	users, err := c.recipientUsers(method, in.GetUsers())
	if err != nil {
		return business.Recipients{}, err
	}
	r := business.RecipientsFromInput(in, users)
	if r.Empty() {
		err = mtproto.ErrBusinessRecipientsEmpty
		c.Logger.Errorf("%s - error: %v", method, err)
		return business.Recipients{}, err
	}
	return r, nil
}

// saveAutoReply keeps a as the automatic reply of its kind of the caller,
// or with a nil turns kind off. The shortcut must be one of the caller's
// with a message to send, else SHORTCUT_INVALID, the error
// core.telegram.org/method/messages.editQuickReplyShortcut gives a
// shortcut that is not the caller's.
func (c *BusinessCore) saveAutoReply(method string, kind business.AutoReplyKind, a *business.AutoReply) error {
	err := c.saveProfile(method, func(tx *sqlx.Tx, now int64) error {
		if a == nil {
			return business.DeleteAutoReplyTx(tx, c.MD.UserId, kind)
		}
		err := business.SetAutoReplyTx(tx, a, int32(now/1000))
		if errors.Is(err, business.ErrShortcutUnusable) {
			return mtproto.ErrShortcutInvalid
		}
		return err
	})
	return err
}

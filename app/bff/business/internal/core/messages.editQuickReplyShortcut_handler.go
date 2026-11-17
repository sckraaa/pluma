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
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// MessagesEditQuickReplyShortcut
// messages.editQuickReplyShortcut#5c003cef shortcut_id:int shortcut:string = Bool;
//
// Renames a shortcut of the account (task 105): SHORTCUT_INVALID for a
// name the clients would not let one type or a shortcut that is not the
// account's, SHORTCUT_OCCUPIED for a name another shortcut has. The other
// devices get updateQuickReplies.
func (c *BusinessCore) MessagesEditQuickReplyShortcut(in *mtproto.TLMessagesEditQuickReplyShortcut) (*mtproto.Bool, error) {
	const method = "messages.editQuickReplyShortcut"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Dao.MsgClient.MsgEditQuickReplyShortcut(c.ctx, &msgpb.TLMsgEditQuickReplyShortcut{
		UserId:     c.MD.UserId,
		AuthKeyId:  c.MD.PermAuthKeyId,
		ShortcutId: in.ShortcutId,
		Shortcut:   in.Shortcut,
	})
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return r, nil
}

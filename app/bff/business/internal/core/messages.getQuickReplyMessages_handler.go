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

// MessagesGetQuickReplyMessages
// messages.getQuickReplyMessages#94a495c3 flags:# shortcut_id:int id:flags.0?Vector<int> hash:long = messages.Messages;
//
// The messages of a shortcut of the account, all of them or those with id
// (task 105). A shortcut that is not the account's is SHORTCUT_INVALID.
func (c *BusinessCore) MessagesGetQuickReplyMessages(in *mtproto.TLMessagesGetQuickReplyMessages) (*mtproto.Messages_Messages, error) {
	const method = "messages.getQuickReplyMessages"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Dao.MsgClient.MsgGetQuickReplyMessages(c.ctx, &msgpb.TLMsgGetQuickReplyMessages{
		UserId:     c.MD.UserId,
		ShortcutId: in.ShortcutId,
		Id:         in.Id,
		Hash:       in.Hash,
	})
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return r, nil
}

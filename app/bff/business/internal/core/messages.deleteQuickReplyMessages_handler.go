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

// MessagesDeleteQuickReplyMessages
// messages.deleteQuickReplyMessages#e105e910 shortcut_id:int id:Vector<int> = Updates;
//
// Deletes messages of a shortcut of the account (task 105): the answer and
// the other devices get updateDeleteQuickReplyMessages. A shortcut that is
// not the account's is SHORTCUT_INVALID; one left empty stays.
func (c *BusinessCore) MessagesDeleteQuickReplyMessages(in *mtproto.TLMessagesDeleteQuickReplyMessages) (*mtproto.Updates, error) {
	const method = "messages.deleteQuickReplyMessages"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Dao.MsgClient.MsgDeleteQuickReplyMessages(c.ctx, &msgpb.TLMsgDeleteQuickReplyMessages{
		UserId:     c.MD.UserId,
		AuthKeyId:  c.MD.PermAuthKeyId,
		ShortcutId: in.ShortcutId,
		Id:         in.Id,
	})
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return r, nil
}

// Copyright 2024 Teamgram Authors
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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// InboxEditMessageToInboxV2
// inbox.editMessageToInboxV2 flags:# user_id:long from_id:long peer_type:int peer_id:long new_message:MessageBox users:flags.2?Vector<User> chats:flags.3?Vector<Chat> = Void;
//
// The recipient's copy is found by dialog_message_id in the transaction; no
// copy, nothing to do.
func (c *InboxCore) InboxEditMessageToInboxV2(in *inbox.TLInboxEditMessageToInboxV2) error {
	bot := isBot(in.UserId, in.Users)
	return c.apply(in.UserId, nil, func(b *userbox.Box) (bool, error) {
		update, err := c.svcCtx.Dao.EditIncoming(b, in.GetNewMessage().GetDialogMessageId(), in.GetNewMessage().GetMessage())
		if err != nil || update == nil {
			return true, err
		}
		return true, c.svcCtx.Dao.EmitPushUpdates(b, in.UserId, bot,
			mtproto.MakeUpdatesByUpdatesUsersChats(in.Users, in.Chats, update))
	})
}

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

package core

import (
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// InboxDeleteMessagesToInbox
// inbox.deleteMessagesToInbox user_id:long from_id:long peer_type:int peer_id:long id:Vector<long> = Void;
//
// The recipient's copies of the messages are deleted in portions, one
// transaction and one push each. The connected bot of Telegram Business of
// the recipient that serves the private chat is told of each portion (task
// 105).
func (c *InboxCore) InboxDeleteMessagesToInbox(in *inbox.TLInboxDeleteMessagesToInbox) error {
	peer := recipientPeer(in.FromId, in.PeerType, in.PeerId)
	businessChat, err := c.privateBusinessChat(in.UserId, in.FromId, in.PeerType, in.PeerId)
	if err != nil {
		return err
	}
	var chats map[int64]*dao.BusinessChat
	if businessChat != nil {
		chats = map[int64]*dao.BusinessChat{businessChat.PeerId: businessChat}
	}
	return c.apply(in.UserId, peer, func(b *userbox.Box) (bool, error) {
		deleted, err := c.svcCtx.Dao.DeleteCopies(b, in.Id)
		if err != nil || deleted == nil {
			return true, err
		}
		if err = c.svcCtx.Dao.EmitBusinessDeletedChats(b, deleted, chats); err != nil {
			return true, err
		}
		return deleted.Left == 0, c.push(b, deleted.Update)
	})
}

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

// InboxSendUserMessageToInboxV2
// inbox.sendUserMessageToInboxV2 flags:# user_id:long from_id:long peer_type:int peer_id:long box_list:Vector<MessageBox> users:flags.1?Vector<User> chats:flags.2?Vector<Chat> = Void;
//
// All the messages of the event land in the recipient's box in one
// transaction.
func (c *InboxCore) InboxSendUserMessageToInboxV2(in *inbox.TLInboxSendUserMessageToInboxV2) error {
	bot := isBot(in.UserId, in.Users)
	return c.apply(in.UserId, recipientPeer(in.FromId, in.PeerType, in.PeerId), func(b *userbox.Box) (bool, error) {
		received, err := c.svcCtx.Dao.ReceiveIncoming(b, in.FromId, in.PeerType, in.PeerId, in.BoxList)
		if err != nil || len(received.Updates) == 0 {
			return true, err
		}
		return true, c.svcCtx.Dao.EmitPushUpdates(b, in.UserId, bot,
			mtproto.MakeUpdatesByUpdatesUsersChats(in.Users, in.Chats, received.Updates...))
	})
}

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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// InboxDeleteChatHistoryToInbox
// inbox.deleteChatHistoryToInbox user_id:long from_id:long peer_chat_id:long = Void;
//
// from_id deleted the chat's history for every member: the recipient's
// history of the chat goes in portions. The recipient stays a member of the
// group, so its dialog row stays.
func (c *InboxCore) InboxDeleteChatHistoryToInbox(in *inbox.TLInboxDeleteChatHistoryToInbox) error {
	return c.deleteDialog(in.UserId, mtproto.MakePeerUtil(mtproto.PEER_CHAT, in.PeerChatId), false)
}

// deleteDialog deletes userId's history of the dialog with peer in portions,
// one transaction each, each pushed. With dropRow the last portion deletes
// the dialog row too, in its transaction: a message that arrives later
// creates a new row. A pinned dialog is unpinned on the recipient's devices
// with seq in that transaction (DeleteDialogRow), after the push of the
// deleted messages.
func (c *InboxCore) deleteDialog(userId int64, peer *mtproto.PeerUtil, dropRow bool) error {
	return c.apply(userId, peer, func(b *userbox.Box) (bool, error) {
		deleted, err := c.svcCtx.Dao.DeleteDialogPortion(b, peer.PeerType, peer.PeerId)
		if err != nil {
			return false, err
		}
		var left int32
		if deleted != nil {
			left = deleted.Left
			if err = c.push(b, deleted.Update); err != nil {
				return false, err
			}
		}
		if left == 0 && dropRow {
			_, err = c.svcCtx.Dao.DeleteDialogRow(b, peer.PeerType, peer.PeerId)
		}
		return left == 0, err
	})
}

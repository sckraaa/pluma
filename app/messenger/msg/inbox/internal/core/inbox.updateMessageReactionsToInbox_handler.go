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
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/seqpeers"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// InboxUpdateMessageReactionsToInbox
// inbox.updateMessageReactionsToInbox user_id:long from_id:long peer_type:int peer_id:long dialog_message_id:long = Void;
//
// from_id changed its reactions to a message of a chat with the recipient
// (task 053). The recipient's transaction reads the reactions as they are
// now and journals them for its copy with seq (ADR 0020): a later event of
// the same message reads them again, so the last one applied carries the
// last state. The author's copy gets its unread mark and the dialog its
// counter. A recipient without the copy (deleted for it) gets nothing.
func (c *InboxCore) InboxUpdateMessageReactionsToInbox(in *inbox.TLInboxUpdateMessageReactionsToInbox) error {
	peer := recipientPeer(in.FromId, in.PeerType, in.PeerId)

	// The users the reactions may name, as the recipient sees them: the
	// users of the group, or the two users of the private chat; looked
	// up before the box transaction, which calls no service.
	h := mtproto.NewIDListHelper(in.UserId, in.FromId)
	if peer.PeerType == mtproto.PEER_CHAT {
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: peer.PeerId})
		if err != nil {
			return err
		}
		// Members now or before: a reaction stays when its user leaves.
		chat.Walk(func(userId int64, _ *mtproto.ImmutableChatParticipant) error {
			h.AppendUsers(userId)
			return nil
		})
		h.AppendChats(peer.PeerId)
	}
	peers, err := seqpeers.Resolve(c.ctx, c.svcCtx.Dao, in.UserId, h)
	if err != nil {
		return err
	}
	if peers.Viewer == nil {
		return fmt.Errorf("inbox: user %d, the recipient of reactions, not loaded", in.UserId)
	}
	if peers.Viewer.IsBot() {
		// Bots get no reactions of private chats and basic groups here
		// (the Bot API's message_reaction is not served).
		return c.apply(in.UserId, peer, func(*userbox.Box) (bool, error) { return true, nil })
	}

	return c.apply(in.UserId, peer, func(b *userbox.Box) (bool, error) {
		t, err := c.svcCtx.Dao.OwnCopy(b, in.DialogMessageId)
		if err != nil || t == nil {
			return true, err
		}
		rows, err := c.svcCtx.Dao.ReactionRows(b, []int64{in.DialogMessageId})
		if err != nil {
			return true, err
		}
		if err = c.svcCtx.Dao.MarkUnreadReactions(b, t, rows[in.DialogMessageId]); err != nil {
			return true, err
		}
		u := dao.ReactionsUpdate(in.UserId, t, rows[in.DialogMessageId])
		users, chats := peers.Of(u)
		return true, c.svcCtx.Dao.EmitSeqUpdate(b, u, users, chats)
	})
}

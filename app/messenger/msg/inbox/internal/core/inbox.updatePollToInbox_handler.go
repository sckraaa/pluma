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
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/seqpeers"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// InboxUpdatePollToInbox
// inbox.updatePollToInbox user_id:long from_id:long peer_type:int peer_id:long dialog_message_id:long poll_id:long with_poll:int = Void;
//
// The poll of a message of the chat with the recipient changed: from_id
// voted, or the poll closed or changed (with_poll). The recipient's
// transaction reads the poll as it is now and journals updateMessagePoll
// with seq (ADR 0020), min for everyone but the poll's creator (api/poll),
// so the last event applied carries the last state. The creator of a public
// poll counts the vote as unread here.
func (c *InboxCore) InboxUpdatePollToInbox(in *inbox.TLInboxUpdatePollToInbox) error {
	peer := recipientPeer(in.FromId, in.PeerType, in.PeerId)

	// As for reactions: who the update may name, resolved before the box
	// transaction, which calls no service.
	h := mtproto.NewIDListHelper(in.UserId, in.FromId)
	if peer.PeerType == mtproto.PEER_CHAT {
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: peer.PeerId})
		if err != nil {
			return err
		}
		chat.Walk(func(userId int64, _ *mtproto.ImmutableChatParticipant) error {
			h.AppendUsers(userId)
			return nil
		})
		h.AppendChats(peer.PeerId)
	}
	if peer.PeerType == mtproto.PEER_CHANNEL {
		h.AppendChannels(peer.PeerId)
	}
	peers, err := seqpeers.Resolve(c.ctx, c.svcCtx.Dao, in.UserId, h)
	if err != nil {
		return err
	}
	if peers.Viewer == nil {
		return fmt.Errorf("inbox: user %d, the recipient of a poll update, not loaded", in.UserId)
	}
	if peers.Viewer.IsBot() {
		return c.apply(in.UserId, peer, func(*userbox.Box) (bool, error) { return true, nil })
	}
	return c.apply(in.UserId, peer, func(b *userbox.Box) (bool, error) {
		u, err := c.svcCtx.Dao.ReceivePollUpdate(b, in.FromId, in.DialogMessageId, in.PollId, in.WithPoll != 0)
		if err != nil || u == nil {
			return true, err
		}
		users, chats := peers.Of(u)
		return true, c.svcCtx.Dao.EmitSeqUpdate(b, u, users, chats)
	})
}

// InboxUpdateTodoToInbox
// inbox.updateTodoToInbox user_id:long from_id:long peer_type:int peer_id:long dialog_message_id:long = Void;
//
// A checklist of the chat with the recipient changed: its copy takes the
// current state.
func (c *InboxCore) InboxUpdateTodoToInbox(in *inbox.TLInboxUpdateTodoToInbox) error {
	return c.apply(in.UserId, recipientPeer(in.FromId, in.PeerType, in.PeerId), func(b *userbox.Box) (bool, error) {
		update, err := c.svcCtx.Dao.ApplyTodo(b, in.DialogMessageId)
		if err != nil || update == nil {
			return true, err
		}
		return true, c.svcCtx.Dao.EmitPushUpdates(b, in.UserId, false, mtproto.MakeUpdatesByUpdatesUsersChats(nil, nil, update))
	})
}

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
	peers, err := c.chatPeers(in.UserId, in.FromId, peer)
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
	peer := recipientPeer(in.FromId, in.PeerType, in.PeerId)
	peers, err := c.chatPeers(in.UserId, in.FromId, peer)
	if err != nil {
		return err
	}
	return c.apply(in.UserId, peer, func(b *userbox.Box) (bool, error) {
		update, err := c.svcCtx.Dao.ApplyTodo(b, in.DialogMessageId)
		if err != nil || update == nil {
			return true, err
		}
		return true, c.svcCtx.Dao.EmitPushUpdates(b, in.UserId, false, mtproto.MakeUpdatesByUpdatesUsersChats(peers.Users, peers.Chats, update))
	})
}

// chatPeers resolves, before the box transaction, which calls no service, who
// an update of the recipient's chat with peer may name, as for reactions.
func (c *InboxCore) chatPeers(userId, fromId int64, peer *mtproto.PeerUtil) (*seqpeers.Peers, error) {
	h := mtproto.NewIDListHelper(userId, fromId)
	switch peer.PeerType {
	case mtproto.PEER_CHAT:
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: peer.PeerId})
		if err != nil {
			return nil, err
		}
		chat.Walk(func(id int64, _ *mtproto.ImmutableChatParticipant) error {
			h.AppendUsers(id)
			return nil
		})
		h.AppendChats(peer.PeerId)
	case mtproto.PEER_CHANNEL:
		h.AppendChannels(peer.PeerId)
	}
	return seqpeers.Resolve(c.ctx, c.svcCtx.Dao, userId, h)
}

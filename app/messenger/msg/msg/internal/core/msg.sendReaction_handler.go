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
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/reactions"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgSendReaction
// msg.sendReaction user_id:long auth_key_id:long peer_type:int peer_id:long msg_id:int big:Bool add_to_recent:Bool reaction:Vector<Reaction> = Updates;
//
// The user's reactions to its copy msg_id become reaction, in this order
// (task 053). Each member of the chat gets the reactions as it sees them:
// the user's devices with seq from this transaction, the others through an
// inbox event, in their own transactions, with their own copy's id. The
// reply holds the same update as the user's devices get.
func (c *MsgCore) MsgSendReaction(in *msg.TLMsgSendReaction) (*mtproto.Updates, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	keys, err := reactions.Keys(in.Reaction)
	if err != nil {
		return nil, err
	}
	if peerType == mtproto.PEER_CHANNEL {
		r, err := c.sendChannelReaction(in, keys)
		if err != nil {
			c.Logger.Errorf("msg.sendReaction - error: %v", err)
			return nil, err
		}
		return r, nil
	}
	t, err := c.reactionPeers(in.UserId, peerType, peerId, keys)
	if err != nil {
		c.Logger.Errorf("msg.sendReaction - error: %v", err)
		return nil, err
	}

	var reply *mtproto.Updates
	_, err = c.svcCtx.Dao.RunBox(c.ctx, in.UserId,
		[]string{dialog.GetDialogCacheKeyByPeer(in.UserId, peerType, peerId)},
		func(b *userbox.Box) error {
			target, err := c.svcCtx.Dao.ReactionTarget(b, peerType, peerId, in.MsgId)
			if err != nil {
				return err
			}
			changed, err := c.svcCtx.Dao.SetReactions(b, target, keys, mtproto.FromBool(in.Big))
			if err != nil {
				return err
			}
			rows, err := c.svcCtx.Dao.ReactionRows(b, []int64{target.DialogMessageId})
			if err != nil {
				return err
			}
			update := dao.ReactionsUpdate(in.UserId, target, rows[target.DialogMessageId])
			reply = mtproto.MakeUpdatesByUpdatesUsersChats(t.users(in.UserId), t.chats(in.UserId), update)
			if !changed {
				return nil
			}
			if err = c.svcCtx.Dao.MarkUnreadReactions(b, target, rows[target.DialogMessageId]); err != nil {
				return err
			}
			if err = c.svcCtx.Dao.EmitSeqUpdate(b, update, t.users(in.UserId), t.chats(in.UserId)); err != nil {
				return err
			}
			if target.Kind == reactions.Saved {
				// The tags and their counts changed: the devices load them
				// again.
				if err = c.svcCtx.Dao.EmitSeqUpdate(b, mtproto.MakeTLUpdateSavedReactionTags(nil).To_Update(), nil, nil); err != nil {
					return err
				}
			}
			for _, to := range t.recipients {
				if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxUpdateMessageReactionsToInbox{
					UserId:          to,
					FromId:          in.UserId,
					PeerType:        peerType,
					PeerId:          peerId,
					DialogMessageId: target.DialogMessageId,
				}); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		c.Logger.Errorf("msg.sendReaction - error: %v", err)
		return nil, err
	}
	return reply, nil
}

// reactionTarget are the members of a chat that learn of a change of the
// reactions to one of its messages, and the users and chats the update
// refers to, as each member sees them.
type reactionTarget struct {
	recipients []int64
	users      func(userId int64) []*mtproto.User
	chats      func(userId int64) []*mtproto.Chat
}

// reactionPeers checks that userId may react with keys in its dialog with
// peer and returns who learns of it: the other user of a private chat,
// the other members of a basic group; nobody for Saved Messages. In a
// group the reactions must be among those the group allows.
func (c *MsgCore) reactionPeers(userId int64, peerType int32, peerId int64, keys []string) (*reactionTarget, error) {
	switch peerType {
	case mtproto.PEER_USER:
		ids := []int64{userId}
		if peerId != userId {
			ids = append(ids, peerId)
		}
		users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids, To: ids})
		if err != nil {
			return nil, err
		}
		t := &reactionTarget{
			users: func(viewer int64) []*mtproto.User { return users.GetUserListByIdList(viewer, ids...) },
			chats: func(int64) []*mtproto.Chat { return []*mtproto.Chat{} },
		}
		if peerId != userId {
			t.recipients = []int64{peerId}
		}
		return t, nil
	case mtproto.PEER_CHAT:
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: peerId})
		if err != nil {
			return nil, err
		}
		if me, ok := chat.GetImmutableChatParticipant(userId); !ok || !me.IsChatMemberStateNormal() || chat.Chat.GetDeactivated() {
			return nil, mtproto.ErrChatWriteForbidden
		}
		if err = allowedInChat(chat.Chat, keys); err != nil {
			return nil, err
		}
		everyone := everyMember(chat)
		members := chat.ParticipantIdList()
		mutable, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
			Id:      append([]int64{userId}, everyone...),
			Privacy: true,
			HasTo:   true,
			To:      members,
		})
		if err != nil {
			return nil, err
		}
		users := chatUsers{users: mutable, shown: everyone}
		return &reactionTarget{
			recipients: chatRecipients(chat, userId, false),
			users:      users.For,
			chats:      func(viewer int64) []*mtproto.Chat { return []*mtproto.Chat{chat.ToUnsafeChat(viewer)} },
		}, nil
	}
	return nil, mtproto.ErrPeerIdInvalid
}

// allowedInChat checks keys against the reactions a basic group allows. A
// group that never chose has all the standard ones: Web K and Android read
// a missing available_reactions as none, so the server answers
// chatReactionsAll for it (docs/tasks/053-research/clients.md, sum. 8).
func allowedInChat(chat *mtproto.ImmutableChat, keys []string) error {
	custom := func(k string) bool { return len(k) > 0 && k[0] == 'c' }
	switch chat.GetAvailableReactionsType() {
	case mtproto.ChatReactionsTypeNotDefined, mtproto.ChatReactionsTypeAllNoAllowCustom:
		if slices.ContainsFunc(keys, custom) {
			return mtproto.ErrReactionInvalid
		}
	case mtproto.ChatReactionsTypeAllAllowCustom:
	case mtproto.ChatReactionsTypeNone:
		if len(keys) > 0 {
			return mtproto.ErrReactionInvalid
		}
	case mtproto.ChatReactionsTypeSome:
		allowed := make([]string, 0, len(chat.GetAvailableReactions()))
		for _, s := range chat.GetAvailableReactions() {
			if k, err := reactions.Key(mtproto.FromReaction(s)); err == nil {
				allowed = append(allowed, k)
			}
		}
		for _, k := range keys {
			if !slices.Contains(allowed, k) {
				return mtproto.ErrReactionInvalid
			}
		}
	}
	return nil
}

// everyMember are the ids of all users chat knows, members now or before:
// a reaction stays when its user leaves, and the update that names it in
// recent_reactions carries the user too.
func everyMember(chat *mtproto.MutableChat) []int64 {
	var ids []int64
	chat.Walk(func(userId int64, _ *mtproto.ImmutableChatParticipant) error {
		ids = append(ids, userId)
		return nil
	})
	slices.Sort(ids)
	return ids
}

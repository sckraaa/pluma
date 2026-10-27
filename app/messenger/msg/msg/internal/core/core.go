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
	"context"
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/internal/svc"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

type MsgCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *MsgCore {
	return &MsgCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
	}
}

// selfPeer maps the user's own dialog given as PEER_SELF to the private
// dialog with itself, the way its rows are stored.
func selfPeer(userId int64, peerType int32, peerId int64) (int32, int64) {
	if peerType == mtproto.PEER_SELF {
		return mtproto.PEER_USER, userId
	}
	return peerType, peerId
}

// chatRecipients returns the members of chat other than except who get the
// events of a message operation, by ascending id: members in good standing,
// and with migrated also those of a chat migrated to a supergroup.
func chatRecipients(chat *mtproto.MutableChat, except int64, migrated bool) []int64 {
	var ids []int64
	chat.Walk(func(userId int64, p *mtproto.ImmutableChatParticipant) error {
		if userId != except && (p.IsChatMemberStateNormal() || migrated && p.IsChatMemberStateMigrated()) {
			ids = append(ids, userId)
		}
		return nil
	})
	slices.Sort(ids)
	return ids
}

// withRemoved adds to recipients the users the messages tell of their
// removal from chat (messageActionChatDeleteUser), other than the sender:
// a removed user keeps the chat, forbidden, and gets the message that says
// so, as in Telegram. Who left sends that message themselves.
func withRemoved(recipients []int64, chat *mtproto.MutableChat, fromUserId int64, msgs []*msg.OutboxMessage) []int64 {
	for _, m := range msgs {
		action := m.GetMessage().GetAction()
		if action.GetPredicateName() != mtproto.Predicate_messageActionChatDeleteUser {
			continue
		}
		id := action.GetUserId()
		if _, ok := chat.GetImmutableChatParticipant(id); ok && id != fromUserId && !slices.Contains(recipients, id) {
			recipients = append(recipients, id)
		}
	}
	slices.Sort(recipients)
	return recipients
}

// isBlockedBy tells whether peerId blocked userId: then userId's messages
// and their changes do not reach peerId.
func (c *MsgCore) isBlockedBy(userId, peerId int64) (bool, error) {
	if userId == peerId {
		return false, nil
	}
	blocked, err := c.svcCtx.Dao.UserClient.UserBlockedByUser(c.ctx, &userpb.TLUserBlockedByUser{
		UserId:     peerId,
		PeerUserId: userId,
	})
	if err != nil {
		return false, err
	}
	return mtproto.FromBool(blocked), nil
}

// participants returns the members of chatId who get the events of
// userId's operation (chatRecipients).
func (c *MsgCore) participants(chatId, userId int64) ([]int64, error) {
	chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: chatId})
	if err != nil {
		return nil, err
	}
	return chatRecipients(chat, userId, false), nil
}

// recipients returns the other users who get the events of userId's
// operation on the dialog with peer: the peer of a private chat (nobody for
// the chat with oneself), the members of a basic group.
func (c *MsgCore) recipients(userId int64, peerType int32, peerId int64) ([]int64, error) {
	switch peerType {
	case mtproto.PEER_USER:
		if peerId == userId {
			return nil, nil
		}
		return []int64{peerId}, nil
	case mtproto.PEER_CHAT:
		return c.participants(peerId, userId)
	}
	return nil, nil
}

// dialogRecipients returns recipients for each of userId's dialogs, keyed
// by peer_dialog_id.
func (c *MsgCore) dialogRecipients(userId int64, dialogs []mtproto.DialogID) (map[int64][]int64, error) {
	out := make(map[int64][]int64, len(dialogs))
	for _, did := range dialogs {
		peer := mtproto.FromPeer(mtproto.GetPeerByDialogId(userId, did))
		key := mtproto.MakePeerDialogId(peer.PeerType, peer.PeerId)
		if _, ok := out[key]; ok {
			continue
		}
		ids, err := c.recipients(userId, peer.PeerType, peer.PeerId)
		if err != nil {
			return nil, err
		}
		out[key] = ids
	}
	return out, nil
}

// affectedMessages is the reply of an operation on messages: the committed
// pts of the box and the pts the operation took.
func affectedMessages(res *userbox.Result) *mtproto.Messages_AffectedMessages {
	return mtproto.MakeTLMessagesAffectedMessages(&mtproto.Messages_AffectedMessages{
		Pts:      res.State.Pts,
		PtsCount: ptsCount(res),
	}).To_Messages_AffectedMessages()
}

// ptsCount is the number of pts a transaction took.
func ptsCount(res *userbox.Result) int32 {
	var n int32
	for _, u := range res.Updates {
		n += u.PtsCount
	}
	return n
}

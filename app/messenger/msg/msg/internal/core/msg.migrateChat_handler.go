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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// MsgMigrateChat
// msg.migrateChat user_id:long auth_key_id:long chat_id:long = Updates;
//
// The creator turns a basic group into a supergroup (task 047, ADR 0034,
// p. 10) in one box transaction (dao.MigrateChat). Each other member gets
// its chatMigrateTo, then the supergroup's dialog, as inbox events of the
// same transaction; the creator's other devices get both with seq. The
// answer holds updateNewMessage of chatMigrateTo, updateChannel and
// updateNewChannelMessage of channelMigrateFrom, with the group (migrated
// to) and the supergroup in chats: iOS takes the supergroup from the first
// message that is not the group's (ConvertGroupToSupergroup.swift:13-29),
// Android and tdesktop from chats (MessagesController.java:15103-15122,
// apiwrap.cpp:1387-1394), Web K from updateChannel
// (appChatsManager.ts:784-786). A group moved already answers the same
// chats and updateChannel, without making a second supergroup.
func (c *MsgCore) MsgMigrateChat(in *msg.TLMsgMigrateChat) (*mtproto.Updates, error) {
	r, err := c.migrateChat(in)
	if err != nil {
		c.Logger.Errorf("msg.migrateChat - error: %v", err)
		return nil, err
	}
	return r, nil
}

func (c *MsgCore) migrateChat(in *msg.TLMsgMigrateChat) (*mtproto.Updates, error) {
	chat, users, err := c.chatAndUsers(in.ChatId, mtproto.NewIDListHelper(in.UserId))
	if err != nil {
		return nil, err
	}
	members := chatRecipients(chat, in.UserId, false)
	keys := []string{dialog.GetDialogCacheKeyByPeer(in.UserId, mtproto.PEER_CHAT, in.ChatId), chatpb.CacheKey(in.ChatId)}
	chat.Walk(func(userId int64, _ *mtproto.ImmutableChatParticipant) error {
		keys = append(keys, chatpb.ParticipantCacheKey(in.ChatId, userId))
		return nil
	})

	dialogMessageId, err := c.svcCtx.Dao.NextId(c.ctx)
	if err != nil {
		return nil, err
	}
	var m *dao.Migrated
	moved := proto.Clone(chat).(*mtproto.MutableChat)
	if _, err = c.svcCtx.Dao.RunBox(c.ctx, in.UserId, keys, func(b *userbox.Box) error {
		if m, err = c.svcCtx.Dao.MigrateChat(b, chat, dialogMessageId); err != nil || m.Again {
			return err
		}
		moved.Chat.Deactivated = true
		moved.Chat.MigratedTo = mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: m.ChannelId, AccessHash: m.AccessHash}).To_InputChannel()
		mine := mtproto.MakeUpdatesByUpdatesUsersChats(users.For(in.UserId), []*mtproto.Chat{moved.ToUnsafeChat(in.UserId)}, newMessageUpdates(m.Service)...)
		if err := c.svcCtx.Dao.EmitUpdatesNotMe(b, in.UserId, in.AuthKeyId, mine); err != nil {
			return err
		}
		for _, to := range members {
			if err := c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxSendUserMessageToInboxV2{
				UserId:   to,
				FromId:   in.UserId,
				PeerType: mtproto.PEER_CHAT,
				PeerId:   in.ChatId,
				BoxList:  m.Service,
				Users:    users.For(to),
				Chats:    []*mtproto.Chat{moved.ToUnsafeChat(to)},
			}); err != nil {
				return err
			}
			if err := c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxChannelInvitedToInbox{UserId: to, FromId: in.UserId, ChannelId: m.ChannelId}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: m.ChannelId, Id: []int64{in.UserId}})
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	view := mc.ToChat(in.UserId, now)
	if m.Again {
		moved.Chat.Deactivated = true
		moved.Chat.MigratedTo = mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: m.ChannelId, AccessHash: m.AccessHash}).To_InputChannel()
		return mtproto.MakeTLUpdates(&mtproto.Updates{
			Updates: []*mtproto.Update{mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: m.ChannelId}).To_Update()},
			Users:   users.For(in.UserId),
			Chats:   []*mtproto.Chat{moved.ToUnsafeChat(in.UserId), view},
			Date:    now,
		}).To_Updates(), nil
	}
	// The creator's other devices: the supergroup's dialog.
	if _, err = c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		return c.svcCtx.Dao.EmitSeqUpdate(b, mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: m.ChannelId}).To_Update(), nil, []*mtproto.Chat{view})
	}); err != nil {
		// The supergroup exists; the other devices learn it from the dialogs.
		c.Logger.Errorf("msg.migrateChat - updateChannel of %d: %v", m.ChannelId, err)
	}
	first := proto.Clone(m.First).(*mtproto.Message)
	first.Out = true
	updates := newMessageUpdates(m.Service)
	updates = append(updates,
		mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: m.ChannelId}).To_Update(),
		mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: first, Pts_INT32: m.FirstPts, PtsCount: 1}).To_Update())
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: updates,
		Users:   users.For(in.UserId),
		Chats:   []*mtproto.Chat{moved.ToUnsafeChat(in.UserId), view},
		Date:    now,
	}).To_Updates(), nil
}

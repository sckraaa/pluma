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
	"math/rand/v2"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// ChannelsLimit is the most channels and supergroups a user is in:
// channels_limit_default of the clients and Telegram (core.telegram.org/
// api/config, 500); the owner kept Telegram's limits (task 035).
const ChannelsLimit = 500

// Rights of a creator (channelParticipantCreator.admin_rights). Telegram
// does not document them; these are the rights each kind of chat has
// (UNVERIFIED against Telegram's answer).
const (
	broadcastCreatorRights = mtproto.ADMIN_CHANGE_INFO | mtproto.ADMIN_POST_MESSAGES | mtproto.ADMIN_EDIT_MESSAGES |
		mtproto.ADMIN_DELETE_MESSAGES | mtproto.ADMIN_INVITE_USERS | mtproto.ADMIN_ADD_ADMINS | mtproto.ADMIN_MANAGE_CALL |
		mtproto.ADMIN_OTHER | mtproto.ADMIN_POST_STORIES | mtproto.ADMIN_EDIT_STORIES | mtproto.ADMIN_DELETE_STORIES
	megagroupCreatorRights = mtproto.ADMIN_CHANGE_INFO | mtproto.ADMIN_DELETE_MESSAGES | mtproto.ADMIN_BAN_USERS |
		mtproto.ADMIN_INVITE_USERS | mtproto.ADMIN_PIN_MESSAGES | mtproto.ADMIN_ADD_ADMINS | mtproto.ADMIN_MANAGE_CALL |
		mtproto.ADMIN_OTHER | mtproto.ADMIN_MANAGE_TOPICS | mtproto.ADMIN_POST_STORIES | mtproto.ADMIN_EDIT_STORIES |
		mtproto.ADMIN_DELETE_STORIES
)

// ChannelCreateChannel
// channel.createChannel creator_id:long title:string about:string broadcast:Bool megagroup:Bool = Updates;
//
// The channel, its creator, its first message (messageActionChannelCreate,
// id 1, the channel's pts BasePts+1), the creator's dialog and permanent
// invite link are one box transaction of the creator (ADR 0034, p. 2): no
// half-made channel is ever seen. The creator's other devices get
// updateChannel with seq. The answer holds updateNewChannelMessage of the
// first message and the channel first in chats, as the clients take the new
// channel from it (Web K appChatsManager.ts:588-593, Android
// MessagesController.java:15036-15069, tdesktop add_contact_box.cpp:822-917,
// iOS ChannelCreation.swift:50-90; task 035). The users of the answer are
// bff's to add.
func (c *ChannelCore) ChannelCreateChannel(in *channel.TLChannelCreateChannel) (*mtproto.Updates, error) {
	broadcast, megagroup := mtproto.FromBool(in.Broadcast), mtproto.FromBool(in.Megagroup)
	if broadcast == megagroup {
		return nil, mtproto.ErrChannelInvalid
	}
	rights := broadcastCreatorRights
	if megagroup {
		rights = megagroupCreatorRights
	}

	var (
		channelId int64
		first     *mtproto.Message
		pts       int32
	)
	_, err := c.svcCtx.Dao.RunBox(c.ctx, in.CreatorId, func(b *userbox.Box) error {
		var joined []int64
		if err := b.Tx().QueryRowsPartial(&joined, "SELECT channel_id FROM channel_participants WHERE user_id = ? AND state = 0 LIMIT ?",
			in.CreatorId, ChannelsLimit); err != nil {
			return err
		}
		if len(joined) >= ChannelsLimit {
			return mtproto.ErrChannelsTooMuch
		}
		ch, err := channelbox.Create(b, channelbox.NewChannel{
			AccessHash:    rand.Int64(), //nolint:gosec // an access hash checks a client's reference, it is no secret of crypto
			CreatorUserId: in.CreatorId,
			Title:         in.Title,
			About:         in.About,
			Broadcast:     broadcast,
			Megagroup:     megagroup,
			Date:          b.Date(),
		})
		if err != nil {
			return err
		}
		channelId = ch.Id()
		if _, err = b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, ?)",
			channelId, in.CreatorId, channel.RoleCreator, rights, b.Date()); err != nil {
			return err
		}
		if _, err = b.Tx().Exec("UPDATE channels SET participants_count = 1, admins_count = 1, top_message = 1 WHERE id = ?", channelId); err != nil {
			return err
		}

		// The first message: from the creator in a supergroup, a post
		// in a channel, as Telegram's service messages are.
		id := ch.NextMessageId()
		first = mtproto.MakeTLMessageService(&mtproto.Message{
			Id:     id,
			Post:   broadcast,
			PeerId: mtproto.MakePeerChannel(channelId),
			Date:   int32(b.Date()),
			Action: mtproto.MakeTLMessageActionChannelCreate(&mtproto.MessageAction{Title: in.Title}).To_MessageAction(),
		}).To_Message()
		if megagroup {
			first.FromId = mtproto.MakePeerUser(in.CreatorId)
		}
		ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: first}).To_Update(), 1)
		pts = ch.Pts()
		data, err := jsonx.MarshalToString(first)
		if err != nil {
			return err
		}
		if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_data, date2) VALUES (?, ?, ?, ?, ?, ?)",
			channelId, id, in.CreatorId, pts, data, b.Date()); err != nil {
			return err
		}

		// The creator's dialog: everything is read.
		if _, err = b.Tx().Exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, top_message, read_inbox_max_id, read_outbox_max_id, draft_message_data, date2)"+
			" VALUES (?, ?, ?, ?, ?, ?, ?, 'null', ?)",
			in.CreatorId, mtproto.PEER_CHANNEL, channelId, mtproto.MakePeerDialogId(mtproto.PEER_CHANNEL, channelId), id, id, id, b.Date()); err != nil {
			return err
		}
		// The permanent invite link: Android's creation screen takes the
		// first of messages.getExportedChatInvites(limit=1) unchecked
		// (ChannelCreateActivity.java:940-943).
		if _, err = b.Tx().Exec("INSERT INTO chat_invites (chat_id, admin_id, link, permanent, date2) VALUES (?, ?, ?, 1, ?)",
			channelId, in.CreatorId, chatpb.GenChannelInviteHash(), b.Date()); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		c.Logger.Errorf("channel.createChannel - error: %v", err)
		return nil, err
	}

	// The channel as the creator sees it, read once committed.
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, channelId, []int64{in.CreatorId})
	if err != nil {
		c.Logger.Errorf("channel.createChannel - read the new channel %d: %v", channelId, err)
		return nil, err
	}
	view := mc.ToChat(in.CreatorId, int32(time.Now().Unix()))
	if _, err = c.svcCtx.Dao.RunBox(c.ctx, in.CreatorId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.EmitSeqUpdate(b, mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: channelId}).To_Update(), nil, []*mtproto.Chat{view})
	}); err != nil {
		// The channel exists; the creator's other devices learn it from
		// the dialogs.
		c.Logger.Errorf("channel.createChannel - updateChannel of %d: %v", channelId, err)
	}

	shown := proto.Clone(first).(*mtproto.Message)
	shown.Out = true
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{
			mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: pts, PtsCount: 1}).To_Update(),
			mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: channelId}).To_Update(),
		},
		Users: []*mtproto.User{},
		Chats: []*mtproto.Chat{view},
		Date:  int32(time.Now().Unix()),
	}).To_Updates(), nil
}

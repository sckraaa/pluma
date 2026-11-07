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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// ChannelsGetFullChannel
// channels.getFullChannel#8736a09 channel:InputChannel = messages.ChatFull;
//
// The full channel as the account sees it (task 038): pts and the read
// marks and counts of its dialog, which the clients start the channel's
// state from (tdesktop does not ask for a difference from pts 0,
// api_updates.cpp:726; Web K throws on it, apiUpdatesManager.ts:531-534);
// the counts, the rights to view members and to set a username, and for
// those who may invite the permanent link, which tdesktop asks to export
// when it is missing (task 035, U7).
func (c *ChannelsCore) ChannelsGetFullChannel(in *mtproto.TLChannelsGetFullChannel) (*mtproto.Messages_ChatFull, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if err = readable(mc, c.MD.UserId); err != nil {
		return nil, err
	}
	ch := mc.GetChannel()
	me, _ := mc.GetParticipant(c.MD.UserId)
	admin := me.Member() && (me.GetRole() == channelpb.RoleCreator || me.GetRole() == channelpb.RoleAdmin)

	full := &mtproto.ChatFull{
		Id:                  ch.GetId(),
		About:               ch.GetAbout(),
		ParticipantsCount:   mtproto.MakeFlagsInt32(ch.GetParticipantsCount()),
		ChatPhoto:           ch.GetPhoto(),
		BotInfo:             []*mtproto.BotInfo{},
		CanViewParticipants: ch.GetMegagroup() || admin,
		CanSetUsername:      me.GetRole() == channelpb.RoleCreator,
		CanDeleteChannel:    me.GetRole() == channelpb.RoleCreator,
		HiddenPrehistory:    ch.GetHiddenPrehistory(),
		Pts:                 ch.GetPts(),
		// Reactions of channels are task 044: the standard ones.
		AvailableReactions_FLAGCHATREACTIONS: mtproto.FromChatReactions(mtproto.ChatReactionsTypeAllNoAllowCustom, nil),
	}
	if full.ChatPhoto == nil {
		full.ChatPhoto = mtproto.MakeTLPhotoEmpty(nil).To_Photo()
	}
	if admin {
		full.AdminsCount = mtproto.MakeFlagsInt32(ch.GetAdminsCount())
		full.KickedCount = mtproto.MakeFlagsInt32(ch.GetKickedCount())
		full.BannedCount = mtproto.MakeFlagsInt32(ch.GetBannedCount())
	}
	if ch.GetMigratedFromChatId() != 0 {
		full.MigratedFromChatId = mtproto.MakeFlagsInt64(ch.GetMigratedFromChatId())
		full.MigratedFromMaxId = mtproto.MakeFlagsInt32(ch.GetMigratedFromMaxId())
	}
	if ch.GetLinkedChatId() != 0 {
		// The discussion group, or the channel a group discusses (task 048).
		full.LinkedChatId = mtproto.MakeFlagsInt64(ch.GetLinkedChatId())
	}
	if ch.GetSlowmodeSeconds() > 0 {
		full.SlowmodeSeconds = mtproto.MakeFlagsInt32(ch.GetSlowmodeSeconds())
	}

	dialogs, err := c.svcCtx.Channel.ChannelGetChannelDialogs(c.ctx, &channelpb.TLChannelGetChannelDialogs{SelfId: c.MD.UserId, IdList: []int64{ch.GetId()}})
	if err != nil {
		c.Logger.Errorf("channels.getFullChannel - error: %v", err)
		return nil, err
	}
	if d := dialogs.GetDatas(); len(d) > 0 {
		full.ReadInboxMaxId = d[0].GetReadInboxMaxId()
		full.ReadOutboxMaxId = d[0].GetReadOutboxMaxId()
		full.UnreadCount = d[0].GetUnreadCount()
		full.Pts = d[0].GetPts().GetValue()
	}
	// The pinned bar shows the newest pinned message (task 043).
	pinned, err := c.svcCtx.Channel.ChannelSearch(c.ctx, &channelpb.TLChannelSearch{
		SelfId: c.MD.UserId, ChannelId: ch.GetId(), MediaType: mtproto.MEDIA_PINNED, Limit: 1,
	})
	if err != nil {
		c.Logger.Errorf("channels.getFullChannel - error: %v", err)
		return nil, err
	}
	if m := pinned.GetMessages(); len(m) > 0 {
		full.PinnedMsgId = mtproto.MakeFlagsInt32(m[0].GetId())
	}
	if full.NotifySettings, err = c.svcCtx.User.UserGetNotifySettings(c.ctx, &userpb.TLUserGetNotifySettings{
		UserId:   c.MD.UserId,
		PeerType: mtproto.PEER_CHANNEL,
		PeerId:   ch.GetId(),
	}); err != nil {
		c.Logger.Errorf("channels.getFullChannel - error: %v", err)
		return nil, err
	}
	if admin {
		invites, err := c.svcCtx.Chat.ChatGetExportedChatInvites(c.ctx, &chatpb.TLChatGetExportedChatInvites{ChatId: ch.GetId(), AdminId: c.MD.UserId, Limit: 50})
		if err != nil {
			c.Logger.Errorf("channels.getFullChannel - error: %v", err)
			return nil, err
		}
		for _, it := range invites.GetDatas() {
			if it.GetPermanent() && !it.GetRevoked() {
				full.ExportedInvite = it
				break
			}
		}
	}

	users, err := c.svcCtx.User.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{c.MD.UserId}, To: []int64{c.MD.UserId}})
	if err != nil {
		c.Logger.Errorf("channels.getFullChannel - error: %v", err)
		return nil, err
	}
	chats := []*mtproto.Chat{mc.ToChat(c.MD.UserId, int32(time.Now().Unix()))}
	if linked := ch.GetLinkedChatId(); linked != 0 {
		l, err := c.svcCtx.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: []int64{linked}})
		if err != nil {
			c.Logger.Errorf("channels.getFullChannel - error: %v", err)
			return nil, err
		}
		chats = append(chats, l.GetChannelListByIdList(c.MD.UserId)...)
	}
	return mtproto.MakeTLMessagesChatFull(&mtproto.Messages_ChatFull{
		FullChat: mtproto.MakeTLChannelFull(full).To_ChatFull(),
		Chats:    chats,
		Users:    users.GetUserListByIdList(c.MD.UserId, c.MD.UserId),
	}).To_Messages_ChatFull(), nil
}

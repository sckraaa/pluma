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
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// availableMinId is channelFull.available_min_id for participant p. The row
// keeps the first message p sees, the field the last it does not
// (core.telegram.org/constructor/channelFull, History::clearUpTill, tdesktop).
// The row outlives leaving, but one outside a public channel reads all of it
// (viewerOf in biz channel), so only a member has anything hidden.
func availableMinId(p *channelpb.ImmutableChannelParticipant) *wrapperspb.Int32Value {
	if !p.Member() || p.GetAvailableMinId() <= 1 {
		return nil
	}
	return mtproto.MakeFlagsInt32(p.GetAvailableMinId() - 1)
}

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
		CanViewParticipants: ch.GetMegagroup() && !ch.GetParticipantsHidden() || admin,
		ParticipantsHidden:  ch.GetParticipantsHidden(),
		CanSetUsername:      me.GetRole() == channelpb.RoleCreator,
		CanDeleteChannel:    me.GetRole() == channelpb.RoleCreator,
		HiddenPrehistory:    ch.GetHiddenPrehistory(),
		Pts:                 ch.GetPts(),
		// The reactions the members may use (task 044); a channel that never
		// chose has the standard ones.
		AvailableReactions_FLAGCHATREACTIONS: availableReactions(ch),
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
		// The countdown of the input field (tdesktop data_channel.cpp:1415,
		// Android AlertsCreator.java:794, Web K input.ts:3901).
		if next := ch.SlowmodeNextSendDate(me, time.Now().Unix()); next != 0 {
			full.SlowmodeNextSendDate = mtproto.MakeFlagsInt32(next)
		}
	}
	full.AvailableMinId = availableMinId(me)
	if full.BotInfo, err = c.channelBotInfo(ch.GetId(), admin); err != nil {
		c.Logger.Errorf("channels.getFullChannel - error: %v", err)
		return nil, err
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
		full.ViewForumAsMessages = d[0].GetViewForumAsMessages()
		full.TtlPeriod = d[0].GetTtlPeriod()
	}
	if c.svcCtx.Look != nil {
		wallpaper, emoticon, err := c.svcCtx.Look.ChannelLook(c.ctx, ch.GetId())
		if err != nil {
			c.Logger.Errorf("channels.getFullChannel - error: %v", err)
			return nil, err
		}
		full.Wallpaper = wallpaper
		full.ThemeEmoticon = mtproto.MakeFlagsString(emoticon)
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
	var requesters []int64
	if admin && ch.Can(me, channelperm.InviteUsers, int32(time.Now().Unix())) == nil {
		// Those who may answer the requests see them (task 041).
		pending, err := c.svcCtx.Channel.ChannelGetPendingJoinRequests(c.ctx, &channelpb.TLChannelGetPendingJoinRequests{SelfId: c.MD.UserId, ChannelId: ch.GetId()})
		if err != nil {
			c.Logger.Errorf("channels.getFullChannel - error: %v", err)
			return nil, err
		}
		if n := pending.GetRequestsPending(); n > 0 {
			requesters = pending.GetRecentRequesters()
			full.RequestsPending = mtproto.MakeFlagsInt32(n)
			full.RecentRequesters = requesters
		}
	}
	hasScheduled, err := c.svcCtx.Msg.MsgHasScheduled(c.ctx, &msgpb.TLMsgHasScheduled{
		UserId:   c.MD.UserId,
		PeerType: mtproto.PEER_CHANNEL,
		PeerId:   ch.GetId(),
	})
	if err != nil {
		c.Logger.Errorf("channels.getFullChannel - error: %v", err)
		return nil, err
	}
	full.HasScheduled = mtproto.FromBool(hasScheduled)
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

	ids := append([]int64{c.MD.UserId}, requesters...)
	users, err := c.svcCtx.User.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids, To: []int64{c.MD.UserId}})
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
		Users:    users.GetUserListByIdList(c.MD.UserId, ids...),
	}).To_Messages_ChatFull(), nil
}

// availableReactions are the reactions of the channel: the standard ones
// until it chooses (as new groups have them, task 053).
func availableReactions(ch *channelpb.ImmutableChannel) *mtproto.ChatReactions {
	t := ch.GetAvailableReactionsType()
	if t == mtproto.ChatReactionsTypeNotDefined {
		t = mtproto.ChatReactionsTypeAllNoAllowCustom
	}
	return mtproto.FromChatReactions(t, ch.GetAvailableReactions())
}

// channelBotInfo is the bot_info of the channel's bots, as the user sees
// them (core.telegram.org/api/bots/commands).
func (c *ChannelsCore) channelBotInfo(channelId int64, admin bool) ([]*mtproto.BotInfo, error) {
	members, err := c.svcCtx.Channel.ChannelGetMemberIdList(c.ctx, &channelpb.TLChannelGetMemberIdList{ChannelId: channelId})
	if err != nil {
		return nil, err
	}
	bots, err := c.svcCtx.User.UserCheckBots(c.ctx, &userpb.TLUserCheckBots{Id: members.GetDatas()})
	if err != nil {
		return nil, err
	}
	out := make([]*mtproto.BotInfo, 0, len(bots.GetDatas()))
	for _, id := range bots.GetDatas() {
		info, err := c.svcCtx.Bots.BotInfo(c.ctx, id, botstore.Viewer{
			UserId: c.MD.UserId, PeerType: mtproto.PEER_CHANNEL, PeerId: channelId, Admin: admin, Lang: c.MD.LangCode,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

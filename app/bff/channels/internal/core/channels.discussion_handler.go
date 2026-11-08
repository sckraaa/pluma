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
	"errors"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// ChannelsSetDiscussionGroup
// channels.setDiscussionGroup#40582bb2 broadcast:InputChannel group:InputChannel = Bool;
//
// The discussion group of a channel (task 048, biz channel.setDiscussionGroup):
// inputChannelEmpty as the group unlinks it. A basic group the clients
// turn into a supergroup first (messages.migrateChat).
func (c *ChannelsCore) ChannelsSetDiscussionGroup(in *mtproto.TLChannelsSetDiscussionGroup) (*mtproto.Bool, error) {
	mc, err := c.mutableChannel(in.Broadcast)
	if err != nil {
		if errors.Is(err, mtproto.ErrChannelInvalid) {
			return nil, mtproto.ErrBroadcastIdInvalid
		}
		return nil, err
	}
	var groupId int64
	if in.GetGroup().GetPredicateName() != mtproto.Predicate_inputChannelEmpty {
		g, err := c.mutableChannel(in.Group)
		if err != nil {
			if errors.Is(err, mtproto.ErrChannelInvalid) {
				return nil, mtproto.ErrMegagroupIdInvalid
			}
			return nil, err
		}
		groupId = g.GetChannel().GetId()
	}
	return c.svcCtx.Channel.ChannelSetDiscussionGroup(c.ctx, &channelpb.TLChannelSetDiscussionGroup{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
		GroupId:       groupId,
	})
}

// ChannelsGetGroupsForDiscussion
// channels.getGroupsForDiscussion#f5dad378 = messages.Chats;
//
// The groups the account may link to a channel (task 048): the supergroups
// (biz channel) and the basic groups it created, which only its creator
// may migrate. "Returned basic group chats must be first upgraded to
// supergroups before they can be set as a discussion group"
// (core.telegram.org/method/channels.getGroupsForDiscussion): all the
// clients take the chat constructor in the list and call
// messages.migrateChat before channels.setDiscussionGroup (Android
// ChatLinkActivity.java:578-585, tdesktop edit_discussion_link_box.cpp:199,
// iOS ChannelDiscussionGroupSetupController.swift:365, Web
// appChatsManager.ts:1241).
func (c *ChannelsCore) ChannelsGetGroupsForDiscussion(in *mtproto.TLChannelsGetGroupsForDiscussion) (*mtproto.Messages_Chats, error) {
	ids, err := c.svcCtx.Channel.ChannelGetGroupsForDiscussion(c.ctx, &channelpb.TLChannelGetGroupsForDiscussion{SelfId: c.MD.UserId})
	if err != nil {
		return nil, err
	}
	chats := []*mtproto.Chat{}
	if len(ids.GetDatas()) > 0 {
		channels, err := c.svcCtx.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: ids.GetDatas()})
		if err != nil {
			return nil, err
		}
		chats = channels.GetChannelListByIdList(c.MD.UserId)
	}
	basic, err := c.basicGroupsForDiscussion()
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesChats(&mtproto.Messages_Chats{Chats: append(chats, basic...)}).To_Messages_Chats(), nil
}

// basicGroupsForDiscussion are the live basic groups the account created.
func (c *ChannelsCore) basicGroupsForDiscussion() ([]*mtproto.Chat, error) {
	lists, err := c.svcCtx.Chat.ChatGetUsersChatIdList(c.ctx, &chatpb.TLChatGetUsersChatIdList{Id: []int64{c.MD.UserId}})
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, l := range lists.GetDatas() {
		if l.GetUserId() == c.MD.UserId {
			ids = l.GetChatIdList()
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	chats, err := c.svcCtx.Chat.ChatGetChatListByIdList(c.ctx, &chatpb.TLChatGetChatListByIdList{SelfId: c.MD.UserId, IdList: ids})
	if err != nil {
		return nil, err
	}
	var out []*mtproto.Chat
	for _, m := range chats.GetDatas() {
		if m.Creator() == c.MD.UserId && !m.Deactivated() && m.MigratedTo() == nil {
			out = append(out, m.ToUnsafeChat(c.MD.UserId))
		}
	}
	return out, nil
}

// Copyright (c) 2026 The Teamgram Authors (https://teamgram.net).
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

package core

import (
	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// MessagesGetFutureChatCreatorAfterLeave
// messages.getFutureChatCreatorAfterLeave#3b7d0ea6 peer:InputPeer = User;
//
// Who owns the group if the account, its owner, leaves it now
// (core.telegram.org/method/messages.getFutureChatCreatorAfterLeave): a
// basic group goes to that member at once (chat.FutureCreator, which
// chat.deleteChatUser hands it to), a supergroup or a channel seven days
// later (biz channel.getFutureCreator, as channel.leaveChannel does).
// tdesktop asks it for both kinds of group (window_peer_menu.cpp:4340,
// DeleteAndLeaveHandler), iOS as well (RemovePeerChat.swift:95). With
// nobody to take the group the answer is PARTICIPANT_ID_INVALID: every
// client leaves at once on an error (tdesktop .fail, iOS `catch` to nil;
// core.telegram.org/api/channel, "Leaving groups/channels"), while iOS
// would show userEmpty as a user without a name
// (ChatListControllerLeaveChatConfirmation.swift).
func (c *ChatsCore) MessagesGetFutureChatCreatorAfterLeave(in *mtproto.TLMessagesGetFutureChatCreatorAfterLeave) (*mtproto.User, error) {
	if c.MD.UserId == 0 {
		return nil, mtproto.ErrAuthKeyUnregistered
	}
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	var (
		next int64
		err  error
	)
	switch peer.PeerType {
	case mtproto.PEER_CHAT:
		next, err = c.futureChatCreator(peer.PeerId)
	case mtproto.PEER_CHANNEL:
		next, err = c.futureChannelCreator(peer.PeerId, peer.AccessHash)
	default:
		err = mtproto.ErrPeerIdInvalid
	}
	if err == nil && next == 0 {
		err = mtproto.ErrParticipantIdInvalid
	}
	if err != nil {
		c.Logger.Errorf("messages.getFutureChatCreatorAfterLeave - error: %v", err)
		return nil, err
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{Id: []int64{c.MD.UserId, next}})
	if err != nil {
		c.Logger.Errorf("messages.getFutureChatCreatorAfterLeave - error: %v", err)
		return nil, err
	}
	return users.GetUnsafeUser(c.MD.UserId, next)
}

// futureChatCreator is who owns the basic group chatId after the account,
// its creator, leaves it, 0 for nobody: CHAT_ADMIN_REQUIRED for anyone
// else, as the method is the owners'.
func (c *ChatsCore) futureChatCreator(chatId int64) (int64, error) {
	chat, err := c.svcCtx.Dao.ChatClient.Client().ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: chatId})
	if err != nil {
		return 0, err
	}
	if chat.GetChat().GetDeactivated() || chat.GetChat().GetMigratedTo() != nil {
		return 0, mtproto.ErrChatIdInvalid
	}
	me, _ := chat.GetImmutableChatParticipant(c.MD.UserId)
	if me == nil || !me.IsChatMemberStateNormal() || !me.IsChatMemberCreator() {
		return 0, mtproto.ErrChatAdminRequired
	}
	var ids []int64
	chat.Walk(func(userId int64, p *mtproto.ImmutableChatParticipant) error {
		if p.IsChatMemberStateNormal() {
			ids = append(ids, userId)
		}
		return nil
	})
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{Id: ids})
	if err != nil {
		return 0, err
	}
	return chatpb.FutureCreator(chat, func(userId int64) bool {
		u, ok := users.GetImmutableUser(userId)
		return !ok || u.Deleted()
	}), nil
}

// futureChannelCreator is who owns the supergroup or channel after the
// account, its owner, leaves it (biz channel.getFutureCreator), 0 for
// nobody; CHANNEL_INVALID for another access hash.
func (c *ChatsCore) futureChannelCreator(channelId, accessHash int64) (int64, error) {
	mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId, Id: []int64{c.MD.UserId}})
	if err != nil {
		return 0, err
	}
	if mc.GetChannel().GetAccessHash() != accessHash {
		return 0, mtproto.ErrChannelInvalid
	}
	r, err := c.svcCtx.Dao.Channel.ChannelGetFutureCreator(c.ctx, &channelpb.TLChannelGetFutureCreator{SelfId: c.MD.UserId, ChannelId: channelId})
	if err != nil {
		return 0, err
	}
	return r.GetV(), nil
}

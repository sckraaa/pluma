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
	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// notifyException tells whether settings of a chat differ from what its
// class gives it: a field of the chat's own that is set. Mute, previews and
// "silent" always count ("chats with non-default notification settings");
// a sound counts with compareSound ("if set, chats with non-default sound
// will be returned"), core.telegram.org/method/account.getNotifyExceptions.
// tdesktop takes a chat with any mute value of its own as an exception
// (data_notify_settings.cpp:683), so a value equal to the class's still
// counts.
//
// compare_stories asks for chats with settings of their own for stories:
// stories are off here (stories_posting of help.getAppConfig) and no
// settings for them are kept, so no chat has any and the flag adds none.
func notifyException(s *mtproto.PeerNotifySettings, compareSound bool) bool {
	if s.GetMuteUntil() != nil || s.GetShowPreviews() != nil || s.GetSilent() != nil {
		return true
	}
	if !compareSound {
		return false
	}
	if v := s.GetSound(); v != nil && v.GetValue() != "default" {
		return true
	}
	for _, sound := range []*mtproto.NotificationSound{s.GetIosSound(), s.GetAndroidSound(), s.GetOtherSound()} {
		if sound != nil && sound.GetPredicateName() != mtproto.Predicate_notificationSoundDefault {
			return true
		}
	}
	return false
}

// AccountGetNotifyExceptions
// account.getNotifyExceptions#53577479 flags:# compare_sound:flags.1?true compare_stories:flags.2?true peer:flags.0?InputNotifyPeer = Updates;
//
// The chats whose notification settings are their own, each as
// updateNotifySettings with the chat: the clients build their lists of
// exceptions from the updates (tdesktop data_notify_settings.cpp:670-676
// applies them; iOS NotificationExceptionsList.swift:29-79 reads the
// settings and the peers).
//
// peer narrows the answer to a category ("only chats of the specified
// category will be returned"): tdesktop asks for inputNotifyUsers,
// inputNotifyChats and inputNotifyBroadcasts apart. Groups and supergroups
// are chats, channels are broadcasts. A forum as the peer gives the
// exceptions of its topics; another chat as the peer gives that chat.
func (c *NotificationCore) AccountGetNotifyExceptions(in *mtproto.TLAccountGetNotifyExceptions) (*mtproto.Updates, error) {
	var (
		category = mtproto.PEER_EMPTY // every category
		onlyPeer *mtproto.PeerUtil
	)
	switch in.GetPeer().GetPredicateName() {
	case "":
	case mtproto.Predicate_inputNotifyUsers:
		category = mtproto.PEER_USERS
	case mtproto.Predicate_inputNotifyChats:
		category = mtproto.PEER_CHATS
	case mtproto.Predicate_inputNotifyBroadcasts:
		category = mtproto.PEER_BROADCASTS
	case mtproto.Predicate_inputNotifyPeer:
		peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer().GetPeer())
		switch peer.PeerType {
		case mtproto.PEER_CHANNEL:
			// A forum as the peer: the exceptions of its topics (iOS
			// ForumChannels.swift:1345-1363 reads notifyForumTopic from the
			// answer; Android asks so when a forum opens,
			// TopicsFragment.java:2720-2724).
			updates, err := c.svcCtx.Dao.UserClient.UserGetTopicNotifyExceptions(c.ctx, &userpb.TLUserGetTopicNotifyExceptions{
				UserId:    c.MD.UserId,
				ChannelId: peer.PeerId,
			})
			if err != nil {
				c.Logger.Errorf("account.getNotifyExceptions - error: %v", err)
				return nil, err
			}
			return updates, nil
		case mtproto.PEER_USER, mtproto.PEER_CHAT:
			onlyPeer = peer
		default:
			err := mtproto.ErrPeerIdInvalid
			c.Logger.Errorf("account.getNotifyExceptions - error: %v", err)
			return nil, err
		}
	default:
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("account.getNotifyExceptions - %s: %v", in.GetPeer().GetPredicateName(), err)
		return nil, err
	}

	settings, err := c.svcCtx.Dao.UserClient.UserGetAllNotifySettings(c.ctx, &userpb.TLUserGetAllNotifySettings{
		UserId: c.MD.UserId,
	})
	if err != nil {
		c.Logger.Errorf("account.getNotifyExceptions - error: %v", err)
		return nil, err
	}

	var (
		rUpdates   = mtproto.MakeEmptyUpdates()
		idHelper   = mtproto.NewIDListHelper()
		exceptions []*userpb.PeerPeerNotifySettings
	)
	for _, setting := range settings.GetDatas() {
		switch setting.GetPeerType() {
		case mtproto.PEER_USER:
			if category != mtproto.PEER_EMPTY && category != mtproto.PEER_USERS {
				continue
			}
		case mtproto.PEER_CHAT:
			if category != mtproto.PEER_EMPTY && category != mtproto.PEER_CHATS {
				continue
			}
		case mtproto.PEER_CHANNEL:
			// A supergroup or a channel: told apart once it is loaded.
			if category == mtproto.PEER_USERS {
				continue
			}
		default:
			// The settings of a class are no exception.
			continue
		}
		if onlyPeer != nil && (setting.GetPeerType() != onlyPeer.PeerType || setting.GetPeerId() != onlyPeer.PeerId) {
			continue
		}
		if !notifyException(setting.GetSettings(), in.GetCompareSound()) {
			continue
		}
		exceptions = append(exceptions, setting)
		idHelper.PickByPeerUtil(setting.GetPeerType(), setting.GetPeerId())
	}

	// The kind of each channel: megagroup true for a supergroup.
	megagroup := map[int64]bool{}
	var loadErr error
	idHelper.Visit(
		func(userIdList []int64) {
			users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx,
				&userpb.TLUserGetMutableUsers{
					Id: append(userIdList, c.MD.UserId),
				})
			if err != nil {
				loadErr = err
				return
			}
			rUpdates.Users = users.GetUserListByIdList(c.MD.UserId, userIdList...)
		},
		func(chatIdList []int64) {
			chats, err := c.svcCtx.Dao.ChatClient.ChatGetChatListByIdList(c.ctx,
				&chatpb.TLChatGetChatListByIdList{
					IdList: chatIdList,
				})
			if err != nil {
				loadErr = err
				return
			}
			rUpdates.PushChat(chats.GetChatListByIdList(c.MD.UserId, chatIdList...)...)
		},
		func(channelIdList []int64) {
			channels, err := c.svcCtx.Dao.Channel.ChannelGetChannelListByIdList(c.ctx,
				&channelpb.TLChannelGetChannelListByIdList{
					SelfId: c.MD.UserId,
					IdList: channelIdList,
				})
			if err != nil {
				loadErr = err
				return
			}
			for _, chat := range channels.GetChannelListByIdList(c.MD.UserId) {
				megagroup[chat.GetId()] = chat.GetMegagroup()
				if category == mtproto.PEER_EMPTY || (category == mtproto.PEER_CHATS) == chat.GetMegagroup() {
					rUpdates.PushChat(chat)
				}
			}
		})
	if loadErr != nil {
		c.Logger.Errorf("account.getNotifyExceptions - error: %v", loadErr)
		return nil, loadErr
	}

	for _, setting := range exceptions {
		if setting.GetPeerType() == mtproto.PEER_CHANNEL && category != mtproto.PEER_EMPTY {
			// A channel that was not loaded is of neither category.
			if group, ok := megagroup[setting.GetPeerId()]; !ok || group != (category == mtproto.PEER_CHATS) {
				continue
			}
		}
		rUpdates.PushBackUpdate(mtproto.MakeTLUpdateNotifySettings(&mtproto.Update{
			Peer_NOTIFYPEER: mtproto.MakePeerUtil(setting.GetPeerType(), setting.GetPeerId()).ToNotifyPeer(),
			NotifySettings:  setting.GetSettings(),
		}).To_Update())
	}

	return rUpdates, nil
}

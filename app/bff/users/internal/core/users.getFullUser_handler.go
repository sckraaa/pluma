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
	"github.com/teamgram/marmota/pkg/utils"
	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	dialogclient "github.com/teamgram/teamgram-server/app/service/biz/dialog/client"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/premium"

	"github.com/zeromicro/go-zero/core/mr"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// UsersGetFullUser
// users.getFullUser#b60f5918 id:InputUser = users.UserFull;
func (c *UsersCore) UsersGetFullUser(in *mtproto.TLUsersGetFullUser) (*mtproto.Users_UserFull, error) {
	var (
		peerId int64
		id     = mtproto.FromInputUser(c.MD.UserId, in.Id)
		me     *mtproto.ImmutableUser
		user   *mtproto.ImmutableUser
	)

	switch id.PeerType {
	case mtproto.PEER_SELF, mtproto.PEER_USER:
		peerId = id.PeerId
	default:
		err := mtproto.ErrUserIdInvalid
		c.Logger.Errorf("users.getFullUser - error: %v", err)
		return nil, err
	}

	mutableUsers, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      []int64{c.MD.UserId, peerId},
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId, peerId},
	})
	if err != nil {
		c.Logger.Errorf("users.getFullUser - error: %v", err)
		return nil, err
	} else if len(mutableUsers.GetUsers()) == 0 {
		err = mtproto.ErrInternalServerError
		c.Logger.Errorf("users.getFullUser - error: %v", err)
		return nil, err
	}

	me, _ = mutableUsers.GetImmutableUser(c.MD.UserId)
	user, _ = mutableUsers.GetImmutableUser(peerId)

	if me == nil || user == nil {
		err = mtproto.ErrInternalServerError
		c.Logger.Errorf("users.getFullUser - error: %v", err)
		return nil, err
	}

	hasScheduled, err := c.svcCtx.Dao.MsgClient.MsgHasScheduled(c.ctx, &msgpb.TLMsgHasScheduled{
		UserId:   c.MD.UserId,
		PeerType: mtproto.PEER_USER,
		PeerId:   peerId,
	})
	if err != nil {
		c.Logger.Errorf("users.getFullUser - error: %v", err)
		return nil, err
	}

	userFull := mtproto.MakeTLUserFull(&mtproto.UserFull{
		Blocked:                  false,
		PhoneCallsAvailable:      c.MD.UserId != peerId,
		PhoneCallsPrivate:        false,
		CanPinMessage:            true,
		HasScheduled:             mtproto.FromBool(hasScheduled),
		VideoCallsAvailable:      c.MD.UserId != peerId,
		VoiceMessagesForbidden:   false,
		TranslationsDisabled:     false,
		StoriesPinnedAvailable:   false,
		BlockedMyStoriesFrom:     false,
		WallpaperOverridden:      false,
		Id:                       peerId,
		About:                    nil,
		Settings:                 nil,
		PersonalPhoto:            nil,
		ProfilePhoto:             nil,
		FallbackPhoto:            nil,
		NotifySettings:           nil,
		BotInfo:                  nil,
		PinnedMsgId:              nil,
		CommonChatsCount:         0,
		FolderId:                 nil,
		TtlPeriod:                nil,
		ThemeEmoticon:            nil,
		Theme:                    nil,
		PrivateForwardName:       nil,
		BotGroupAdminRights:      nil,
		BotBroadcastAdminRights:  nil,
		PremiumGifts:             nil,
		Wallpaper:                nil,
		Stories_FLAGPEERSTORIES:  nil,
		Stories_FLAGUSERSTORIES:  nil,
		BusinessWorkHours:        nil,
		BusinessLocation:         nil,
		BusinessGreetingMessage:  nil,
		BusinessAwayMessage:      nil,
		BusinessIntro:            nil,
		Birthday:                 nil,
		PersonalChannelId:        nil,
		PersonalChannelMessage:   nil,
		StargiftsCount:           nil,
		StarrefProgram:           nil,
		BotVerification:          nil,
		SendPaidMessagesStars:    nil,
		DisallowedGifts:          nil,
		StarsMyPendingRating:     nil,
		StarsMyPendingRatingDate: nil,
		MainTab:                  user.GetUser().GetMainTab(),
		SavedMusic:               nil,
		Note:                     nil,
	}).To_UserFull()

	// Pluma sells no Premium and nobody gifts it, the admin grants it
	// (ADR 0016): no premium_gifts.

	// What the privacy of the user lets me see (task 034, ADR 0031): the
	// rules of every key come with the user, chat rules resolved for me.
	if user.CheckPrivacyFor(mtproto.ABOUT, me) {
		userFull.About = user.GetUser().GetAbout()
	}
	// The photos (task 051): the personal photo I set for the user, its
	// main photo if its privacy lets me see it, otherwise its public photo,
	// which clients recognise in user.photo by its id
	// (ProfileActivity.java:6829-6836) and the account sees as its own.
	// Whether Telegram also sends fallback_photo to those who see the main
	// photo is not documented; it is not sent to them.
	photoVisible := user.CheckPrivacyFor(mtproto.PROFILE_PHOTO, me)
	if photoVisible {
		userFull.ProfilePhoto = user.GetUser().GetProfilePhoto()
	}
	if (peerId == c.MD.UserId || !photoVisible) && !user.IsBot() {
		userFull.FallbackPhoto = user.FallbackPhoto()
	}
	if personalId := user.ContactRowOf(me).GetPersonalPhotoId(); personalId != 0 && peerId != c.MD.UserId {
		if userFull.PersonalPhoto, err = c.svcCtx.Dao.MediaClient.MediaGetPhoto(c.ctx, &mediapb.TLMediaGetPhoto{
			PhotoId: personalId,
		}); err != nil {
			c.Logger.Errorf("users.getFullUser - error: %v", err)
			return nil, err
		}
	}
	if user.CheckPrivacyFor(mtproto.BIRTHDAY, me) {
		userFull.Birthday = user.Birthday()
	}
	if user.CheckPrivacyFor(mtproto.SAVED_MUSIC, me) {
		userFull.SavedMusic = user.GetUser().GetSavedMusic()
	}
	if peerId != c.MD.UserId {
		userFull.VoiceMessagesForbidden = !user.CheckPrivacyFor(mtproto.VOICE_MESSAGES, me)
		userFull.PhoneCallsPrivate = !user.CheckPrivacyFor(mtproto.PHONE_CALL, me)

		// The user hides read dates from those who cannot see their last
		// seen (globalPrivacySettings.hide_read_marks): Android then shows no
		// read date (ChatActivity, read_dates_private).
		settings, err := c.svcCtx.Dao.UserClient.UserGetGlobalPrivacySettings(c.ctx, &userpb.TLUserGetGlobalPrivacySettings{
			UserId: peerId,
		})
		if err != nil {
			c.Logger.Errorf("users.getFullUser - error: %v", err)
			return nil, err
		}
		userFull.ReadDatesPrivate = settings.GetHideReadMarks() && !user.CheckPrivacyFor(mtproto.STATUS_TIMESTAMP, me)
	}

	// Whether this user lets me write only with Premium (task 014): the
	// client shows it in place of the message field.
	if userFull.ContactRequirePremium, err = premium.RequiredToContact(me, user, func() (bool, error) {
		return dialogclient.HasPrivateChat(c.ctx, c.svcCtx.Dao.DialogClient, peerId, c.MD.UserId)
	}); err != nil {
		c.Logger.Errorf("users.getFullUser - error: %v", err)
		return nil, err
	}

	mr.FinishVoid(
		func() {
			// blocked
			if c.MD.UserId != peerId {
				blocked, _ := c.svcCtx.Dao.UserClient.UserBlockedByUser(
					c.ctx,
					&userpb.TLUserBlockedByUser{
						UserId:     c.MD.UserId,
						PeerUserId: peerId,
					})
				userFull.Blocked = mtproto.FromBool(blocked)
			}
		},
		func() {
			userFull.Settings, _ = c.svcCtx.Dao.UserClient.UserGetPeerSettings(c.ctx, &userpb.TLUserGetPeerSettings{
				UserId:   c.MD.UserId,
				PeerType: mtproto.PEER_USER,
				PeerId:   peerId,
			})
		},
		func() {
			userFull.NotifySettings, _ = c.svcCtx.Dao.UserClient.UserGetNotifySettings(c.ctx, &userpb.TLUserGetNotifySettings{
				UserId:   c.MD.UserId,
				PeerType: mtproto.PEER_USER,
				PeerId:   peerId,
			})
		},
		func() {
			if user.GetUser().GetBot() != nil {
				userFull.PhoneCallsAvailable = false
				userFull.VideoCallsAvailable = false
				userFull.BotInfo, _ = c.svcCtx.Dao.UserClient.UserGetBotInfo(c.ctx, &userpb.TLUserGetBotInfo{
					BotId: peerId,
				})
			}
		},
		func() {
			// TODO: PinnedMsgId:         nil,
			if c.MD.UserId != peerId {
				usersChatIdList, _ := c.svcCtx.Dao.ChatClient.ChatGetUsersChatIdList(c.ctx, &chatpb.TLChatGetUsersChatIdList{
					Id: []int64{c.MD.UserId, peerId},
				})
				if usersChatIdList != nil && len(usersChatIdList.Datas) == 2 {
					commonChats := utils.Int64Intersect(
						usersChatIdList.Datas[0].ChatIdList,
						usersChatIdList.Datas[1].ChatIdList)
					userFull.CommonChatsCount = int32(len(commonChats))
				}
				// TODO: Fetch CommonChannelsCount
			}
		},
		func() {
			if peerId != c.MD.UserId {
				// theme_emoticon
				dialogExt, _ := c.svcCtx.Dao.DialogClient.DialogGetDialogById(c.ctx, &dialog.TLDialogGetDialogById{
					UserId:   c.MD.UserId,
					PeerType: mtproto.PEER_USER,
					PeerId:   peerId,
				})
				if dialogExt != nil {
					// The archived chat says so (userFull.folder_id, task 050);
					// Android moves the dialog by it.
					if f := dialogExt.GetDialog().GetFolderId(); f.GetValue() != 0 {
						userFull.FolderId = f
					}
					userFull.ThemeEmoticon = mtproto.MakeFlagsString(dialogExt.ThemeEmoticon)
					userFull.TtlPeriod = mtproto.MakeFlagsInt32(dialogExt.TtlPeriod)
					if dialogExt.WallpaperId != 0 && c.svcCtx.Dao.WallpaperPlugin != nil {
						userFull.Wallpaper = c.svcCtx.Dao.WallpaperPlugin.GetChatWallpaper(c.ctx, c.MD.UserId, dialogExt.WallpaperId)
						userFull.WallpaperOverridden = true
					}
				}
			}
		})

	// TODO: FolderId:    0,

	// TODO: WallPaper

	// TODO: Stories
	if c.svcCtx.Dao.StoryPlugin != nil {
		userFull.StoriesPinnedAvailable = c.svcCtx.Dao.StoryPlugin.GetStoriesPinnedAvailable(c.ctx, peerId, c.MD.UserId)
		userFull.BlockedMyStoriesFrom = c.svcCtx.Dao.StoryPlugin.GetBlockedMyStoriesFrom(c.ctx, peerId, c.MD.UserId)
		// c.Logger.Debugf("getActiveStories: peerId: %s, userId: %s", user, me)
		if peerId == c.MD.UserId {
			// c.Logger.Debugf("getActiveStories(peerId == c.MD.UserId): peerId: %d, userId: %d", peerId, c.MD.UserId)
			userFull.Stories_FLAGPEERSTORIES = c.svcCtx.Dao.StoryPlugin.GetActiveStories(c.ctx, peerId, c.MD.UserId)
		} else if ok, _ := me.CheckReverseContact(peerId); ok {
			// c.Logger.Debugf("getActiveStories(ok, _ := user.CheckContact(c.MD.UserId)): peerId: %d, userId: %d", peerId, c.MD.UserId)
			userFull.Stories_FLAGPEERSTORIES = c.svcCtx.Dao.StoryPlugin.GetActiveStories(c.ctx, peerId, c.MD.UserId)
		}
	}

	chats := make([]*mtproto.Chat, 0)

	if c.svcCtx.Dao.PersonalChannelPlugin != nil {
		personalChannelId := user.GetUser().GetPersonalChannelId()
		if personalChannelId != 0 {
			userFull.PersonalChannelId = mtproto.MakeFlagsInt64(personalChannelId)
			pChannel, topMessageId := c.svcCtx.Dao.PersonalChannelPlugin.GetPersonalChannel(c.ctx, personalChannelId, c.MD.UserId)
			if pChannel != nil {
				userFull.PersonalChannelMessage = &wrapperspb.Int32Value{Value: topMessageId}
				chats = append(chats, pChannel)
			}
		}
	}

	if c.MD.UserId == 777000 {
		userFull.PhoneCallsAvailable = false
		userFull.PhoneCallsPrivate = false
		userFull.VideoCallsAvailable = false
	}

	unsafeUser := user.ToUnsafeUser(me)
	patchBotUsernameFromImmutable(unsafeUser, user)

	return mtproto.MakeTLUsersUserFull(&mtproto.Users_UserFull{
		FullUser: userFull,
		Chats:    chats,
		Users:    []*mtproto.User{unsafeUser},
	}).To_Users_UserFull(), nil
}

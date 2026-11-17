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
	"time"

	"github.com/teamgram/marmota/pkg/utils"
	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	dialogclient "github.com/teamgram/teamgram-server/app/service/biz/dialog/client"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/env2"
	"github.com/teamgram/teamgram-server/pkg/premium"
	"github.com/teamgram/teamgram-server/pkg/translate"

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

	// Pluma sells no Premium and nobody gifts it, everybody has it
	// (ADR 0041): no premium_gifts.

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
	// My private note on the contact (task 084): "Notes for contacts are
	// only visible to you and appear on the user's profile under their bio"
	// (core.telegram.org/api/contacts); tdesktop takes it from here
	// (data_user.cpp:1114-1120).
	if peerId != c.MD.UserId && user.ContactRowOf(me) != nil && c.svcCtx.Dao.Notes != nil {
		if userFull.Note, err = c.svcCtx.Dao.Notes.Get(c.ctx, c.MD.UserId, peerId); err != nil {
			c.Logger.Errorf("users.getFullUser - error: %v", err)
			return nil, err
		}
	}
	// Whether I hid the translation bar of the chat with the user
	// (messages.togglePeerTranslations, task 101): the clients read it from
	// here (Android TranslateController.java:204-216, tdesktop
	// data/data_user.cpp:992).
	if userFull.TranslationsDisabled, err = c.translationsDisabled(peerId); err != nil {
		c.Logger.Errorf("users.getFullUser - error: %v", err)
		return nil, err
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
				c.fillBotInfo(userFull, peerId)
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
					// The field clients of this layer read
					// (core.telegram.org/constructor/userFull).
					if dialogExt.ThemeEmoticon != "" {
						userFull.Theme = mtproto.MakeTLChatTheme(&mtproto.ChatTheme{Emoticon: dialogExt.ThemeEmoticon}).To_ChatTheme()
					}
					userFull.TtlPeriod = mtproto.MakeFlagsInt32(dialogExt.TtlPeriod)
				}
				if c.svcCtx.Dao.WallpaperPlugin != nil {
					w, overridden, err := c.svcCtx.Dao.WallpaperPlugin.GetChatWallpaper(c.ctx, c.MD.UserId, peerId)
					if err != nil {
						c.Logger.Errorf("users.getFullUser - error: %v", err)
					} else if w != nil {
						userFull.Wallpaper = w
						userFull.WallpaperOverridden = overridden
					}
				}
			}
		})

	// TODO: FolderId:    0,

	// The Business part of the profile (task 105): work hours, location
	// and intro, which every client shows of any user (tdesktop
	// data/data_user.cpp:1057-1061), and the automatic replies, which the
	// clients read from their own userFull only (:1066-1071). Bots have
	// none.
	if !user.IsBot() && c.svcCtx.Dao.Business != nil {
		if err = c.fillBusiness(userFull, peerId, peerId == c.MD.UserId); err != nil {
			c.Logger.Errorf("users.getFullUser - error: %v", err)
			return nil, err
		}
	}

	// The bar of the connected bot of the caller in the chat (task 105):
	// iOS takes the peerSettings of a user from userFull.settings whenever
	// it loads the full user (TelegramCore UpdateCachedPeerData.swift:377),
	// so they carry business_bot_* as messages.getPeerSettings does, and
	// the bot goes in users.
	var businessBot *mtproto.User
	if !user.IsBot() && !c.MD.IsBot && peerId != c.MD.UserId && c.svcCtx.Dao.Business != nil {
		if businessBot, err = c.fillBusinessBot(userFull, me, peerId); err != nil {
			c.Logger.Errorf("users.getFullUser - error: %v", err)
			return nil, err
		}
	}

	// The stories of the user (task 084): those in the feeds that the
	// viewer may open, and whether the profile has any for it; the plugin
	// applies the rules of each story.
	if c.svcCtx.Dao.StoryPlugin != nil && !user.IsBot() {
		userFull.StoriesPinnedAvailable = c.svcCtx.Dao.StoryPlugin.GetStoriesPinnedAvailable(c.ctx, peerId, c.MD.UserId)
		userFull.BlockedMyStoriesFrom = c.svcCtx.Dao.StoryPlugin.GetBlockedMyStoriesFrom(c.ctx, peerId, c.MD.UserId)
		userFull.Stories_FLAGPEERSTORIES = c.svcCtx.Dao.StoryPlugin.GetActiveStories(c.ctx, peerId, c.MD.UserId)
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

	users := []*mtproto.User{unsafeUser}
	if businessBot != nil {
		users = append(users, businessBot)
	}

	return mtproto.MakeTLUsersUserFull(&mtproto.Users_UserFull{
		FullUser: userFull,
		Chats:    chats,
		Users:    users,
	}).To_Users_UserFull(), nil
}

// fillBusinessBot sets business_bot_* of userFull.settings when the
// connected bot of the caller serves its chat with userId, and returns the
// bot as me sees it; nil when no bot serves the chat.
func (c *UsersCore) fillBusinessBot(userFull *mtproto.UserFull, me *mtproto.ImmutableUser, userId int64) (*mtproto.User, error) {
	bar, err := c.svcCtx.Dao.Business.PeerBar(c.ctx, c.svcCtx.Dao.UserClient, env2.TDotMe, c.MD.UserId, userId)
	if err != nil || bar == nil {
		return nil, err
	}
	bot, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: bar.BotID})
	if err != nil {
		return nil, err
	}
	userFull.Settings = bar.Apply(userFull.Settings)
	return bot.ToUnsafeUser(me), nil
}

// translationsDisabled tells whether the caller hid the translation bar
// of its chat with userId; its own id is Saved Messages, which
// togglePeerTranslations keeps under the same peer.
func (c *UsersCore) translationsDisabled(userId int64) (bool, error) {
	peer := translate.TranslationPeer{Type: mtproto.PEER_USER, Id: userId}
	disabled, err := c.svcCtx.Translations.TranslationsDisabled(c.ctx, c.MD.UserId, peer)
	if err != nil {
		return false, err
	}
	return disabled[peer], nil
}

// fillBotInfo sets userFull.bot_info, what the chat with the bot shows as the
// user sees it (core.telegram.org/api/bots/commands), and the about of the
// user's language. A failure leaves the profile without it: the client
// fetches the full user again when bot_info_version changes.
func (c *UsersCore) fillBotInfo(userFull *mtproto.UserFull, botId int64) {
	v := botstore.Viewer{UserId: c.MD.UserId, PeerType: mtproto.PEER_USER, PeerId: c.MD.UserId, Lang: c.MD.LangCode}
	info, err := c.svcCtx.Dao.Bots.BotInfo(c.ctx, botId, v)
	if err != nil {
		c.Logger.Errorf("users.getFullUser - bot info of %d: %v", botId, err)
		return
	}
	userFull.BotInfo = info
	texts, err := c.svcCtx.Dao.Bots.LangTexts(c.ctx, botId, c.MD.LangCode)
	if err != nil {
		c.Logger.Errorf("users.getFullUser - bot texts of %d: %v", botId, err)
		return
	}
	if texts.About != nil {
		userFull.About = mtproto.MakeFlagsString(*texts.About)
	}
}

// fillBusiness sets the Business part of the profile of userId in
// userFull, with the document of the sticker of the intro, and, for self,
// the caller's own profile, its automatic replies.
func (c *UsersCore) fillBusiness(userFull *mtproto.UserFull, userId int64, self bool) error {
	p, err := c.svcCtx.Dao.Business.Profile(c.ctx, userId)
	if err != nil {
		return err
	}
	if self {
		replies, err := c.svcCtx.Dao.Business.AutoReplies(c.ctx, userId)
		if err != nil {
			return err
		}
		setAutoReplies(userFull, replies)
	}
	var sticker *mtproto.Document
	if p.Intro != nil && p.Intro.StickerID != 0 {
		if sticker, err = c.svcCtx.Dao.MediaClient.MediaGetDocument(c.ctx, &mediapb.TLMediaGetDocument{Id: p.Intro.StickerID}); err != nil {
			return err
		}
	}
	setBusinessProfile(userFull, p, sticker, time.Now(), func(err error) {
		c.Logger.Errorf("users.getFullUser - the work hours of %d: %v", userId, err)
	})
	return nil
}

// setBusinessProfile sets p in userFull at now: business_work_hours with
// open_now, which only the server sets (core.telegram.org/api/business),
// business_location and business_intro with sticker, the document of its
// sticker. A sticker whose document is gone leaves the intro without one.
// Work hours whose time zone the server no longer knows (it was known when
// they were set) are left out and reported to fail: open_now cannot be told,
// and the rest of the profile is still answered.
func setBusinessProfile(userFull *mtproto.UserFull, p business.Profile, sticker *mtproto.Document, now time.Time, fail func(error)) {
	if p.Hours != nil {
		if h, err := p.Hours.ToTL(now); err != nil {
			fail(err)
		} else {
			userFull.BusinessWorkHours = h
		}
	}
	if p.Location != nil {
		userFull.BusinessLocation = p.Location.ToTL()
	}
	if p.Intro != nil {
		if sticker.GetPredicateName() != mtproto.Predicate_document {
			sticker = nil
		}
		userFull.BusinessIntro = p.Intro.ToTL(sticker)
	}
}

// setAutoReplies sets the automatic replies of the owner of userFull:
// business_greeting_message and business_away_message. Only the owner gets
// them: they are its settings, which the clients read only from their own
// userFull (tdesktop data/data_user.cpp:1066-1071).
func setAutoReplies(userFull *mtproto.UserFull, replies []*business.AutoReply) {
	if a := business.AutoReplyOf(replies, business.KindGreeting); a != nil {
		userFull.BusinessGreetingMessage = a.GreetingTL()
	}
	if a := business.AutoReplyOf(replies, business.KindAway); a != nil {
		userFull.BusinessAwayMessage = a.AwayTL()
	}
}

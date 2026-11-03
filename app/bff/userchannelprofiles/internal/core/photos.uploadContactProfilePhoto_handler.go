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
	"math/rand"
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
)

// PhotosUploadContactProfilePhoto
// photos.uploadContactProfilePhoto#e14c4a71 flags:# suggest:flags.3?true save:flags.4?true user_id:InputUser file:flags.0?InputFile video:flags.1?InputFile video_start_ts:flags.2?double video_emoji_markup:flags.5?VideoSize = photos.Photo;
//
// save sets the personal photo the caller sees for its contact, seen by the
// caller alone; save without a file removes it, as tdesktop, Android and
// iOS send it (api_peer_photo.cpp:382-395, ContactAddActivity.java:429-436,
// PeerPhotoUpdater.swift:441-443). suggest sends the contact the photo in a
// service message (messageActionSuggestProfilePhoto), which the contact may
// make its main photo. iOS sends both together (PeerPhotoUpdater.swift:
// 212-226). Neither is NEED_ACTION_MISSING; a user who is not the caller's
// contact is CONTACT_MISSING (core.telegram.org/method/
// photos.uploadContactProfilePhoto). It answers the photo and the contact as
// the caller sees it now (task 051).
func (c *UserChannelProfilesCore) PhotosUploadContactProfilePhoto(in *mtproto.TLPhotosUploadContactProfilePhoto) (*mtproto.Photos_Photo, error) {
	if !in.GetSave() && !in.GetSuggest() {
		err := errNeedActionMissing
		c.Logger.Errorf("photos.uploadContactProfilePhoto - error: %v", err)
		return nil, err
	}
	peer := mtproto.FromInputUser(c.MD.UserId, in.GetUserId())
	if peer.PeerType != mtproto.PEER_USER || peer.PeerId == c.MD.UserId {
		err := mtproto.ErrUserIdInvalid
		c.Logger.Errorf("photos.uploadContactProfilePhoto - error: %v", err)
		return nil, err
	}
	hasFile := in.GetFile() != nil || in.GetVideo() != nil || in.GetVideoEmojiMarkup() != nil
	if in.GetSuggest() && !hasFile {
		err := mtproto.ErrPhotoFileMissing
		c.Logger.Errorf("photos.uploadContactProfilePhoto - error: %v", err)
		return nil, err
	}

	ids := []int64{c.MD.UserId, peer.PeerId}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      ids,
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		c.Logger.Errorf("photos.uploadContactProfilePhoto - error: %v", err)
		return nil, err
	}
	me, ok := users.GetImmutableUser(c.MD.UserId)
	contact, ok2 := users.GetImmutableUser(peer.PeerId)
	if !ok || !ok2 || contact.Deleted() {
		err = mtproto.ErrUserIdInvalid
		c.Logger.Errorf("photos.uploadContactProfilePhoto - error: %v", err)
		return nil, err
	}
	// A suggestion goes to contacts only, as a personal photo does: the
	// clients offer both on a contact's page only (ContactAddActivity.java,
	// api_peer_photo.cpp). Whether Telegram also refuses a suggestion to a
	// non-contact is not documented; this is the stricter reading. biz
	// checks the contact again under the lock of the change.
	if contact.ContactRowOf(me) == nil {
		err = errContactMissing
		c.Logger.Errorf("photos.uploadContactProfilePhoto - error: %v", err)
		return nil, err
	}

	var photo *mtproto.Photo
	if hasFile {
		// Not one of anybody's photos: neither the caller's album nor the
		// contact's until the contact accepts a suggestion.
		if photo, err = c.svcCtx.Dao.MediaClient.MediaUploadProfilePhotoFile(c.ctx, &mediapb.TLMediaUploadProfilePhotoFile{
			OwnerId:          c.MD.PermAuthKeyId,
			File:             in.GetFile(),
			Video:            in.GetVideo(),
			VideoStartTs:     in.GetVideoStartTs(),
			VideoEmojiMarkup: in.GetVideoEmojiMarkup(),
		}); err != nil {
			c.Logger.Errorf("photos.uploadContactProfilePhoto - error: %v", err)
			return nil, err
		}
	}

	// The caller's own state first, then the message to the contact.
	seen := contact.ToUnsafeUser(me)
	if in.GetSave() {
		// biz pushes updateUser with seq and the contact as the caller sees
		// it to every device of the caller, this one included, in the
		// transaction of the change (ADR 0020).
		if seen, err = c.svcCtx.Dao.UserClient.UserSetContactPersonalPhoto(c.ctx, &userpb.TLUserSetContactPersonalPhoto{
			UserId:    c.MD.UserId,
			ContactId: peer.PeerId,
			PhotoId:   photo.GetId(),
			HasVideo:  mtproto.ToBool(len(photo.GetVideoSizes()) > 0),
		}); err != nil {
			c.Logger.Errorf("photos.uploadContactProfilePhoto - error: %v", err)
			return nil, err
		}
	}
	if in.GetSuggest() {
		if err = c.suggestPhoto(peer.PeerId, photo); err != nil {
			c.Logger.Errorf("photos.uploadContactProfilePhoto - error: %v", err)
			return nil, err
		}
	}

	if photo == nil {
		photo = mtproto.MakeTLPhotoEmpty(nil).To_Photo()
	}
	return mtproto.MakeTLPhotosPhoto(&mtproto.Photos_Photo{
		Photo: photo,
		Users: []*mtproto.User{seen},
	}).To_Photos_Photo(), nil
}

// suggestPhoto suggests photo to the user toId: the record that lets toId
// make it its main photo first, then the service message from the caller in
// their private chat, which toId accepts with photos.updateProfilePhoto of
// the photo (tdesktop history_view_userpic_suggestion.cpp:51-56, Android
// ChatActivity.java:36994-36999).
//
// The method answers photos.Photo, not Updates, so the caller's device gets
// the message as a push like its other devices: no authorization is left
// out of it.
func (c *UserChannelProfilesCore) suggestPhoto(toId int64, photo *mtproto.Photo) error {
	if _, err := c.svcCtx.Dao.UserClient.UserAddSuggestedPhoto(c.ctx, &userpb.TLUserAddSuggestedPhoto{
		UserId:     toId,
		FromUserId: c.MD.UserId,
		PhotoId:    photo.GetId(),
	}); err != nil {
		return err
	}
	_, err := c.svcCtx.Dao.MsgClient.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: 0,
		PeerType:  mtproto.PEER_USER,
		PeerId:    toId,
		Message: []*msgpb.OutboxMessage{
			msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
				NoWebpage: true,
				RandomId:  rand.Int63(), //nolint:gosec // random_id of a service message, not a secret
				Message: mtproto.MakeTLMessageService(&mtproto.Message{
					Out:    true,
					FromId: mtproto.MakePeerUser(c.MD.UserId),
					PeerId: mtproto.MakePeerUser(toId),
					Date:   int32(time.Now().Unix()),
					Action: mtproto.MakeTLMessageActionSuggestProfilePhoto(&mtproto.MessageAction{
						Photo: photo,
					}).To_MessageAction(),
				}).To_Message(),
			}).To_OutboxMessage(),
		},
	})
	return err
}

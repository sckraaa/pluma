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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
)

// PhotosUploadProfilePhoto
// photos.uploadProfilePhoto#388a3b5 flags:# fallback:flags.3?true bot:flags.5?InputUser file:flags.0?InputFile video:flags.1?InputFile video_start_ts:flags.2?double video_emoji_markup:flags.4?VideoSize = photos.Photo;
//
// With fallback the photo becomes the public photo: shown to the users the
// privacy of the profile photo hides the main photo from, and not one of
// the account's photos (task 051). tdesktop, Android, iOS and Web K set it
// so from the privacy settings (api_peer_photo.cpp:474-481,
// PrivacyControlActivity.java:257-258, PeerPhotoUpdater.swift:195-201).
func (c *UserChannelProfilesCore) PhotosUploadProfilePhoto(in *mtproto.TLPhotosUploadProfilePhoto) (*mtproto.Photos_Photo, error) {
	if err := checkBot(in.GetBot(), in.GetFallback()); err != nil {
		c.Logger.Errorf("photos.uploadProfilePhoto - error: %v", err)
		return nil, err
	}

	photo, err := c.svcCtx.Dao.MediaClient.MediaUploadProfilePhotoFile(c.ctx, &mediapb.TLMediaUploadProfilePhotoFile{
		OwnerId:          c.MD.PermAuthKeyId,
		File:             in.GetFile(),
		Video:            in.GetVideo(),
		VideoStartTs:     in.GetVideoStartTs(),
		VideoEmojiMarkup: in.GetVideoEmojiMarkup(),
	})
	if err != nil {
		c.Logger.Errorf("photos.uploadProfilePhoto - error: %v", err)
		return nil, err
	}

	// biz pushes updateUser with seq and the account as it sees itself to
	// every device of the account, this one included, in the transaction
	// of the change (ADR 0020).
	// TODO: ALBUM_PHOTOS_TOO_MANY
	_, err = c.svcCtx.Dao.UserClient.UserUpdateProfilePhoto(c.ctx, &userpb.TLUserUpdateProfilePhoto{
		UserId:   c.MD.UserId,
		Id:       photo.GetId(),
		Uploaded: true,
		Fallback: in.GetFallback(),
	})
	if err != nil {
		c.Logger.Errorf("photos.uploadProfilePhoto - error: %v", err)
		return nil, err
	}

	users, err := c.selfUsers()
	if err != nil {
		c.Logger.Errorf("photos.uploadProfilePhoto - error: %v", err)
		return nil, err
	}

	return mtproto.MakeTLPhotosPhoto(&mtproto.Photos_Photo{
		Photo: photo,
		Users: users,
	}).To_Photos_Photo(), nil
}

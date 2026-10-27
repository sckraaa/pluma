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

// PhotosUpdateProfilePhoto
// photos.updateProfilePhoto#9e82039 flags:# fallback:flags.0?true bot:flags.1?InputUser id:InputPhoto = photos.Photo;
func (c *UserChannelProfilesCore) PhotosUpdateProfilePhoto(in *mtproto.TLPhotosUpdateProfilePhoto) (*mtproto.Photos_Photo, error) {
	// inputPhoto makes a photo main: the photo with its access_hash, as in
	// Telegram, otherwise PHOTO_ID_INVALID. inputPhotoEmpty removes the
	// main photo.
	var (
		photo   *mtproto.Photo
		photoId int64
	)
	if id := in.GetId(); id.GetPredicateName() == mtproto.Predicate_inputPhoto {
		p, err := c.svcCtx.Dao.MediaClient.MediaGetPhoto(c.ctx, &mediapb.TLMediaGetPhoto{
			PhotoId: id.GetId(),
		})
		if err != nil {
			c.Logger.Errorf("photos.updateProfilePhoto - error: %v", err)
			return nil, err
		}
		if p.GetPredicateName() != mtproto.Predicate_photo || p.GetAccessHash() != id.GetAccessHash() {
			return nil, mtproto.ErrPhotoIdInvalid
		}
		photo, photoId = p, id.GetId()
	}

	// biz pushes updateUser with seq and the account as it sees itself to
	// every device of the account, this one included, and to the users who
	// see the account, in the transaction of the change (ADR 0020, 0021).
	// TODO: ALBUM_PHOTOS_TOO_MANY
	updatedPhotoId, err := c.svcCtx.Dao.UserClient.UserUpdateProfilePhoto(c.ctx, &userpb.TLUserUpdateProfilePhoto{
		UserId: c.MD.UserId,
		Id:     photoId,
	})
	if err != nil {
		c.Logger.Errorf("photos.updateProfilePhoto - error: %v", err)
		return nil, err
	}

	// The removal of the main photo makes the latest of the others main.
	if updatedPhotoId.V != photo.GetId() {
		photo = nil
		if updatedPhotoId.V != 0 {
			photo, err = c.svcCtx.Dao.MediaClient.MediaGetPhoto(c.ctx, &mediapb.TLMediaGetPhoto{
				PhotoId: updatedPhotoId.V,
			})
			if err != nil {
				c.Logger.Errorf("photos.updateProfilePhoto - error: %v", err)
				return nil, err
			}
		}
	}

	if photo == nil {
		photo = mtproto.MakeTLPhotoEmpty(nil).To_Photo()
	}

	return mtproto.MakeTLPhotosPhoto(&mtproto.Photos_Photo{
		Photo: photo,
		Users: []*mtproto.User{},
	}).To_Photos_Photo(), nil
}

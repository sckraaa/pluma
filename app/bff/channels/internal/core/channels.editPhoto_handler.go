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
	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
)

// ChannelsEditPhoto
// channels.editPhoto#f12e57c9 channel:InputChannel photo:InputChatPhoto = Updates;
//
// The channel's photo (task 044, biz channel.editPhoto): an uploaded one is
// stored as a basic group's is (media.uploadProfilePhotoFile, with a video
// when given), a photo already stored is taken by id, none removes it.
func (c *ChannelsCore) ChannelsEditPhoto(in *mtproto.TLChannelsEditPhoto) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	var photoId int64
	p := in.GetPhoto()
	switch p.GetPredicateName() {
	case mtproto.Predicate_inputChatPhotoEmpty:
	case mtproto.Predicate_inputChatUploadedPhoto:
		photo, err := c.svcCtx.Media.MediaUploadProfilePhotoFile(c.ctx, &mediapb.TLMediaUploadProfilePhotoFile{
			OwnerId:      c.MD.PermAuthKeyId,
			File:         p.GetFile(),
			Video:        p.GetVideo(),
			VideoStartTs: p.GetVideoStartTs(),
		})
		if err != nil {
			c.Logger.Errorf("channels.editPhoto - error: %v", err)
			return nil, err
		}
		photoId = photo.GetId()
	case mtproto.Predicate_inputChatPhoto:
		if id := p.GetId(); id.GetPredicateName() == mtproto.Predicate_inputPhoto {
			photoId = id.GetId()
		}
	default:
		return nil, mtproto.ErrPhotoInvalid
	}
	r, err := c.svcCtx.Channel.ChannelEditPhoto(c.ctx, &channelpb.TLChannelEditPhoto{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
		PhotoId:       photoId,
	})
	if err != nil {
		return nil, err
	}
	return r, c.addUsers(r)
}

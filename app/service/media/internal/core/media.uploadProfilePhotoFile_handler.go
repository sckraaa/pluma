/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// MediaUploadProfilePhotoFile
// media.uploadProfilePhotoFile flags:# owner_id:long file:flags.0?InputFile video:flags.1?InputFile video_start_ts:flags.2?double = Photo;
func (c *MediaCore) MediaUploadProfilePhotoFile(in *media.TLMediaUploadProfilePhotoFile) (*mtproto.Photo, error) {
	if in.GetFile() == nil && in.GetVideo() == nil {
		c.Logger.Errorf("media.uploadProfilePhotoFile - error: file is nil")
		return nil, mtproto.ErrMediaInvalid
	}

	photo, err := c.svcCtx.Dao.DfsClient.DfsUploadProfilePhotoFileV2(c.ctx, &dfs.TLDfsUploadProfilePhotoFileV2{
		Creator:      in.OwnerId,
		File:         in.GetFile(),
		Video:        in.GetVideo(),
		VideoStartTs: in.GetVideoStartTs(),
	})
	if err != nil {
		c.Logger.Error("media.uploadProfilePhotoFile - error: %v", err.Error())
		return nil, err
	}

	hasVideo := len(photo.GetVideoSizes()) > 0

	if err = c.svcCtx.Dao.SavePhotoSizeV2(c.ctx, photo.GetId(), photo.GetSizes()); err != nil {
		c.Logger.Error("media.uploadProfilePhotoFile - error: %v", err.Error())
		return nil, err
	}
	// A photo not saved whole cannot be shown: media would answer it
	// without its video or as photoEmpty (task 051).
	if hasVideo {
		if err = c.svcCtx.Dao.SaveVideoSizeV2(c.ctx, photo.GetId(), photo.GetVideoSizes()); err != nil {
			c.Logger.Errorf("media.uploadProfilePhotoFile - error: %v", err)
			return nil, err
		}
	}

	if err = c.svcCtx.SavePhotoV2(c.ctx,
		photo.GetId(),
		photo.GetAccessHash(),
		photo.GetHasStickers(),
		hasVideo,
		in.GetFile().GetName()); err != nil {
		c.Logger.Errorf("media.uploadProfilePhotoFile - error: %v", err)
		return nil, err
	}

	return photo, nil
}

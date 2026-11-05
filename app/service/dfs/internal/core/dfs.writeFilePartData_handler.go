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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/model"
)

// DfsWriteFilePartData
// dfs.writeFilePartData flags:# creator:long file_id:long bytes:bytes big:flags.0?true file_total_parts:flags.1?int = Bool;
func (c *DfsCore) DfsWriteFilePartData(in *dfs.TLDfsWriteFilePartData) (*mtproto.Bool, error) {
	var (
		err error
	)

	// core.telegram.org/api/files (task 057).
	if err = model.CheckFilePartData(in.GetBytes()); err == nil {
		err = model.CheckFilePart(in.GetFilePart())
	}
	// A total of -1: a streamed upload whose size is not known yet (Android
	// FileUploadOperation: file_total_parts = -1 while estimatedSize != 0);
	// the part that knows sends it. Missing or extra parts are found when
	// the file is opened.
	if total := in.GetFileTotalParts().GetValue(); err == nil && total > 0 {
		err = model.CheckFileParts(total)
	}
	if err != nil {
		c.Logger.Errorf("dfs.writeFilePartData - part %d: %v", in.GetFilePart(), err)
		return nil, err
	}

	err = c.svcCtx.Dao.WriteFilePartData(c.ctx, in.Creator, in.FileId, in.FilePart, in.Bytes)
	if err != nil {
		c.Logger.Errorf("dfs.writeFilePartData - error: %v", err)
		return nil, err
	}

	if in.FilePart == 0 {
		err = c.svcCtx.Dao.SetFileInfo(c.ctx, &model.DfsFileInfo{
			Creator:           in.Creator,
			FileId:            in.FileId,
			Big:               in.Big,
			FileName:          "",
			FileTotalParts:    int(in.GetFileTotalParts().GetValue()),
			FirstFilePartSize: len(in.Bytes),
			FilePartSize:      0,
			LastFilePartSize:  0,
			MimeType:          "",
			Mtime:             time.Now().Unix(),
		})

		// TODO(@benqi): error
		if err != nil {
			c.Logger.Errorf("dfs.writeFilePartData - error: %v", err)
			return nil, mtproto.ErrFilePartInvalid
		}
	} else if in.FilePart == 1 {
		err = c.svcCtx.Dao.SetFileInfo(c.ctx, &model.DfsFileInfo{
			Creator:      in.Creator,
			FileId:       in.FileId,
			FilePartSize: len(in.Bytes),
		})

		// TODO(@benqi): error
		if err != nil {
			c.Logger.Errorf("dfs.writeFilePartData - error: %v", err)
			return nil, mtproto.ErrFilePartInvalid
		}
	}

	if in.GetFileTotalParts() != nil {
		if in.GetFileTotalParts().GetValue() == in.FilePart+1 {
			err = c.svcCtx.Dao.SetFileInfo(
				c.ctx,
				&model.DfsFileInfo{
					Creator:          in.Creator,
					FileId:           in.FileId,
					LastFilePartSize: len(in.Bytes),
				})

			// TODO(@benqi): error
			if err != nil {
				c.Logger.Errorf("dfs.writeFilePartData - error: %v", err)
				return nil, mtproto.ErrFilePartInvalid
			}
		}
	}

	return mtproto.BoolTrue, nil
}

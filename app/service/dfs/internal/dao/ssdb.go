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

package dao

import (
	"context"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	_fileInfoKeyPrefix = "file_info_%d_%d"
)

func getFileInfoKey(ownerId, fileId int64) string {
	return fmt.Sprintf(_fileInfoKeyPrefix, ownerId, fileId)
}

// WriteFilePartData stores a part of an upload on dfs's disk (task 057).
func (d *Dao) WriteFilePartData(ctx context.Context, ownerId, fileId int64, filePart int32, bytes []byte) error {
	if err := d.parts.write(ownerId, fileId, filePart, bytes); err != nil {
		logx.WithContext(ctx).Errorf("dfs: write part %d of %d_%d: %v", filePart, ownerId, fileId, err)
		return err
	}
	// A part sent again may change the layout OpenFile verified.
	if _, err := d.ssdb.HdelCtx(ctx, getFileInfoKey(ownerId, fileId), "verified"); err != nil {
		logx.WithContext(ctx).Errorf("dfs: unverify %d_%d: %v", ownerId, fileId, err)
		return err
	}
	return nil
}

// DropFile removes the parts of an uploaded file and its info, once the
// file is stored for good: otherwise they stay for partsTTL.
func (d *Dao) DropFile(ctx context.Context, ownerId, fileId int64) error {
	if err := d.parts.drop(ownerId, fileId); err != nil {
		return err
	}
	_, err := d.ssdb.DelCtx(ctx, getFileInfoKey(ownerId, fileId))
	return err
}

// OpenFile opens an upload for reading. parts is the number of parts the
// client names (InputFile.parts): checked against the parts on disk, which
// are then recorded as verified. 0 takes the upload as it is: as verified,
// without looking at its parts again, else the parts there are.
func (d *Dao) OpenFile(ctx context.Context, ownerId, fileId int64, parts int32) (*PartsReader, error) {
	if parts < 0 || parts > model.MaxFileParts {
		return nil, mtproto.ErrFilePartsInvalid
	}
	fileInfo, err := d.GetFileInfo(ctx, ownerId, fileId)
	if err != nil {
		return nil, err
	}
	if parts == 0 && fileInfo.Verified {
		return newPartsReader(d.parts, fileInfo), nil
	}
	if parts > 0 {
		if fileInfo.Big && fileInfo.FileTotalParts > 0 && fileInfo.FileTotalParts != int(parts) {
			return nil, mtproto.ErrFilePartsInvalid
		}
		fileInfo.FileTotalParts = int(parts)
	}
	r, err := d.parts.openParts(fileInfo)
	if err != nil {
		return nil, err
	}
	if parts > 0 {
		if err := d.SetFileInfo(ctx, fileInfo); err != nil {
			logx.WithContext(ctx).Errorf("dfs: record the parts of %d_%d: %v", ownerId, fileId, err)
		}
	}
	return r, nil
}

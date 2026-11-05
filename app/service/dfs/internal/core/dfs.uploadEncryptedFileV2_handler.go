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
	"fmt"
	"math/rand"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/dao"
)

// DfsUploadEncryptedFileV2
// dfs.uploadEncryptedFileV2 creator:long file:InputEncryptedFile = EncryptedFile;
func (c *DfsCore) DfsUploadEncryptedFileV2(in *dfs.TLDfsUploadEncryptedFileV2) (*mtproto.EncryptedFile, error) {
	var (
		file            = in.GetFile()
		creatorId       = in.GetCreator()
		encryptedFileId int64
		accessHash      = int64(mtproto.CRC32_storage_filePartial)<<32 | int64(rand.Uint32())
	)

	encryptedFileId, err := c.svcCtx.Dao.IDGenClient2.NextId(c.ctx)
	if err != nil {
		return nil, err
	}

	r, err := c.svcCtx.Dao.OpenFile(c.ctx, creatorId, file.Id, file.GetParts())
	if err != nil {
		c.Logger.Errorf("dfs.uploadEncryptedFile - error: %v", err)
		return nil, err
	}
	fileInfo := r.DfsFileInfo
	c.svcCtx.Dao.SetCacheFileInfo(c.ctx, encryptedFileId, fileInfo)
	path := fmt.Sprintf("%d.dat", encryptedFileId)
	c.svcCtx.Dao.PersistParts(c.ctx, dao.StoreEncrypted, path, encryptedFileId, r)

	encryptedFile := mtproto.MakeTLEncryptedFile(&mtproto.EncryptedFile{
		Id:             encryptedFileId,
		AccessHash:     accessHash,
		Size2_INT32:    int32(fileInfo.GetFileSize()),
		Size2_INT64:    fileInfo.GetFileSize(),
		DcId:           1,
		KeyFingerprint: 0,
	}).To_EncryptedFile()

	return encryptedFile, nil
}

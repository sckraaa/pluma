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

package webpage

import (
	"context"
	"crypto/md5" //nolint:gosec // MTProto's md5_checksum of an uploaded file, a check of the upload, not security
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	dfs_client "github.com/teamgram/teamgram-server/app/service/dfs/client"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// RPCPhotos stores pictures as a client stores the photo it sends: the
// parts to dfs, then media.uploadPhotoFile, which makes the sizes
// (m, x, y) and keeps the photo for upload.getFile.
type RPCPhotos struct {
	Dfs   dfs_client.DfsClient
	Media media_client.MediaClient
}

const photoPart = 512 << 10

// UploadPhoto stores jpeg as a photo of no user.
func (p RPCPhotos) UploadPhoto(ctx context.Context, jpeg []byte) (*mtproto.Photo, error) {
	owner, err := newID()
	if err != nil {
		return nil, err
	}
	fileID, err := newID()
	if err != nil {
		return nil, err
	}
	parts := int32(0)
	for off := 0; off < len(jpeg); off += photoPart {
		end := min(off+photoPart, len(jpeg))
		if _, err := p.Dfs.DfsWriteFilePartData(ctx, &dfs.TLDfsWriteFilePartData{
			Creator:  owner,
			FileId:   fileID,
			FilePart: parts,
			Bytes:    jpeg[off:end],
		}); err != nil {
			return nil, fmt.Errorf("dfs: part %d: %w", parts, err)
		}
		parts++
	}
	sum := md5.Sum(jpeg) //nolint:gosec // see the import
	photo, err := p.Media.MediaUploadPhotoFile(ctx, &media.TLMediaUploadPhotoFile{
		OwnerId: owner,
		File: mtproto.MakeTLInputFile(&mtproto.InputFile{
			Id_INT64:    fileID,
			Parts:       parts,
			Name:        "preview.jpg",
			Md5Checksum: hex.EncodeToString(sum[:]),
		}).To_InputFile(),
	})
	if err != nil {
		return nil, fmt.Errorf("media: %w", err)
	}
	if photo.GetPredicateName() != mtproto.Predicate_photo || photo.GetId() == 0 {
		return nil, errors.New("media: no photo made of the picture")
	}
	return photo, nil
}

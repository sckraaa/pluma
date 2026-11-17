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

package webfetch

import (
	"context"
	"crypto/md5" //nolint:gosec // MTProto's md5_checksum of an uploaded file, a check of the upload, not security
	"crypto/rand"
	"encoding/binary"
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
	return p.UploadPhotoOf(ctx, owner, jpeg, "preview.jpg")
}

// UploadPhotoOf stores jpeg as a photo of owner, the account the files are
// written under in dfs (its creator), named name.
func (p RPCPhotos) UploadPhotoOf(ctx context.Context, owner int64, jpeg []byte, name string) (*mtproto.Photo, error) {
	file, err := p.writeParts(ctx, owner, jpeg, name)
	if err != nil {
		return nil, err
	}
	photo, err := p.Media.MediaUploadPhotoFile(ctx, &media.TLMediaUploadPhotoFile{
		OwnerId: owner,
		File:    file,
	})
	if err != nil {
		return nil, fmt.Errorf("media: %w", err)
	}
	if photo.GetPredicateName() != mtproto.Predicate_photo || photo.GetId() == 0 {
		return nil, errors.New("media: no photo made of the picture")
	}
	return photo, nil
}

// animationName is the file name of an imported animation (ADR 0062).
const animationName = "animation.mp4"

// UploadAnimation stores mp4, a silent looping video, as a GIF document of
// owner: the parts to dfs, then media.uploadedDocumentMedia with video/mp4,
// documentAttributeAnimated and documentAttributeVideo. That is what makes
// a document a GIF for the clients: Android takes video/mp4 with the
// animated attribute and both sides at most MaxSide as one
// (MessageObject.isNewGifDocument, MessageObject.java:7215-7233), so a
// larger video is refused here. The mp4 branch of the media service makes
// its thumbnail of the first frame (media.uploadedDocumentMedia_handler.go,
// dfs.uploadMp4DocumentMedia). w, h and duration (seconds) describe the
// video; clients size the cell by w and h before anything is loaded.
func (p RPCPhotos) UploadAnimation(ctx context.Context, owner int64, mp4 []byte, w, h int32, duration float64) (*mtproto.Document, error) {
	if w <= 0 || h <= 0 || w > MaxSide || h > MaxSide || duration < 0 {
		return nil, fmt.Errorf("webfetch: animation of %d x %d, %g s", w, h, duration)
	}
	file, err := p.writeParts(ctx, owner, mp4, animationName)
	if err != nil {
		return nil, err
	}
	m, err := p.Media.MediaUploadedDocumentMedia(ctx, &media.TLMediaUploadedDocumentMedia{
		OwnerId: owner,
		Media:   AnimationMedia(file, w, h, duration),
	})
	if err != nil {
		return nil, fmt.Errorf("media: %w", err)
	}
	doc := m.GetDocument()
	if doc.GetPredicateName() != mtproto.Predicate_document || doc.GetId() == 0 {
		return nil, errors.New("media: no document made of the animation")
	}
	return doc, nil
}

// AnimationMedia is the inputMediaUploadedDocument of a GIF as MP4.
func AnimationMedia(file *mtproto.InputFile, w, h int32, duration float64) *mtproto.InputMedia {
	return mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{
		NosoundVideo: true,
		File:         file,
		MimeType:     "video/mp4",
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeAnimated(nil).To_DocumentAttribute(),
			mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{
				SupportsStreaming: true,
				Nosound:           true,
				Duration:          duration,
				Duration_FLOAT64:  duration,
				Duration_INT32:    int32(duration + 0.5),
				W:                 w,
				H:                 h,
			}).To_DocumentAttribute(),
			mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: animationName}).To_DocumentAttribute(),
		},
	}).To_InputMedia()
}

// writeParts writes data to dfs as the parts of a new file of owner and
// returns the InputFile that names it.
func (p RPCPhotos) writeParts(ctx context.Context, owner int64, data []byte, name string) (*mtproto.InputFile, error) {
	fileID, err := newID()
	if err != nil {
		return nil, err
	}
	parts := int32(0)
	for off := 0; off < len(data); off += photoPart {
		end := min(off+photoPart, len(data))
		if _, err := p.Dfs.DfsWriteFilePartData(ctx, &dfs.TLDfsWriteFilePartData{
			Creator:  owner,
			FileId:   fileID,
			FilePart: parts,
			Bytes:    data[off:end],
		}); err != nil {
			return nil, fmt.Errorf("dfs: part %d: %w", parts, err)
		}
		parts++
	}
	sum := md5.Sum(data) //nolint:gosec // see the import
	return mtproto.MakeTLInputFile(&mtproto.InputFile{
		Id_INT64:    fileID,
		Parts:       parts,
		Name:        name,
		Md5Checksum: hex.EncodeToString(sum[:]),
	}).To_InputFile(), nil
}

// newID is a positive random id of a file.
func newID() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(b[:])>>1) | 1, nil
}

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
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/model"
)

// DfsImportDocument
// dfs.importDocument creator:long id:long date:int mime_type:string file:InputFile thumbs:Vector<PhotoSize> thumb_files:Vector<InputFile> video_thumbs:Vector<VideoSize> attributes:Vector<DocumentAttribute> = Document;
//
// pluma: a document made elsewhere (the catalog from Telegram, task 024,
// ADR 0028) stored as it is, unlike an upload: its thumbnails are the ones
// given, inline ('i', 'j') or files (thumb_files, named by type), and its
// attributes stay. It gets an id of this server (or keeps the one given)
// and an access hash that carries its storage type, as dfs.uploadDocumentFileV2 makes it, and every
// file is in storage before the answer: a failure is an error, nothing is
// left half stored for a client to fetch.
func (c *DfsCore) DfsImportDocument(in *dfs.TLDfsImportDocument) (*mtproto.Document, error) {
	file := in.GetFile()
	if file == nil || file.GetParts() <= 0 {
		return nil, mtproto.ErrMediaInvalid
	}
	thumbFiles, err := thumbFilesByType(in)
	if err != nil {
		c.Logger.Errorf("dfs.importDocument - %v", err)
		return nil, mtproto.ErrMediaInvalid
	}

	// A document clients name by its id in their code keeps it (ADR 0028);
	// media.importDocument refuses an id that is taken.
	documentId := in.GetId()
	if documentId == 0 {
		if documentId, err = c.svcCtx.Dao.IDGenClient2.NextId(c.ctx); err != nil {
			return nil, err
		}
	}
	ext := model.GetFileExtName(file.GetName())
	var secret [4]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	accessHash := int64(model.GetStorageFileTypeConstructor(ext))<<32 | int64(binary.LittleEndian.Uint32(secret[:]))

	r, err := c.svcCtx.Dao.OpenFile(c.ctx, in.GetCreator(), file.GetId_INT64(), file.GetParts())
	if err != nil {
		c.Logger.Errorf("dfs.importDocument - open the file: %v", err)
		return nil, mtproto.ErrMediaInvalid
	}
	size := r.Size()
	if _, err := c.svcCtx.Dao.PutDocumentFile(c.ctx, fmt.Sprintf("%d.dat", documentId), io.NewSectionReader(r, 0, size), size); err != nil {
		c.Logger.Errorf("dfs.importDocument - store the file: %v", err)
		return nil, err
	}

	// The thumbnails' files, each at <type>/<id>.dat where dfs.downloadFile
	// looks for it: video types in videos, the rest in photos.
	sizes := make(map[string]int32, len(thumbFiles))
	for typ, f := range thumbFiles {
		tr, err := c.svcCtx.Dao.OpenFile(c.ctx, in.GetCreator(), f.GetId_INT64(), f.GetParts())
		if err != nil {
			c.Logger.Errorf("dfs.importDocument - open the thumbnail %q: %v", typ, err)
			return nil, mtproto.ErrMediaInvalid
		}
		data, err := tr.ReadAll(c.ctx)
		if err != nil {
			return nil, err
		}
		path := fmt.Sprintf("%s/%d.dat", typ, documentId)
		if mtproto.PhotoSizeIsVideo(typ) {
			_, err = c.svcCtx.Dao.PutVideoFileV2(c.ctx, path, bytes.NewReader(data), int64(len(data)))
		} else {
			_, err = c.svcCtx.Dao.PutPhotoFileV2(c.ctx, path, bytes.NewReader(data), int64(len(data)))
		}
		if err != nil {
			c.Logger.Errorf("dfs.importDocument - store the thumbnail %q: %v", typ, err)
			return nil, err
		}
		sizes[typ] = int32(len(data))
	}

	// The parts are stored for good now. A catalog brings tens of thousands
	// of documents at once; left for their expiry, their parts would pile
	// up on dfs's disk. One that is not dropped still expires.
	for _, f := range append([]*mtproto.InputFile{file}, in.GetThumbFiles()...) {
		if err := c.svcCtx.Dao.DropFile(c.ctx, in.GetCreator(), f.GetId_INT64()); err != nil {
			c.Logger.Errorf("dfs.importDocument - drop the parts of %d: %v", f.GetId_INT64(), err)
		}
	}

	thumbs := make([]*mtproto.PhotoSize, 0, len(in.GetThumbs()))
	for _, t := range in.GetThumbs() {
		if n, ok := sizes[t.GetType()]; ok && t.GetPredicateName() == mtproto.Predicate_photoSize {
			t.Size2 = n
		}
		thumbs = append(thumbs, t)
	}
	videoThumbs := make([]*mtproto.VideoSize, 0, len(in.GetVideoThumbs()))
	for _, v := range in.GetVideoThumbs() {
		if n, ok := sizes[v.GetType()]; ok {
			v.Size2 = n
		}
		videoThumbs = append(videoThumbs, v)
	}

	return mtproto.MakeTLDocument(&mtproto.Document{
		Id:            documentId,
		AccessHash:    accessHash,
		FileReference: []byte{},
		Date:          in.GetDate(),
		MimeType:      in.GetMimeType(),
		Size2:         size,
		Size2_INT32:   int32(size),
		Size2_INT64:   size,
		Thumbs:        thumbs,
		VideoThumbs:   videoThumbs,
		DcId:          1,
		Attributes:    in.GetAttributes(),
	}).To_Document(), nil
}

// thumbFilesByType pairs the downloadable thumbnails of a document to
// import with their files: one file for each, none without a thumbnail.
// Inline thumbnails (photoStrippedSize, photoPathSize, photoCachedSize) and
// video markup carry their bytes and have no file.
func thumbFilesByType(in *dfs.TLDfsImportDocument) (map[string]*mtproto.InputFile, error) {
	want := map[string]bool{}
	for _, t := range in.GetThumbs() {
		switch t.GetPredicateName() {
		case mtproto.Predicate_photoSize, mtproto.Predicate_photoSizeProgressive:
			if t.GetType() == "" || want[t.GetType()] {
				return nil, fmt.Errorf("thumbnail type %q empty or twice", t.GetType())
			}
			want[t.GetType()] = true
		}
	}
	for _, v := range in.GetVideoThumbs() {
		if v.GetPredicateName() != mtproto.Predicate_videoSize {
			continue
		}
		if !mtproto.PhotoSizeIsVideo(v.GetType()) || want[v.GetType()] {
			return nil, fmt.Errorf("video thumbnail type %q not a video type or twice", v.GetType())
		}
		want[v.GetType()] = true
	}
	files := make(map[string]*mtproto.InputFile, len(in.GetThumbFiles()))
	for _, f := range in.GetThumbFiles() {
		typ := f.GetName()
		if !want[typ] || files[typ] != nil || f.GetParts() <= 0 {
			return nil, fmt.Errorf("thumbnail file %q has no thumbnail, comes twice or is empty", typ)
		}
		files[typ] = f
	}
	if len(files) != len(want) {
		return nil, fmt.Errorf("%d thumbnails, %d files", len(want), len(files))
	}
	return files, nil
}

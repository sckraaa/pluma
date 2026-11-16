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

package svc

import (
	"context"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/teamgram/teamgram-server/pkg/secretchat"
)

// The sources of the CDN at home (task 102): the sizes of the files from
// media and the secret chats' table, their bytes from dfs.

// mediaSource is what cdnFiles asks media.
type mediaSource interface {
	MediaGetDocument(ctx context.Context, in *media.TLMediaGetDocument) (*mtproto.Document, error)
	MediaGetPhoto(ctx context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error)
}

// encryptedFiles is what cdnFiles asks the secret chats' table.
type encryptedFiles interface {
	File(ctx context.Context, id int64) (*secretchat.File, error)
}

// dfsSource is what cdnFiles asks dfs.
type dfsSource interface {
	DfsDownloadFile(ctx context.Context, in *dfs.TLDfsDownloadFile) (*mtproto.Upload_File, error)
}

// cdnFiles is the cdn.Sizer and cdn.FileReader of files.
type cdnFiles struct {
	media     mediaSource
	encrypted encryptedFiles
	dfs       dfsSource
}

var (
	_ cdn.Sizer      = (*cdnFiles)(nil)
	_ cdn.FileReader = (*cdnFiles)(nil)
)

// FileSize returns the size of a document, of one size of a photo, or of a
// secret chat file.
func (s *cdnFiles) FileSize(ctx context.Context, loc *mtproto.InputFileLocation) (int64, error) {
	switch loc.GetPredicateName() {
	case mtproto.Predicate_inputDocumentFileLocation:
		doc, err := s.media.MediaGetDocument(ctx, &media.TLMediaGetDocument{Id: loc.GetId()})
		if err != nil {
			return 0, fmt.Errorf("media.getDocument: %w", err)
		}
		if doc == nil || doc.GetPredicateName() != mtproto.Predicate_document {
			return 0, cdn.ErrNoFile
		}
		return doc.GetSize2_INT64(), nil
	case mtproto.Predicate_inputPhotoFileLocation:
		photo, err := s.media.MediaGetPhoto(ctx, &media.TLMediaGetPhoto{PhotoId: loc.GetId()})
		if err != nil {
			return 0, fmt.Errorf("media.getPhoto: %w", err)
		}
		if photo == nil || photo.GetPredicateName() != mtproto.Predicate_photo {
			return 0, cdn.ErrNoFile
		}
		return photoSize(photo, loc.GetThumbSize())
	case mtproto.Predicate_inputEncryptedFileLocation:
		f, err := s.encrypted.File(ctx, loc.GetId())
		if err != nil {
			return 0, fmt.Errorf("encrypted file: %w", err)
		}
		if f == nil {
			return 0, cdn.ErrNoFile
		}
		return f.Size, nil
	}
	return 0, cdn.ErrNoFile
}

// photoSize is the size of the file of one size of photo: the bytes of a
// photoSize or videoSize, the last (full) one of a photoSizeProgressive
// (core.telegram.org/api/files#progressive-jpegs). The sizes with their
// bytes inline (stripped, cached, path) have no file.
func photoSize(photo *mtproto.Photo, thumb string) (int64, error) {
	for _, sz := range photo.GetSizes() {
		if sz.GetType() != thumb {
			continue
		}
		switch sz.GetPredicateName() {
		case mtproto.Predicate_photoSize:
			return int64(sz.GetSize2()), nil
		case mtproto.Predicate_photoSizeProgressive:
			if n := len(sz.GetSizes()); n > 0 {
				return int64(sz.GetSizes()[n-1]), nil
			}
			return int64(sz.GetSize2()), nil
		}
	}
	for _, sz := range photo.GetVideoSizes() {
		if sz.GetType() == thumb && sz.GetPredicateName() == mtproto.Predicate_videoSize {
			return int64(sz.GetSize2()), nil
		}
	}
	return 0, cdn.ErrNoFile
}

// ReadPart reads a part of a file from dfs, as upload.getFile does.
func (s *cdnFiles) ReadPart(ctx context.Context, loc *mtproto.InputFileLocation, offset int64, limit int32) ([]byte, error) {
	f, err := s.dfs.DfsDownloadFile(ctx, &dfs.TLDfsDownloadFile{Location: loc, Offset: offset, Limit: limit})
	if err != nil {
		return nil, err
	}
	return f.GetBytes(), nil
}

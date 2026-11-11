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

package usersets

import (
	"context"
	"errors"

	"github.com/teamgram/proto/mtproto"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/catalog"
)

// ThumbFiles finds the file of a set's own thumbnail
// (inputStickerSetThumb) for upload.getFile: the document the set keeps it
// in (task 084). Only sets users made have one: the catalog's sets come
// without (ADR 0028).
type ThumbFiles struct {
	Sets interface {
		SetByID(ctx context.Context, id int64) (*catalog.Set, error)
		SetByShortName(ctx context.Context, name string) (*catalog.Set, error)
	}
	Media media_client.MediaClient
}

var errNoThumb = errors.New("usersets: the set has no thumbnail")

// GetStickerSetThumbFileLocation implements the plugin of app/bff/files.
// The version is not checked: a client that asks for an old one gets the
// thumbnail the set has now.
func (t ThumbFiles) GetStickerSetThumbFileLocation(ctx context.Context, _ int64, in *mtproto.InputStickerSet, _ int32) (*mtproto.InputFileLocation, error) {
	var (
		set *catalog.Set
		err error
	)
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputStickerSetID:
		set, err = t.Sets.SetByID(ctx, in.GetId())
		if set != nil && set.AccessHash != in.GetAccessHash() {
			set = nil
		}
	case mtproto.Predicate_inputStickerSetShortName:
		set, err = t.Sets.SetByShortName(ctx, in.GetShortName())
	}
	if err != nil {
		return nil, err
	}
	if set == nil || set.ThumbDocumentID == 0 {
		return nil, errNoThumb
	}
	doc, err := t.Media.MediaGetDocument(ctx, &media.TLMediaGetDocument{Id: set.ThumbDocumentID})
	if err != nil {
		return nil, err
	}
	if doc.GetPredicateName() != mtproto.Predicate_document {
		return nil, errNoThumb
	}
	return mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{
		Id:            doc.GetId(),
		AccessHash:    doc.GetAccessHash(),
		FileReference: []byte{},
	}).To_InputFileLocation(), nil
}

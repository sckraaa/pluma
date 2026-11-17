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
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/teamgram/teamgram-server/pkg/secretchat"
)

type fakeMedia struct {
	docs   map[int64]*mtproto.Document
	photos map[int64]*mtproto.Photo
}

func (m *fakeMedia) MediaGetDocument(_ context.Context, in *media.TLMediaGetDocument) (*mtproto.Document, error) {
	if d, ok := m.docs[in.GetId()]; ok {
		return d, nil
	}
	return mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: in.GetId()}).To_Document(), nil
}

func (m *fakeMedia) MediaGetPhoto(_ context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error) {
	if p, ok := m.photos[in.GetPhotoId()]; ok {
		return p, nil
	}
	return mtproto.MakeTLPhotoEmpty(&mtproto.Photo{Id: in.GetPhotoId()}).To_Photo(), nil
}

type fakeEncrypted map[int64]*secretchat.File

func (f fakeEncrypted) File(_ context.Context, id int64) (*secretchat.File, error) { return f[id], nil }

// TestCdnFileSize: the sizes the CDN decides by, from media and the secret
// chats' table.
func TestCdnFileSize(t *testing.T) {
	files := &cdnFiles{
		media: &fakeMedia{
			docs: map[int64]*mtproto.Document{1: mtproto.MakeTLDocument(&mtproto.Document{Id: 1, Size2_INT64: 5 << 20}).To_Document()},
			photos: map[int64]*mtproto.Photo{2: mtproto.MakeTLPhoto(&mtproto.Photo{
				Id: 2,
				Sizes: []*mtproto.PhotoSize{
					mtproto.MakeTLPhotoStrippedSize(&mtproto.PhotoSize{Type: "i", Bytes: []byte{1, 2}}).To_PhotoSize(),
					mtproto.MakeTLPhotoSize(&mtproto.PhotoSize{Type: "x", W: 800, H: 600, Size2: 90000}).To_PhotoSize(),
					mtproto.MakeTLPhotoSizeProgressive(&mtproto.PhotoSize{Type: "y", W: 1280, H: 960, Sizes: []int32{10000, 600000, 1500000}}).To_PhotoSize(),
				},
				VideoSizes: []*mtproto.VideoSize{
					mtproto.MakeTLVideoSize(&mtproto.VideoSize{Type: "u", W: 800, H: 800, Size2: 2 << 20}).To_VideoSize(),
				},
			}).To_Photo()},
		},
		encrypted: fakeEncrypted{3: {Id: 3, Size: 7 << 20}},
	}
	ctx := context.Background()
	for _, c := range []struct {
		name string
		loc  *mtproto.InputFileLocation
		size int64
		err  error
	}{
		{"document", mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{Id: 1}).To_InputFileLocation(), 5 << 20, nil},
		{"no document", mtproto.MakeTLInputDocumentFileLocation(&mtproto.InputFileLocation{Id: 9}).To_InputFileLocation(), 0, cdn.ErrNoFile},
		{"photo size", mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{Id: 2, ThumbSize: "x"}).To_InputFileLocation(), 90000, nil},
		{"progressive", mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{Id: 2, ThumbSize: "y"}).To_InputFileLocation(), 1500000, nil},
		{"video size", mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{Id: 2, ThumbSize: "u"}).To_InputFileLocation(), 2 << 20, nil},
		{"inline size", mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{Id: 2, ThumbSize: "i"}).To_InputFileLocation(), 0, cdn.ErrNoFile},
		{"no photo", mtproto.MakeTLInputPhotoFileLocation(&mtproto.InputFileLocation{Id: 9, ThumbSize: "x"}).To_InputFileLocation(), 0, cdn.ErrNoFile},
		{"secret chat file", mtproto.MakeTLInputEncryptedFileLocation(&mtproto.InputFileLocation{Id: 3}).To_InputFileLocation(), 7 << 20, nil},
		{"no secret chat file", mtproto.MakeTLInputEncryptedFileLocation(&mtproto.InputFileLocation{Id: 9}).To_InputFileLocation(), 0, cdn.ErrNoFile},
	} {
		size, err := files.FileSize(ctx, c.loc)
		if size != c.size || !errors.Is(err, c.err) {
			t.Errorf("%s: %d, %v; want %d, %v", c.name, size, err, c.size, c.err)
		}
	}
}

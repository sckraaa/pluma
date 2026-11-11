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

//go:build integration

package attached

import (
	"context"
	"slices"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

type docs map[int64]*mtproto.Document

func (d docs) MediaGetDocument(_ context.Context, in *media.TLMediaGetDocument) (*mtproto.Document, error) {
	if doc, ok := d[in.GetId()]; ok {
		return doc, nil
	}
	return mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: in.GetId()}).To_Document(), nil
}

func sticker(id int64) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{Id: id, AccessHash: id + 1, Attributes: []*mtproto.DocumentAttribute{
		mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{Alt: "😺"}).To_DocumentAttribute(),
	}}).To_Document()
}

func input(id, hash int64) *mtproto.InputDocument {
	return mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: id, AccessHash: hash}).To_InputDocument()
}

// The stickers put on an uploaded photo or video are kept by the media's
// id, in order and each once, and mark the media: at the upload, and when
// the photo or the document is sent again by its id.
func TestStore(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	s := NewStore(sqlx.NewMySQL(&conf))
	d := docs{1: sticker(1), 2: sticker(2), 3: mtproto.MakeTLDocument(&mtproto.Document{Id: 3, AccessHash: 4}).To_Document()}

	photo := func(id int64) *mtproto.MessageMedia {
		return mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Photo_FLAGPHOTO: mtproto.MakeTLPhoto(&mtproto.Photo{Id: id}).To_Photo()}).To_MessageMedia()
	}
	video := func(id int64) *mtproto.MessageMedia {
		return mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: mtproto.MakeTLDocument(&mtproto.Document{Id: id}).To_Document()}).To_MessageMedia()
	}
	// Stickers 2 and 1, 2 named twice; a document that is no sticker, a
	// sticker with a wrong access hash and one that is not there are left out.
	upload := mtproto.MakeTLInputMediaUploadedPhoto(&mtproto.InputMedia{Stickers: []*mtproto.InputDocument{
		input(2, 3), input(3, 4), input(1, 2), input(2, 3), input(1, 99), input(404, 1),
	}}).To_InputMedia()
	made := photo(100)
	if err := s.Mark(ctx, d, upload, made); err != nil {
		t.Fatal(err)
	}
	if !made.GetPhoto_FLAGPHOTO().GetHasStickers() {
		t.Errorf("an uploaded photo with stickers is not marked")
	}
	if ids, err := s.Of(ctx, 100); err != nil || !slices.Equal(ids, []int64{2, 1}) {
		t.Errorf("the stickers of the photo: %v, %v", ids, err)
	}

	// Sent again by its id, the photo is marked; another one is not.
	byID := mtproto.MakeTLInputMediaPhoto(nil).To_InputMedia()
	again, plain := photo(100), photo(101)
	if err := s.Mark(ctx, d, byID, again); err != nil || !again.GetPhoto_FLAGPHOTO().GetHasStickers() {
		t.Errorf("the photo sent by its id: marked %v, %v", again.GetPhoto_FLAGPHOTO().GetHasStickers(), err)
	}
	if err := s.Mark(ctx, d, byID, plain); err != nil || plain.GetPhoto_FLAGPHOTO().GetHasStickers() {
		t.Errorf("a photo without stickers: marked %v, %v", plain.GetPhoto_FLAGPHOTO().GetHasStickers(), err)
	}
	none := photo(102)
	if err := s.Mark(ctx, d, mtproto.MakeTLInputMediaUploadedPhoto(&mtproto.InputMedia{Stickers: []*mtproto.InputDocument{input(3, 4)}}).To_InputMedia(), none); err != nil || none.GetPhoto_FLAGPHOTO().GetHasStickers() {
		t.Errorf("a photo with no real sticker on it: marked %v, %v", none.GetPhoto_FLAGPHOTO().GetHasStickers(), err)
	}
	if has, _ := s.Has(ctx, 102); has {
		t.Errorf("stickers recorded for a photo with none")
	}

	// A video gets documentAttributeHasStickers, once.
	clip := video(200)
	in := mtproto.MakeTLInputMediaUploadedDocument(&mtproto.InputMedia{Stickers: []*mtproto.InputDocument{input(1, 2)}}).To_InputMedia()
	for i := 0; i < 2; i++ {
		if err := s.Mark(ctx, d, in, clip); err != nil {
			t.Fatal(err)
		}
	}
	if a := clip.GetDocument().GetAttributes(); len(a) != 1 || a[0].GetPredicateName() != mtproto.Predicate_documentAttributeHasStickers {
		t.Errorf("the attributes of a video with stickers: %v", a)
	}
	// A sticker sent as a document has none on it, whatever was recorded.
	if err := s.Put(ctx, 1, []int64{2}); err != nil {
		t.Fatal(err)
	}
	st := mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: sticker(1)}).To_MessageMedia()
	if err := s.Mark(ctx, d, mtproto.MakeTLInputMediaDocument(nil).To_InputMedia(), st); err != nil || len(st.GetDocument().GetAttributes()) != 1 {
		t.Errorf("a sticker marked as having stickers: %v, %v", st.GetDocument().GetAttributes(), err)
	}
}

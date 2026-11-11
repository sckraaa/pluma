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
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// A message sends a sticker when it names a sticker document with its
// access hash; a file, another hash, or a document gone send none.
func TestSentSticker(t *testing.T) {
	sticker := mtproto.MakeTLDocument(&mtproto.Document{
		Id: 5, AccessHash: 6, MimeType: "application/x-tgsticker",
		Attributes: []*mtproto.DocumentAttribute{mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{Alt: "😺"}).To_DocumentAttribute()},
	}).To_Document()
	file := mtproto.MakeTLDocument(&mtproto.Document{Id: 5, AccessHash: 6, MimeType: "application/pdf"}).To_Document()
	in := func(id, accessHash int64) *mtproto.InputMedia {
		return mtproto.MakeTLInputMediaDocument(&mtproto.InputMedia{
			Id_INPUTDOCUMENT: mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: id, AccessHash: accessHash}).To_InputDocument(),
		}).To_InputMedia()
	}
	media := func(d *mtproto.Document) *mtproto.MessageMedia {
		return mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: d}).To_MessageMedia()
	}
	for _, c := range []struct {
		name  string
		in    *mtproto.InputMedia
		media *mtproto.MessageMedia
		want  int64
	}{
		{"a sticker", in(5, 6), media(sticker), 5},
		{"another hash", in(5, 7), media(sticker), 0},
		{"a file", in(5, 6), media(file), 0},
		{"gone", in(5, 6), media(mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: 5}).To_Document()), 0},
		{"a photo", mtproto.MakeTLInputMediaPhoto(nil).To_InputMedia(), media(sticker), 0},
	} {
		if got := sentSticker(c.in, c.media); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
	attached := []*mtproto.InputDocument{mtproto.MakeTLInputDocument(&mtproto.InputDocument{Id: 8}).To_InputDocument()}
	if got := attachedStickers(mtproto.MakeTLInputMediaUploadedPhoto(&mtproto.InputMedia{Stickers: attached}).To_InputMedia()); len(got) != 1 {
		t.Errorf("attached to a photo %v", got)
	}
	if got := attachedStickers(in(5, 6)); got != nil {
		t.Errorf("attached to a document %v", got)
	}
}

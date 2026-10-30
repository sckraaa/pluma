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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func doc(id int64, mime string, attrs ...*mtproto.DocumentAttribute) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{Id: id, MimeType: mime, Attributes: attrs}).To_Document()
}

func TestIsGif(t *testing.T) {
	animated := mtproto.MakeTLDocumentAttributeAnimated(nil).To_DocumentAttribute()
	video := mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{W: 320, H: 240}).To_DocumentAttribute()
	for _, c := range []struct {
		name string
		doc  *mtproto.Document
		want bool
	}{
		{"image/gif", doc(1, "image/gif"), true},
		{"mp4 animation", doc(1, "video/mp4", video, animated), true},
		{"mp4 video", doc(1, "video/mp4", video), false},
		{"sticker", doc(1, "image/webp"), false},
		{"gone", mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: 1}).To_Document(), false},
	} {
		if got := isGif(c.doc); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

// The hash of Android's MediaDataController.calcDocumentsHash: unsigned
// shifts, so a large id makes the accumulator negative and the next step
// must not carry its sign.
func TestDocumentsHash(t *testing.T) {
	docs := []*mtproto.Document{doc(5980346543123456789, "image/gif"), doc(-42, "image/gif"), doc(7, "image/gif")}
	if got, want := documentsHash(docs), int64(-8051982839275157892); got != want {
		t.Errorf("hash %d, want %d", got, want)
	}
	if got := documentsHash(nil); got != 0 {
		t.Errorf("hash of no GIFs %d", got)
	}
}

func TestInOrder(t *testing.T) {
	docs := []*mtproto.Document{doc(3, "image/gif"), doc(1, "image/gif"), mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: 2}).To_Document()}
	var ids []int64
	for _, d := range inOrder([]int64{1, 2, 3, 4}, docs) {
		ids = append(ids, d.GetId())
	}
	if !slices.Equal(ids, []int64{1, 3}) {
		t.Errorf("documents %v, want [1 3]", ids)
	}
}

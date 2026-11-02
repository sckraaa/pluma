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
	"github.com/teamgram/teamgram-server/app/service/dfs/dfs"
)

// A sticker as Telegram sends it: an inline path thumbnail ('j'), a webp
// 'm' and a video 'f' thumbnail with files; each file pairs with its
// thumbnail, and a thumbnail without its file, or a file without its
// thumbnail, refuses the import.
func TestThumbFilesByType(t *testing.T) {
	path := mtproto.MakeTLPhotoPathSize(&mtproto.PhotoSize{Type: "j", Bytes: []byte{1, 2}}).To_PhotoSize()
	m := mtproto.MakeTLPhotoSize(&mtproto.PhotoSize{Type: "m", W: 128, H: 128, Size2: 10}).To_PhotoSize()
	f := mtproto.MakeTLVideoSize(&mtproto.VideoSize{Type: "f", W: 100, H: 100}).To_VideoSize()
	file := func(name string) *mtproto.InputFile {
		return mtproto.MakeTLInputFile(&mtproto.InputFile{Id_INT64: 1, Parts: 1, Name: name}).To_InputFile()
	}
	ok := &dfs.TLDfsImportDocument{
		Thumbs:      []*mtproto.PhotoSize{path, m},
		VideoThumbs: []*mtproto.VideoSize{f},
		ThumbFiles:  []*mtproto.InputFile{file("m"), file("f")},
	}
	files, err := thumbFilesByType(ok)
	if err != nil || len(files) != 2 || files["m"] == nil || files["f"] == nil {
		t.Fatalf("files %v, %v", files, err)
	}
	for name, in := range map[string]*dfs.TLDfsImportDocument{
		"a thumbnail without its file": {Thumbs: []*mtproto.PhotoSize{m}},
		"a file without a thumbnail":   {Thumbs: []*mtproto.PhotoSize{path}, ThumbFiles: []*mtproto.InputFile{file("m")}},
		"a file twice":                 {Thumbs: []*mtproto.PhotoSize{m}, ThumbFiles: []*mtproto.InputFile{file("m"), file("m")}},
		"an empty file":                {Thumbs: []*mtproto.PhotoSize{m}, ThumbFiles: []*mtproto.InputFile{mtproto.MakeTLInputFile(&mtproto.InputFile{Name: "m"}).To_InputFile()}},
		"a type twice":                 {Thumbs: []*mtproto.PhotoSize{m, m}, ThumbFiles: []*mtproto.InputFile{file("m")}},
		"a video thumbnail as a photo": {VideoThumbs: []*mtproto.VideoSize{mtproto.MakeTLVideoSize(&mtproto.VideoSize{Type: "m"}).To_VideoSize()}, ThumbFiles: []*mtproto.InputFile{file("m")}},
	} {
		if _, err := thumbFilesByType(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

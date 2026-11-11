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

package stickerlists

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func sticker(id int64, alt string, setID int64) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{
		Id:         id,
		AccessHash: id + 1,
		MimeType:   "image/webp",
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeImageSize(&mtproto.DocumentAttribute{W: 512, H: 512}).To_DocumentAttribute(),
			mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{
				Alt:        alt,
				Stickerset: mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: setID, AccessHash: setID + 1}).To_InputStickerSet(),
			}).To_DocumentAttribute(),
		},
	}).To_Document()
}

func file(id int64) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{Id: id, MimeType: "image/gif"}).To_Document()
}

// A sticker is a document with documentAttributeSticker; nothing else is,
// a document that is gone neither.
func TestStickerAttribute(t *testing.T) {
	if StickerAttribute(sticker(1, "😀", 5)) == nil {
		t.Error("a sticker is no sticker")
	}
	for _, d := range []*mtproto.Document{file(2), mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: 3}).To_Document(), nil} {
		if StickerAttribute(d) != nil {
			t.Errorf("%v is a sticker", d)
		}
	}
}

// The hash is the clients' over the ids in order: it changes with the order.
func TestHash(t *testing.T) {
	a, b := sticker(10, "", 0), sticker(-20, "", 0)
	if Hash(nil) != 0 {
		t.Error("the hash of no stickers is not 0")
	}
	if Hash([]*mtproto.Document{a, b}) == Hash([]*mtproto.Document{b, a}) {
		t.Error("the hash does not change with the order")
	}
	// Android calcHash: acc ^= acc >>> 21; acc ^= acc << 35; acc ^= acc >>> 4; acc += id.
	var acc uint64
	for _, id := range []int64{10, -20} {
		acc ^= acc >> 21
		acc ^= acc << 35
		acc ^= acc >> 4
		acc += uint64(id)
	}
	if got := Hash([]*mtproto.Document{a, b}); got != int64(acc) {
		t.Errorf("hash %d, want %d", got, int64(acc))
	}
}

// The stickers come in the order of the list with the date of each; a
// document gone or no sticker is left out.
func TestInOrder(t *testing.T) {
	entries := []Entry{{DocumentID: 3, UsedAt: 30}, {DocumentID: 1, UsedAt: 10}, {DocumentID: 2, UsedAt: 20}, {DocumentID: 4, UsedAt: 40}}
	docs := []*mtproto.Document{sticker(1, "", 0), file(2), sticker(3, "", 0)}
	got, dates := InOrder(entries, docs)
	var ids []int64
	for _, d := range got {
		ids = append(ids, d.GetId())
	}
	if !slices.Equal(ids, []int64{3, 1}) || !slices.Equal(dates, []int32{30, 10}) {
		t.Errorf("stickers %v dates %v, want [3 1] [30 10]", ids, dates)
	}
}

// The packs give each emoji its stickers in the list's order: the emoji of a
// sticker by the packs of its set, else by its alt.
func TestPacks(t *testing.T) {
	setPacks := []*mtproto.StickerPack{
		mtproto.MakeTLStickerPack(&mtproto.StickerPack{Emoticon: "😀", Documents: []int64{1, 2}}).To_StickerPack(),
		mtproto.MakeTLStickerPack(&mtproto.StickerPack{Emoticon: "😂", Documents: []int64{1}}).To_StickerPack(),
		mtproto.MakeTLStickerPack(&mtproto.StickerPack{Emoticon: "😀", Documents: []int64{1}}).To_StickerPack(),
	}
	docs := []*mtproto.Document{sticker(2, "x", 5), sticker(9, "🐱", 6), sticker(1, "y", 5), sticker(8, "", 7)}
	packs := Packs(docs, func(d *mtproto.Document) []string {
		if StickerAttribute(d).GetStickerset().GetId() == 5 {
			return SetEmoticons(d, setPacks)
		}
		return SetEmoticons(d, nil)
	})
	var got []string
	for _, p := range packs {
		got = append(got, fmt.Sprint(p.GetEmoticon(), p.GetDocuments()))
	}
	if want := []string{"😀[2 1]", "🐱[9]", "😂[1]"}; !slices.Equal(got, want) {
		t.Errorf("packs %v, want %v", got, want)
	}
}

// The clients trim their lists to the limits of help.getConfig and
// help.getAppConfig: the configs must give the limits the server keeps.
func TestLimitsAreTheConfigs(t *testing.T) {
	read := func(name string) map[string]any {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "teamgramd", "bin", name))
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		if err = json.Unmarshal(data, &config); err != nil {
			t.Fatal(err)
		}
		return config
	}
	appConfig := read("app_config.json")
	config, _ := read("config.json")["data2"].(map[string]any)
	for key, limit := range map[string]int{
		"app_config stickers_faved_limit_default": FavedLimit,
		"app_config stickers_faved_limit_premium": FavedLimit,
		"config stickers_faved_limit":             FavedLimit,
		"config stickers_recent_limit":            RecentLimit,
	} {
		from, name, _ := strings.Cut(key, " ")
		m := appConfig
		if from == "config" {
			m = config
		}
		if v, ok := m[name].(float64); !ok || v != float64(limit) {
			t.Errorf("%s %v, the server's limit %d", key, m[name], limit)
		}
	}
	if Limit(Faved) != FavedLimit || Limit(Recent) != RecentLimit || Limit(RecentAttached) != RecentLimit {
		t.Error("Limit gives another limit")
	}
}

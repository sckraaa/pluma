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

package importer

import (
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func tgSticker(id, setID int64) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{
		Id: id, AccessHash: 1, MimeType: "application/x-tgsticker", DcId: 2,
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeSticker(&mtproto.DocumentAttribute{
				Alt:        "😀",
				Stickerset: mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: setID, AccessHash: 2}).To_InputStickerSet(),
			}).To_DocumentAttribute(),
		},
	}).To_Document()
}

// Every id of Telegram in a set becomes one of this server: the set's, its
// documents whole, the packs and keywords that name documents by id, and
// the set a sticker's attribute refers to.
func TestRewriteStickerSet(t *testing.T) {
	const tgSet, tgDoc1, tgDoc2 = int64(1258816259751998), int64(5368324170671202286), int64(77)
	set := mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
		Set:       mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: tgSet, AccessHash: 3, ShortName: "HotCherry", ThumbDocumentId: mtproto.MakeFlagsInt64(tgDoc2)}).To_StickerSet(),
		Packs:     []*mtproto.StickerPack{mtproto.MakeTLStickerPack(&mtproto.StickerPack{Emoticon: "😀", Documents: []int64{tgDoc1, tgDoc2}}).To_StickerPack()},
		Keywords:  []*mtproto.StickerKeyword{mtproto.MakeTLStickerKeyword(&mtproto.StickerKeyword{DocumentId: tgDoc1, Keyword: []string{"smile"}}).To_StickerKeyword()},
		Documents: []*mtproto.Document{tgSticker(tgDoc1, tgSet), tgSticker(tgDoc2, tgSet)},
	}).To_Messages_StickerSet()

	ids := NewIDs()
	ids.Sets[tgSet] = idHash{ID: 1000, AccessHash: 1001}
	for tg, our := range map[int64]int64{tgDoc1: 2001, tgDoc2: 2002} {
		d := tgSticker(tg, tgSet)
		d.Id = our
		d.Attributes[0].Stickerset.Id, d.Attributes[0].Stickerset.AccessHash = 1000, 1001
		ids.Documents[tg] = d
	}
	if err := ids.Rewrite(set); err != nil {
		t.Fatal(err)
	}
	if set.GetSet().GetId() != 1000 || set.GetSet().GetAccessHash() != 1001 || set.GetSet().GetThumbDocumentId().GetValue() != 2002 {
		t.Fatalf("set %v", set.GetSet())
	}
	if p := set.GetPacks()[0].GetDocuments(); p[0] != 2001 || p[1] != 2002 {
		t.Fatalf("pack %v", p)
	}
	if set.GetKeywords()[0].GetDocumentId() != 2001 {
		t.Fatalf("keyword %v", set.GetKeywords()[0])
	}
	for i, d := range set.GetDocuments() {
		if d.GetId() != 2001+int64(i) || d.GetAttributes()[0].GetStickerset().GetId() != 1000 {
			t.Fatalf("document %d: %v", i, d)
		}
	}

	// A reference to what was not imported is an error.
	for name, msg := range map[string]*mtproto.Messages_StickerSet{
		"a document": mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
			Set:       mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: tgSet}).To_StickerSet(),
			Documents: []*mtproto.Document{tgSticker(99, tgSet)},
		}).To_Messages_StickerSet(),
		"a set": mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
			Set: mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: 5}).To_StickerSet(),
		}).To_Messages_StickerSet(),
		"a pack": mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
			Set:   mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: tgSet}).To_StickerSet(),
			Packs: []*mtproto.StickerPack{mtproto.MakeTLStickerPack(&mtproto.StickerPack{Documents: []int64{99}}).To_StickerPack()},
		}).To_Messages_StickerSet(),
	} {
		if err := ids.Rewrite(msg); err == nil || !strings.Contains(err.Error(), "not imported") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Reactions and effects name documents whole and by id.
func TestRewriteReactionsAndEffects(t *testing.T) {
	ids := NewIDs()
	ids.Sets[9] = idHash{ID: 900, AccessHash: 901}
	for tg, our := range map[int64]int64{1: 101, 2: 102, 3: 103} {
		d := tgSticker(our, 900)
		ids.Documents[tg] = d
	}
	effects := mtproto.MakeTLMessagesAvailableEffects(&mtproto.Messages_AvailableEffects{
		Effects: []*mtproto.AvailableEffect{mtproto.MakeTLAvailableEffect(&mtproto.AvailableEffect{
			Id: 5, Emoticon: "🔥", StaticIconId: mtproto.MakeFlagsInt64(1), EffectStickerId: 2, EffectAnimationId: mtproto.MakeFlagsInt64(3),
		}).To_AvailableEffect()},
		Documents: []*mtproto.Document{tgSticker(1, 9), tgSticker(2, 9), tgSticker(3, 9)},
	}).To_Messages_AvailableEffects()
	if err := ids.Rewrite(effects); err != nil {
		t.Fatal(err)
	}
	e := effects.GetEffects()[0]
	if e.GetStaticIconId().GetValue() != 101 || e.GetEffectStickerId() != 102 || e.GetEffectAnimationId().GetValue() != 103 || e.GetId() != 5 {
		t.Fatalf("effect %v", e)
	}
	top := mtproto.MakeTLMessagesReactions(&mtproto.Messages_Reactions{Reactions: []*mtproto.Reaction{
		mtproto.MakeTLReactionCustomEmoji(&mtproto.Reaction{DocumentId: 2}).To_Reaction(),
		mtproto.MakeTLReactionEmoji(&mtproto.Reaction{Emoticon: "👍"}).To_Reaction(),
	}}).To_Messages_Reactions()
	if err := ids.Rewrite(top); err != nil || top.GetReactions()[0].GetDocumentId() != 102 {
		t.Fatalf("reactions %v, %v", top, err)
	}
	list := mtproto.MakeTLEmojiList(&mtproto.EmojiList{DocumentId: []int64{3, 1}}).To_EmojiList()
	if err := ids.Rewrite(list); err != nil || list.GetDocumentId()[0] != 103 || list.GetDocumentId()[1] != 101 {
		t.Fatalf("emoji list %v, %v", list, err)
	}
}

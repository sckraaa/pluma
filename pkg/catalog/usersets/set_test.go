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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
)

func doc(id int64) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{Id: id, AccessHash: id + 1}).To_Document()
}

func ids(ms *mtproto.Messages_StickerSet) []int64 {
	out := []int64{}
	for _, d := range ms.GetDocuments() {
		out = append(out, d.GetId())
	}
	return out
}

func packs(ms *mtproto.Messages_StickerSet) map[string][]int64 {
	out := map[string][]int64{}
	for _, p := range ms.GetPacks() {
		out[p.GetEmoticon()] = p.GetDocuments()
	}
	return out
}

// A set a user makes is the messages.stickerSet clients get: its stickers
// in order, the packs of their emoji in that order, their keywords, the
// count and a hash that changes with anything in it.
func TestSet(t *testing.T) {
	ms := New(5, 6, "Cats", "cats", installed.Stickers, true)
	if ms.GetSet().GetTextColor() || ms.GetSet().GetEmojis() || ms.GetSet().GetMasks() {
		t.Fatalf("a set of stickers: %v", ms.GetSet())
	}
	seal := func() int32 {
		t.Helper()
		if err := Seal(ms); err != nil {
			t.Fatal(err)
		}
		return ms.GetSet().GetHash()
	}
	empty := seal()
	Insert(ms, 0, Sticker{Document: doc(1), Emoji: []string{"😺"}, Keywords: []string{"cat"}})
	Insert(ms, 1, Sticker{Document: doc(2), Emoji: []string{"🐶", "😺"}})
	Insert(ms, 99, Sticker{Document: doc(3), Emoji: []string{"🐶"}})
	h1 := seal()
	if h1 == empty || h1 == 0 || ms.GetSet().GetCount() != 3 {
		t.Fatalf("hash %d (empty %d), count %d", h1, empty, ms.GetSet().GetCount())
	}
	if got := packs(ms); !slices.Equal(got["😺"], []int64{1, 2}) || !slices.Equal(got["🐶"], []int64{2, 3}) || len(got) != 2 {
		t.Fatalf("packs %v", got)
	}
	if ms.GetPacks()[0].GetEmoticon() != "😺" {
		t.Errorf("the first pack is %q, want the first sticker's emoji", ms.GetPacks()[0].GetEmoticon())
	}
	if again := seal(); again != h1 {
		t.Errorf("the hash of the same set changed: %d, %d", h1, again)
	}

	Move(ms, 3, 0)
	if !slices.Equal(ids(ms), []int64{3, 1, 2}) {
		t.Fatalf("after the move: %v", ids(ms))
	}
	if got := packs(ms); !slices.Equal(got["🐶"], []int64{3, 2}) || ms.GetPacks()[0].GetEmoticon() != "🐶" {
		t.Errorf("packs after the move: %v", ms.GetPacks())
	}
	if h := seal(); h == h1 {
		t.Errorf("the hash did not change with the order")
	}

	SetEmoji(ms, 1, []string{"🦁"})
	SetKeywords(ms, 1, []string{"lion"})
	if got := packs(ms); !slices.Equal(got["🦁"], []int64{1}) || !slices.Equal(got["😺"], []int64{2}) {
		t.Errorf("packs after the emoji changed: %v", got)
	}
	if !slices.Equal(Emoji(ms, 2), []string{"🐶", "😺"}) || !slices.Equal(Keywords(ms, 1), []string{"lion"}) {
		t.Errorf("emoji of 2 %v, keywords of 1 %v", Emoji(ms, 2), Keywords(ms, 1))
	}

	ms.Set.ThumbDocumentId = mtproto.MakeFlagsInt64(1)
	Remove(ms, 1)
	if !slices.Equal(ids(ms), []int64{3, 2}) || len(ms.GetKeywords()) != 0 || ms.GetSet().GetThumbDocumentId() != nil {
		t.Errorf("after the removal: %v, keywords %v, thumb %v", ids(ms), ms.GetKeywords(), ms.GetSet().GetThumbDocumentId())
	}
	if _, ok := packs(ms)["🦁"]; ok {
		t.Errorf("the pack of a removed sticker stays: %v", ms.GetPacks())
	}

	// What an account has of the set is not stored with it.
	ms.Set.Creator, ms.Set.Archived, ms.Set.InstalledDate = true, true, mtproto.MakeFlagsInt32(5)
	seal()
	if ms.GetSet().GetCreator() || ms.GetSet().GetArchived() || ms.GetSet().GetInstalledDate() != nil {
		t.Errorf("an account's view sealed into the set: %v", ms.GetSet())
	}
	if e := New(1, 2, "E", "e", installed.Emojis, true); !e.GetSet().GetEmojis() || !e.GetSet().GetTextColor() || Limit(installed.Emojis) != 200 || Limit(installed.Masks) != 120 {
		t.Errorf("a set of custom emoji: %v", e.GetSet())
	}
}

// Short names are those of core.telegram.org/method/stickers.createStickerSet,
// and one is made of any title.
func TestNames(t *testing.T) {
	for name, want := range map[string]bool{
		"Cats": true, "c": true, "cats_2_go": true, "a123": true,
		"": false, "1cats": false, "_cats": false, "ca__ts": false, "котики": false, "ca ts": false, "ca-ts": false,
		"a123456789012345678901234567890123456789012345678901234567890123":  true,
		"a1234567890123456789012345678901234567890123456789012345678901234": false,
	} {
		if got := ValidShortName(name); got != want {
			t.Errorf("ValidShortName(%q) = %v", name, got)
		}
	}
	for title, want := range map[string]string{
		"My Cats!":       "My_Cats",
		"  2 fast  ":     "fast",
		"Котики и Щенки": "Kotiki_i_Schenki",
		"😺😺":             "",
		"a__b":           "a_b",
	} {
		if got := ShortNameOf(title, ""); got != want {
			t.Errorf("ShortNameOf(%q) = %q, want %q", title, got, want)
		}
		if got := ShortNameOf(title, ""); got != "" && !ValidShortName(got) {
			t.Errorf("ShortNameOf(%q) = %q is no short name", title, got)
		}
	}
	long := ShortNameOf("a123456789012345678901234567890123456789012345678901234567890123456789", "42")
	if len(long) != MaxShortName || !ValidShortName(long) || long[len(long)-3:] != "_42" {
		t.Errorf("a long title with a suffix: %q", long)
	}
	if ValidTitle("") || ValidTitle("   ") || !ValidTitle("Котики") || ValidTitle(string(make([]rune, 65))) {
		t.Errorf("ValidTitle")
	}
}

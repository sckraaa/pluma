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

// Telegram's answers of 2026-11-02 (public lists, no account's data):
// messages.getTopReactions (emoji only) and messages.getDefaultTagReactions
// (custom emoji only), with the hashes Telegram gave them. Their hashes
// count no id of Telegram's that this server would renumber in the emoji
// list, so reactionsHash must give exactly Telegram's.
const topEmoji = "❤ 👍 👎 🔥 🥰 👏 😁 🤔 🤯 😱 🤬 😢 🎉 🤩 🤮 💩 🙏 👌 🕊 🤡 🥱 🥴 😍 🐳 ❤‍🔥 🌚 🌭 💯 🤣 ⚡ 🍌 🏆 💔 🤨 😐 🍓 🍾 💋 🖕 😈 😴 😭 🤓 👻 👨‍💻 👀 🎃 🙈 😇 😨 🤝 ✍ 🤗 🫡 🎅 🎄 ☃ 💅 🤪 🗿 🆒 💘 🙉 🦄 😘 💊 🙊 😎 👾 🤷‍♂ 🤷 🤷‍♀ 😡"

var tagEmoji = []int64{
	5435957248314579621, 5470060791883374114, 5472146462362048818, 5431897022456145283,
	5420315771991497307, 5431449001532594346, 5433982607035474385, 5467666648263564704,
	5373052667671093676, 5361600266225326825, 5373330964372004748, 5371074117971745503,
	5359785904535774578, 5373098009640836781, 5431721976769027887, 5427009714745517609,
}

func TestReactionsHashIsTelegrams(t *testing.T) {
	var top []*mtproto.Reaction
	for _, e := range strings.Fields(topEmoji) {
		top = append(top, mtproto.MakeTLReactionEmoji(&mtproto.Reaction{Emoticon: e}).To_Reaction())
	}
	if h := reactionsHash(&mtproto.Messages_Reactions{Reactions: top}); h != -5948247740602425726 {
		t.Errorf("top reactions: %d, Telegram's -5948247740602425726", h)
	}
	var tags []*mtproto.Reaction
	for _, id := range tagEmoji {
		tags = append(tags, mtproto.MakeTLReactionCustomEmoji(&mtproto.Reaction{DocumentId: id}).To_Reaction())
	}
	if h := reactionsHash(&mtproto.Messages_Reactions{Reactions: tags}); h != -2068317817079727459 {
		t.Errorf("tag reactions: %d, Telegram's -2068317817079727459", h)
	}
}

func TestFeaturedAndWallPapersHash(t *testing.T) {
	set := func(id int64) *mtproto.StickerSetCovered {
		return mtproto.MakeTLStickerSetCovered(&mtproto.StickerSetCovered{Set: mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: id}).To_StickerSet()}).To_StickerSetCovered()
	}
	f := &mtproto.Messages_FeaturedStickers{Sets: []*mtproto.StickerSetCovered{set(10), set(20)}, Unread: []int64{20}}
	want := int64(hashUpdate(hashUpdate(hashUpdate(0, 10), 20), 1))
	if h := featuredHash(f); h != want {
		t.Errorf("featured %d, want %d", h, want)
	}
	w := &mtproto.Account_WallPapers{Wallpapers: []*mtproto.WallPaper{{Id: 7}, {Id: -1}, {Id: 9}}}
	if h, want := wallPapersHash(w), int64(hashUpdate(hashUpdate(0, 7), 9)); h != want {
		t.Errorf("wallpapers %d, want %d", h, want)
	}
	if digestHash(&mtproto.EmojiList{DocumentId: []int64{1}}) == digestHash(&mtproto.EmojiList{DocumentId: []int64{2}}) {
		t.Error("two lists, one digest")
	}
	if int32Hash(1<<40|5) != 5 {
		t.Error("int32Hash")
	}
}

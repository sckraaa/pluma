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

package catalog

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestKindOf(t *testing.T) {
	for want, in := range map[string]*mtproto.InputStickerSet{
		KindAnimatedEmoji:        mtproto.MakeTLInputStickerSetAnimatedEmoji(nil).To_InputStickerSet(),
		KindEmojiDefaultStatuses: mtproto.MakeTLInputStickerSetEmojiDefaultStatuses(nil).To_InputStickerSet(),
		"inputStickerSetDice:🎲":  mtproto.MakeTLInputStickerSetDice(&mtproto.InputStickerSet{Emoticon: "🎲"}).To_InputStickerSet(),
		"":                       mtproto.MakeTLInputStickerSetID(&mtproto.InputStickerSet{Id: 1, AccessHash: 2}).To_InputStickerSet(),
	} {
		if got := KindOf(in); got != want {
			t.Errorf("%s: %q, want %q", in.GetPredicateName(), got, want)
		}
	}
	if KindOf(mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: "x"}).To_InputStickerSet()) != "" ||
		KindOf(mtproto.MakeTLInputStickerSetDice(nil).To_InputStickerSet()) != "" || KindOf(nil) != "" {
		t.Error("a set by name, a die without its emoticon or nothing has a kind")
	}
}

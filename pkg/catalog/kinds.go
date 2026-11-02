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
	"github.com/teamgram/proto/mtproto"
)

// The kinds of sets clients ask for by kind, not by id or name: the
// predicates of their InputStickerSet (layer 228). A die is a kind per
// emoticon (KindOf).
const (
	KindAnimatedEmoji               = mtproto.Predicate_inputStickerSetAnimatedEmoji
	KindAnimatedEmojiAnimations     = mtproto.Predicate_inputStickerSetAnimatedEmojiAnimations
	KindPremiumGifts                = mtproto.Predicate_inputStickerSetPremiumGifts
	KindEmojiGenericAnimations      = mtproto.Predicate_inputStickerSetEmojiGenericAnimations
	KindEmojiDefaultStatuses        = mtproto.Predicate_inputStickerSetEmojiDefaultStatuses
	KindEmojiDefaultTopicIcons      = mtproto.Predicate_inputStickerSetEmojiDefaultTopicIcons
	KindEmojiChannelDefaultStatuses = mtproto.Predicate_inputStickerSetEmojiChannelDefaultStatuses
	KindTonGifts                    = mtproto.Predicate_inputStickerSetTonGifts
)

// KindOf is the kind of a set asked for by kind, "" for a set asked for
// by id or short name (or none).
func KindOf(in *mtproto.InputStickerSet) string {
	switch p := in.GetPredicateName(); p {
	case KindAnimatedEmoji, KindAnimatedEmojiAnimations, KindPremiumGifts, KindEmojiGenericAnimations,
		KindEmojiDefaultStatuses, KindEmojiDefaultTopicIcons, KindEmojiChannelDefaultStatuses, KindTonGifts:
		return p
	case mtproto.Predicate_inputStickerSetDice:
		if in.GetEmoticon() == "" {
			return ""
		}
		return p + ":" + in.GetEmoticon()
	default:
		return ""
	}
}

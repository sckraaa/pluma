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
	"github.com/teamgram/proto/mtproto"
)

// The answers of the methods while the catalog has nothing for them, and
// to the methods of an account's own lists (installed, recent, faved),
// which are kept from stage 2 on: what Telegram answers an account with
// none, hash 0.

func emptyReactions() *mtproto.Messages_Reactions {
	return mtproto.MakeTLMessagesReactions(&mtproto.Messages_Reactions{Reactions: []*mtproto.Reaction{}}).To_Messages_Reactions()
}

func emptyFeatured() *mtproto.Messages_FeaturedStickers {
	return mtproto.MakeTLMessagesFeaturedStickers(&mtproto.Messages_FeaturedStickers{
		Sets:   []*mtproto.StickerSetCovered{},
		Unread: []int64{},
	}).To_Messages_FeaturedStickers()
}

// featuredNotModified carries the count of the sets, as
// messages.featuredStickersNotModified has it.
func featuredNotModified(answer *mtproto.Messages_FeaturedStickers) *mtproto.Messages_FeaturedStickers {
	return mtproto.MakeTLMessagesFeaturedStickersNotModified(&mtproto.Messages_FeaturedStickers{
		Count: answer.GetCount(),
	}).To_Messages_FeaturedStickers()
}

func emptyEmojiGroups() *mtproto.Messages_EmojiGroups {
	return mtproto.MakeTLMessagesEmojiGroups(&mtproto.Messages_EmojiGroups{Groups: []*mtproto.EmojiGroup{}}).To_Messages_EmojiGroups()
}

func emptyPeerColors() *mtproto.Help_PeerColors {
	return mtproto.MakeTLHelpPeerColors(&mtproto.Help_PeerColors{Colors: []*mtproto.Help_PeerColorOption{}}).To_Help_PeerColors()
}

func emptyThemes() *mtproto.Account_Themes {
	return mtproto.MakeTLAccountThemes(&mtproto.Account_Themes{Themes: []*mtproto.Theme{}}).To_Account_Themes()
}

func emptyEmojiStatuses() *mtproto.Account_EmojiStatuses {
	return mtproto.MakeTLAccountEmojiStatuses(&mtproto.Account_EmojiStatuses{Statuses: []*mtproto.EmojiStatus{}}).To_Account_EmojiStatuses()
}

func emptyEmojiList() *mtproto.EmojiList {
	return mtproto.MakeTLEmojiList(&mtproto.EmojiList{DocumentId: []int64{}}).To_EmojiList()
}

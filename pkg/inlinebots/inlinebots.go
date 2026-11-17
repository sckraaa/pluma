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

// Package inlinebots is the registry of the inline bots the server answers
// for itself (task 099, ADR 0062): @gif and @pic. They have no token and no
// bot process; their accounts come from
// teamgramd/deploy/sql/z_pluma-inline-bots.sql, and the clients find them by
// the usernames of help.getConfig (gif_search_username and
// img_search_username in teamgramd/bin/config.json).
package inlinebots

// Kind is what a bot of the registry answers with.
type Kind int

const (
	// KindGif: animations, botInlineMediaResult with a document.
	KindGif Kind = iota + 1
	// KindPhoto: pictures, botInlineMediaResult with a photo.
	KindPhoto
)

// String is the name of the kind, as metrics label it.
func (k Kind) String() string {
	switch k {
	case KindGif:
		return "gif"
	case KindPhoto:
		return "photo"
	}
	return "unknown"
}

// Bot is a bot of the registry.
type Bot struct {
	Id       int64
	Username string
	Kind     Kind
	// About is the key of the bot's description in
	// pkg/langpack/inlinebots.json.
	About string
}

// The bots. The ids are in the range of Pluma's service accounts (the
// antispam account is 1000001, pkg/antispam); the ids of users start at
// 136907713 (AUTO_INCREMENT of users in 1_teamgram.sql).
var (
	Gif = Bot{Id: 1000101, Username: "gif", Kind: KindGif, About: "PlumaInlineGifAbout"}
	Pic = Bot{Id: 1000102, Username: "pic", Kind: KindPhoto, About: "PlumaInlinePicAbout"}
)

// placeholder is the bot_inline_placeholder of every bot: a symbol, not a
// word, because the user object is the same for every viewer and so cannot
// be in the viewer's language (ADR 0062, p. 6). iOS shows no GIF tab without
// a placeholder (GifContext.swift:65).
const placeholder = "🔍"

// GifSearchEmojies are the emoji categories of the GIF tab of the emoji
// panel (gif_search_emojies of the app config) while @gif searches by text:
// the default list of Android (MessagesController.java:1948-1957). Each is
// a query to @gif (Android EmojiView.java:9015, iOS
// ChatEntityKeyboardInputNode.swift:233, tdesktop
// stickers_list_footer.cpp:137); without a search the list is empty, as
// the categories would show empty lists (ADR 0062, p. 2).
var GifSearchEmojies = []string{"👍", "👎", "😍", "😂", "😮", "🙄", "😥", "😡", "🥳", "😎"}

// All returns the bots of the registry.
func All() []Bot {
	return []Bot{Gif, Pic}
}

// ByID returns the bot of the registry with the id.
func ByID(id int64) (Bot, bool) {
	for _, b := range All() {
		if b.Id == id {
			return b, true
		}
	}
	return Bot{}, false
}

// IsBuiltin reports whether id is a bot of the registry: such a bot is the
// server's, and the admin tool does not change it.
func IsBuiltin(id int64) bool {
	_, ok := ByID(id)
	return ok
}

// Placeholder returns the bot_inline_placeholder of the bots.
func Placeholder() string {
	return placeholder
}

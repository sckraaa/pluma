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

package langpack

import (
	_ "embed"
	"strings"
)

// The texts of the games of bots (task 106, ADR 0068 p. 7): the button the
// server puts under a game sent without one, "Play game_title" as
// core.telegram.org/bots/api#sendgame names it ("If empty, one 'Play
// game_title' button will be shown"). The message carries the text, so it
// is in the language of the session that sends the game. They are in every
// language of own.json (the test holds them to it).
//
// games.json has mail.json's shape: {key: {lang_code: value}}.
//
//go:embed games.json
var gamesJSON []byte

var gameTexts = mustLoadTexts("games.json", gamesJSON)

// gamePlayKey is the text of the game's button, with {title}.
const gamePlayKey = "PlumaGamePlay"

// GamePlayButton is the text of the button that starts the game of the
// title, in lang with MailText's fallbacks.
func GamePlayButton(title, lang string) string {
	return strings.ReplaceAll(pick(gameTexts[gamePlayKey], lang), "{title}", title)
}

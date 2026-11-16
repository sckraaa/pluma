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

// Package botgames makes the messages that carry a game of a bot (task 106,
// ADR 0068 p. 7, core.telegram.org/api/bots/games): it finds the game an
// InputGame names, turns a game of pkg/botstore into the TL game and gives
// the message the button that starts it. messages.sendMedia
// (inputMediaGame) and the inline results of games
// (inputBotInlineResultGame) send games through it.
package botgames

import (
	"context"
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/langpack"
)

// Store finds the games of bots (*botstore.Store).
type Store interface {
	GameByShortName(ctx context.Context, botId int64, shortName string) (*botstore.Game, error)
	GameById(ctx context.Context, id, accessHash int64) (*botstore.Game, error)
}

// Media gives the picture and the animation of a game (the media service).
type Media interface {
	MediaGetPhoto(ctx context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error)
	MediaGetDocument(ctx context.Context, in *media.TLMediaGetDocument) (*mtproto.Document, error)
}

// Sender is who sends the game: its id, whether it is a bot, and how the
// users an InputGame names are told apart from bots.
type Sender struct {
	Id    int64
	IsBot bool
	// BotOf tells whether the user is a bot that exists; false for a user
	// that is not one or does not exist.
	BotOf func(ctx context.Context, userId int64) (bool, error)
}

// Find returns the game in names, as sender may send it:
//   - inputGameShortName names the bot and the short name of its game. A
//     bot sends its own games ("The current bot's info to
//     inputGameShortName.bot_id", core.telegram.org/api/bots/games), another
//     bot's is GAME_BOT_INVALID. A user sends one by the link of a game
//     (t.me/<bot>?game=<short_name>), as every client does: Android
//     LaunchActivity.java:4562-4577, iOS ForwardGame.swift:25, tdesktop
//     window_peer_menu.cpp:150-195 (ShareBotGame), Web K
//     appGamesManager.ts:20-41.
//   - inputGameID is a game already sent, which users forward ("can then be
//     used by users to forward the game using sendMedia with inputGameID",
//     api/bots/games); a bot again only its own.
//
// A game that does not exist, or was removed, is MEDIA_INVALID: a removed
// game is no longer sent, while the messages that have it keep it.
func Find(ctx context.Context, store Store, in *mtproto.InputGame, sender Sender) (*botstore.Game, error) {
	var (
		game *botstore.Game
		err  error
	)
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputGameShortName:
		bot := mtproto.FromInputUser(sender.Id, in.GetBotId())
		if !bot.IsUser() {
			return nil, mtproto.ErrBotInvalid
		}
		if sender.IsBot && bot.PeerId != sender.Id {
			return nil, mtproto.ErrGameBotInvalid
		}
		if !sender.IsBot {
			ok, err := sender.BotOf(ctx, bot.PeerId)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, mtproto.ErrBotInvalid
			}
		}
		game, err = store.GameByShortName(ctx, bot.PeerId, in.GetShortName())
	case mtproto.Predicate_inputGameID:
		game, err = store.GameById(ctx, in.GetId(), in.GetAccessHash())
	default:
		return nil, mtproto.ErrMediaInvalid
	}
	if err != nil {
		return nil, err
	}
	if game == nil {
		return nil, mtproto.ErrMediaInvalid
	}
	if sender.IsBot && game.BotId != sender.Id {
		return nil, mtproto.ErrGameBotInvalid
	}
	return game, nil
}

// TL is the game as messages carry it (game#bdf9653b): its picture, a
// Photo the game must have, and its animation when it has one. A picture
// or an animation the media service no longer has is an error of the
// game's setup, not of the request.
func TL(ctx context.Context, m Media, g *botstore.Game) (*mtproto.Game, error) {
	photo, err := m.MediaGetPhoto(ctx, &media.TLMediaGetPhoto{PhotoId: g.PhotoId})
	if err != nil {
		return nil, fmt.Errorf("botgames: photo %d of game %d: %w", g.PhotoId, g.Id, err)
	}
	if photo.GetPredicateName() != mtproto.Predicate_photo {
		return nil, fmt.Errorf("botgames: game %d has no photo %d", g.Id, g.PhotoId)
	}
	out := &mtproto.Game{
		Id:          g.Id,
		AccessHash:  g.AccessHash,
		ShortName:   g.ShortName,
		Title:       g.Title,
		Description: g.Description,
		Photo:       photo,
	}
	if g.DocumentId != 0 {
		document, err := m.MediaGetDocument(ctx, &media.TLMediaGetDocument{Id: g.DocumentId})
		if err != nil {
			return nil, fmt.Errorf("botgames: document %d of game %d: %w", g.DocumentId, g.Id, err)
		}
		if document.GetPredicateName() != mtproto.Predicate_document {
			return nil, fmt.Errorf("botgames: game %d has no document %d", g.Id, g.DocumentId)
		}
		out.Document = document
	}
	return mtproto.MakeTLGame(out).To_Game(), nil
}

// MessageMedia is the media of a message with the game.
func MessageMedia(g *mtproto.Game) *mtproto.MessageMedia {
	return mtproto.MakeTLMessageMediaGame(&mtproto.MessageMedia{Game: g}).To_MessageMedia()
}

// Markup is the keyboard of a message with the game of the title, out of
// the markup the bot gave, already checked by botmarkup.Normalize with
// Env.Game. No markup, or one without buttons, becomes one button that
// starts the game, in lang: "If empty, one 'Play game_title' button will be
// shown" (core.telegram.org/bots/api#sendgame). A user's game always gets
// it: users send no keyboards. Otherwise the keyboard must be inline and
// start with the game's button ("If not empty, the first button must launch
// the game"), BUTTON_TYPE_INVALID when it does not, REPLY_MARKUP_INVALID
// for a reply keyboard. Clients need the button: tdesktop opens the game by
// the button at row 0, column 0 (history_view_game.cpp:58-69).
func Markup(markup *mtproto.ReplyMarkup, title, lang string) (*mtproto.ReplyMarkup, error) {
	if markup == nil || (markup.GetPredicateName() == mtproto.Predicate_replyInlineMarkup && !hasButtons(markup)) {
		return mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
			Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{
				Buttons: []*mtproto.KeyboardButton{
					mtproto.MakeTLKeyboardButtonGame(&mtproto.KeyboardButton{Text: langpack.GamePlayButton(title, lang)}).To_KeyboardButton(),
				},
			}).To_KeyboardButtonRow()},
		}).To_ReplyMarkup(), nil
	}
	if markup.GetPredicateName() != mtproto.Predicate_replyInlineMarkup {
		return nil, mtproto.ErrReplyMarkupInvalid
	}
	if !HasButton(markup) {
		return nil, mtproto.ErrButtonTypeInvalid
	}
	return markup, nil
}

// Check is what Markup checks of a keyboard a bot gives a game before it
// is sent, when the button is put in later, in the sender's language.
func Check(markup *mtproto.ReplyMarkup) error {
	_, err := Markup(markup, "", "")
	return err
}

// HasButton tells whether the keyboard starts with the game's button.
func HasButton(markup *mtproto.ReplyMarkup) bool {
	if markup.GetPredicateName() != mtproto.Predicate_replyInlineMarkup {
		return false
	}
	rows := markup.GetRows()
	return len(rows) > 0 && len(rows[0].GetButtons()) > 0 &&
		rows[0].GetButtons()[0].GetPredicateName() == mtproto.Predicate_keyboardButtonGame
}

func hasButtons(markup *mtproto.ReplyMarkup) bool {
	for _, row := range markup.GetRows() {
		if len(row.GetButtons()) > 0 {
			return true
		}
	}
	return false
}

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
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// A game result sends its game and nothing else; another result sends no
// game.
func TestOutOfGameResult(t *testing.T) {
	game := mtproto.MakeTLGame(&mtproto.Game{Id: 7, AccessHash: 70, ShortName: "jump", Title: "Jump",
		Photo: mtproto.MakeTLPhoto(&mtproto.Photo{Id: 5}).To_Photo()}).To_Game()
	auto := mtproto.MakeTLBotInlineMessageMediaAuto(&mtproto.BotInlineMessage{}).To_BotInlineMessage()
	result := mtproto.MakeTLBotInlineMediaResult(&mtproto.BotInlineResult{Id: "g", Type: "game", Photo: game.GetPhoto(), SendMessage: auto}).To_BotInlineResult()

	out, err := outOfResult(result, game)
	if err != nil {
		t.Fatal(err)
	}
	if out.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaGame || out.GetMedia().GetGame().GetId() != 7 {
		t.Fatalf("media %v", out.GetMedia())
	}
	if _, err = outOfResult(result, nil); !errors.Is(err, mtproto.ErrResultTypeInvalid) {
		t.Errorf("a game result without its game: %v", err)
	}
	photo := mtproto.MakeTLBotInlineMediaResult(&mtproto.BotInlineResult{Id: "p", Type: "photo", Photo: game.GetPhoto(), SendMessage: auto}).To_BotInlineResult()
	if _, err = outOfResult(photo, game); !errors.Is(err, mtproto.ErrResultTypeInvalid) {
		t.Errorf("a photo result with a game: %v", err)
	}
	if out, err = outOfResult(photo, nil); err != nil || out.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaPhoto {
		t.Errorf("a photo result: %v %v", out, err)
	}
}

// A result of a game is refused where only plain results go (the answer of
// a web view): its game would be lost.
func TestInlineResultRefusesGames(t *testing.T) {
	c := testCore(&dao.Dao{}, 20)
	r := mtproto.MakeTLInputBotInlineResultGame(&mtproto.InputBotInlineResult{Id: "g", ShortName: "jump",
		SendMessage: mtproto.MakeTLInputBotInlineMessageGame(&mtproto.InputBotInlineMessage{}).To_InputBotInlineMessage()}).To_InputBotInlineResult()
	if _, err := c.inlineResult(r); !errors.Is(err, mtproto.ErrSendMessageTypeInvalid) && !errors.Is(err, mtproto.ErrResultTypeInvalid) {
		t.Errorf("a game result: %v", err)
	}
	// Nor is a result that only calls itself a game: clients would show it
	// as one, and it could not be sent.
	article := mtproto.MakeTLInputBotInlineResult(&mtproto.InputBotInlineResult{Id: "a", Type: "game",
		SendMessage: mtproto.MakeTLInputBotInlineMessageText(&mtproto.InputBotInlineMessage{Message: "x"}).To_InputBotInlineMessage()}).To_InputBotInlineResult()
	if _, err := c.inlineResult(article); !errors.Is(err, mtproto.ErrResultTypeInvalid) {
		t.Errorf("an article of type game: %v", err)
	}
}

// One message per score of a player in a game message, a new one for
// another score.
func TestScoreRandomId(t *testing.T) {
	chat := botstore.ScoreChat{Type: mtproto.PEER_CHAT, Id: 77}
	a := scoreRandomId(chat, 4, 7, 10, 30)
	if a != scoreRandomId(chat, 4, 7, 10, 30) || a <= 0 {
		t.Fatalf("not stable: %d", a)
	}
	for name, b := range map[string]int64{
		"another score":   scoreRandomId(chat, 4, 7, 10, 31),
		"another player":  scoreRandomId(chat, 4, 7, 11, 30),
		"another message": scoreRandomId(chat, 5, 7, 10, 30),
		"another chat":    scoreRandomId(botstore.ScoreChat{Type: mtproto.PEER_CHAT, Id: 78}, 4, 7, 10, 30),
	} {
		if a == b {
			t.Errorf("%s: the same random id", name)
		}
	}
}

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

package botgames

import (
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

const (
	gameBot  = int64(20)
	otherBot = int64(21)
	player   = int64(10)
)

// games has the game "jump" of gameBot.
type games struct{}

var jump = botstore.Game{Id: 7, AccessHash: 70, BotId: gameBot, ShortName: "jump", Title: "Jump", Description: "Up", PhotoId: 5, DocumentId: 6}

func (games) GameByShortName(_ context.Context, botId int64, shortName string) (*botstore.Game, error) {
	if botId == jump.BotId && shortName == jump.ShortName {
		g := jump
		return &g, nil
	}
	return nil, nil
}

func (games) GameById(_ context.Context, id, accessHash int64) (*botstore.Game, error) {
	if id == jump.Id && accessHash == jump.AccessHash {
		g := jump
		return &g, nil
	}
	return nil, nil
}

func isBot(_ context.Context, id int64) (bool, error) { return id == gameBot || id == otherBot, nil }

func byName(bot int64, name string) *mtproto.InputGame {
	return mtproto.MakeTLInputGameShortName(&mtproto.InputGame{
		BotId:     mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: bot}).To_InputUser(),
		ShortName: name,
	}).To_InputGame()
}

func byId(id, hash int64) *mtproto.InputGame {
	return mtproto.MakeTLInputGameID(&mtproto.InputGame{Id: id, AccessHash: hash}).To_InputGame()
}

func TestFind(t *testing.T) {
	ctx := context.Background()
	asBot := func(id int64) Sender { return Sender{Id: id, IsBot: true, BotOf: isBot} }
	asUser := Sender{Id: player, BotOf: isBot}
	self := mtproto.MakeTLInputGameShortName(&mtproto.InputGame{
		BotId:     mtproto.MakeTLInputUserSelf(nil).To_InputUser(),
		ShortName: "jump",
	}).To_InputGame()

	for name, tc := range map[string]struct {
		in     *mtproto.InputGame
		sender Sender
		want   error
	}{
		"the bot by its short name":       {byName(gameBot, "jump"), asBot(gameBot), nil},
		"the bot as inputUserSelf":        {self, asBot(gameBot), nil},
		"the bot by id":                   {byId(7, 70), asBot(gameBot), nil},
		"a user by the link of the game":  {byName(gameBot, "jump"), asUser, nil},
		"a user forwarding it by id":      {byId(7, 70), asUser, nil},
		"another bot by short name":       {byName(gameBot, "jump"), asBot(otherBot), mtproto.ErrGameBotInvalid},
		"another bot by id":               {byId(7, 70), asBot(otherBot), mtproto.ErrGameBotInvalid},
		"a user naming a user as the bot": {byName(player, "jump"), asUser, mtproto.ErrBotInvalid},
		"an unknown short name":           {byName(gameBot, "fly"), asUser, mtproto.ErrMediaInvalid},
		"a wrong access hash":             {byId(7, 71), asUser, mtproto.ErrMediaInvalid},
		"no game":                         {&mtproto.InputGame{}, asUser, mtproto.ErrMediaInvalid},
	} {
		g, err := Find(ctx, games{}, tc.in, tc.sender)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
			continue
		}
		if tc.want == nil && (g == nil || g.Id != jump.Id) {
			t.Errorf("%s: game %+v", name, g)
		}
	}
}

// files has photo 5 and document 6.
type files struct{}

func (files) MediaGetPhoto(_ context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error) {
	if in.PhotoId != 5 {
		return mtproto.MakeTLPhotoEmpty(&mtproto.Photo{Id: in.PhotoId}).To_Photo(), nil
	}
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: 5, AccessHash: 50}).To_Photo(), nil
}

func (files) MediaGetDocument(_ context.Context, in *media.TLMediaGetDocument) (*mtproto.Document, error) {
	if in.Id != 6 {
		return mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: in.Id}).To_Document(), nil
	}
	return mtproto.MakeTLDocument(&mtproto.Document{Id: 6, AccessHash: 60}).To_Document(), nil
}

func TestTL(t *testing.T) {
	ctx := context.Background()
	g, err := TL(ctx, files{}, &jump)
	if err != nil {
		t.Fatal(err)
	}
	if g.GetPredicateName() != mtproto.Predicate_game || g.GetId() != 7 || g.GetAccessHash() != 70 || g.GetShortName() != "jump" ||
		g.GetTitle() != "Jump" || g.GetDescription() != "Up" || g.GetPhoto().GetId() != 5 || g.GetDocument().GetId() != 6 {
		t.Fatalf("game %v", g)
	}
	if m := MessageMedia(g); m.GetPredicateName() != mtproto.Predicate_messageMediaGame || m.GetGame() != g {
		t.Fatalf("media %v", m)
	}
	noAnimation := jump
	noAnimation.DocumentId = 0
	if g, err = TL(ctx, files{}, &noAnimation); err != nil || g.GetDocument() != nil {
		t.Fatalf("without an animation: %v %v", g, err)
	}
	lost := jump
	lost.PhotoId = 9
	if _, err = TL(ctx, files{}, &lost); err == nil {
		t.Fatal("a game whose photo is gone")
	}
}

func row(buttons ...*mtproto.KeyboardButton) *mtproto.KeyboardButtonRow {
	return mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: buttons}).To_KeyboardButtonRow()
}

func inline(rows ...*mtproto.KeyboardButtonRow) *mtproto.ReplyMarkup {
	return mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{Rows: rows}).To_ReplyMarkup()
}

func TestMarkup(t *testing.T) {
	play := mtproto.MakeTLKeyboardButtonGame(&mtproto.KeyboardButton{Text: "Go!"}).To_KeyboardButton()
	link := mtproto.MakeTLKeyboardButtonUrl(&mtproto.KeyboardButton{Text: "Site", Url: "https://example.com"}).To_KeyboardButton()

	for name, m := range map[string]*mtproto.ReplyMarkup{"none": nil, "an empty keyboard": inline(row())} {
		got, err := Markup(m, "Jump", "ru")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !HasButton(got) || len(got.GetRows()) != 1 || len(got.GetRows()[0].GetButtons()) != 1 ||
			got.GetRows()[0].GetButtons()[0].GetText() != "Играть в Jump" {
			t.Fatalf("%s: %v", name, got)
		}
	}
	own := inline(row(play, link))
	if got, err := Markup(own, "Jump", "en"); err != nil || got != own {
		t.Fatalf("the bot's own keyboard: %v %v", got, err)
	}
	if _, err := Markup(inline(row(link, play)), "Jump", "en"); !errors.Is(err, mtproto.ErrButtonTypeInvalid) {
		t.Errorf("a keyboard that does not start with the game: %v", err)
	}
	hide := mtproto.MakeTLReplyKeyboardHide(&mtproto.ReplyMarkup{}).To_ReplyMarkup()
	if _, err := Markup(hide, "Jump", "en"); !errors.Is(err, mtproto.ErrReplyMarkupInvalid) {
		t.Errorf("a reply keyboard: %v", err)
	}
	if err := Check(nil); err != nil {
		t.Errorf("Check of none: %v", err)
	}
	if err := Check(inline(row(link))); !errors.Is(err, mtproto.ErrButtonTypeInvalid) {
		t.Errorf("Check of a keyboard without the game: %v", err)
	}
}

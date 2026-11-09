//go:build integration

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

package botstore

import (
	"context"
	"errors"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

func TestStoreOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	conf := mysql.Config()
	store := New(sqlx.NewMySQL(&conf))

	id, token, err := store.Create(ctx, "keeper_bot", "Keeper", 7)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.BotIdOfToken(ctx, token); err != nil || got != id {
		t.Fatalf("token: %d, %v", got, err)
	}
	if got, err := store.BotIdOfToken(ctx, "1:nope"); err != nil || got != 0 {
		t.Fatalf("wrong token: %d, %v", got, err)
	}
	if _, _, err := store.Create(ctx, "keeper_bot", "Other", 0); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("repeat: %v", err)
	}

	next, err := store.NewToken(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := store.BotIdOfToken(ctx, token); got != 0 {
		t.Error("the old token still works")
	}
	if got, _ := store.BotIdOfToken(ctx, next); got != id {
		t.Error("the new token does not work")
	}
	if ids, err := store.Admined(ctx, 7); err != nil || len(ids) != 1 || ids[0] != id {
		t.Errorf("admined: %v, %v", ids, err)
	}

	// The commands of the language and the fallback, and the order of scopes.
	cmd := func(c string) []*mtproto.BotCommand {
		return []*mtproto.BotCommand{mtproto.MakeTLBotCommand(&mtproto.BotCommand{Command: c, Description: "d"}).To_BotCommand()}
	}
	if err = store.SetCommands(ctx, id, scopeDefault, "", cmd("all")); err != nil {
		t.Fatal(err)
	}
	if err = store.SetCommands(ctx, id, scopeUsers, "de", cmd("nur")); err != nil {
		t.Fatal(err)
	}
	info, err := store.BotInfo(ctx, id, Viewer{UserId: 9, PeerType: mtproto.PEER_USER, PeerId: 9, Lang: "de"})
	if err != nil || info.GetCommands()[0].GetCommand() != "nur" {
		t.Fatalf("de: %v, %v", info.GetCommands(), err)
	}
	info, _ = store.BotInfo(ctx, id, Viewer{UserId: 9, PeerType: mtproto.PEER_USER, PeerId: 9, Lang: "fr"})
	if info.GetCommands()[0].GetCommand() != "all" {
		t.Errorf("fr falls to the default: %v", info.GetCommands())
	}
	if err = store.ResetCommands(ctx, id, scopeUsers, "de"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Commands(ctx, id, scopeUsers, "de"); len(got) != 0 {
		t.Errorf("after reset: %v", got)
	}

	// Texts: another language beside the default description.
	desc, about := "beschreibung", "kurz"
	if err = store.SetTexts(ctx, id, "de", Texts{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	if err = store.SetTexts(ctx, id, "de", Texts{About: &about}); err != nil {
		t.Fatal(err)
	}
	texts, err := store.LangTexts(ctx, id, "de")
	if err != nil || texts.Description == nil || *texts.Description != desc || texts.About == nil || *texts.About != about {
		t.Errorf("texts: %+v, %v", texts, err)
	}
	info, _ = store.BotInfo(ctx, id, Viewer{UserId: 9, PeerType: mtproto.PEER_USER, PeerId: 9, Lang: "de"})
	if info.GetDescription_STRING() != desc {
		t.Errorf("description: %q", info.GetDescription_STRING())
	}

	// A user's button beats the default of all, and it can be removed.
	all := mtproto.MakeTLBotMenuButton(&mtproto.BotMenuButton{Text: "Open", Url: "https://a.b"}).To_BotMenuButton()
	if err = store.SetMenuButton(ctx, id, 0, all); err != nil {
		t.Fatal(err)
	}
	if err = store.SetMenuButton(ctx, id, 9, mtproto.MakeTLBotMenuButtonCommands(nil).To_BotMenuButton()); err != nil {
		t.Fatal(err)
	}
	if b, _ := store.MenuButton(ctx, id, 9); b.GetPredicateName() != mtproto.Predicate_botMenuButtonCommands {
		t.Errorf("own button: %s", b.GetPredicateName())
	}
	if b, _ := store.MenuButton(ctx, id, 10); b.GetPredicateName() != mtproto.Predicate_botMenuButton {
		t.Errorf("default button: %s", b.GetPredicateName())
	}
	if err = store.SetMenuButton(ctx, id, 9, mtproto.MakeTLBotMenuButtonDefault(nil).To_BotMenuButton()); err != nil {
		t.Fatal(err)
	}
	if b, _ := store.MenuButton(ctx, id, 9); b.GetPredicateName() != mtproto.Predicate_botMenuButton {
		t.Errorf("after default: %s", b.GetPredicateName())
	}
	if err = store.SetMenuButton(ctx, id, 0, mtproto.MakeTLBotMenuButton(&mtproto.BotMenuButton{Text: "Open", Url: "http://a.b"}).To_BotMenuButton()); !errors.Is(err, mtproto.ErrButtonUrlInvalid) {
		t.Errorf("http url: %v", err)
	}
}

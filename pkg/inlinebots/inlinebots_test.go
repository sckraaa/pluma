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

package inlinebots

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestByID(t *testing.T) {
	for _, want := range []Bot{Gif, Pic, Places} {
		got, ok := ByID(want.Id)
		if !ok || got != want || !IsBuiltin(want.Id) {
			t.Errorf("%d: %+v %v", want.Id, got, ok)
		}
	}
	for _, id := range []int64{0, 1000001, 1000104, 101, 136907713} {
		if _, ok := ByID(id); ok || IsBuiltin(id) {
			t.Errorf("%d is a built-in bot", id)
		}
	}
	if !Places.Geo || Gif.Geo || Pic.Geo {
		t.Error("only @places asks for the location")
	}
	if Gif.Kind.String() != "gif" || Pic.Kind.String() != "photo" || Places.Kind.String() != "venue" {
		t.Error("kinds")
	}
	if Placeholder() == "" {
		t.Error("no placeholder: iOS shows no GIF tab without one")
	}
	// All is a copy: a caller cannot change the registry.
	All()[0].Username = "changed"
	if Gif.Username != "gif" || All()[0].Username != "gif" {
		t.Error("All shares the registry")
	}
}

func repoFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The clients take the usernames from help.getConfig and the app config;
// they must be the registry's (Android MessagesController.java:5688-5693,
// iOS AccountContext.swift:2039-2042).
func TestUsernamesOfTheConfigs(t *testing.T) {
	var config struct {
		Data2 map[string]json.RawMessage `json:"data2"`
	}
	if err := json.Unmarshal(repoFile(t, "teamgramd", "bin", "config.json"), &config); err != nil {
		t.Fatal(err)
	}
	for field, b := range map[string]Bot{"gif_search_username": Gif, "img_search_username": Pic, "venue_search_username": Places} {
		var v struct{ Value string }
		if err := json.Unmarshal(config.Data2[field], &v); err != nil || v.Value != b.Username {
			t.Errorf("config.json %s = %q (%v), want %q", field, v.Value, err, b.Username)
		}
	}
	var app map[string]json.RawMessage
	if err := json.Unmarshal(repoFile(t, "teamgramd", "bin", "app_config.json"), &app); err != nil {
		t.Fatal(err)
	}
	var venue string
	if err := json.Unmarshal(app["stories_venue_search_username"], &venue); err != nil || venue != Places.Username {
		t.Errorf("app_config.json stories_venue_search_username = %q (%v)", venue, err)
	}
	// No text search behind @gif yet: no emoji categories (ADR 0062, p. 2).
	var emojies []string
	if err := json.Unmarshal(app["gif_search_emojies"], &emojies); err != nil || emojies == nil || len(emojies) != 0 {
		t.Errorf("app_config.json gif_search_emojies = %s (%v), want []", app["gif_search_emojies"], err)
	}
}

// The accounts of z_pluma-inline-bots.sql are the registry's.
func TestAccountsFile(t *testing.T) {
	sql := string(repoFile(t, "teamgramd", "deploy", "sql", "z_pluma-inline-bots.sql"))
	for _, b := range All() {
		re := regexp.MustCompile(`SELECT ` + regexp.QuoteMeta(itoa(b.Id)) + `, 3, \d+, '[^']+', '` + regexp.QuoteMeta(b.Username) + `', '`)
		if !re.MatchString(sql) {
			t.Errorf("no account of @%s %d", b.Username, b.Id)
		}
		if !strings.Contains(sql, "username = '"+b.Username+"' AND NOT (peer_type = 2 AND peer_id = "+itoa(b.Id)+")") {
			t.Errorf("@%s is not guarded by its username", b.Username)
		}
	}
	if !strings.Contains(sql, "IF(id = "+itoa(Places.Id)+", 1, 0)") {
		t.Error("bot_inline_geo of @places")
	}
}

func itoa(id int64) string {
	b, _ := json.Marshal(id)
	return string(b)
}

func goodAccount(b Bot) account {
	return account{
		user:     &userRow{Id: b.Id, IsBot: true},
		bot:      &botRow{BotId: b.Id, Placeholder: Placeholder(), Geo: b.Geo},
		username: &usernameRow{Username: b.Username, PeerType: peerUser, PeerId: b.Id},
	}
}

func TestCheckAccount(t *testing.T) {
	for _, b := range All() {
		if err := b.check(goodAccount(b)); err != nil {
			t.Errorf("@%s: %v", b.Username, err)
		}
	}
	b := Places
	for name, change := range map[string]func(a *account){
		"no username":           func(a *account) { a.username = nil },
		"username of a person":  func(a *account) { a.username.PeerId = 136907800 },
		"username of a channel": func(a *account) { a.username.PeerType = 4 },
		"username deleted":      func(a *account) { a.username.Deleted = true },
		"no account":            func(a *account) { a.user = nil },
		"not a bot":             func(a *account) { a.user.IsBot = false },
		"account deleted":       func(a *account) { a.user.Deleted = true },
		"no bots row":           func(a *account) { a.bot = nil },
		"no placeholder":        func(a *account) { a.bot.Placeholder = "" },
		"no location":           func(a *account) { a.bot.Geo = false },
	} {
		a := goodAccount(b)
		change(&a)
		if err := b.check(a); err == nil {
			t.Errorf("%s: in order", name)
		}
	}
	a := goodAccount(Gif)
	a.bot.Geo = true
	if Gif.check(a) == nil {
		t.Error("@gif asking for the location: in order")
	}
}

// fakeDB answers the three queries of load with its rows, or fails.
type fakeDB struct {
	accounts map[int64]account
	err      error
}

func (f *fakeDB) QueryRowsPartial(_ context.Context, v interface{}, _ string, _ ...interface{}) error {
	if f.err != nil {
		return f.err
	}
	for _, a := range f.accounts {
		switch rows := v.(type) {
		case *[]userRow:
			if a.user != nil {
				*rows = append(*rows, *a.user)
			}
		case *[]botRow:
			if a.bot != nil {
				*rows = append(*rows, *a.bot)
			}
		case *[]usernameRow:
			if a.username != nil {
				*rows = append(*rows, *a.username)
			}
		}
	}
	return nil
}

func TestHealth(t *testing.T) {
	ctx := context.Background()
	h := NewHealth()
	// Before a check, nothing is served.
	if h.Usable(Gif.Id) || h.Verified() {
		t.Fatal("served before a check")
	}
	// A database that cannot be read: still nothing.
	down := errors.New("mysql is down")
	if err := h.Check(ctx, &fakeDB{err: down}); !errors.Is(err, down) || h.Usable(Gif.Id) {
		t.Fatalf("database down: %v, served %v", err, h.Usable(Gif.Id))
	}

	db := &fakeDB{accounts: map[int64]account{}}
	for _, b := range All() {
		db.accounts[b.Id] = goodAccount(b)
	}
	// The username of @places is a person's, in other letters.
	db.accounts[Places.Id] = account{
		user:     db.accounts[Places.Id].user,
		bot:      db.accounts[Places.Id].bot,
		username: &usernameRow{Username: "PLACES", PeerType: peerUser, PeerId: 136907800},
	}
	if err := h.Check(ctx, db); !errors.Is(err, ErrMisconfigured) {
		t.Fatalf("check: %v", err)
	}
	if !h.Usable(Gif.Id) || !h.Usable(Pic.Id) || h.Usable(Places.Id) || h.Verified() {
		t.Fatalf("gif %v, pic %v, places %v, verified %v", h.Usable(Gif.Id), h.Usable(Pic.Id), h.Usable(Places.Id), h.Verified())
	}
	// A database that fails later keeps what the last check found.
	if err := h.Check(ctx, &fakeDB{err: down}); !errors.Is(err, down) || !h.Usable(Gif.Id) {
		t.Fatalf("database down later: %v, served %v", err, h.Usable(Gif.Id))
	}
	// Repaired: all served.
	db.accounts[Places.Id] = goodAccount(Places)
	if err := h.Check(ctx, db); err != nil || !h.Usable(Places.Id) || !h.Verified() {
		t.Fatalf("repaired: %v, places %v, verified %v", err, h.Usable(Places.Id), h.Verified())
	}
}

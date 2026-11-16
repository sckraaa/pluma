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
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	_ "github.com/go-sql-driver/mysql"
)

const platformMigration = "migrate-20261121-bot-platform.sql"

// platformVersions reads the versions of a bot that its platform changes
// raise.
func platformVersions(t *testing.T, s *Store, botId int64) (platform, info int32) {
	t.Helper()
	var rows []struct {
		Platform int32 `db:"platform_version"`
		Info     int32 `db:"bot_info_version"`
	}
	if err := s.db.QueryRowsPartial(context.Background(), &rows, "SELECT platform_version, bot_info_version FROM bots WHERE bot_id = ?", botId); err != nil || len(rows) != 1 {
		t.Fatalf("versions of %d: %v, %v", botId, rows, err)
	}
	return rows[0].Platform, rows[0].Info
}

func TestPlatformOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	conf := mysql.Config()
	s := New(sqlx.NewMySQL(&conf))

	newBot := func(t *testing.T, username string) int64 {
		t.Helper()
		id, _, err := s.Create(ctx, username, "Bot", 0)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	t.Run("apps", func(t *testing.T) {
		bot := newBot(t, "apps_bot")
		p0, i0 := platformVersions(t, s, bot)
		app := App{BotId: bot, ShortName: "shop", Title: "Shop", Description: "d", URL: "https://shop.example", PhotoId: 9}
		got, err := s.AddApp(ctx, app)
		if err != nil {
			t.Fatal(err)
		}
		if got.Id == 0 || got.AccessHash == 0 || got.Version != 1 {
			t.Fatalf("added: %+v", got)
		}
		if p, i := platformVersions(t, s, bot); p != p0+1 || i != i0+1 {
			t.Errorf("versions after add: %d %d, were %d %d", p, i, p0, i0)
		}
		if _, err = s.AddApp(ctx, app); !errors.Is(err, ErrShortNameTaken) {
			t.Errorf("same name: %v", err)
		}
		if _, err = s.AddApp(ctx, App{BotId: bot, ShortName: "SHOP", Title: "x", URL: "https://a.example", PhotoId: 1}); !errors.Is(err, ErrShortNameTaken) {
			t.Errorf("same name in other case: %v", err)
		}
		if _, err = s.AddApp(ctx, App{BotId: bot + 1000, ShortName: "x", Title: "x", URL: "https://a.example", PhotoId: 1}); !errors.Is(err, ErrNoBot) {
			t.Errorf("no bot: %v", err)
		}
		if _, err = s.AddApp(ctx, App{BotId: bot, ShortName: "plain", Title: "x", URL: "http://a.example", PhotoId: 1}); !errors.Is(err, ErrAppInvalid) {
			t.Errorf("http: %v", err)
		}

		app.Title = "Shop 2"
		edited, err := s.EditApp(ctx, app)
		if err != nil || edited.Version != 2 || edited.Title != "Shop 2" || edited.Id != got.Id {
			t.Fatalf("edit: %+v, %v", edited, err)
		}
		if a, err := s.AppById(ctx, got.Id, got.AccessHash); err != nil || a == nil || a.Version != 2 {
			t.Errorf("by id: %+v, %v", a, err)
		}
		if a, err := s.AppById(ctx, got.Id, got.AccessHash+1); err != nil || a != nil {
			t.Errorf("wrong access hash: %+v, %v", a, err)
		}
		if a, err := s.AppByShortName(ctx, bot, "shop"); err != nil || a == nil || a.Id != got.Id {
			t.Errorf("by name: %+v, %v", a, err)
		}

		launched, err := s.AppLaunched(ctx, 77, got.Id)
		if err != nil || launched {
			t.Fatalf("launched before: %v, %v", launched, err)
		}
		if first, err := s.MarkAppLaunched(ctx, 77, got.Id, 100); err != nil || !first {
			t.Errorf("first launch: %v, %v", first, err)
		}
		if first, err := s.MarkAppLaunched(ctx, 77, got.Id, 101); err != nil || first {
			t.Errorf("second launch: %v, %v", first, err)
		}
		if launched, _ = s.AppLaunched(ctx, 77, got.Id); !launched {
			t.Error("not launched after the launch")
		}

		if err = s.DeleteApp(ctx, bot, "shop"); err != nil {
			t.Fatal(err)
		}
		if a, _ := s.AppById(ctx, got.Id, got.AccessHash); a != nil {
			t.Error("a removed app is found")
		}
		if list, _ := s.Apps(ctx, bot); len(list) != 0 {
			t.Errorf("apps after delete: %+v", list)
		}
		if err = s.DeleteApp(ctx, bot, "shop"); !errors.Is(err, ErrNoApp) {
			t.Errorf("delete twice: %v", err)
		}
		back, err := s.AddApp(ctx, app)
		if err != nil || back.Id != got.Id || back.AccessHash != got.AccessHash || back.Version <= edited.Version {
			t.Errorf("brought back: %+v (was %+v), %v", back, edited, err)
		}

		if err = s.SetMainApp(ctx, bot, "https://main.example"); err != nil {
			t.Fatal(err)
		}
		if u, err := s.MainApp(ctx, bot); err != nil || u != "https://main.example" {
			t.Errorf("main app: %q, %v", u, err)
		}
		if err = s.SetMainApp(ctx, bot, ""); err != nil {
			t.Fatal(err)
		}
		if u, _ := s.MainApp(ctx, bot); u != "" {
			t.Errorf("main app after removal: %q", u)
		}
	})

	t.Run("attach menu", func(t *testing.T) {
		bot := newBot(t, "attach_bot")
		const user, other = 501, 502
		if _, err := s.InstallAttachMenu(ctx, user, bot, 1); !errors.Is(err, ErrNoAttachMenu) {
			t.Fatalf("install without an entry: %v", err)
		}
		menu := AttachMenu{
			BotId: bot, ShortName: "menu", URL: "https://menu.example", ShowInAttachMenu: true, PeerTypes: AttachPeerPM,
			Icons: []AttachIcon{{Name: "default_static", DocumentId: 3, Colors: []AttachIconColor{{Name: "dark_icon", Color: 0x112233}}}},
		}
		if installed, err := s.SetAttachMenu(ctx, menu); err != nil || len(installed) != 0 {
			t.Fatalf("set: %v, %v", installed, err)
		}
		if added, err := s.InstallAttachMenu(ctx, user, bot, 10); err != nil || !added {
			t.Fatalf("install: %v, %v", added, err)
		}
		if added, err := s.InstallAttachMenu(ctx, user, bot, 11); err != nil || added {
			t.Errorf("install twice: %v, %v", added, err)
		}
		if _, err := s.InstallAttachMenu(ctx, other, bot, 12); err != nil {
			t.Fatal(err)
		}
		list, err := s.InstalledAttachMenus(ctx, user)
		if err != nil || len(list) != 1 || list[0].Icons[0].Colors[0].Color != 0x112233 {
			t.Fatalf("installed: %+v, %v", list, err)
		}
		before := AttachMenusHash(list)

		menu.URL = "https://menu2.example"
		installed, err := s.SetAttachMenu(ctx, menu)
		if err != nil || !slices.Equal(installed, []int64{user, other}) {
			t.Fatalf("edit: %v, %v", installed, err)
		}
		list, _ = s.InstalledAttachMenus(ctx, user)
		if len(list) != 1 || list[0].URL != "https://menu2.example" || AttachMenusHash(list) == before {
			t.Errorf("after edit: %+v, hash %d was %d", list, AttachMenusHash(list), before)
		}

		if removed, err := s.UninstallAttachMenu(ctx, user, bot); err != nil || !removed {
			t.Errorf("uninstall: %v, %v", removed, err)
		}
		if ok, _ := s.AttachMenuInstalled(ctx, user, bot); ok {
			t.Error("installed after uninstall")
		}
		if installed, err = s.DeleteAttachMenu(ctx, bot); err != nil || !slices.Equal(installed, []int64{other}) {
			t.Errorf("delete: %v, %v", installed, err)
		}
		if list, _ = s.InstalledAttachMenus(ctx, other); len(list) != 0 {
			t.Errorf("entries after delete: %+v", list)
		}
		if m, _ := s.AttachMenu(ctx, bot); m != nil {
			t.Errorf("entry after delete: %+v", m)
		}
	})

	t.Run("games and scores", func(t *testing.T) {
		bot := newBot(t, "games_bot")
		g, err := s.AddGame(ctx, Game{BotId: bot, ShortName: "race", Title: "Race", PhotoId: 4})
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := s.GameByShortName(ctx, bot, "race"); got == nil || got.Id != g.Id {
			t.Errorf("by name: %+v", got)
		}
		if got, _ := s.GameById(ctx, g.Id, g.AccessHash); got == nil {
			t.Error("by id")
		}
		if _, err = s.AddGame(ctx, Game{BotId: bot, ShortName: "race", Title: "Again", PhotoId: 4}); !errors.Is(err, ErrShortNameTaken) {
			t.Errorf("same name: %v", err)
		}

		chat, _ := ScoreChatOf(0, mtproto.PEER_CHAT, 900)
		if err = s.SetScore(ctx, g.Id, chat, 1, -1, false, 1); !errors.Is(err, mtproto.ErrScoreInvalid) {
			t.Errorf("negative: %v", err)
		}
		if err = s.SetScore(ctx, g.Id, chat, 1, 50, false, 1); err != nil {
			t.Fatal(err)
		}
		if err = s.SetScore(ctx, g.Id, chat, 1, 50, false, 2); !errors.Is(err, mtproto.ErrBotScoreNotModified) {
			t.Errorf("same score: %v", err)
		}
		if err = s.SetScore(ctx, g.Id, chat, 1, 40, false, 2); !errors.Is(err, mtproto.ErrBotScoreNotModified) {
			t.Errorf("lower score: %v", err)
		}
		if err = s.SetScore(ctx, g.Id, chat, 1, 40, true, 3); err != nil {
			t.Errorf("forced lower score: %v", err)
		}
		if err = s.SetScore(ctx, g.Id, chat, 1, 40, true, 3); err != nil {
			t.Errorf("forced same score: %v", err)
		}

		// Two scores of one player at once: the greater stays, whatever
		// the order; the lesser fails only when it comes second.
		for round := range 10 {
			player := int64(1000 + round)
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i, score := range []int32{10, 20} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs[i] = s.SetScore(ctx, g.Id, chat, player, score, false, 5)
				}()
			}
			wg.Wait()
			if errs[1] != nil {
				t.Errorf("round %d: the greater score failed: %v", round, errs[1])
			}
			if errs[0] != nil && !errors.Is(errs[0], mtproto.ErrBotScoreNotModified) {
				t.Errorf("round %d: the lesser score: %v", round, errs[0])
			}
			table, err := s.HighScores(ctx, g.Id, chat, player)
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(table, func(h HighScore) bool { return h.UserId == player })
			if i < 0 || table[i].Score != 20 {
				t.Errorf("round %d: %+v", round, table)
			}
		}
	})

	t.Run("high scores", func(t *testing.T) {
		bot := newBot(t, "table_bot")
		g, err := s.AddGame(ctx, Game{BotId: bot, ShortName: "table", Title: "Table", PhotoId: 4})
		if err != nil {
			t.Fatal(err)
		}
		chat, _ := ScoreChatOf(3, mtproto.PEER_USER, 8)
		// Players 1..12 with scores 120, 110, ... 10; player 13 ties with
		// player 6 later, so ranks after it.
		for p := int64(1); p <= 12; p++ {
			if err := s.SetScore(ctx, g.Id, chat, p, int32(130-10*p), false, 100); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.SetScore(ctx, g.Id, chat, 13, 70, false, 200); err != nil {
			t.Fatal(err)
		}
		lines := func(table []HighScore) string {
			out := ""
			for _, h := range table {
				out += fmt.Sprintf("%d:%d ", h.Pos, h.UserId)
			}
			return out
		}
		check := func(user int64, want string) {
			t.Helper()
			table, err := s.HighScores(ctx, g.Id, chat, user)
			if err != nil {
				t.Fatal(err)
			}
			if got := lines(table); got != want {
				t.Errorf("table of %d: %q, want %q", user, got, want)
			}
		}
		check(10, "1:1 2:2 3:3 9:8 10:9 11:10 12:11 13:12 ")
		check(1, "1:1 2:2 3:3 ")
		check(13, "1:1 2:2 3:3 5:5 6:6 7:13 8:7 9:8 ")
		check(2, "1:1 2:2 3:3 4:4 ")
		check(999, "1:1 2:2 3:3 ")
	})

	t.Run("login domain and client", func(t *testing.T) {
		one := newBot(t, "login_one_bot")
		two := newBot(t, "login_two_bot")
		if err := s.SetLoginDomain(ctx, one, "Site.Example"); err != nil {
			t.Fatal(err)
		}
		if err := s.SetLoginDomain(ctx, two, "site.example"); !errors.Is(err, ErrLoginDomainTaken) {
			t.Errorf("taken domain: %v", err)
		}
		if err := s.SetLoginDomain(ctx, one, "site.example"); err != nil {
			t.Errorf("own domain again: %v", err)
		}
		if err := s.SetLoginDomain(ctx, two, "bad domain"); !errors.Is(err, ErrLoginDomainInvalid) {
			t.Errorf("invalid: %v", err)
		}
		if id, err := s.BotOfLoginDomain(ctx, "SITE.example"); err != nil || id != one {
			t.Errorf("bot of domain: %d, %v", id, err)
		}
		if d, _ := s.LoginDomain(ctx, one); d != "site.example" {
			t.Errorf("domain: %q", d)
		}
		if err := s.SetLoginDomain(ctx, two, "other.example"); err != nil {
			t.Fatal(err)
		}
		if list, err := s.LoginDomains(ctx); err != nil || !slices.Equal(list, []string{"other.example", "site.example"}) {
			t.Errorf("domains: %v, %v", list, err)
		}
		// Two bots taking one free domain at once: one gets it, the other
		// is told it is taken.
		for round := range 5 {
			domain := fmt.Sprintf("race%d.example", round)
			errs := make([]error, 2)
			var wg sync.WaitGroup
			for i, bot := range []int64{one, two} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs[i] = s.SetLoginDomain(ctx, bot, domain)
				}()
			}
			wg.Wait()
			won := 0
			for _, err := range errs {
				switch {
				case err == nil:
					won++
				case !errors.Is(err, ErrLoginDomainTaken):
					t.Errorf("round %d: %v", round, err)
				}
			}
			var owners []int64
			if err := s.db.QueryRowsPartial(ctx, &owners, "SELECT bot_id FROM bots WHERE login_domain = ?", domain); err != nil || won != 1 || len(owners) != 1 {
				t.Errorf("round %d: %d won, owners %v, %v", round, won, owners, err)
			}
		}
		if err := s.SetLoginDomain(ctx, two, "other.example"); err != nil {
			t.Fatal(err)
		}
		if err := s.SetLoginDomain(ctx, one, ""); err != nil {
			t.Fatal(err)
		}
		if id, _ := s.BotOfLoginDomain(ctx, "site.example"); id != 0 {
			t.Errorf("after unlink: %d", id)
		}

		secret, err := s.SetLoginClient(ctx, one, []string{"https://site.example/cb"}, []string{"https://site.example"}, 50)
		if err != nil || secret == "" {
			t.Fatalf("client: %q, %v", secret, err)
		}
		if ok, err := s.CheckLoginClientSecret(ctx, one, secret); err != nil || !ok {
			t.Errorf("secret: %v, %v", ok, err)
		}
		if ok, _ := s.CheckLoginClientSecret(ctx, one, secret+"x"); ok {
			t.Error("a wrong secret passes")
		}
		if err = s.EditLoginClient(ctx, one, []string{"https://site.example/cb2"}, nil); err != nil {
			t.Fatal(err)
		}
		c, err := s.LoginClient(ctx, one)
		if err != nil || c == nil || !slices.Equal(c.RedirectURIs, []string{"https://site.example/cb2"}) || len(c.Origins) != 0 {
			t.Errorf("client after edit: %+v, %v", c, err)
		}
		if ok, _ := s.CheckLoginClientSecret(ctx, one, secret); !ok {
			t.Error("edit changed the secret")
		}
		next, err := s.SetLoginClient(ctx, one, []string{"https://site.example/cb"}, nil, 60)
		if err != nil || next == secret {
			t.Fatalf("new secret: %v", err)
		}
		if ok, _ := s.CheckLoginClientSecret(ctx, one, secret); ok {
			t.Error("the old secret still passes")
		}
		if err = s.DeleteLoginClient(ctx, one); err != nil {
			t.Fatal(err)
		}
		if ok, _ := s.CheckLoginClientSecret(ctx, one, next); ok {
			t.Error("a removed client's secret passes")
		}
		if err = s.EditLoginClient(ctx, one, []string{"https://site.example/cb"}, nil); !errors.Is(err, ErrNoLoginClient) {
			t.Errorf("edit without a client: %v", err)
		}
	})

	t.Run("web authorizations", func(t *testing.T) {
		bot := newBot(t, "web_bot")
		const user = 601
		first, err := s.PutWebAuthorization(ctx, WebAuthorization{UserId: user, BotId: bot, Domain: "Web.Example", Browser: "Firefox", Platform: "Linux", IP: "10.0.0.1", Region: "Berlin, DE", WriteAllowed: true}, 100)
		if err != nil || first.Hash == 0 || first.Domain != "web.example" || first.DateCreated != 100 {
			t.Fatalf("put: %+v, %v", first, err)
		}
		again, err := s.PutWebAuthorization(ctx, WebAuthorization{UserId: user, BotId: bot, Domain: "web.example", Browser: "Chrome", Platform: "macOS"}, 200)
		if err != nil || again.Hash != first.Hash || again.Browser != "Chrome" || !again.WriteAllowed || again.DateCreated != 100 || again.DateActive != 200 {
			t.Fatalf("put again: %+v, %v", again, err)
		}
		other, err := s.PutWebAuthorization(ctx, WebAuthorization{UserId: user, BotId: bot, Domain: "two.example"}, 150)
		if err != nil || other.Hash == first.Hash {
			t.Fatalf("other domain: %+v, %v", other, err)
		}
		if _, err = s.PutWebAuthorization(ctx, WebAuthorization{UserId: user, BotId: bot, Domain: "nodot"}, 1); !errors.Is(err, ErrWebAuthorizationInvalid) {
			t.Errorf("bad domain: %v", err)
		}
		list, err := s.WebAuthorizations(ctx, user)
		if err != nil || len(list) != 2 || list[0].Hash != first.Hash {
			t.Fatalf("list: %+v, %v", list, err)
		}
		if w, _ := s.WebAuthorizationOf(ctx, user, bot, "WEB.example"); w == nil || w.Hash != first.Hash {
			t.Errorf("of: %+v", w)
		}
		if err = s.TouchWebAuthorization(ctx, user, other.Hash, 300); err != nil {
			t.Fatal(err)
		}
		if list, _ = s.WebAuthorizations(ctx, user); list[0].Hash != other.Hash {
			t.Errorf("after touch: %+v", list)
		}
		if removed, err := s.ResetWebAuthorization(ctx, user+1, first.Hash); err != nil || removed {
			t.Errorf("another user's reset: %v, %v", removed, err)
		}
		if removed, err := s.ResetWebAuthorization(ctx, user, first.Hash); err != nil || !removed {
			t.Errorf("reset: %v, %v", removed, err)
		}
		if w, _ := s.WebAuthorizationOf(ctx, user, bot, "web.example"); w != nil {
			t.Error("found after reset")
		}
		if n, err := s.ResetWebAuthorizations(ctx, user); err != nil || n != 1 {
			t.Errorf("reset all: %d, %v", n, err)
		}

		// Many logins of one user, bot and domain at once make one record.
		var wg sync.WaitGroup
		for i := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := s.PutWebAuthorization(ctx, WebAuthorization{UserId: user, BotId: bot, Domain: "race.example"}, int32(400+i)); err != nil {
					t.Errorf("concurrent put: %v", err)
				}
			}()
		}
		wg.Wait()
		if list, _ = s.WebAuthorizations(ctx, user); len(list) != 1 {
			t.Errorf("concurrent logins: %+v", list)
		}

		if added, err := s.AllowWrite(ctx, user, bot, WriteAllowedDomain, 1); err != nil || !added {
			t.Errorf("allow: %v, %v", added, err)
		}
		if added, err := s.AllowWrite(ctx, user, bot, WriteAllowedApp, 2); err != nil || added {
			t.Errorf("allow twice: %v, %v", added, err)
		}
		if ok, _ := s.WriteAllowed(ctx, user, bot); !ok {
			t.Error("not allowed")
		}
		if ok, _ := s.WriteAllowed(ctx, user+1, bot); ok {
			t.Error("another user allowed")
		}
		if _, err := s.AllowWrite(ctx, user, bot, 9, 1); err == nil {
			t.Error("unknown source passed")
		}
	})

	t.Run("prepared", func(t *testing.T) {
		const bot, user = 701, 702
		p, err := s.PutPrepared(ctx, PreparedInlineMessage, bot, user, []byte(`{"a":1}`), 1000)
		if err != nil || len(p.Id) != 24 || p.ExpireAt != 1000+PreparedTTL {
			t.Fatalf("put: %+v, %v", p, err)
		}
		if got, err := s.PreparedOf(ctx, p.Id, PreparedInlineMessage, 1001); err != nil || got == nil || string(got.Payload) != `{"a":1}` || got.UserId != user {
			t.Errorf("of: %+v, %v", got, err)
		}
		if got, _ := s.PreparedOf(ctx, p.Id, PreparedKeyboardButton, 1001); got != nil {
			t.Error("found as another kind")
		}
		// Ids are case sensitive: base64url.
		if got, _ := s.PreparedOf(ctx, swapCase(p.Id), PreparedInlineMessage, 1001); got != nil && swapCase(p.Id) != p.Id {
			t.Error("found by an id in another case")
		}
		if got, _ := s.PreparedOf(ctx, p.Id, PreparedInlineMessage, p.ExpireAt); got != nil {
			t.Error("found at its expiry")
		}
		if _, err = s.PutPrepared(ctx, 3, bot, user, []byte("x"), 1); !errors.Is(err, ErrPreparedInvalid) {
			t.Errorf("unknown kind: %v", err)
		}

		// A pair keeps the newest MaxPreparedPerPair live items.
		var ids []string
		for i := range MaxPreparedPerPair + 5 {
			q, err := s.PutPrepared(ctx, PreparedKeyboardButton, bot, user+1, []byte("b"), int32(2000+i))
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, q.Id)
		}
		now := int32(2000 + MaxPreparedPerPair + 5)
		for i, id := range ids {
			got, err := s.PreparedOf(ctx, id, PreparedKeyboardButton, now)
			if err != nil {
				t.Fatal(err)
			}
			if (got != nil) != (i >= 5) {
				t.Errorf("item %d live: %v", i, got != nil)
			}
		}

		// The purge takes only what expired.
		n, err := s.PurgeExpiredPrepared(ctx, 1000+PreparedTTL)
		if err != nil || n != 1 {
			t.Errorf("purge: %d, %v", n, err)
		}
		n, err = s.PurgeExpiredPrepared(ctx, 1<<30)
		if err != nil || n != MaxPreparedPerPair {
			t.Errorf("purge all: %d, %v", n, err)
		}
	})

	t.Run("widgets", func(t *testing.T) {
		const bot = 801
		w, err := s.CreateWidget(ctx, Widget{BotId: bot, Payload: []byte{0x78, 0x9c, 1}, Width: 300, Height: 200, AllowQueries: true, PublicData: `{"n":1}`}, 10)
		if err != nil || w.Id == 0 || w.Period != DefaultWidgetPeriod || w.PublicHash != WidgetDataHash(`{"n":1}`) {
			t.Fatalf("create: %+v, %v", w, err)
		}
		if _, err = s.CreateWidget(ctx, Widget{BotId: bot, Payload: []byte{1}, Width: 0, Height: 1}, 10); !errors.Is(err, mtproto.ErrMediaInvalid) {
			t.Errorf("zero width: %v", err)
		}
		if _, err = s.CreateWidget(ctx, Widget{BotId: bot, Payload: []byte{1}, Width: 1, Height: 1, PublicData: string(make([]byte, MaxWidgetData+1))}, 10); !errors.Is(err, mtproto.ErrDataTooLong) {
			t.Errorf("long data: %v", err)
		}
		if got, err := s.BotWidget(ctx, bot+1, w.Id); !errors.Is(err, ErrNoWidget) || got != nil {
			t.Errorf("another bot's widget: %+v, %v", got, err)
		}
		set, err := s.SetWidgetPublicData(ctx, bot, w.Id, `{"n":2}`, 60, 20)
		if err != nil || set.PublicHash == w.PublicHash || set.Period != 60 || set.PublicData != `{"n":2}` {
			t.Fatalf("set: %+v, %v", set, err)
		}
		if same, err := s.SetWidgetPublicData(ctx, bot, w.Id, `{"n":2}`, 60, 20); err != nil || same.PublicHash != set.PublicHash {
			t.Errorf("same data again: %+v, %v", same, err)
		}
		if _, err = s.SetWidgetPublicData(ctx, bot+1, w.Id, "x", 60, 20); !errors.Is(err, ErrNoWidget) {
			t.Errorf("another bot sets: %v", err)
		}
		if got, err := s.BotWidget(ctx, bot, w.Id); err != nil || got.PublicData != `{"n":2}` || string(got.Payload) != string(w.Payload) {
			t.Errorf("read back: %+v, %v", got, err)
		}
	})
}

func swapCase(s string) string {
	b := []byte(s)
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z':
			b[i] = c - 32
		case c >= 'A' && c <= 'Z':
			b[i] = c + 32
		}
	}
	return string(b)
}

// The migration applies on the database production has before it, twice,
// keeps the rows there and leaves the new columns at their defaults.
func TestBotPlatformMigration(t *testing.T) {
	ctx := context.Background()
	files, err := testinfra.SchemaFilesBefore(platformMigration)
	if err != nil {
		t.Fatal(err)
	}
	old, err := testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles(files))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Terminate(ctx) })
	db, err := sql.Open("mysql", old.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec("INSERT INTO bots (bot_id, creator_user_id, token, bot_nochats) VALUES (424242, 0, 'h', 0)"); err != nil {
		t.Fatal(err)
	}

	path, err := testinfra.SchemaFile(platformMigration)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = old.ApplySQLFile(ctx, path); err != nil {
			t.Fatal(err)
		}
	}
	var (
		domain   string
		platform int32
	)
	if err = db.QueryRow("SELECT login_domain, platform_version FROM bots WHERE bot_id = 424242").Scan(&domain, &platform); err != nil {
		t.Fatal(err)
	}
	if domain != "" || platform != 0 {
		t.Errorf("defaults: %q %d", domain, platform)
	}
	for _, table := range []string{
		"bot_secret_keys", "bot_login_clients", "web_authorizations", "bot_write_allowed", "bot_apps", "bot_app_launches",
		"bot_attach_menus", "bot_attach_menu_installs", "bot_games", "game_scores", "bot_prepared", "bot_widgets",
	} {
		var n int
		if err = db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", table).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s: %d, %v", table, n, err)
		}
	}
	var n int
	if err = db.QueryRow("SELECT COUNT(DISTINCT index_name) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'bots' AND index_name = 'login_domain'").Scan(&n); err != nil || n != 1 {
		t.Errorf("index login_domain: %d, %v", n, err)
	}
	// Prepared ids compare byte for byte.
	if _, err = db.Exec("INSERT INTO bot_prepared (id, kind, bot_id, user_id, payload, expire_at) VALUES ('aB', 1, 1, 1, 'x', 1), ('Ab', 1, 1, 1, 'x', 1)"); err != nil {
		t.Errorf("ids in another case collide: %v", err)
	}
}

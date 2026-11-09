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

//go:build integration

package appearance

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

func newStore(ctx context.Context, t *testing.T) *Store {
	t.Helper()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	boxDB, err := userbox.Open(db.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = boxDB.Close() })
	if err := outbox.Register(outbox.Topics{Push: []string{"Sync-T"}}); err != nil {
		t.Fatal(err)
	}
	return NewStore(sqlx.NewMySQL(&conf), boxDB, "Sync-T")
}

func paper(id int64, slug string) *mtproto.WallPaper {
	return mtproto.MakeTLWallPaper(&mtproto.WallPaper{Id: id, AccessHash: id * 2, Slug: slug}).To_WallPaper()
}

// The list of an account (migrate-20261109-wallpapers-themes.sql): the
// last saved first, saved again goes on top with its new settings; removing a
// preinstalled one keeps a row saying so, an uploaded one loses it; reset
// empties it.
func TestWallpaperList(t *testing.T) {
	ctx := context.Background()
	s := newStore(ctx, t)

	if err := s.AddWallpaper(ctx, 7, true, paper(1, "wallpaper-one")); err != nil {
		t.Fatal(err)
	}
	w, creator, err := s.WallpaperBySlug(ctx, "wallpaper-one")
	if err != nil || w.GetId() != 1 || creator != 7 {
		t.Fatalf("by slug: %v, %v, %v", w, creator, err)
	}
	if w, _, err = s.WallpaperByID(ctx, 99); err != nil || w != nil {
		t.Fatalf("no such wallpaper: %v, %v", w, err)
	}

	if err = s.SaveWallpaper(ctx, 7, paper(1, "wallpaper-one")); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveWallpaper(ctx, 7, paper(2, "wallpaper-two")); err != nil {
		t.Fatal(err)
	}
	withSettings := mtproto.MakeTLWallPaper(&mtproto.WallPaper{Id: 1, AccessHash: 2, Slug: "wallpaper-one",
		Settings: mtproto.MakeTLWallPaperSettings(&mtproto.WallPaperSettings{Blur: true}).To_WallPaperSettings()}).To_WallPaper()
	if err = s.SaveWallpaper(ctx, 7, withSettings); err != nil {
		t.Fatal(err)
	}
	saved, err := s.SavedWallpapers(ctx, 7)
	if err != nil || len(saved) != 2 || saved[0].WallpaperID != 1 {
		t.Fatalf("order %+v, %v", saved, err)
	}
	if top, err := saved[0].Wallpaper(); err != nil || !top.GetSettings().GetBlur() {
		t.Fatalf("the new settings: %v, %v", top, err)
	}

	if err = s.UnsaveWallpaper(ctx, 7, 2, false); err != nil {
		t.Fatal(err)
	}
	if err = s.UnsaveWallpaper(ctx, 7, 50, true); err != nil {
		t.Fatal(err)
	}
	saved, err = s.SavedWallpapers(ctx, 7)
	if err != nil || len(saved) != 2 {
		t.Fatalf("after removing: %+v, %v", saved, err)
	}
	if !saved[1].Removed || saved[1].WallpaperID != 50 {
		t.Fatalf("the removed preinstalled one: %+v", saved)
	}

	if err = s.ResetWallpapers(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if saved, err = s.SavedWallpapers(ctx, 7); err != nil || len(saved) != 0 {
		t.Fatalf("after reset: %+v, %v", saved, err)
	}
}

// A wallpaper the other side forced on the account (for_both): it remembers
// the account's own one, a second override keeps that first memory, a revert
// brings it back and clears the flag. A revert with nothing overridden is
// ErrNotOverridden; a reset forgets the override too.
func TestChatWallpaperOverride(t *testing.T) {
	ctx := context.Background()
	s := newStore(ctx, t)
	const me, peer = int64(10), int64(20)

	if _, err := s.RevertChatWallpaper(ctx, me, peer); !errors.Is(err, ErrNotOverridden) {
		t.Fatalf("revert with nothing: %v", err)
	}
	own := paper(5, "own-wall")
	if err := s.SetChatWallpaper(ctx, me, peer, own); err != nil {
		t.Fatal(err)
	}
	if err := s.OverrideChatWallpaper(ctx, me, peer, paper(6, "forced-one")); err != nil {
		t.Fatal(err)
	}
	if err := s.OverrideChatWallpaper(ctx, me, peer, paper(7, "forced-two")); err != nil {
		t.Fatal(err)
	}
	if w, overridden, err := s.GetChatWallpaper(ctx, me, peer); err != nil || w.GetId() != 7 || !overridden {
		t.Fatalf("overridden: %v, %v, %v", w, overridden, err)
	}
	reverted, err := s.RevertChatWallpaper(ctx, me, peer)
	if err != nil || reverted.GetId() != 5 {
		t.Fatalf("revert: %v, %v", reverted, err)
	}
	if w, overridden, err := s.GetChatWallpaper(ctx, me, peer); err != nil || w.GetId() != 5 || overridden {
		t.Fatalf("after revert: %v, %v, %v", w, overridden, err)
	}
	if _, err := s.RevertChatWallpaper(ctx, me, peer); !errors.Is(err, ErrNotOverridden) {
		t.Fatalf("second revert: %v", err)
	}

	if err := s.OverrideChatWallpaper(ctx, me, peer, paper(8, "forced-three")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatWallpaper(ctx, me, peer, nil); err != nil {
		t.Fatal(err)
	}
	if w, overridden, err := s.GetChatWallpaper(ctx, me, peer); err != nil || w != nil || overridden {
		t.Fatalf("after reset: %v, %v, %v", w, overridden, err)
	}
	if w, _, err := s.GetChatWallpaper(ctx, peer, me); err != nil || w != nil {
		t.Fatalf("the other side's row: %v, %v", w, err)
	}
}

// Cloud themes: a slug is one theme's, the count counts the first install of
// an account only, however many run at once, and the saved list follows.
func TestThemes(t *testing.T) {
	ctx := context.Background()
	s := newStore(ctx, t)

	theme := func(id int64, slug string) *mtproto.Theme {
		return mtproto.MakeTLTheme(&mtproto.Theme{Id: id, AccessHash: id + 1, Slug: slug, Title: slug}).To_Theme()
	}
	if err := s.CreateTheme(ctx, 1, theme(100, "my_theme")); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTheme(ctx, 2, theme(101, "my_theme")); !errors.Is(err, mtproto.ErrThemeSlugOccupied) {
		t.Fatalf("the same slug: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.InstallTheme(ctx, 3, 100); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := s.InstallTheme(ctx, 4, 100); err != nil {
		t.Fatal(err)
	}
	if _, _, installs, err := s.ThemeByID(ctx, 100); err != nil || installs != 2 {
		t.Fatalf("installs %d, %v", installs, err)
	}

	if err := s.SaveTheme(ctx, 5, 100); err != nil {
		t.Fatal(err)
	}
	saved, err := s.SavedThemes(ctx, 5)
	if err != nil || len(saved) != 1 || saved[0].Theme.GetId() != 100 {
		t.Fatalf("saved %+v, %v", saved, err)
	}
	if err = s.UnsaveTheme(ctx, 5, 100, false); err != nil {
		t.Fatal(err)
	}
	if saved, err = s.SavedThemes(ctx, 5); err != nil || len(saved) != 0 {
		t.Fatalf("after unsave: %+v, %v", saved, err)
	}

	moved := theme(100, "moved_theme")
	if err = s.UpdateTheme(ctx, 1, moved); err != nil {
		t.Fatal(err)
	}
	if got, _, _, err := s.ThemeBySlug(ctx, "moved_theme"); err != nil || got.GetId() != 100 {
		t.Fatalf("after the update: %v, %v", got, err)
	}
	if err = s.CreateTheme(ctx, 2, theme(102, "taken_slug")); err != nil {
		t.Fatal(err)
	}
	moved.Slug = "taken_slug"
	if err = s.UpdateTheme(ctx, 1, moved); !errors.Is(err, mtproto.ErrThemeSlugOccupied) {
		t.Fatalf("update to a taken slug: %v", err)
	}
}

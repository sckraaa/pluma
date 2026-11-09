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
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/catalog/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/appearance"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"google.golang.org/protobuf/proto"
)

// memAppearance is the appearance store of a test.
type memAppearance struct {
	papers map[int64]*mtproto.WallPaper // uploaded, by id
	saved  []appearance.SavedWallpaper
	themes map[int64]*mtproto.Theme
	owners map[int64]int64 // the creator of each theme
	slugs  map[string]int64
	saves  map[int64]int // themes saved, by id
}

func newMemAppearance() *memAppearance {
	return &memAppearance{
		papers: map[int64]*mtproto.WallPaper{},
		themes: map[int64]*mtproto.Theme{},
		owners: map[int64]int64{},
		slugs:  map[string]int64{},
		saves:  map[int64]int{},
	}
}

func (m *memAppearance) AddWallpaper(_ context.Context, _ int64, _ bool, w *mtproto.WallPaper) error {
	m.papers[w.GetId()] = w
	return nil
}
func (m *memAppearance) WallpaperByID(_ context.Context, id int64) (*mtproto.WallPaper, int64, error) {
	return m.papers[id], 1, nil
}
func (m *memAppearance) WallpaperBySlug(_ context.Context, slug string) (*mtproto.WallPaper, int64, error) {
	for _, w := range m.papers {
		if w.GetSlug() == slug {
			return w, 1, nil
		}
	}
	return nil, 0, nil
}
func (m *memAppearance) SavedWallpapers(context.Context, int64) ([]appearance.SavedWallpaper, error) {
	return m.saved, nil
}
func (m *memAppearance) SaveWallpaper(_ context.Context, _ int64, w *mtproto.WallPaper) error {
	data, err := proto.Marshal(w)
	if err != nil {
		return err
	}
	m.saved = append([]appearance.SavedWallpaper{{WallpaperID: w.GetId(), Data: data}}, m.saved...)
	return nil
}
func (m *memAppearance) UnsaveWallpaper(_ context.Context, _, id int64, inCatalog bool) error {
	m.saved = append(m.saved, appearance.SavedWallpaper{WallpaperID: id, Removed: inCatalog})
	return nil
}
func (m *memAppearance) ResetWallpapers(context.Context, int64) error { m.saved = nil; return nil }
func (m *memAppearance) SetChatWallpaper(context.Context, int64, int64, *mtproto.WallPaper) error {
	return nil
}
func (m *memAppearance) OverrideChatWallpaper(context.Context, int64, int64, *mtproto.WallPaper) error {
	return nil
}
func (m *memAppearance) RevertChatWallpaper(context.Context, int64, int64) (*mtproto.WallPaper, error) {
	return nil, appearance.ErrNotOverridden
}
func (m *memAppearance) CreateTheme(_ context.Context, creator int64, t *mtproto.Theme) error {
	m.themes[t.GetId()], m.owners[t.GetId()], m.slugs[t.GetSlug()] = t, creator, t.GetId()
	return nil
}
func (m *memAppearance) ThemeByID(_ context.Context, id int64) (*mtproto.Theme, int64, int32, error) {
	return m.themes[id], m.owners[id], 0, nil
}
func (m *memAppearance) ThemeBySlug(_ context.Context, slug string) (*mtproto.Theme, int64, int32, error) {
	if id, ok := m.slugs[slug]; ok {
		return m.themes[id], m.owners[id], 0, nil
	}
	return nil, 0, 0, nil
}
func (m *memAppearance) UpdateTheme(_ context.Context, _ int64, t *mtproto.Theme) error {
	m.themes[t.GetId()] = t
	return nil
}
func (m *memAppearance) SaveTheme(_ context.Context, _, id int64) error {
	m.saves[id]++
	return nil
}
func (m *memAppearance) UnsaveTheme(context.Context, int64, int64, bool) error { return nil }
func (m *memAppearance) InstallTheme(context.Context, int64, int64) error      { return nil }
func (m *memAppearance) ThemeEntries(context.Context, int64) ([]appearance.ThemeEntry, error) {
	return nil, nil
}
func (m *memAppearance) SavedThemes(context.Context, int64) ([]appearance.SavedTheme, error) {
	return nil, nil
}

func appearanceCore(m *memCatalog, a *memAppearance, md *fakeMedia) *CatalogCore {
	c := New(context.Background(), &svc.ServiceContext{Catalog: m, Media: md, Installed: &memInstalled{}, Appearance: a})
	c.MD = &metadata.RpcMetadata{UserId: 1}
	return c
}

func (f *fakeMedia) MediaUploadWallPaperFile(context.Context, *media.TLMediaUploadWallPaperFile) (*mtproto.Document, error) {
	return mtproto.MakeTLDocument(&mtproto.Document{Id: 500, MimeType: "image/jpeg"}).To_Document(), nil
}

// A JPEG or a PNG only; the rest is not a wallpaper the clients can use.
func TestUploadWallPaperMime(t *testing.T) {
	c := appearanceCore(&memCatalog{}, newMemAppearance(), &fakeMedia{})
	for _, mime := range []string{"image/webp", "application/x-tgwallpattern", ""} {
		if _, err := c.AccountUploadWallPaper(&mtproto.TLAccountUploadWallPaper{MimeType: mime}); !errors.Is(err, mtproto.ErrWallpaperMimeInvalid) {
			t.Errorf("mime %q: %v", mime, err)
		}
	}
	w, err := c.AccountUploadWallPaper(&mtproto.TLAccountUploadWallPaper{MimeType: "image/jpeg"})
	if err != nil || !w.GetCreator() || w.GetDocument().GetId() != 500 || w.GetSlug() == "" {
		t.Fatalf("uploaded: %v, %v", w, err)
	}
	if !appearance.ValidThemeSlug(w.GetSlug()) {
		t.Fatalf("slug %q", w.GetSlug())
	}
}

// The personal list comes first and the catalog's entries it replaced or
// removed drop out; its hash is not the catalog's.
func TestWallPaperListMerge(t *testing.T) {
	m := &memCatalog{answers: map[[2]string]*catalog.Answer{}}
	a := newMemAppearance()
	put(t, m, "account.getWallPapers", "", 77, mtproto.MakeTLAccountWallPapers(&mtproto.Account_WallPapers{Hash: 77, Wallpapers: []*mtproto.WallPaper{
		mtproto.MakeTLWallPaper(&mtproto.WallPaper{Id: 1, AccessHash: 1, Slug: "a"}).To_WallPaper(),
		mtproto.MakeTLWallPaper(&mtproto.WallPaper{Id: 2, AccessHash: 1, Slug: "b"}).To_WallPaper(),
	}}).To_Account_WallPapers())
	c := appearanceCore(m, a, &fakeMedia{})

	r, err := c.AccountGetWallPapers(&mtproto.TLAccountGetWallPapers{Hash: 77})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_account_wallPapersNotModified {
		t.Fatalf("no personal list keeps the catalog's hash: %v, %v", r, err)
	}

	// Save 2 again with its own settings and remove 1.
	saved := mtproto.MakeTLWallPaper(&mtproto.WallPaper{Id: 2, AccessHash: 1, Slug: "b",
		Settings: mtproto.MakeTLWallPaperSettings(&mtproto.WallPaperSettings{Blur: true}).To_WallPaperSettings()}).To_WallPaper()
	if err := a.SaveWallpaper(context.Background(), 1, saved); err != nil {
		t.Fatal(err)
	}
	if err := a.UnsaveWallpaper(context.Background(), 1, 1, true); err != nil {
		t.Fatal(err)
	}
	r, err = c.AccountGetWallPapers(&mtproto.TLAccountGetWallPapers{Hash: 77})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_account_wallPapers || r.GetHash() == 77 {
		t.Fatalf("a new hash: %v, %v", r, err)
	}
	if len(r.GetWallpapers()) != 1 || r.GetWallpapers()[0].GetId() != 2 || !r.GetWallpapers()[0].GetSettings().GetBlur() {
		t.Fatalf("list: %v", r.GetWallpapers())
	}
	if r2, _ := c.AccountGetWallPapers(&mtproto.TLAccountGetWallPapers{Hash: r.GetHash()}); r2.GetPredicateName() != mtproto.Predicate_account_wallPapersNotModified {
		t.Fatalf("the personal hash answers not modified: %v", r2)
	}
}

// A fill wallpaper is not put in the list of an account.
func TestSaveFillWallpaperRefused(t *testing.T) {
	c := appearanceCore(&memCatalog{}, newMemAppearance(), &fakeMedia{})
	fill := mtproto.MakeTLInputWallPaperNoFile(&mtproto.InputWallPaper{Id: 0}).To_InputWallPaper()
	if _, err := c.AccountSaveWallPaper(&mtproto.TLAccountSaveWallPaper{Wallpaper: fill, Unsave: mtproto.BoolFalse}); !errors.Is(err, mtproto.ErrWallpaperInvalid) {
		t.Fatalf("save: %v", err)
	}
	if _, err := c.AccountInstallWallPaper(&mtproto.TLAccountInstallWallPaper{Wallpaper: fill}); !errors.Is(err, mtproto.ErrWallpaperInvalid) {
		t.Fatalf("install: %v", err)
	}
}

// A fill wallpaper of a chat takes the settings given; without them there is
// nothing to fill with.
func TestResolveFillWallpaper(t *testing.T) {
	c := appearanceCore(&memCatalog{}, newMemAppearance(), &fakeMedia{})
	fill := mtproto.MakeTLInputWallPaperNoFile(&mtproto.InputWallPaper{Id: 0}).To_InputWallPaper()
	settings := mtproto.MakeTLWallPaperSettings(&mtproto.WallPaperSettings{BackgroundColor: mtproto.MakeFlagsInt32(5)}).To_WallPaperSettings()
	w, err := c.wallpaperForChat(fill, settings)
	if err != nil || w.GetPredicateName() != mtproto.Predicate_wallPaperNoFile || w.GetSettings().GetBackgroundColor().GetValue() != 5 {
		t.Fatalf("fill: %v, %v", w, err)
	}
	if _, err = c.wallpaperForChat(fill, nil); !errors.Is(err, mtproto.ErrWallpaperInvalid) {
		t.Fatalf("no settings: %v", err)
	}
}

// The slug is answered first, an empty title only after it: what Android's
// link editor relies on (ThemeSetUrlActivity).
func TestCreateThemeOrderOfChecks(t *testing.T) {
	a := newMemAppearance()
	c := appearanceCore(&memCatalog{}, a, &fakeMedia{})
	if err := a.CreateTheme(context.Background(), 2, mtproto.MakeTLTheme(&mtproto.Theme{Id: 9, AccessHash: 1, Slug: "taken_slug"}).To_Theme()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AccountCreateTheme(&mtproto.TLAccountCreateTheme{Slug: "taken_slug", Title: ""}); !errors.Is(err, mtproto.ErrThemeSlugOccupied) {
		t.Fatalf("taken: %v", err)
	}
	if _, err := c.AccountCreateTheme(&mtproto.TLAccountCreateTheme{Slug: "_bad", Title: ""}); !errors.Is(err, mtproto.ErrThemeSlugInvalid) {
		t.Fatalf("bad: %v", err)
	}
	if _, err := c.AccountCreateTheme(&mtproto.TLAccountCreateTheme{Slug: "free_slug", Title: ""}); !errors.Is(err, mtproto.ErrThemeTitleInvalid) {
		t.Fatalf("empty title: %v", err)
	}
}

// Only the creator updates a theme; a theme's file is shown only for its format.
func TestThemeRules(t *testing.T) {
	a := newMemAppearance()
	c := appearanceCore(&memCatalog{}, a, &fakeMedia{})
	doc := mtproto.MakeTLDocument(&mtproto.Document{Id: 1, MimeType: "application/x-tgtheme-android"}).To_Document()
	theirs := mtproto.MakeTLTheme(&mtproto.Theme{Id: 9, AccessHash: 3, Slug: "their_theme", Title: "T", Document: doc}).To_Theme()
	if err := a.CreateTheme(context.Background(), 2, theirs); err != nil {
		t.Fatal(err)
	}
	ask := mtproto.MakeTLInputTheme(&mtproto.InputTheme{Id: 9, AccessHash: 3}).To_InputTheme()
	if _, err := c.AccountUpdateTheme(&mtproto.TLAccountUpdateTheme{Format: "android", Theme: ask}); !errors.Is(err, mtproto.ErrThemeInvalid) {
		t.Fatalf("not the creator: %v", err)
	}
	if got, err := c.AccountGetTheme(&mtproto.TLAccountGetTheme{Format: "android", Theme: ask}); err != nil || got.GetDocument() == nil {
		t.Fatalf("own format: %v, %v", got, err)
	}
	if got, err := c.AccountGetTheme(&mtproto.TLAccountGetTheme{Format: "ios", Theme: ask}); err != nil || got.GetDocument() != nil {
		t.Fatalf("other format: %v, %v", got, err)
	}
	if _, err := c.AccountGetTheme(&mtproto.TLAccountGetTheme{Format: "", Theme: ask}); !errors.Is(err, mtproto.ErrThemeFormatInvalid) {
		t.Fatalf("no format: %v", err)
	}
	wrong := mtproto.MakeTLInputTheme(&mtproto.InputTheme{Id: 9, AccessHash: 4}).To_InputTheme()
	if _, err := c.AccountGetTheme(&mtproto.TLAccountGetTheme{Format: "android", Theme: wrong}); !errors.Is(err, mtproto.ErrThemeInvalid) {
		t.Fatalf("wrong hash: %v", err)
	}
}

// The chat theme is one of the catalog's emoticons; a gift's theme is refused.
func TestChatThemeEmoticon(t *testing.T) {
	m := &memCatalog{answers: map[[2]string]*catalog.Answer{}}
	put(t, m, "account.getChatThemes", "", 0, mtproto.MakeTLAccountThemes(&mtproto.Account_Themes{Themes: []*mtproto.Theme{
		mtproto.MakeTLTheme(&mtproto.Theme{Id: 1, Emoticon: mtproto.MakeFlagsString("🌿")}).To_Theme(),
	}}).To_Account_Themes())
	c := appearanceCore(m, newMemAppearance(), &fakeMedia{})

	e, err := c.chatThemeEmoticon(mtproto.MakeTLInputChatTheme(&mtproto.InputChatTheme{Emoticon: "🌿"}).To_InputChatTheme())
	if err != nil || e != "🌿" {
		t.Fatalf("known: %q, %v", e, err)
	}
	if e, err = c.chatThemeEmoticon(mtproto.MakeTLInputChatThemeEmpty(nil).To_InputChatTheme()); err != nil || e != "" {
		t.Fatalf("empty: %q, %v", e, err)
	}
	if _, err = c.chatThemeEmoticon(mtproto.MakeTLInputChatTheme(&mtproto.InputChatTheme{Emoticon: "🦄"}).To_InputChatTheme()); !errors.Is(err, mtproto.ErrEmojiInvalid) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err = c.chatThemeEmoticon(mtproto.MakeTLInputChatThemeUniqueGift(&mtproto.InputChatTheme{Slug: "x"}).To_InputChatTheme()); !errors.Is(err, mtproto.ErrEmojiInvalid) {
		t.Fatalf("a gift: %v", err)
	}
}

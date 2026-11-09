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
	"strings"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/appearance"
	"google.golang.org/protobuf/proto"
)

// themeMimePrefix: the file of a theme has the MIME type
// application/x-tgtheme-<format> (core.telegram.org/method/account.uploadTheme).
const themeMimePrefix = "application/x-tgtheme-"

// maxThemeTitle bounds the title; the documentation gives no number
// (UNVERIFIED), so it is a generous limit that the themes table can hold.
const maxThemeTitle = 64

// maxFormat: every format the clients use is one short word.
const maxFormat = 32

func checkThemeMime(mime string) error {
	if !strings.HasPrefix(mime, themeMimePrefix) || len(mime) == len(themeMimePrefix) {
		return mtproto.ErrThemeMimeInvalid
	}
	return nil
}

// forFormat drops the theme's file when it is for another engine: a theme
// made on Android has an .attheme no tdesktop can open, so the client must
// fall back to the settings or show the theme as unavailable
// (core.telegram.org/api/themes, Android LaunchActivity 5246).
func forFormat(t *mtproto.Theme, format string) *mtproto.Theme {
	if t.GetDocument() == nil || t.GetDocument().GetMimeType() == themeMimePrefix+format {
		return t
	}
	shown := proto.Clone(t).(*mtproto.Theme)
	shown.Document = nil
	return shown
}

func checkFormat(format string) error {
	if format == "" || len(format) > maxFormat {
		return mtproto.ErrThemeFormatInvalid
	}
	return nil
}

// resolveThemeSettings gives a theme's settings their wallpapers.
func (c *CatalogCore) resolveThemeSettings(in []*mtproto.InputThemeSettings) ([]*mtproto.ThemeSettings, error) {
	out := make([]*mtproto.ThemeSettings, 0, len(in))
	for _, s := range in {
		v := &mtproto.ThemeSettings{
			MessageColorsAnimated: s.GetMessageColorsAnimated(),
			BaseTheme:             s.GetBaseTheme(),
			AccentColor:           s.GetAccentColor(),
			OutboxAccentColor:     s.GetOutboxAccentColor(),
			MessageColors:         s.GetMessageColors(),
		}
		if s.GetWallpaper() != nil {
			w, err := c.wallpaperForChat(s.GetWallpaper(), s.GetWallpaperSettings())
			if err != nil {
				return nil, err
			}
			v.Wallpaper = w
		}
		out = append(out, mtproto.MakeTLThemeSettings(v).To_ThemeSettings())
	}
	return out, nil
}

// themeDocument finds the document of a theme: an uploaded one, with the
// right access hash, in a theme's MIME type.
func (c *CatalogCore) themeDocument(ask *mtproto.InputDocument) (*mtproto.Document, error) {
	if ask == nil || ask.GetPredicateName() == mtproto.Predicate_inputDocumentEmpty {
		return nil, nil
	}
	d, err := c.svcCtx.Media.MediaGetDocument(c.ctx, &media.TLMediaGetDocument{Id: ask.GetId()})
	if err != nil {
		return nil, err
	}
	if d == nil || d.GetId() == 0 || d.GetAccessHash() != ask.GetAccessHash() {
		return nil, mtproto.ErrThemeFileInvalid
	}
	if err = checkThemeMime(d.GetMimeType()); err != nil {
		return nil, err
	}
	return d, nil
}

// checkThemeTitle: an empty title is what Android's slug check sends and has
// nothing to name the theme.
func checkThemeTitle(title string) error {
	if title == "" || utf8.RuneCountInString(title) > maxThemeTitle {
		return mtproto.ErrThemeTitleInvalid
	}
	return nil
}

// catalogTheme is a theme of the catalog by id and access hash or by slug,
// with the format's list and the chat themes, which a client may also ask
// for by id; nil if there is none.
func (c *CatalogCore) catalogTheme(format string, ask *mtproto.InputTheme) (*mtproto.Theme, error) {
	lists := make([]*mtproto.Account_Themes, 2)
	lists[0], lists[1] = &mtproto.Account_Themes{}, &mtproto.Account_Themes{}
	if _, _, err := c.stored("account.getThemes", format, lists[0]); err != nil {
		return nil, err
	}
	if _, _, err := c.stored("account.getChatThemes", "", lists[1]); err != nil {
		return nil, err
	}
	for _, l := range lists {
		for _, t := range l.GetThemes() {
			switch ask.GetPredicateName() {
			case mtproto.Predicate_inputThemeSlug:
				if t.GetSlug() == ask.GetSlug() {
					return t, nil
				}
			case mtproto.Predicate_inputTheme:
				if t.GetId() == ask.GetId() && t.GetAccessHash() == ask.GetAccessHash() {
					return t, nil
				}
			}
		}
	}
	return nil, nil
}

// ownedTheme is a theme of the themes table, with its creator and installs;
// nil if the asked theme is not one.
func (c *CatalogCore) ownedTheme(ask *mtproto.InputTheme) (*mtproto.Theme, int64, int32, error) {
	var (
		t        *mtproto.Theme
		creator  int64
		installs int32
		err      error
	)
	switch ask.GetPredicateName() {
	case mtproto.Predicate_inputThemeSlug:
		t, creator, installs, err = c.svcCtx.Appearance.ThemeBySlug(c.ctx, ask.GetSlug())
	case mtproto.Predicate_inputTheme:
		t, creator, installs, err = c.svcCtx.Appearance.ThemeByID(c.ctx, ask.GetId())
		if err == nil && t != nil && t.GetAccessHash() != ask.GetAccessHash() {
			return nil, 0, 0, nil
		}
	}
	return t, creator, installs, err
}

// resolveTheme is the theme asked for: an account's or a catalog's.
// inCatalog tells which, for what cannot be written to a catalog theme.
func (c *CatalogCore) resolveTheme(format string, ask *mtproto.InputTheme) (t *mtproto.Theme, inCatalog bool, err error) {
	t, creator, installs, err := c.ownedTheme(ask)
	if err != nil {
		return nil, false, err
	}
	if t != nil {
		return appearance.ThemeOf(t, creator, c.MD.UserId, installs), false, nil
	}
	t, err = c.catalogTheme(format, ask)
	if err != nil {
		return nil, false, err
	}
	if t == nil {
		if ask.GetPredicateName() == mtproto.Predicate_inputThemeSlug && ask.GetSlug() == "" {
			return nil, false, mtproto.ErrThemeSlugInvalid
		}
		return nil, false, mtproto.ErrThemeInvalid
	}
	return t, true, nil
}

// themesListHash is the hash of an account's list on top of the catalog's: a
// theme that changed changes it too.
func themesListHash(catalogHash int64, entries []appearance.ThemeEntry, saved []appearance.SavedTheme) int64 {
	h := catalogHash
	for _, s := range saved {
		h = appearance.CalcHash(h, s.Theme.GetId())
		h = appearance.CalcHash(h, s.UpdatedAt)
		h = appearance.CalcHash(h, int64(s.Theme.GetInstallsCount().GetValue()))
	}
	for _, e := range entries {
		if e.Removed() {
			h = appearance.CalcHash(h, e.ThemeID)
		}
	}
	return h
}

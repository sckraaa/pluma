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
	"crypto/rand"
	"encoding/binary"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/appearance"
)

// AccountCreateTheme
// account.createTheme#652e4400 flags:# slug:string title:string document:flags.2?InputDocument settings:flags.3?Vector<InputThemeSettings> = Theme;
//
// A new theme of the account, kept in its list: the client that made it
// applies it right after and shows it among its own (Android
// MessagesController.java:6330, tdesktop window_theme_editor_box.cpp:543).
//
// The slug is checked first and alone: Android tries a slug for its link
// editor by this method with an empty title (ThemeSetUrlActivity.java:512),
// and any answer but THEME_SLUG_INVALID or THEME_SLUG_OCCUPIED means free.
func (c *CatalogCore) AccountCreateTheme(in *mtproto.TLAccountCreateTheme) (*mtproto.Theme, error) {
	slug := in.GetSlug()
	if slug != "" {
		if !appearance.ValidThemeSlug(slug) {
			return nil, mtproto.ErrThemeSlugInvalid
		}
		t, _, _, err := c.svcCtx.Appearance.ThemeBySlug(c.ctx, slug)
		if err != nil {
			return nil, err
		}
		if t != nil {
			return nil, mtproto.ErrThemeSlugOccupied
		}
	}
	if err := checkThemeTitle(in.GetTitle()); err != nil {
		return nil, err
	}
	document, err := c.themeDocument(in.GetDocument())
	if err != nil {
		return nil, err
	}
	settings, err := c.resolveThemeSettings(in.GetSettings())
	if err != nil {
		return nil, err
	}
	if document == nil && len(settings) == 0 {
		return nil, mtproto.ErrThemeInvalid
	}
	if slug == "" {
		if slug, err = appearance.NewSlug(); err != nil {
			return nil, err
		}
	}
	id, err := appearance.NewID()
	if err != nil {
		return nil, err
	}
	var hash [8]byte
	if _, err = rand.Read(hash[:]); err != nil {
		return nil, err
	}
	t := mtproto.MakeTLTheme(&mtproto.Theme{
		Id:         id,
		AccessHash: int64(binary.BigEndian.Uint64(hash[:])),
		Slug:       slug,
		Title:      in.GetTitle(),
		Document:   document,
		Settings:   settings,
	}).To_Theme()
	if err = c.svcCtx.Appearance.CreateTheme(c.ctx, c.MD.UserId, t); err != nil {
		c.Logger.Errorf("account.createTheme - error: %v", err)
		return nil, err
	}
	if err = c.svcCtx.Appearance.SaveTheme(c.ctx, c.MD.UserId, id); err != nil {
		c.Logger.Errorf("account.createTheme - error: %v", err)
		return nil, err
	}
	return appearance.ThemeOf(t, c.MD.UserId, c.MD.UserId, 0), nil
}

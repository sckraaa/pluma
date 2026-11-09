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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/appearance"
	"google.golang.org/protobuf/proto"
)

// AccountUpdateTheme
// account.updateTheme#2bf40ccc flags:# format:string theme:InputTheme slug:flags.0?string title:flags.1?string document:flags.2?InputDocument settings:flags.3?Vector<InputThemeSettings> = Theme;
//
// Only the creator changes a theme (core.telegram.org/api/themes): THEME_INVALID
// for anybody else and for a catalog theme, which has no creator here.
// Those who saved or installed the theme are told with updateTheme.
func (c *CatalogCore) AccountUpdateTheme(in *mtproto.TLAccountUpdateTheme) (*mtproto.Theme, error) {
	if err := checkFormat(in.GetFormat()); err != nil {
		return nil, err
	}
	t, creator, installs, err := c.ownedTheme(in.GetTheme())
	if err != nil {
		return nil, err
	}
	if t == nil || creator != c.MD.UserId {
		return nil, mtproto.ErrThemeInvalid
	}
	t = proto.Clone(t).(*mtproto.Theme)
	if s := in.GetSlug(); s != nil && s.GetValue() != t.GetSlug() {
		if !appearance.ValidThemeSlug(s.GetValue()) {
			return nil, mtproto.ErrThemeSlugInvalid
		}
		t.Slug = s.GetValue()
	}
	if s := in.GetTitle(); s != nil {
		if err = checkThemeTitle(s.GetValue()); err != nil {
			return nil, err
		}
		t.Title = s.GetValue()
	}
	if in.GetDocument() != nil {
		d, err := c.themeDocument(in.GetDocument())
		if err != nil {
			return nil, err
		}
		t.Document = d
	}
	if in.GetSettings() != nil {
		if t.Settings, err = c.resolveThemeSettings(in.GetSettings()); err != nil {
			return nil, err
		}
	}
	t.InstallsCount = mtproto.MakeFlagsInt32(installs)
	if err = c.svcCtx.Appearance.UpdateTheme(c.ctx, creator, t); err != nil {
		c.Logger.Errorf("account.updateTheme - error: %v", err)
		return nil, err
	}
	return appearance.ThemeOf(t, creator, c.MD.UserId, installs), nil
}

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
)

// AccountSaveWallPaper
// account.saveWallPaper#6c5a5b37 wallpaper:InputWallPaper unsave:Bool settings:WallPaperSettings = Bool;
//
// Saves the wallpaper in the account's list or takes it out, a preinstalled
// one too (core.telegram.org/api/wallpapers). A fill wallpaper has no place in
// the list and saving one is WALLPAPER_INVALID, but one of the catalog can be
// taken out by its bare id: inputWallPaperNoFile with unsave
// (core.telegram.org/constructor/inputWallPaperNoFile, Android
// WallpapersListActivity.java:554).
func (c *CatalogCore) AccountSaveWallPaper(in *mtproto.TLAccountSaveWallPaper) (*mtproto.Bool, error) {
	unsave := mtproto.FromBool(in.GetUnsave())
	if in.GetWallpaper().GetPredicateName() == mtproto.Predicate_inputWallPaperNoFile {
		if !unsave {
			return nil, mtproto.ErrWallpaperInvalid
		}
		return c.unsaveWallPaper(in.GetWallpaper().GetId())
	}
	w, err := c.wallPaper(in.GetWallpaper())
	if err != nil {
		return nil, err
	}
	if unsave {
		return c.unsaveWallPaper(w.GetId())
	}
	if err = c.svcCtx.Appearance.SaveWallpaper(c.ctx, c.MD.UserId, withSettings(w, in.GetSettings())); err != nil {
		c.Logger.Errorf("account.saveWallPaper - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

func (c *CatalogCore) unsaveWallPaper(id int64) (*mtproto.Bool, error) {
	inCatalog, err := c.hasCatalogWallpaper(id)
	if err != nil {
		return nil, err
	}
	if err = c.svcCtx.Appearance.UnsaveWallpaper(c.ctx, c.MD.UserId, id, inCatalog); err != nil {
		c.Logger.Errorf("account.saveWallPaper - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

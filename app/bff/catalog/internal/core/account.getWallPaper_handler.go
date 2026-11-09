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
	"strconv"
)

// AccountGetWallPaper
// account.getWallPaper#fc8ddbea wallpaper:InputWallPaper = WallPaper;
//
// A wallpaper of the catalog by its slug or id (wallpaperKey), or an uploaded
// one; one neither has is WALLPAPER_INVALID.
func (c *CatalogCore) AccountGetWallPaper(in *mtproto.TLAccountGetWallPaper) (*mtproto.WallPaper, error) {
	return c.wallPaper(in.GetWallpaper())
}

func (c *CatalogCore) wallPaper(ask *mtproto.InputWallPaper) (*mtproto.WallPaper, error) {
	key := wallpaperKey(ask)
	if key == "" {
		return nil, mtproto.ErrWallpaperInvalid
	}
	wallpaper := &mtproto.WallPaper{}
	_, found, err := c.stored("account.getWallPaper", key, wallpaper)
	if err != nil {
		return nil, err
	}
	if !found {
		uploaded, err := c.resolveUploaded(ask)
		if err != nil {
			return nil, err
		}
		if uploaded == nil {
			return nil, mtproto.ErrWallpaperInvalid
		}
		return uploaded, nil
	}
	if ask.GetPredicateName() == mtproto.Predicate_inputWallPaper && wallpaper.GetAccessHash() != ask.GetAccessHash() {
		return nil, mtproto.ErrWallpaperInvalid
	}
	return wallpaper, nil
}

// wallpaperKey is the argument a wallpaper is stored under: "slug:" and its
// slug, or "id:" and its id; "" for one the catalog cannot have (a chat
// theme's inputWallPaperNoFile has no file, but has an id).
func wallpaperKey(ask *mtproto.InputWallPaper) string {
	switch ask.GetPredicateName() {
	case mtproto.Predicate_inputWallPaperSlug:
		if ask.GetSlug() == "" {
			return ""
		}
		return "slug:" + ask.GetSlug()
	case mtproto.Predicate_inputWallPaper, mtproto.Predicate_inputWallPaperNoFile:
		return "id:" + strconv.FormatInt(ask.GetId(), 10)
	default:
		return ""
	}
}

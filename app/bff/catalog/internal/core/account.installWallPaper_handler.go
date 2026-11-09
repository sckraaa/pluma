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

// AccountInstallWallPaper
// account.installWallPaper#feed5769 wallpaper:InputWallPaper settings:WallPaperSettings = Bool;
//
// The client set the wallpaper as the default of its chats; the server has
// nothing to apply and keeps it in the list, which installing does if it was
// not there (core.telegram.org/api/wallpapers). A fill wallpaper is local to
// the client: nothing to record, and Android and tdesktop send none here.
func (c *CatalogCore) AccountInstallWallPaper(in *mtproto.TLAccountInstallWallPaper) (*mtproto.Bool, error) {
	if in.GetWallpaper().GetPredicateName() == mtproto.Predicate_inputWallPaperNoFile {
		return nil, mtproto.ErrWallpaperInvalid
	}
	w, err := c.wallPaper(in.GetWallpaper())
	if err != nil {
		return nil, err
	}
	if err = c.svcCtx.Appearance.SaveWallpaper(c.ctx, c.MD.UserId, withSettings(w, in.GetSettings())); err != nil {
		c.Logger.Errorf("account.installWallPaper - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

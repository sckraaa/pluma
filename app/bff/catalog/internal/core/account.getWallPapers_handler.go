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

// AccountGetWallPapers
// account.getWallPapers#7967d36 hash:long = account.WallPapers;
//
// The wallpapers of the gallery: those the account saved, the last first, and
// the catalog's it did not remove. An account that changed nothing gets the
// catalog's own answer and hash.
func (c *CatalogCore) AccountGetWallPapers(in *mtproto.TLAccountGetWallPapers) (*mtproto.Account_WallPapers, error) {
	answer := &mtproto.Account_WallPapers{}
	hash, _, err := c.stored("account.getWallPapers", "", answer)
	if err != nil {
		return nil, err
	}
	saved, err := c.svcCtx.Appearance.SavedWallpapers(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("account.getWallPapers - error: %v", err)
		return nil, err
	}
	if len(saved) > 0 {
		hash = wallpaperListHash(hash, saved)
		list := make([]*mtproto.WallPaper, 0, len(saved)+len(answer.GetWallpapers()))
		taken := make(map[int64]bool, len(saved))
		for _, e := range saved {
			taken[e.WallpaperID] = true
			if e.Removed {
				continue
			}
			w, err := e.Wallpaper()
			if err != nil {
				return nil, err
			}
			list = append(list, w)
		}
		for _, w := range answer.GetWallpapers() {
			if !taken[w.GetId()] {
				list = append(list, w)
			}
		}
		answer = mtproto.MakeTLAccountWallPapers(&mtproto.Account_WallPapers{Hash: hash, Wallpapers: list}).To_Account_WallPapers()
	}
	if notModified(in.GetHash(), hash) {
		return mtproto.MakeTLAccountWallPapersNotModified(nil).To_Account_WallPapers(), nil
	}
	if answer.GetWallpapers() == nil {
		return mtproto.MakeTLAccountWallPapers(&mtproto.Account_WallPapers{Wallpapers: []*mtproto.WallPaper{}}).To_Account_WallPapers(), nil
	}
	return answer, nil
}

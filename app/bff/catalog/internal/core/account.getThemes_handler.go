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

// AccountGetThemes
// account.getThemes#7206e458 format:string hash:long = account.Themes;
//
// The themes of the platform format asks for (android, ios, macos, tdesktop):
// the account's saved ones, the last first, then the catalog's it did not
// remove. An account that changed nothing gets the catalog's answer and hash.
func (c *CatalogCore) AccountGetThemes(in *mtproto.TLAccountGetThemes) (*mtproto.Account_Themes, error) {
	answer := &mtproto.Account_Themes{}
	hash, found, err := c.stored("account.getThemes", in.GetFormat(), answer)
	if err != nil {
		return nil, err
	}
	entries, err := c.svcCtx.Appearance.ThemeEntries(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("account.getThemes - error: %v", err)
		return nil, err
	}
	saved, err := c.svcCtx.Appearance.SavedThemes(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("account.getThemes - error: %v", err)
		return nil, err
	}
	if len(entries) > 0 || len(saved) > 0 {
		hash = themesListHash(hash, entries, saved)
		removed := make(map[int64]bool, len(entries))
		for _, e := range entries {
			removed[e.ThemeID] = e.Removed()
		}
		list := make([]*mtproto.Theme, 0, len(saved)+len(answer.GetThemes()))
		taken := make(map[int64]bool, len(saved))
		for _, s := range saved {
			taken[s.Theme.GetId()] = true
			list = append(list, forFormat(s.Theme, in.GetFormat()))
		}
		for _, t := range answer.GetThemes() {
			if !taken[t.GetId()] && !removed[t.GetId()] {
				list = append(list, t)
			}
		}
		answer = mtproto.MakeTLAccountThemes(&mtproto.Account_Themes{Hash: hash, Themes: list}).To_Account_Themes()
		found = true
	}
	if !found {
		return emptyThemes(), nil
	}
	if notModified(in.GetHash(), hash) {
		return mtproto.MakeTLAccountThemesNotModified(nil).To_Account_Themes(), nil
	}
	return answer, nil
}

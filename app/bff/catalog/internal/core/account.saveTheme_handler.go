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

// AccountSaveTheme
// account.saveTheme#f257106c theme:InputTheme unsave:Bool = Bool;
//
// Adds a theme to the account's list or takes it out (core.telegram.org/api/themes).
func (c *CatalogCore) AccountSaveTheme(in *mtproto.TLAccountSaveTheme) (*mtproto.Bool, error) {
	t, inCatalog, err := c.resolveTheme("", in.GetTheme())
	if err != nil {
		return nil, err
	}
	if mtproto.FromBool(in.GetUnsave()) {
		err = c.svcCtx.Appearance.UnsaveTheme(c.ctx, c.MD.UserId, t.GetId(), inCatalog)
	} else {
		err = c.svcCtx.Appearance.SaveTheme(c.ctx, c.MD.UserId, t.GetId())
	}
	if err != nil {
		c.Logger.Errorf("account.saveTheme - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

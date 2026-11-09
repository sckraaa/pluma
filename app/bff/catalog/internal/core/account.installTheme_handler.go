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

// AccountInstallTheme
// account.installTheme#c727bb3b flags:# dark:flags.0?true theme:flags.1?InputTheme format:flags.2?string base_theme:flags.3?BaseTheme = Bool;
//
// The client applied a theme, so the server counts it and tells it of updates
// to the theme from then on (core.telegram.org/api/themes). Without a theme,
// or for a catalog theme, there is nothing to record: tdesktop sends this
// without a theme when the default comes back (data_cloud_themes.cpp:291).
func (c *CatalogCore) AccountInstallTheme(in *mtproto.TLAccountInstallTheme) (*mtproto.Bool, error) {
	if in.GetTheme() == nil {
		return mtproto.BoolTrue, nil
	}
	t, inCatalog, err := c.resolveTheme(in.GetFormat().GetValue(), in.GetTheme())
	if err != nil {
		return nil, err
	}
	if inCatalog {
		return mtproto.BoolTrue, nil
	}
	if err = c.svcCtx.Appearance.InstallTheme(c.ctx, c.MD.UserId, t.GetId()); err != nil {
		c.Logger.Errorf("account.installTheme - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

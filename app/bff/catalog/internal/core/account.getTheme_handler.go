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

// AccountGetTheme
// account.getTheme#3a5869ec format:string theme:InputTheme = Theme;
//
// A theme by id or slug, its file only in the format asked for
// (core.telegram.org/method/account.getTheme).
func (c *CatalogCore) AccountGetTheme(in *mtproto.TLAccountGetTheme) (*mtproto.Theme, error) {
	if err := checkFormat(in.GetFormat()); err != nil {
		return nil, err
	}
	t, _, err := c.resolveTheme(in.GetFormat(), in.GetTheme())
	if err != nil {
		return nil, err
	}
	return forFormat(t, in.GetFormat()), nil
}

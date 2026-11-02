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

// AccountGetMultiWallPapers
// account.getMultiWallPapers#65ad71dc wallpapers:Vector<InputWallPaper> = Vector<WallPaper>;
//
// Wallpapers of the catalog by slug or id, as account.getWallPaper finds
// each; any one it does not have is WALLPAPER_INVALID.
func (c *CatalogCore) AccountGetMultiWallPapers(in *mtproto.TLAccountGetMultiWallPapers) (*mtproto.Vector_WallPaper, error) {
	if len(in.GetWallpapers()) > maxWallPapers {
		return nil, mtproto.ErrLimitInvalid
	}
	out := make([]*mtproto.WallPaper, 0, len(in.GetWallpapers()))
	for _, ask := range in.GetWallpapers() {
		w, err := c.wallPaper(ask)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return &mtproto.Vector_WallPaper{Datas: out}, nil
}

// maxWallPapers bounds one request: a client asks for the wallpapers of its
// themes, a few.
const maxWallPapers = 100

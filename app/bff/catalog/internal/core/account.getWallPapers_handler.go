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
// The wallpapers of the gallery. From the catalog; nothing while it is not imported.
func (c *CatalogCore) AccountGetWallPapers(in *mtproto.TLAccountGetWallPapers) (*mtproto.Account_WallPapers, error) {
	answer := &mtproto.Account_WallPapers{}
	hash, found, err := c.stored("account.getWallPapers", "", answer)
	if err != nil {
		return nil, err
	}
	if !found {
		return mtproto.MakeTLAccountWallPapers(&mtproto.Account_WallPapers{Wallpapers: []*mtproto.WallPaper{}}).To_Account_WallPapers(), nil
	}
	if notModified(in.GetHash(), hash) {
		return mtproto.MakeTLAccountWallPapersNotModified(nil).To_Account_WallPapers(), nil
	}
	return answer, nil
}

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
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/langpack"
)

// LangpackGetLangPack
// langpack.getLangPack#f2f2330a lang_pack:string lang_code:string = LangPackDifference;
// langpack.getLangPack#9ab5c58e lang_code:string = LangPackDifference;
//
// The whole pack with from_version 0 (langpack.Difference). Android uses
// the constructor without lang_pack (TLRPC.java:57563-57624).
func (c *LangpackCore) LangpackGetLangPack(in *mtproto.TLLangpackGetLangPack) (*mtproto.LangPackDifference, error) {
	pack := c.pack(in.GetLangPack())
	l, err := c.language(pack, in.GetLangCode())
	if err != nil {
		return nil, err
	}
	diff, err := c.svcCtx.Dao.FullPack(c.ctx, pack, l.LangCode)
	if errors.Is(err, langpack.ErrNotFound) {
		// Removed since the language was read.
		return nil, c.notFound(pack)
	}
	return diff, err
}

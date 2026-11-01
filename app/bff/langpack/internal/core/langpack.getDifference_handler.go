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

// LangpackGetDifference
// langpack.getDifference#cd984aa5 lang_pack:string lang_code:string from_version:int = LangPackDifference;
//
// The strings changed since from_version, with from_version as asked; the
// whole pack from version 0; nothing from the current version or a later
// one (langpack.Difference). Web K gets no langPackStringDeleted: it would
// show their keys.
func (c *LangpackCore) LangpackGetDifference(in *mtproto.TLLangpackGetDifference) (*mtproto.LangPackDifference, error) {
	pack := c.pack(in.GetLangPack())
	l, err := c.language(pack, in.GetLangCode())
	if err != nil {
		return nil, err
	}
	from := in.GetFromVersion()
	switch {
	case from <= 0:
		diff, err := c.svcCtx.Dao.FullPack(c.ctx, pack, l.LangCode)
		if errors.Is(err, langpack.ErrNotFound) {
			return nil, c.notFound(pack)
		}
		return diff, err
	case from >= l.Version:
		return langpack.Difference(l, nil, from, false), nil
	}
	l, strs, err := c.svcCtx.Dao.Read(c.ctx, pack, l.LangCode, from)
	if errors.Is(err, langpack.ErrNotFound) {
		return nil, c.notFound(pack)
	}
	if err != nil {
		return nil, err
	}
	return langpack.Difference(l, strs, from, pack != "webk"), nil
}

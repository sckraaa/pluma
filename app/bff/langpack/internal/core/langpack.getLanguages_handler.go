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

// LangpackGetLanguages
// langpack.getLanguages#42c6978f lang_pack:string = Vector<LangPackLanguage>;
// langpack.getLanguages#800fd57d = Vector<LangPackLanguage>;
//
// The languages of the pack in Telegram's order; an empty vector for a pack
// the server does not have, not an error: iOS repeats a failed getLanguages
// without end (retryRequest, Localizations.swift:58).
func (c *LangpackCore) LangpackGetLanguages(in *mtproto.TLLangpackGetLanguages) (*mtproto.Vector_LangPackLanguage, error) {
	out := &mtproto.Vector_LangPackLanguage{Datas: []*mtproto.LangPackLanguage{}}
	pack := c.pack(in.GetLangPack())
	if pack == "" {
		return out, nil
	}
	langs, err := c.svcCtx.Dao.Languages(c.ctx, pack)
	if err != nil {
		return nil, err
	}
	for i := range langs {
		out.Datas = append(out.Datas, langs[i].ToTL())
	}
	return out, nil
}

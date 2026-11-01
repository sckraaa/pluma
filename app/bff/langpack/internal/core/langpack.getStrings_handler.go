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

// LangpackGetStrings
// langpack.getStrings#efea3803 lang_pack:string lang_code:string keys:Vector<string> = Vector<LangPackString>;
// langpack.getStrings#2e1ee318 lang_code:string keys:Vector<string> = Vector<LangPackString>;
//
// The strings of keys, langPackStringDeleted for those the pack lacks, as
// Telegram answers. An unknown pack or language gets an empty vector, not an
// error: iOS repeats a failed getStrings without end (retryRequest,
// Localizations.swift:9, 24).
func (c *LangpackCore) LangpackGetStrings(in *mtproto.TLLangpackGetStrings) (*mtproto.Vector_LangPackString, error) {
	empty := &mtproto.Vector_LangPackString{Datas: []*mtproto.LangPackString{}}
	pack := c.pack(in.GetLangPack())
	if pack == "" {
		return empty, nil
	}
	l, err := c.svcCtx.Dao.Language(c.ctx, pack, langpack.CanonicalLangCode(in.GetLangCode()))
	if errors.Is(err, langpack.ErrNotFound) {
		return empty, nil
	}
	if err != nil {
		return nil, err
	}
	found, err := c.svcCtx.Dao.StringsByKey(c.ctx, pack, l.LangCode, in.GetKeys())
	if err != nil {
		return nil, err
	}
	return &mtproto.Vector_LangPackString{Datas: langpack.StringsByKeys(in.GetKeys(), found)}, nil
}

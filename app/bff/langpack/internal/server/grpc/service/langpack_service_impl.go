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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/langpack/internal/core"
)

// LangpackGetLangPack
// langpack.getLangPack#f2f2330a lang_pack:string lang_code:string = LangPackDifference;
// langpack.getLangPack#9ab5c58e lang_code:string = LangPackDifference;
func (s *Service) LangpackGetLangPack(ctx context.Context, request *mtproto.TLLangpackGetLangPack) (*mtproto.LangPackDifference, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("langpack.getLangPack - metadata: {%s}", c.MD)

	r, err := c.LangpackGetLangPack(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// LangpackGetStrings
// langpack.getStrings#efea3803 lang_pack:string lang_code:string keys:Vector<string> = Vector<LangPackString>;
// langpack.getStrings#2e1ee318 lang_code:string keys:Vector<string> = Vector<LangPackString>;
func (s *Service) LangpackGetStrings(ctx context.Context, request *mtproto.TLLangpackGetStrings) (*mtproto.Vector_LangPackString, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("langpack.getStrings - metadata: {%s}", c.MD)

	r, err := c.LangpackGetStrings(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// LangpackGetDifference
// langpack.getDifference#cd984aa5 lang_pack:string lang_code:string from_version:int = LangPackDifference;
func (s *Service) LangpackGetDifference(ctx context.Context, request *mtproto.TLLangpackGetDifference) (*mtproto.LangPackDifference, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("langpack.getDifference - metadata: {%s}", c.MD)

	r, err := c.LangpackGetDifference(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// LangpackGetLanguages
// langpack.getLanguages#42c6978f lang_pack:string = Vector<LangPackLanguage>;
// langpack.getLanguages#800fd57d = Vector<LangPackLanguage>;
func (s *Service) LangpackGetLanguages(ctx context.Context, request *mtproto.TLLangpackGetLanguages) (*mtproto.Vector_LangPackLanguage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("langpack.getLanguages - metadata: {%s}", c.MD)

	r, err := c.LangpackGetLanguages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// LangpackGetLanguage
// langpack.getLanguage#6a596502 lang_pack:string lang_code:string = LangPackLanguage;
func (s *Service) LangpackGetLanguage(ctx context.Context, request *mtproto.TLLangpackGetLanguage) (*mtproto.LangPackLanguage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("langpack.getLanguage - metadata: {%s}", c.MD)

	r, err := c.LangpackGetLanguage(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

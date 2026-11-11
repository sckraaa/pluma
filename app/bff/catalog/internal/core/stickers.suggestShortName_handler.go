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
	"github.com/teamgram/teamgram-server/pkg/catalog/usersets"
)

// StickersSuggestShortName
// stickers.suggestShortName#4dafc503 title:string = stickers.SuggestedShortName;
//
// A short name nobody has for a set titled title (freeShortName). Android
// and iOS put it in the link field of a new set, which the user may change
// (StickersAlert.java:1743, ImportStickers.swift:680).
func (c *CatalogCore) StickersSuggestShortName(in *mtproto.TLStickersSuggestShortName) (*mtproto.Stickers_SuggestedShortName, error) {
	const method = "stickers.suggestShortName"
	if !usersets.ValidTitle(in.GetTitle()) {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrPackTitleInvalid)
		return nil, mtproto.ErrPackTitleInvalid
	}
	name, err := c.freeShortName(method, in.GetTitle())
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLStickersSuggestedShortName(&mtproto.Stickers_SuggestedShortName{ShortName: name}).To_Stickers_SuggestedShortName(), nil
}

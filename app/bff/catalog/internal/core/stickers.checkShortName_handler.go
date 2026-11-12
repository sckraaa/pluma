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

// StickersCheckShortName
// stickers.checkShortName#284b3639 short_name:string = Bool;
//
// Tells if a new set may have the short name: SHORT_NAME_INVALID for one
// that is no short name, SHORT_NAME_OCCUPIED for one a set has. iOS tells
// "taken" by that error and "invalid" by any other
// (ImportStickers.swift:707-712); Android takes any error for "taken"
// (StickersAlert.java:1845-1853).
func (c *CatalogCore) StickersCheckShortName(in *mtproto.TLStickersCheckShortName) (*mtproto.Bool, error) {
	const method = "stickers.checkShortName"
	if !usersets.ValidShortName(in.GetShortName()) {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrShortNameInvalid)
		return nil, mtproto.ErrShortNameInvalid
	}
	taken, err := c.svcCtx.UserSets.ShortNameTaken(c.ctx, in.GetShortName())
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	if taken {
		c.Logger.Errorf("%s - error: %v", method, mtproto.ErrShortNameOccupied)
		return nil, mtproto.ErrShortNameOccupied
	}
	return mtproto.BoolTrue, nil
}

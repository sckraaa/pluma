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
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/demand"
	"google.golang.org/protobuf/proto"
)

// findSet finds the set ask names in the catalog: by id and access hash,
// by short name or by kind. A set by name the catalog lacks is fetched
// from Telegram (task 030), as a link t.me/addstickers/<name> or
// t.me/addemoji/<name> asks; one Telegram has not either is
// STICKERSET_INVALID.
func (c *CatalogCore) findSet(method string, ask *mtproto.InputStickerSet) (*catalog.Set, error) {
	var (
		set *catalog.Set
		err error
	)
	switch ask.GetPredicateName() {
	case mtproto.Predicate_inputStickerSetID:
		set, err = c.svcCtx.Catalog.SetByID(c.ctx, ask.GetId())
		if set != nil && set.AccessHash != ask.GetAccessHash() {
			set = nil
		}
	case mtproto.Predicate_inputStickerSetShortName:
		set, err = c.svcCtx.Catalog.SetByShortName(c.ctx, ask.GetShortName())
	default:
		set, err = c.svcCtx.Catalog.SetByKind(c.ctx, catalog.KindOf(ask))
	}
	if err != nil {
		c.Logger.Errorf("%s - the catalog: %v", method, err)
		return nil, err
	}
	if set == nil && ask.GetPredicateName() == mtproto.Predicate_inputStickerSetShortName && c.svcCtx.Demand != nil {
		set, err = c.svcCtx.Demand.ByShortName(c.ctx, ask.GetShortName())
		if errors.Is(err, demand.ErrNoSet) {
			return nil, mtproto.ErrStickersetInvalid
		}
		if err != nil {
			// Not fetched now: the client asks again, the fetch goes on.
			c.Logger.Errorf("%s - from Telegram: %v", method, err)
			return nil, err
		}
	}
	if set == nil {
		return nil, mtproto.ErrStickersetInvalid
	}
	return set, nil
}

// decodeSet is a stored set's messages.stickerSet.
func (c *CatalogCore) decodeSet(method string, set *catalog.Set) (*mtproto.Messages_StickerSet, error) {
	answer := &mtproto.Messages_StickerSet{}
	if err := proto.Unmarshal(set.Data, answer); err != nil {
		c.Logger.Errorf("%s - a stored set: %v", method, err)
		return nil, err
	}
	// The account that made the set may change it (task 084): the clients
	// offer that by creator (Android StickersAlert.java:2365, Telegram
	// Desktop StickersSetFlag::AmCreator, api_stickers_creator.cpp:144).
	if answer.GetSet() != nil {
		answer.Set.Creator = set.OwnerID != 0 && set.OwnerID == c.MD.UserId
	}
	return answer, nil
}

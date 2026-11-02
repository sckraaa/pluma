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
	"github.com/teamgram/teamgram-server/pkg/catalog/installed"
	"google.golang.org/protobuf/proto"
)

// allStickers is messages.allStickers of the sets of kind the account
// installed, in its order, each with its installed_date and not archived
// (Telegram Desktop drops a set otherwise, docs/tasks/030-research). Its
// hash is the one clients count over the sets' own hashes
// (stickersHash); for that hash, allStickersNotModified.
func (c *CatalogCore) allStickers(method string, kind installed.Kind, hash int64) (*mtproto.Messages_AllStickers, error) {
	rows, err := c.svcCtx.Installed.Installed(c.ctx, c.MD.UserId, kind)
	if err != nil {
		// Android asks again every second after an error with hash 0;
		// nothing else can be answered without the list.
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	sets := make([]*mtproto.StickerSet, 0, len(rows))
	for _, r := range rows {
		set, err := c.svcCtx.Catalog.SetByID(c.ctx, r.SetID)
		if err != nil {
			c.Logger.Errorf("%s - the catalog: %v", method, err)
			return nil, err
		}
		if set == nil {
			continue // gone from the catalog
		}
		ms, err := c.decodeSet(method, set)
		if err != nil {
			return nil, err
		}
		sets = append(sets, asInstalled(ms, r.InstalledAt).GetSet())
	}
	h := stickersHash(sets)
	if notModified(hash, h) {
		return mtproto.MakeTLMessagesAllStickersNotModified(nil).To_Messages_AllStickers(), nil
	}
	return mtproto.MakeTLMessagesAllStickers(&mtproto.Messages_AllStickers{Hash: h, Sets: sets}).To_Messages_AllStickers(), nil
}

// stickersHash is the hash of a list of sets as clients count it: the
// sets' own hashes, sign-extended, in order (Android calcStickersHash,
// Telegram Desktop CountStickersOrderHash, docs/tasks/030-research/hash.md).
func stickersHash(sets []*mtproto.StickerSet) int64 {
	var h uint64
	for _, s := range sets {
		h ^= h >> 21
		h ^= h << 35
		h ^= h >> 4
		h += uint64(int64(s.GetHash()))
	}
	return int64(h)
}

// asInstalled is a set as an installed one: with its installed_date, not
// archived.
func asInstalled(ms *mtproto.Messages_StickerSet, installedAt int64) *mtproto.Messages_StickerSet {
	out := proto.Clone(ms).(*mtproto.Messages_StickerSet)
	out.Set.InstalledDate = mtproto.MakeFlagsInt32(int32(installedAt))
	out.Set.Archived = false
	return out
}

// asArchived is a set as an archived one: archived, with its installed_date.
func asArchived(ms *mtproto.Messages_StickerSet, installedAt int64) *mtproto.StickerSet {
	s := proto.Clone(ms.GetSet()).(*mtproto.StickerSet)
	s.InstalledDate = mtproto.MakeFlagsInt32(int32(installedAt))
	s.Archived = true
	return s
}

// toggle applies action to the sets asked for; a set the catalog does not
// have is STICKERSET_INVALID.
func (c *CatalogCore) toggle(method string, action installed.Action, asks []*mtproto.InputStickerSet) error {
	refs := make([]installed.Ref, 0, len(asks))
	for _, ask := range asks {
		set, err := c.findSet(method, ask)
		if err != nil {
			return err
		}
		ms, err := c.decodeSet(method, set)
		if err != nil {
			return err
		}
		refs = append(refs, installed.Ref{SetID: set.ID, Kind: installed.KindOf(ms.GetSet())})
	}
	if err := c.svcCtx.Installed.Toggle(c.ctx, c.MD.UserId, action, refs); err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return err
	}
	return nil
}

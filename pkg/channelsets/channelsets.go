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

// Package channelsets keeps the sticker set and the emoji set a supergroup
// chose for its members (channels.setStickers, channels.setEmojiStickers,
// channelFull.stickerset and emojiset; task 078). The sets are those of the
// catalog imported from Telegram (task 024).
package channelsets

import (
	"context"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"google.golang.org/protobuf/proto"
)

// Store reads the chosen sets and the catalog.
type Store struct {
	db      *sqlx.DB
	catalog *catalog.Store
}

// New is a Store over the database.
func New(db *sqlx.DB) *Store {
	return &Store{db: db, catalog: catalog.NewStore(db)}
}

// Resolve is the catalog set in names: nil for inputStickerSetEmpty (no
// set), STICKERSET_INVALID for one the catalog does not have.
func (s *Store) Resolve(ctx context.Context, in *mtproto.InputStickerSet) (*catalog.Set, error) {
	var (
		set *catalog.Set
		err error
	)
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputStickerSetEmpty, "":
		return nil, nil
	case mtproto.Predicate_inputStickerSetID:
		set, err = s.catalog.SetByID(ctx, in.GetId())
		if set != nil && set.AccessHash != in.GetAccessHash() {
			set = nil
		}
	case mtproto.Predicate_inputStickerSetShortName:
		set, err = s.catalog.SetByShortName(ctx, in.GetShortName())
	}
	if err != nil {
		return nil, err
	}
	if set == nil {
		return nil, mtproto.ErrStickersetInvalid
	}
	return set, nil
}

// Of is the sticker set and the emoji set of the channel, nil where it chose
// none.
func (s *Store) Of(ctx context.Context, channelId int64) (stickers, emoji *mtproto.StickerSet, err error) {
	var rows []struct {
		Emoji bool  `db:"emoji"`
		SetId int64 `db:"set_id"`
	}
	if err = s.db.QueryRowsPartial(ctx, &rows, "SELECT emoji, set_id FROM channel_sticker_sets WHERE channel_id = ?", channelId); err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		set, err := s.catalog.SetByID(ctx, r.SetId)
		if err != nil {
			return nil, nil, err
		}
		if set == nil {
			continue
		}
		ms := &mtproto.Messages_StickerSet{}
		if err = proto.Unmarshal(set.Data, ms); err != nil {
			return nil, nil, err
		}
		if r.Emoji {
			emoji = ms.GetSet()
		} else {
			stickers = ms.GetSet()
		}
	}
	return stickers, emoji, nil
}

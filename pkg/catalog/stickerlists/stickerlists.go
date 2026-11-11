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

// Package stickerlists keeps the recently used and the faved stickers of an
// account (task 079; migrate-20261111-user-sticker-lists.sql), each list the
// newest first, and tells every device of the account of a change as the
// clients expect: updateRecentStickers or updateFavedStickers, on which
// they load the list again (Telegram Desktop api_updates.cpp, case
// mtpc_updateRecentStickers and mtpc_updateFavedStickers). The updates go
// with seq (ADR 0020).
package stickerlists

import (
	"context"
	"database/sql"
	"slices"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// List is a list of stickers of an account.
type List int8

const (
	// Recent is the stickers the account sent lately.
	Recent List = 0
	// RecentAttached is the stickers the account attached lately to photos
	// and videos (messages.getRecentStickers with attached).
	RecentAttached List = 1
	// Faved is the stickers the account faved.
	Faved List = 2
)

// RecentLimit is the most stickers a recent list keeps: stickers_recent_limit
// of help.getConfig, to which Android and iOS trim their lists
// (MediaDataController.maxRecentStickersCount, ManagedConfigurationUpdates
// maxRecentStickerCount) and Telegram Desktop its old local one
// (Stickers::incrementSticker).
const RecentLimit = 200

// FavedLimit is the most stickers the faved list keeps. Telegram Desktop and
// iOS read it from help.getAppConfig, stickers_faved_limit_premium for
// Premium (data_premium_limits.cpp, UserLimitsConfiguration.swift), and
// everybody has Premium (ADR 0041); Android keeps 5 whatever the config says
// (MessagesController.maxFaveStickersCount), so the app config gives 5 for
// both and the list keeps as many: every client shows the same stickers.
// A sticker faved over the limit drops the oldest, as Telegram Desktop does
// with its own list (Stickers::checkFavedLimit).
const FavedLimit = 5

// Limit is the most stickers list keeps.
func Limit(list List) int {
	if list == Faved {
		return FavedLimit
	}
	return RecentLimit
}

// Entry is a sticker of a list and the date it was used or faved.
type Entry struct {
	DocumentID int64 `db:"document_id"`
	Order      int64 `db:"order2"`
	UsedAt     int64 `db:"used_at"`
}

// Store is the lists of stickers of accounts.
type Store struct {
	db    *sqlx.DB
	boxDB *sql.DB
	topic string // of the pushes (sync_client.EmitSeqUpdate)
}

// NewStore returns the store over db (reads) and boxDB (changes, in the
// account's box transaction), pushing to topic.
func NewStore(db *sqlx.DB, boxDB *sql.DB, topic string) *Store {
	return &Store{db: db, boxDB: boxDB, topic: topic}
}

const columns = "document_id, order2, used_at"

// Entries returns the stickers of list, the newest first.
func (s *Store) Entries(ctx context.Context, userID int64, list List) ([]Entry, error) {
	var rows []Entry
	err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+columns+" FROM user_sticker_lists WHERE user_id = ? AND list = ? ORDER BY order2 DESC", userID, list)
	return rows, err
}

func entries(b *userbox.Box, userID int64, list List) ([]Entry, error) {
	var rows []Entry
	err := b.Tx().QueryRowsPartial(&rows, "SELECT "+columns+" FROM user_sticker_lists WHERE user_id = ? AND list = ? ORDER BY order2 DESC", userID, list)
	return rows, err
}

// run is a change in the account's box; the pushes it emits go out after.
func (s *Store) run(ctx context.Context, userID int64, fn func(b *userbox.Box) error) error {
	res, err := userbox.Run(ctx, s.boxDB, userID, fn)
	if err != nil {
		return err
	}
	if res.Emitted > 0 {
		outbox.Notify()
	}
	return nil
}

// Add puts the sticker at the top of list with the date of now, and drops
// the oldest past the list's limit. A sticker at the top already only gets
// the new date: the list the clients count their hash over is the same, so
// nothing is pushed.
func (s *Store) Add(ctx context.Context, userID int64, list List, documentID int64) error {
	return s.run(ctx, userID, func(b *userbox.Box) error {
		rows, err := entries(b, userID, list)
		if err != nil {
			return err
		}
		now := b.Date()
		if len(rows) > 0 && rows[0].DocumentID == documentID {
			_, err := b.Tx().Exec("UPDATE user_sticker_lists SET used_at = ? WHERE user_id = ? AND list = ? AND document_id = ?", now, userID, list, documentID)
			return err
		}
		order := int64(1)
		if len(rows) > 0 {
			order = rows[0].Order + 1
		}
		if _, err := b.Tx().Exec("INSERT INTO user_sticker_lists (user_id, list, document_id, order2, used_at) VALUES (?, ?, ?, ?, ?)"+
			" AS new ON DUPLICATE KEY UPDATE order2 = new.order2, used_at = new.used_at", userID, list, documentID, order, now); err != nil {
			return err
		}
		// The list is now the sticker, then the other rows in their order:
		// those past the limit go.
		if at := slices.IndexFunc(rows, func(e Entry) bool { return e.DocumentID == documentID }); at >= 0 {
			rows = slices.Delete(rows, at, at+1)
		}
		for i := Limit(list) - 1; i < len(rows); i++ {
			if _, err := b.Tx().Exec("DELETE FROM user_sticker_lists WHERE user_id = ? AND list = ? AND document_id = ?", userID, list, rows[i].DocumentID); err != nil {
				return err
			}
		}
		return s.emit(b, list)
	})
}

// Remove takes the sticker off list; a sticker not in it changes nothing
// and nothing is pushed.
func (s *Store) Remove(ctx context.Context, userID int64, list List, documentID int64) error {
	return s.run(ctx, userID, func(b *userbox.Box) error {
		res, err := b.Tx().Exec("DELETE FROM user_sticker_lists WHERE user_id = ? AND list = ? AND document_id = ?", userID, list, documentID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		return s.emit(b, list)
	})
}

// Clear empties list; an empty one changes nothing and nothing is pushed.
func (s *Store) Clear(ctx context.Context, userID int64, list List) error {
	return s.run(ctx, userID, func(b *userbox.Box) error {
		res, err := b.Tx().Exec("DELETE FROM user_sticker_lists WHERE user_id = ? AND list = ?", userID, list)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		return s.emit(b, list)
	})
}

// Changed is the update that tells the devices to load list again. The
// recent lists share updateRecentStickers, which has no flags. Telegram
// Desktop loads the list again at once (api_updates.cpp,
// setLastRecentUpdate(0) and setLastFavedUpdate(0)); Android loads the
// faved stickers at once and the recent ones at its next load of stickers
// (MessagesController.processUpdateArray: loadRecents(TYPE_FAVE), and
// lastStickersLoadTime reset); iOS takes neither and loads both lists on
// its own every hour (StickerManagement.swift, manageStickerPacks).
func Changed(list List) *mtproto.Update {
	if list == Faved {
		return mtproto.MakeTLUpdateFavedStickers(nil).To_Update()
	}
	return mtproto.MakeTLUpdateRecentStickers(nil).To_Update()
}

// emit journals the change of list for every device of the account, the one
// that made it too: the update only says "load the list again", so the echo
// costs that device a request answered not modified, and a replay of the
// journal's tail ends in the list the account has.
func (s *Store) emit(b *userbox.Box, list List) error {
	return sync_client.EmitSeqUpdate(b, s.topic, Changed(list), nil, nil)
}

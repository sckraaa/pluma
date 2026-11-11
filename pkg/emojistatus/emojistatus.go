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

// Package emojistatus keeps the emoji statuses an account used recently
// (task 084): the list account.getRecentEmojiStatuses gives and
// account.clearRecentEmojiStatuses empties. A status joins the list in the
// transaction that sets it (Use, called by biz user), and every change of
// the list tells the devices of the account to load it again
// (updateRecentEmojiStatuses with seq, ADR 0020; "Other logged-in clients
// will also receive an updateRecentEmojiStatuses update",
// core.telegram.org/api/emoji-status).
package emojistatus

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

// RecentLimit is how many recent statuses an account keeps. Telegram's
// number is not documented and no client limits the list itself; this is
// the server's own bound, so the list of an account cannot grow without
// end.
const RecentLimit = 50

// Entry is a recent status of an account.
type Entry struct {
	DocumentID int64 `db:"document_id"`
	Order      int64 `db:"order2"`
	UsedAt     int64 `db:"used_at"`
}

// Store reads and clears the recent statuses. boxDB and topic serve Clear;
// a Store that only reads needs neither.
type Store struct {
	db    *sqlx.DB
	boxDB *sql.DB
	topic string // of the pushes (sync_client.EmitSeqUpdate)
}

// NewStore returns a Store on db, the box connection and the push topic.
func NewStore(db *sqlx.DB, boxDB *sql.DB, topic string) *Store {
	return &Store{db: db, boxDB: boxDB, topic: topic}
}

const selectRecent = "SELECT document_id, order2, used_at FROM user_recent_emoji_statuses WHERE user_id = ? ORDER BY order2 DESC"

// Recent returns the recent statuses of the account, the newest first.
func (s *Store) Recent(ctx context.Context, userID int64) ([]Entry, error) {
	var rows []Entry
	err := s.db.QueryRowsPartial(ctx, &rows, selectRecent, userID)
	return rows, err
}

// Clear empties the recent statuses of the account. An empty list changes
// nothing and tells nobody.
func (s *Store) Clear(ctx context.Context, userID int64) error {
	res, err := userbox.Run(ctx, s.boxDB, userID, func(b *userbox.Box) error {
		r, err := b.Tx().Exec("DELETE FROM user_recent_emoji_statuses WHERE user_id = ?", userID)
		if err != nil {
			return err
		}
		if n, err := r.RowsAffected(); err != nil || n == 0 {
			return err
		}
		return sync_client.EmitSeqUpdate(b, s.topic, Changed(), nil, nil)
	})
	if err != nil {
		return err
	}
	if res.Emitted > 0 {
		outbox.Notify()
	}
	return nil
}

// Changed is the update that tells a device to load the recent statuses
// again.
func Changed() *mtproto.Update {
	return mtproto.MakeTLUpdateRecentEmojiStatuses(nil).To_Update()
}

// Use puts the status documentID at the head of the recent statuses of b's
// owner, in b, and drops what no longer fits RecentLimit. It tells whether
// the list changed: a status that already heads it only gets its date.
// The caller emits Changed with seq when it did.
func Use(b *userbox.Box, documentID int64) (bool, error) {
	userID := b.UserId()
	var rows []Entry
	if err := b.Tx().QueryRowsPartial(&rows, selectRecent, userID); err != nil {
		return false, err
	}
	now := b.Date()
	if len(rows) > 0 && rows[0].DocumentID == documentID {
		_, err := b.Tx().Exec("UPDATE user_recent_emoji_statuses SET used_at = ? WHERE user_id = ? AND document_id = ?", now, userID, documentID)
		return false, err
	}
	order := int64(1)
	if len(rows) > 0 {
		order = rows[0].Order + 1
	}
	if _, err := b.Tx().Exec("INSERT INTO user_recent_emoji_statuses (user_id, document_id, order2, used_at) VALUES (?, ?, ?, ?)"+
		" AS new ON DUPLICATE KEY UPDATE order2 = new.order2, used_at = new.used_at", userID, documentID, order, now); err != nil {
		return false, err
	}
	if at := slices.IndexFunc(rows, func(e Entry) bool { return e.DocumentID == documentID }); at >= 0 {
		rows = slices.Delete(rows, at, at+1)
	}
	for i := RecentLimit - 1; i < len(rows); i++ {
		if _, err := b.Tx().Exec("DELETE FROM user_recent_emoji_statuses WHERE user_id = ? AND document_id = ?", userID, rows[i].DocumentID); err != nil {
			return false, err
		}
	}
	return true, nil
}

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

// Package saved keeps the notification sounds an account saved (task
// 084): the list account.getSavedRingtones gives, which account.saveRingtone
// and account.uploadRingtone change. Every change of the list tells the
// devices of the account to load it again (updateSavedRingtones with seq,
// ADR 0020; "The client will receive an updateSavedRingtones update if the
// list is modified by the user on other clients",
// core.telegram.org/api/ringtones).
package saved

import (
	"context"
	"database/sql"
	"errors"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/ringtones"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ErrTooMany is returned by Save when the account already keeps
// ringtones.SavedCountMax sounds.
var ErrTooMany = errors.New("ringtones: the list of the account is full")

// Entry is a saved sound of an account.
type Entry struct {
	DocumentID int64 `db:"document_id"`
	Order      int64 `db:"order2"`
	SavedAt    int64 `db:"saved_at"`
}

// Store reads and writes the saved sounds.
type Store struct {
	db    *sqlx.DB
	boxDB *sql.DB
	topic string // of the pushes (sync_client.EmitSeqUpdate)
}

// NewStore returns a Store on db, the box connection and the push topic.
func NewStore(db *sqlx.DB, boxDB *sql.DB, topic string) *Store {
	return &Store{db: db, boxDB: boxDB, topic: topic}
}

const selectSaved = "SELECT document_id, order2, saved_at FROM user_saved_ringtones WHERE user_id = ? ORDER BY order2 DESC"

// Saved returns the sounds of the account, the newest first.
func (s *Store) Saved(ctx context.Context, userID int64) ([]Entry, error) {
	var rows []Entry
	err := s.db.QueryRowsPartial(ctx, &rows, selectSaved, userID)
	return rows, err
}

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

// Save puts the sound documentID at the head of the list of the account. A
// sound already saved stays where it is and tells nobody; a new one over
// ringtones.SavedCountMax is ErrTooMany. The count is taken under the lock of the
// account's box, so two devices saving at once cannot pass the limit.
func (s *Store) Save(ctx context.Context, userID, documentID int64) error {
	return s.run(ctx, userID, func(b *userbox.Box) error {
		var rows []Entry
		if err := b.Tx().QueryRowsPartial(&rows, selectSaved, userID); err != nil {
			return err
		}
		order := int64(1)
		for _, r := range rows {
			if r.DocumentID == documentID {
				return nil
			}
		}
		if len(rows) >= ringtones.SavedCountMax {
			return ErrTooMany
		}
		if len(rows) > 0 {
			order = rows[0].Order + 1
		}
		if _, err := b.Tx().Exec("INSERT INTO user_saved_ringtones (user_id, document_id, order2, saved_at) VALUES (?, ?, ?, ?)",
			userID, documentID, order, b.Date()); err != nil {
			return err
		}
		return sync_client.EmitSeqUpdate(b, s.topic, Changed(), nil, nil)
	})
}

// Unsave removes the sound documentID from the list of the account; one
// that is not there changes nothing and tells nobody.
func (s *Store) Unsave(ctx context.Context, userID, documentID int64) error {
	return s.run(ctx, userID, func(b *userbox.Box) error {
		res, err := b.Tx().Exec("DELETE FROM user_saved_ringtones WHERE user_id = ? AND document_id = ?", userID, documentID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			return err
		}
		return sync_client.EmitSeqUpdate(b, s.topic, Changed(), nil, nil)
	})
}

// Changed is the update that tells a device to load the saved sounds again.
func Changed() *mtproto.Update {
	return mtproto.MakeTLUpdateSavedRingtones(nil).To_Update()
}

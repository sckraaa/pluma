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

// Package installed keeps the sticker, mask and custom emoji sets an
// account installed (task 030; migrate-20261103-user-sticker-sets.sql),
// each kind in its own order, and tells the account's other devices of a
// change as Telegram does (docs/tasks/030-research/updates.md): an
// installed set whole (updateNewStickerSet), a new order
// (updateStickerSetsOrder), anything else as "load the kind again"
// (updateStickerSets). The updates go with seq (ADR 0020) to every device
// of the account but the one that made the change (ADR 0022).
package installed

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

// Kind is a kind of set, ordered on its own.
type Kind int8

const (
	Stickers Kind = 0
	Masks    Kind = 1
	Emojis   Kind = 2
)

// KindOf is the kind of set.
func KindOf(set *mtproto.StickerSet) Kind {
	switch {
	case set.GetEmojis():
		return Emojis
	case set.GetMasks():
		return Masks
	default:
		return Stickers
	}
}

// Entry is a set of an account: installed, or archived when ArchivedAt is
// not 0.
type Entry struct {
	SetID       int64 `db:"set_id"`
	Kind        Kind  `db:"kind"`
	InstalledAt int64 `db:"installed_at"`
	ArchivedAt  int64 `db:"archived_at"`
	Order       int64 `db:"order2"`
}

// Store is the sets of accounts.
type Store struct {
	db    *sqlx.DB
	boxDB *sql.DB
	topic string // of the pushes (sync_client.EmitSeqUpdateExcept)
}

// NewStore returns the store over db (reads) and boxDB (changes, in the
// account's box transaction), pushing to topic.
func NewStore(db *sqlx.DB, boxDB *sql.DB, topic string) *Store {
	return &Store{db: db, boxDB: boxDB, topic: topic}
}

const columns = "set_id, kind, installed_at, archived_at, order2"

// Installed returns the installed sets of kind, in the account's order.
func (s *Store) Installed(ctx context.Context, userID int64, kind Kind) ([]Entry, error) {
	var rows []Entry
	err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+columns+" FROM user_sticker_sets WHERE user_id = ? AND kind = ? AND archived_at = 0 ORDER BY order2 DESC", userID, kind)
	return rows, err
}

// Archived returns the archived sets of kind, the last archived first.
func (s *Store) Archived(ctx context.Context, userID int64, kind Kind) ([]Entry, error) {
	var rows []Entry
	err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+columns+" FROM user_sticker_sets WHERE user_id = ? AND kind = ? AND archived_at > 0 ORDER BY archived_at DESC, set_id DESC", userID, kind)
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

func entries(b *userbox.Box, userID int64, kind Kind) ([]Entry, error) {
	var rows []Entry
	err := b.Tx().QueryRowsPartial(&rows, "SELECT "+columns+" FROM user_sticker_sets WHERE user_id = ? AND kind = ? ORDER BY order2 DESC", userID, kind)
	return rows, err
}

func topOrder(rows []Entry) int64 {
	top := int64(0)
	for _, r := range rows {
		top = max(top, r.Order)
	}
	return top + 1
}

// Install installs set at the top of its kind, or archives it if archived:
// a set installed already moves to the top, an archived one is installed
// again. update is what the other devices get for an install: the set
// whole (updateNewStickerSet), with its installed_date; for an archive they
// get updateStickerSets. A change that changes nothing pushes nothing.
func (s *Store) Install(ctx context.Context, userID, authKeyID int64, kind Kind, setID int64, archived bool, newSet func(installedAt int64) *mtproto.Update) error {
	return s.run(ctx, userID, func(b *userbox.Box) error {
		rows, err := entries(b, userID, kind)
		if err != nil {
			return err
		}
		at := slices.IndexFunc(rows, func(e Entry) bool { return e.SetID == setID })
		now := b.Date()
		if archived {
			if at >= 0 && rows[at].ArchivedAt > 0 {
				return nil
			}
			installedAt := now
			if at >= 0 {
				installedAt = rows[at].InstalledAt
			}
			if _, err := b.Tx().Exec("INSERT INTO user_sticker_sets (user_id, set_id, kind, order2, installed_at, archived_at) VALUES (?, ?, ?, ?, ?, ?)"+
				" AS new ON DUPLICATE KEY UPDATE archived_at = new.archived_at", userID, setID, kind, topOrder(rows), installedAt, now); err != nil {
				return err
			}
			return s.emit(b, setsChanged(kind), authKeyID)
		}
		installed := installedOf(rows)
		if len(installed) > 0 && installed[0].SetID == setID {
			return nil // at the top already
		}
		installedAt := now
		if at >= 0 && rows[at].ArchivedAt == 0 {
			installedAt = rows[at].InstalledAt
		}
		if _, err := b.Tx().Exec("INSERT INTO user_sticker_sets (user_id, set_id, kind, order2, installed_at, archived_at) VALUES (?, ?, ?, ?, ?, 0)"+
			" AS new ON DUPLICATE KEY UPDATE order2 = new.order2, installed_at = new.installed_at, archived_at = 0",
			userID, setID, kind, topOrder(rows), installedAt); err != nil {
			return err
		}
		return s.emit(b, newSet(installedAt), authKeyID)
	})
}

func installedOf(rows []Entry) []Entry {
	var out []Entry
	for _, r := range rows {
		if r.ArchivedAt == 0 {
			out = append(out, r)
		}
	}
	return out
}

// Action is what Toggle does to sets.
type Action int

const (
	Uninstall Action = iota
	Archive
	Unarchive
)

// Ref is a set and its kind.
type Ref struct {
	SetID int64
	Kind  Kind
}

// Toggle uninstalls, archives or unarchives sets (an unarchived set goes to
// the top of its kind). The other devices load each kind it changed again
// (updateStickerSets); a set it does not change is left alone.
func (s *Store) Toggle(ctx context.Context, userID, authKeyID int64, action Action, sets []Ref) error {
	return s.run(ctx, userID, func(b *userbox.Box) error {
		changed := map[Kind]bool{}
		for _, ref := range sets {
			rows, err := entries(b, userID, ref.Kind)
			if err != nil {
				return err
			}
			at := slices.IndexFunc(rows, func(e Entry) bool { return e.SetID == ref.SetID })
			now := b.Date()
			var res sql.Result
			switch {
			case action == Uninstall && at >= 0:
				res, err = b.Tx().Exec("DELETE FROM user_sticker_sets WHERE user_id = ? AND set_id = ?", userID, ref.SetID)
			case action == Archive && at >= 0 && rows[at].ArchivedAt == 0:
				res, err = b.Tx().Exec("UPDATE user_sticker_sets SET archived_at = ? WHERE user_id = ? AND set_id = ?", now, userID, ref.SetID)
			case action == Archive && at < 0:
				res, err = b.Tx().Exec("INSERT INTO user_sticker_sets (user_id, set_id, kind, order2, installed_at, archived_at) VALUES (?, ?, ?, ?, ?, ?)",
					userID, ref.SetID, ref.Kind, topOrder(rows), now, now)
			case action == Unarchive && at >= 0 && rows[at].ArchivedAt > 0:
				res, err = b.Tx().Exec("UPDATE user_sticker_sets SET archived_at = 0, order2 = ?, installed_at = ? WHERE user_id = ? AND set_id = ?",
					topOrder(rows), now, userID, ref.SetID)
			default:
				continue
			}
			if err != nil {
				return err
			}
			if n, err := res.RowsAffected(); err == nil && n > 0 {
				changed[ref.Kind] = true
			}
		}
		for _, k := range []Kind{Stickers, Masks, Emojis} {
			if changed[k] {
				if err := s.emit(b, setsChanged(k), authKeyID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Reorder puts the installed sets of kind in order, and returns the order
// stored: the sets of order the account has installed, each once, then
// those it left out in their old order. The other devices get the whole
// order (updateStickerSetsOrder): iOS and Telegram Desktop load the kind
// again when it differs from what they have.
func (s *Store) Reorder(ctx context.Context, userID, authKeyID int64, kind Kind, order []int64) ([]int64, error) {
	var stored []int64
	err := s.run(ctx, userID, func(b *userbox.Box) error {
		rows, err := entries(b, userID, kind)
		if err != nil {
			return err
		}
		installed := installedOf(rows)
		have := map[int64]bool{}
		for _, r := range installed {
			have[r.SetID] = true
		}
		stored = stored[:0]
		seen := map[int64]bool{}
		for _, id := range order {
			if have[id] && !seen[id] {
				stored = append(stored, id)
				seen[id] = true
			}
		}
		for _, r := range installed {
			if !seen[r.SetID] {
				stored = append(stored, r.SetID)
			}
		}
		old := make([]int64, len(installed))
		for i, r := range installed {
			old[i] = r.SetID
		}
		if slices.Equal(old, stored) {
			return nil
		}
		for i, id := range stored {
			if _, err := b.Tx().Exec("UPDATE user_sticker_sets SET order2 = ? WHERE user_id = ? AND set_id = ?", int64(len(stored)-i), userID, id); err != nil {
				return err
			}
		}
		return s.emit(b, mtproto.MakeTLUpdateStickerSetsOrder(&mtproto.Update{
			Masks: kind == Masks, Emojis: kind == Emojis, Order_VECTORINT64: slices.Clone(stored),
		}).To_Update(), authKeyID)
	})
	return stored, err
}

// setsChanged tells the other devices to load the sets of kind again.
func setsChanged(kind Kind) *mtproto.Update {
	return mtproto.MakeTLUpdateStickerSets(&mtproto.Update{Masks: kind == Masks, Emojis: kind == Emojis}).To_Update()
}

func (s *Store) emit(b *userbox.Box, u *mtproto.Update, authKeyID int64) error {
	return sync_client.EmitSeqUpdateExcept(b, s.topic, u, nil, nil, authKeyID)
}

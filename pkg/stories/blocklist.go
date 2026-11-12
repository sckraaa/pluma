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

package stories

import (
	"context"
	"database/sql"
	"errors"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// The blocklist of stories (contacts.block with my_stories_from): the users
// a user hides its stories from, while they still write and call it. It is
// apart from the main blocklist, whose users see no story of the user
// either. Each change goes with updatePeerBlocked, which carries the state
// of both lists: every client reads the update as the whole state of the
// peer (Android MessagesController.java:19265-19273, iOS
// UpdateInterpreter+Peers.swift:131-147, Web K appProfileManager.ts:
// 1783-1803).

// MaxBlocked is the most users the blocklist of stories of a user holds,
// as contacts.setBlocked is limited for the main one.
const MaxBlocked = 1000

// Blocked is a user on the blocklist of stories and when it was put there.
type Blocked struct {
	PeerId int64 `db:"peer_id"`
	Date   int32 `db:"date"`
}

// SetBlockedTx puts the user peerId on the blocklist of stories of userId
// at now, or takes it off, in tx, and reports whether that changed the
// list.
func SetBlockedTx(tx *sqlx.Tx, userId, peerId int64, blocked bool, now int32) (bool, error) {
	var (
		res sql.Result
		err error
	)
	if blocked {
		res, err = tx.Exec("INSERT IGNORE INTO story_blocked (user_id, peer_id, `date`) VALUES (?, ?, ?)", userId, peerId, now)
	} else {
		res, err = tx.Exec("DELETE FROM story_blocked WHERE user_id = ? AND peer_id = ?", userId, peerId)
	}
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// BlockedListTx returns the whole blocklist of stories of userId in tx,
// with its rows locked for the change that reads it.
func BlockedListTx(tx *sqlx.Tx, userId int64) ([]int64, error) {
	var ids []int64
	if err := tx.QueryRowsPartial(&ids, "SELECT peer_id FROM story_blocked WHERE user_id = ? ORDER BY peer_id FOR UPDATE", userId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return ids, nil
}

// IsBlockedTx tells, in tx, whether userId hides its stories from peerId.
func IsBlockedTx(tx *sqlx.Tx, userId, peerId int64) (bool, error) {
	var ids []int64
	if err := tx.QueryRowsPartial(&ids, "SELECT peer_id FROM story_blocked WHERE user_id = ? AND peer_id = ?", userId, peerId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return false, err
	}
	return len(ids) > 0, nil
}

// MainBlockedTx tells, in tx, whether userId has peerId on its main
// blocklist (the table of the user service, user_peer_blocks).
func MainBlockedTx(tx *sqlx.Tx, userId, peerId int64) (bool, error) {
	var ids []int64
	if err := tx.QueryRowsPartial(&ids, "SELECT peer_id FROM user_peer_blocks WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0",
		userId, mtproto.PEER_USER, peerId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return false, err
	}
	return len(ids) > 0, nil
}

// BlockedPage returns the blocklist of stories of userId, newest first,
// limit users from offset, and how many there are in all.
func (s *Store) BlockedPage(ctx context.Context, userId int64, offset, limit int32) ([]Blocked, int32, error) {
	var list []Blocked
	if err := s.db.QueryRowsPartial(ctx, &list, "SELECT peer_id, `date` FROM story_blocked WHERE user_id = ? ORDER BY `date` DESC, peer_id DESC LIMIT ? OFFSET ?",
		userId, limit, offset); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, 0, err
	}
	total, err := count(ctx, s.db, "SELECT COUNT(*) FROM story_blocked WHERE user_id = ?", userId)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// BlockedOf returns which of the users ids userId hides its stories from.
func BlockedOf(ctx context.Context, db *sqlx.DB, userId int64, ids []int64) (map[int64]bool, error) {
	out := make(map[int64]bool)
	if len(ids) == 0 {
		return out, nil
	}
	var blocked []int64
	if err := db.QueryRowsPartial(ctx, &blocked, "SELECT peer_id FROM story_blocked WHERE user_id = ? AND peer_id IN ("+in(len(ids))+")",
		int64Args([]any{userId}, ids)...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	for _, id := range blocked {
		out[id] = true
	}
	return out, nil
}

// shutOut returns the users who see no story of userId whatever its rules
// say: those on its main blocklist and those on its blocklist of stories.
func shutOut(ctx context.Context, db *sqlx.DB, userId int64) ([]int64, error) {
	var ids []int64
	if err := db.QueryRowsPartial(ctx, &ids, "SELECT peer_id FROM user_peer_blocks WHERE user_id = ? AND peer_type = ? AND deleted = 0"+
		" UNION SELECT peer_id FROM story_blocked WHERE user_id = ?", userId, mtproto.PEER_USER, userId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return ids, nil
}

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

package dao

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// SeqRow is a row of the seq journal (user_seq_updates, ADR 0020).
type SeqRow struct {
	Seq        int32
	Pts        int32 // of the box when it was written
	UpdateType string
	UpdateData string
}

// SeqUpdates returns the rows of userId's seq journal up to maxSeq that
// are after cursor or were written at since or later, in seq order, but
// those the authorization on the permanent key authKeyId must not get (ADR
// 0022; 0 is no authorization) and a bot's presses and inline queries past
// their time. It reads the master (BoxDB), where the state maxSeq is from:
// every row up to it is committed there.
func (d *Dao) SeqUpdates(ctx context.Context, userId, authKeyId int64, maxSeq, cursor int32, since int64) ([]SeqRow, error) {
	rows, err := d.BoxDB.QueryContext(ctx,
		"SELECT seq, pts, update_type, update_data FROM user_seq_updates WHERE user_id = ? AND seq <= ? AND (seq > ? OR date2 >= ?)"+
			" AND (except_auth_key_id = 0 OR except_auth_key_id <> ?)"+userbox.LiveSeqRows+" ORDER BY seq",
		userId, maxSeq, cursor, since, authKeyId, userbox.NotExpiredSince(time.Now().Unix()))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SeqRow
	for rows.Next() {
		var r SeqRow
		if err = rows.Scan(&r.Seq, &r.Pts, &r.UpdateType, &r.UpdateData); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SeqCursor is the state last given to an authorization: its seq and when
// it was given (unix time).
type SeqCursor struct {
	Seq  int32
	Date int64
}

// SeqCursor returns the cursor of the authorization authKeyId of userId,
// and false when it was given no state.
func (d *Dao) SeqCursor(ctx context.Context, userId, authKeyId int64) (SeqCursor, bool, error) {
	var c SeqCursor
	err := d.BoxDB.QueryRowContext(ctx,
		"SELECT seq, date2 FROM user_seq_cursors WHERE user_id = ? AND auth_key_id = ?",
		userId, authKeyId).Scan(&c.Seq, &c.Date)
	if errors.Is(err, sql.ErrNoRows) {
		return SeqCursor{}, false, nil
	}
	if err != nil {
		return SeqCursor{}, false, err
	}
	return c, true, nil
}

// SetSeqCursor stores seq as the state given to the authorization. Two
// requests of one authorization may end in any order: a lower seq written
// last only returns some updates again.
func (d *Dao) SetSeqCursor(ctx context.Context, userId, authKeyId int64, seq int32, now int64) error {
	_, err := d.BoxDB.ExecContext(ctx,
		"INSERT INTO user_seq_cursors (user_id, auth_key_id, seq, date2) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE seq = ?, date2 = ?",
		userId, authKeyId, seq, now, seq, now)
	return err
}

// ChannelState is a channel's committed pts.
type ChannelState struct {
	Id  int64
	Pts int32
}

// ChangedChannels returns the channels userId is in whose box changed at
// since or later (channels.date2, written by every transaction that moves
// their pts), by id. It reads the master, as the account's state is read.
func (d *Dao) ChangedChannels(ctx context.Context, userId, since int64) ([]ChannelState, error) {
	rows, err := d.BoxDB.QueryContext(ctx,
		"SELECT c.id, c.pts FROM channel_participants p JOIN channels c ON c.id = p.channel_id"+
			" WHERE p.user_id = ? AND p.state = 0 AND c.deleted = 0 AND c.date2 >= ? ORDER BY c.id",
		userId, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ChannelState
	for rows.Next() {
		var c ChannelState
		if err = rows.Scan(&c.Id, &c.Pts); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

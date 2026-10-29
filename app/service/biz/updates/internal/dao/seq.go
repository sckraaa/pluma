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
// 0022; 0 is no authorization). It reads the master (BoxDB), where the state
// maxSeq is from: every row up to it is committed there.
func (d *Dao) SeqUpdates(ctx context.Context, userId, authKeyId int64, maxSeq, cursor int32, since int64) ([]SeqRow, error) {
	rows, err := d.BoxDB.QueryContext(ctx,
		"SELECT seq, pts, update_type, update_data FROM user_seq_updates WHERE user_id = ? AND seq <= ? AND (seq > ? OR date2 >= ?)"+
			" AND (except_auth_key_id = 0 OR except_auth_key_id <> ?) ORDER BY seq",
		userId, maxSeq, cursor, since, authKeyId)
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

// SeqCursor returns the seq of the last state given to the authorization
// authKeyId of userId, and false when it was given none.
func (d *Dao) SeqCursor(ctx context.Context, userId, authKeyId int64) (int32, bool, error) {
	var seq int32
	err := d.BoxDB.QueryRowContext(ctx,
		"SELECT seq FROM user_seq_cursors WHERE user_id = ? AND auth_key_id = ?",
		userId, authKeyId).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return seq, true, nil
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

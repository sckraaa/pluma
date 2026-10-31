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
)

// ReadDate is when a reader read a message.
type ReadDate struct {
	ReadUserId int64 `db:"read_user_id"`
	Date       int64 `db:"date"`
}

// ReadDates are the dates the readers of userId's message msgId in the
// dialog peerDialogId read it, the latest first. msg keeps a row per read
// of userId's messages in message_read_outbox, each with userId's newest
// message read: a reader read msgId with their first read at or above it.
func (d *Dao) ReadDates(ctx context.Context, userId, peerDialogId int64, msgId int32) ([]ReadDate, error) {
	var dates []ReadDate
	err := d.DB.QueryRowsPartial(ctx, &dates,
		"SELECT read_user_id, MIN(read_outbox_max_date) AS date FROM message_read_outbox"+
			" WHERE user_id = ? AND peer_dialog_id = ? AND read_outbox_max_id >= ?"+
			" GROUP BY read_user_id ORDER BY date DESC, read_user_id",
		userId, peerDialogId, msgId)
	if err != nil {
		return nil, err
	}
	return dates, nil
}

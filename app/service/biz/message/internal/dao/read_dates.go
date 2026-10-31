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

	"github.com/teamgram/proto/mtproto"
)

// Reader is a user who read a message, with the date they read it: 0 when
// it is not known, for a read before the dates were kept.
type Reader struct {
	UserId int64 `db:"user_id"`
	Date   int64 `db:"date"`
}

// Readers are the users who read userId's message msgId in the dialog with
// the peer, a private chat or a basic group, the latest first.
//
// Who read it is told by the read boundaries msg keeps: in a private chat
// read_outbox_max_id of userId's dialog, in a group each member's
// read_inbox_max_id at or above their copy of the message (members who
// left among them). When, by message_read_outbox: msg keeps a row per read
// in userId's box with the range of userId's messages read with it, and the
// row whose range holds msgId has the date.
func (d *Dao) Readers(ctx context.Context, userId int64, peerType int32, peerId int64, msgId int32) ([]Reader, error) {
	var (
		readers      []Reader
		peerDialogId = mtproto.MakePeerDialogId(peerType, peerId)
		err          error
	)
	switch peerType {
	case mtproto.PEER_USER:
		err = d.DB.QueryRowsPartial(ctx, &readers,
			"SELECT d.peer_id AS user_id, COALESCE(MIN(r.read_outbox_max_date), 0) AS date FROM dialogs d"+
				" LEFT JOIN message_read_outbox r ON r.user_id = d.user_id AND r.peer_dialog_id = ? AND r.read_user_id = d.peer_id"+
				" AND r.read_outbox_min_id <= ? AND r.read_outbox_max_id >= ?"+
				" WHERE d.user_id = ? AND d.peer_type = ? AND d.peer_id = ? AND d.read_outbox_max_id >= ?"+
				" GROUP BY d.peer_id",
			peerDialogId, msgId, msgId, userId, peerType, peerId, msgId)
	case mtproto.PEER_CHAT:
		err = d.DB.QueryRowsPartial(ctx, &readers,
			"SELECT c.user_id, COALESCE(MIN(r.read_outbox_max_date), 0) AS date FROM messages s"+
				" JOIN chat_participants p ON p.chat_id = ? AND p.user_id <> s.user_id"+
				" JOIN messages c ON c.user_id = p.user_id AND c.dialog_message_id = s.dialog_message_id"+
				" JOIN dialogs rd ON rd.user_id = c.user_id AND rd.peer_type = ? AND rd.peer_id = ? AND rd.read_inbox_max_id >= c.user_message_box_id"+
				" LEFT JOIN message_read_outbox r ON r.user_id = s.user_id AND r.peer_dialog_id = ? AND r.read_user_id = c.user_id"+
				" AND r.read_outbox_min_id <= s.user_message_box_id AND r.read_outbox_max_id >= s.user_message_box_id"+
				" WHERE s.user_id = ? AND s.user_message_box_id = ?"+
				" GROUP BY c.user_id ORDER BY date DESC, c.user_id",
			peerId, peerType, peerId, peerDialogId, userId, msgId)
	}
	if err != nil {
		return nil, err
	}
	return readers, nil
}

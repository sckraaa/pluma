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
	"strings"

	"github.com/teamgram/proto/mtproto"
)

// OwnRandomIds returns the random_id of those of the user's messages ids
// that the user sent: out_random_id, which a copy received from another
// sender does not have. It reads the master (BoxDB), where the journal of
// the difference was read: a message added up to the state is there.
func (d *Dao) OwnRandomIds(ctx context.Context, userId int64, ids []int32) (map[int32]int64, error) {
	out := make(map[int32]int64, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, userId)
	for _, id := range ids {
		args = append(args, id)
	}
	in := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	query := "SELECT user_message_box_id, out_random_id FROM messages WHERE user_id = ? AND out_random_id IS NOT NULL AND user_message_box_id IN (" + in + ")" //nolint:gosec // only placeholders are joined into the query
	rows, err := d.BoxDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			id       int32
			randomId int64
		)
		if err = rows.Scan(&id, &randomId); err != nil {
			return nil, err
		}
		out[id] = randomId
	}
	return out, rows.Err()
}

// UnreadAfter returns how many incoming messages of the dialog of userId
// with peer (peerUser or peerChat) after maxId exist and were added up to
// pts: the unread count of the dialog read up to maxId, as the box had it
// at pts (as readInbox in msg counts it, within the difference).
func (d *Dao) UnreadAfter(ctx context.Context, userId int64, peer *mtproto.Peer, maxId, pts int32) (int32, error) {
	var did mtproto.DialogID
	switch peer.GetPredicateName() {
	case mtproto.Predicate_peerUser:
		did = mtproto.MakeDialogId(userId, mtproto.PEER_USER, peer.GetUserId())
	case mtproto.Predicate_peerChat:
		did = mtproto.MakeDialogId(userId, mtproto.PEER_CHAT, peer.GetChatId())
	default:
		return 0, nil
	}
	var n int32
	err := d.BoxDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND deleted = 0 AND user_message_box_id > ? AND sender_user_id <> ? AND pts <= ?",
		userId, did.A, did.B, maxId, userId, pts).Scan(&n)
	return n, err
}

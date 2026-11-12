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

package monoforum

import (
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// A sublist is the chat of one subscriber with the channel, a row of
// channel_monoforum_sublists. The two sides read it apart:
//   - read_inbox_max_id is how far the admins read what the subscriber
//     wrote; it is one mark for all of them, as the answers are the
//     channel's (monoForumDialog.read_inbox_max_id);
//   - read_outbox_max_id is how far the subscriber read the channel's
//     answers (monoForumDialog.read_outbox_max_id); the subscriber's own
//     mark is its row of dialogs, as in any supergroup.
//
// top_message is its last message that is no service message: the clients
// build the list from it and drop a sublist whose last message is one
// (tdesktop data_saved_messages.cpp:424-441).
//
// A message of the channel has send_as_id set (the channel); one of the
// subscriber has 0.

// SublistRow is a row of channel_monoforum_sublists.
type SublistRow struct {
	PeerId          int64 `db:"peer_id"`
	TopMessage      int32 `db:"top_message"`
	ReadInboxMaxId  int32 `db:"read_inbox_max_id"`
	ReadOutboxMaxId int32 `db:"read_outbox_max_id"`
	UnreadMark      bool  `db:"unread_mark"`
	Date2           int64 `db:"date2"`
}

const sublistColumns = "peer_id, top_message, read_inbox_max_id, read_outbox_max_id, unread_mark, date2"

// Sublist reads the sublist peerId of the monoforum; ok is false for none.
func Sublist(q Querier, monoforumId, peerId int64) (row SublistRow, ok bool, err error) {
	var rows []SublistRow
	if err = q.QueryRowsPartial(&rows,
		"SELECT "+sublistColumns+" FROM channel_monoforum_sublists WHERE channel_id = ? AND peer_id = ?", monoforumId, peerId); err != nil {
		return SublistRow{}, false, err
	}
	if len(rows) == 0 {
		return SublistRow{}, false, nil
	}
	return rows[0], true, nil
}

// SublistsByPeers reads the sublists peers of the monoforum that exist, in
// the order of peers.
func SublistsByPeers(q Querier, monoforumId int64, peers []int64) ([]SublistRow, error) {
	if len(peers) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(peers)+1)
	args = append(args, monoforumId)
	for _, p := range peers {
		args = append(args, p)
	}
	var rows []SublistRow
	if err := q.QueryRowsPartial(&rows,
		"SELECT "+sublistColumns+" FROM channel_monoforum_sublists WHERE channel_id = ? AND peer_id IN (?"+strings.Repeat(", ?", len(peers)-1)+")",
		args...); err != nil {
		return nil, err
	}
	byPeer := make(map[int64]SublistRow, len(rows))
	for _, r := range rows {
		byPeer[r.PeerId] = r
	}
	out := make([]SublistRow, 0, len(rows))
	seen := map[int64]bool{}
	for _, p := range peers {
		if r, ok := byPeer[p]; ok && !seen[p] {
			seen[p] = true
			out = append(out, r)
		}
	}
	return out, nil
}

// ListMax is the most sublists of one page of the list.
const ListMax = 100

// List is a page of the sublists of the monoforum that have messages, the
// one written to last first: those below the sublist whose last message is
// offsetId (0: from the top), at most limit of them, and how many there
// are in all.
func List(q Querier, monoforumId int64, offsetId, limit int32) ([]SublistRow, int32, error) {
	limit = min(max(limit, 1), ListMax)
	where := "channel_id = ? AND top_message > 0"
	args := []any{monoforumId}
	if offsetId > 0 {
		where += " AND top_message < ?"
		args = append(args, offsetId)
	}
	var rows []SublistRow
	if err := q.QueryRowsPartial(&rows,
		"SELECT "+sublistColumns+" FROM channel_monoforum_sublists WHERE "+where+" ORDER BY top_message DESC LIMIT ?",
		append(args, limit)...); err != nil {
		return nil, 0, err
	}
	var count []int32
	if err := q.QueryRowsPartial(&count,
		"SELECT COUNT(*) FROM channel_monoforum_sublists WHERE channel_id = ? AND top_message > 0", monoforumId); err != nil {
		return nil, 0, err
	}
	n := int32(0)
	if len(count) > 0 {
		n = count[0]
	}
	return rows, n, nil
}

// Moved records the new message last of the sublist peerId, written by the
// subscriber or, with byChannel, by an admin as the channel: the side that
// wrote has read the sublist up to it. A service message leaves the last
// message of the list as it is. The sublist is made by its first message.
func Moved(tx *sqlx.Tx, monoforumId, peerId int64, last int32, service, byChannel bool, date int64) error {
	top := last
	if service {
		top = 0
	}
	inbox, outbox := int32(0), last
	if byChannel {
		inbox, outbox = last, 0
	}
	_, err := tx.Exec("INSERT INTO channel_monoforum_sublists (channel_id, peer_id, top_message, read_inbox_max_id, read_outbox_max_id, date2) VALUES (?, ?, ?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE top_message = GREATEST(top_message, VALUES(top_message)),"+
		" read_inbox_max_id = GREATEST(read_inbox_max_id, VALUES(read_inbox_max_id)),"+
		" read_outbox_max_id = GREATEST(read_outbox_max_id, VALUES(read_outbox_max_id)), date2 = VALUES(date2)",
		monoforumId, peerId, top, inbox, outbox, date)
	return err
}

// Retop sets the last message of each sublist of peers again, after
// messages of them were deleted: the last one left that is no service
// message, 0 for an emptied sublist, which leaves the list.
func Retop(tx *sqlx.Tx, monoforumId int64, peers []int64) error {
	for _, p := range peers {
		if p == 0 {
			continue
		}
		if _, err := tx.Exec("UPDATE channel_monoforum_sublists s SET s.top_message = (SELECT COALESCE(MAX(m.id), 0) FROM channel_messages m"+
			" WHERE m.channel_id = s.channel_id AND m.saved_peer_id = s.peer_id AND m.deleted = 0"+
			" AND JSON_UNQUOTE(JSON_EXTRACT(m.message_data, '$.predicate_name')) = ?)"+
			" WHERE s.channel_id = ? AND s.peer_id = ?",
			mtproto.Predicate_message, monoforumId, p); err != nil {
			return err
		}
	}
	return nil
}

// ReadInbox moves how far the admins read the sublist peerId up to maxId
// and clears its unread mark. It returns the mark it had before, and
// whether it moved.
func ReadInbox(tx *sqlx.Tx, monoforumId, peerId int64, maxId int32) (before int32, moved bool, err error) {
	var rows []SublistRow
	if err = tx.QueryRowsPartial(&rows,
		"SELECT "+sublistColumns+" FROM channel_monoforum_sublists WHERE channel_id = ? AND peer_id = ? FOR UPDATE", monoforumId, peerId); err != nil {
		return 0, false, err
	}
	if len(rows) == 0 {
		return 0, false, nil
	}
	before = rows[0].ReadInboxMaxId
	if maxId <= before && !rows[0].UnreadMark {
		return before, false, nil
	}
	if _, err = tx.Exec("UPDATE channel_monoforum_sublists SET read_inbox_max_id = GREATEST(read_inbox_max_id, ?), unread_mark = 0 WHERE channel_id = ? AND peer_id = ?",
		maxId, monoforumId, peerId); err != nil {
		return 0, false, err
	}
	return before, maxId > before, nil
}

// ReadOutbox moves how far the subscriber peerId read its sublist up to
// maxId, and tells whether it moved.
func ReadOutbox(tx *sqlx.Tx, monoforumId, peerId int64, maxId int32) (bool, error) {
	res, err := tx.Exec("UPDATE channel_monoforum_sublists SET read_outbox_max_id = ? WHERE channel_id = ? AND peer_id = ? AND read_outbox_max_id < ?",
		maxId, monoforumId, peerId, maxId)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetUnreadMark sets or clears the unread mark of the sublist peerId, and
// tells whether it changed.
func SetUnreadMark(tx *sqlx.Tx, monoforumId, peerId int64, unread bool) (bool, error) {
	res, err := tx.Exec("UPDATE channel_monoforum_sublists SET unread_mark = ? WHERE channel_id = ? AND peer_id = ? AND unread_mark <> ?",
		unread, monoforumId, peerId, unread)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// UnreadMarks are the sublists of the monoforum marked unread.
func UnreadMarks(q Querier, monoforumId int64) ([]int64, error) {
	var peers []int64
	err := q.QueryRowsPartial(&peers,
		"SELECT peer_id FROM channel_monoforum_sublists WHERE channel_id = ? AND unread_mark = 1 AND top_message > 0 ORDER BY top_message DESC", monoforumId)
	return peers, err
}

// UnreadOfAdmins counts the messages of the subscriber the admins have not
// read in the sublist row.
func UnreadOfAdmins(q Querier, monoforumId int64, row SublistRow) (int32, error) {
	var n []int32
	if err := q.QueryRowsPartial(&n,
		"SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND saved_peer_id = ? AND id > ? AND send_as_id = 0 AND deleted = 0",
		monoforumId, row.PeerId, row.ReadInboxMaxId); err != nil {
		return 0, err
	}
	if len(n) == 0 {
		return 0, nil
	}
	return n[0], nil
}

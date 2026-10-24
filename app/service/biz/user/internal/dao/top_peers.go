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
	"strconv"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Top peers (contacts.getTopPeers, task 011, ADR 0015) are computed from
// the user's own sent messages when asked for; what is kept is whether the
// user wants them and since when they count (user_settings), and when the
// user reset the rating of a peer (user_top_peer_resets).

// TopPeersSinceKey is the user_settings key of top peers: missing — on,
// every message counts; "-1" — off; a unix time — on, the messages after
// it count (they were turned back on then, the data before is dropped).
const TopPeersSinceKey = "topPeersSince"

// topPeersOff is the value of TopPeersSinceKey while top peers are off.
const topPeersOff = "-1"

// TopPeersState returns whether top peers are on for userId and since when
// messages count (0: all).
func (d *Dao) TopPeersState(ctx context.Context, userId int64) (enabled bool, since int64, err error) {
	var values []string
	if err = d.DB.QueryRowsPartial(ctx, &values, "SELECT value FROM user_settings WHERE user_id = ? AND key2 = ? AND deleted = 0", userId, TopPeersSinceKey); err != nil {
		return false, 0, err
	}
	if len(values) == 0 {
		return true, 0, nil
	}
	if values[0] == topPeersOff {
		return false, 0, nil
	}
	since, err = strconv.ParseInt(values[0], 10, 64)
	return err == nil, since, err
}

// TopPeersOn reads TopPeersSinceKey of the box owner: whether top peers
// are on.
func TopPeersOn(b *userbox.Box) (bool, error) {
	var values []string
	if err := b.Tx().QueryRowsPartial(&values, "SELECT value FROM user_settings WHERE user_id = ? AND key2 = ? AND deleted = 0", b.UserId(), TopPeersSinceKey); err != nil {
		return false, err
	}
	return len(values) == 0 || values[0] != topPeersOff, nil
}

// SetTopPeers turns top peers of the box owner on (counting from now) or
// off; turning them off drops the resets, which the next start supersedes.
func SetTopPeers(b *userbox.Box, on bool) error {
	value := topPeersOff
	if on {
		value = strconv.FormatInt(b.Date(), 10)
	}
	if _, err := b.Tx().Exec("INSERT INTO user_settings (user_id, key2, value) VALUES (?, ?, ?) AS new"+
		" ON DUPLICATE KEY UPDATE value = new.value, deleted = 0",
		b.UserId(), TopPeersSinceKey, value); err != nil {
		return err
	}
	if on {
		return nil
	}
	// The rows are the owner's alone: only the owner's box transactions
	// lock them.
	_, err := b.Tx().Exec("DELETE FROM user_top_peer_resets WHERE user_id = ?", b.UserId())
	return err
}

// TopPeerReset is when the user reset the rating of a peer in a category.
type TopPeerReset struct {
	Category int32 `db:"category"`
	PeerType int32 `db:"peer_type"`
	PeerId   int64 `db:"peer_id"`
	ResetAt  int64 `db:"reset_at"`
}

// TopPeerResets returns the resets of userId.
func (d *Dao) TopPeerResets(ctx context.Context, userId int64) ([]TopPeerReset, error) {
	var rows []TopPeerReset
	err := d.DB.QueryRowsPartial(ctx, &rows, "SELECT category, peer_type, peer_id, reset_at FROM user_top_peer_resets WHERE user_id = ?", userId)
	return rows, err
}

// ResetTopPeer resets the rating of a peer of the box owner in a category:
// only messages after now count for it.
func ResetTopPeer(b *userbox.Box, category, peerType int32, peerId int64) error {
	_, err := b.Tx().Exec("INSERT INTO user_top_peer_resets (user_id, category, peer_type, peer_id, reset_at) VALUES (?, ?, ?, ?, ?) AS new"+
		" ON DUPLICATE KEY UPDATE reset_at = new.reset_at",
		b.UserId(), category, peerType, peerId, b.Date())
	return err
}

// SentMessage is a message the user sent to a user or a basic group.
type SentMessage struct {
	PeerType int32
	PeerId   int64
	Date     int64
	Forward  bool // a forwarded message
}

// SentMessages returns the messages userId sent to users and basic groups
// after since, among the newest scan messages of the box (incoming too):
// the newest rows of the box are read through its unique key (user_id,
// user_message_box_id) in reverse, so the cost does not grow with the
// history. Service messages do not count.
func (d *Dao) SentMessages(ctx context.Context, userId, since int64, scan int) ([]SentMessage, error) {
	var rows []struct {
		PeerType int32 `db:"peer_type"`
		PeerId   int64 `db:"peer_id"`
		Date     int64 `db:"date2"`
		Forward  int32 `db:"forward"`
	}
	err := d.DB.QueryRowsPartial(ctx, &rows,
		"SELECT peer_type, peer_id, date2, forward FROM ("+
			"SELECT peer_type, peer_id, date2, sender_user_id, deleted,"+
			" COALESCE(JSON_CONTAINS_PATH(message_data, 'one', '$.fwd_from'), 0) AS forward,"+
			" COALESCE(JSON_UNQUOTE(JSON_EXTRACT(message_data, '$.predicate_name')), '') AS predicate"+
			" FROM messages WHERE user_id = ? ORDER BY user_message_box_id DESC LIMIT ?) m"+
			" WHERE sender_user_id = ? AND deleted = 0 AND date2 > ? AND peer_type IN (?, ?) AND predicate <> ?",
		userId, scan, userId, since, mtproto.PEER_USER, mtproto.PEER_CHAT, mtproto.Predicate_messageService)
	if err != nil {
		return nil, err
	}
	list := make([]SentMessage, 0, len(rows))
	for _, r := range rows {
		list = append(list, SentMessage{PeerType: r.PeerType, PeerId: r.PeerId, Date: r.Date, Forward: r.Forward != 0})
	}
	return list, nil
}

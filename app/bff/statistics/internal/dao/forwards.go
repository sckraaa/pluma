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
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
)

// The public forwards of a post or a story of a channel: the copies of it
// in channels and supergroups (channel_stats_public_forwards, written when
// the copy is sent, and for the copies made before task 103 by its
// migration), and for a story also the stories that repost it
// (stories.fwd_*). A copy is listed while its channel is public (it has its
// editable username, as ImmutableChannel.Public tells, so whoever may read
// it), is neither deleted nor blocked, and the copy itself is there: a
// message not deleted, a story in the feeds or in the profile (what a viewer
// who does not manage the channel may open, bff stories shown).

// Forward is a copy: the message Id of the channel PeerId, or with Story
// the story Id of the channel PeerId.
type Forward struct {
	Date   int64 `db:"date"`
	Story  bool  `db:"story"`
	PeerId int64 `db:"peer_id"`
	Id     int32 `db:"id"`
}

// ForwardCursor is where a page of forwards ends: the forwards are listed by
// date, the latest first, then by their kind (messages first), channel and
// id.
type ForwardCursor struct {
	Date   int64
	Story  bool
	PeerId int64
	Id     int32
}

// After is the cursor after f.
func (f Forward) After() ForwardCursor {
	return ForwardCursor(f)
}

// String is the cursor as next_offset gives it: base64url of
// "date:kind:channel:id", kind 0 for a message and 1 for a story. The
// clients pass it back as it is (core.telegram.org/method/
// stats.getMessagePublicForwards: offset is empty first, then next_offset).
func (c ForwardCursor) String() string {
	kind := 0
	if c.Story {
		kind = 1
	}
	return base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, "%d:%d:%d:%d", c.Date, kind, c.PeerId, c.Id))
}

// ParseForwardCursor reads an offset of ForwardCursor.String; ok is false
// for a string it did not make.
func ParseForwardCursor(s string) (c ForwardCursor, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ForwardCursor{}, false
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 4 {
		return ForwardCursor{}, false
	}
	var v [4]int64
	for i, p := range parts {
		if v[i], err = strconv.ParseInt(p, 10, 64); err != nil {
			return ForwardCursor{}, false
		}
	}
	if v[0] < 0 || (v[1] != 0 && v[1] != 1) || v[2] <= 0 || v[3] <= 0 || v[3] > int64(^uint32(0)>>1) {
		return ForwardCursor{}, false
	}
	return ForwardCursor{Date: v[0], Story: v[1] == 1, PeerId: v[2], Id: int32(v[3])}, true
}

// ForwardSource is what was forwarded: the post or the story Id of the
// channel ChannelId; Reposts adds the stories that repost a story.
type ForwardSource struct {
	Kind      channelstats.ForwardKind
	ChannelId int64
	Id        int32
	Reposts   bool
}

// publicChannel is the condition that the channel in column is public and
// may be read: not deleted, not blocked, with its editable username.
func publicChannel(column string) string {
	return "EXISTS (SELECT 1 FROM channels c WHERE c.id = " + column + " AND c.deleted = 0 AND c.blocked = 0)" +
		" AND EXISTS (SELECT 1 FROM username u WHERE u.peer_type = " + strconv.Itoa(mtproto.PEER_CHANNEL) +
		" AND u.peer_id = " + column + " AND u.editable = 1)"
}

// forwardStreams are the queries of the copies of src, a UNION of rows of
// Forward, with their arguments; now is the time stories in the feeds are
// told by.
func forwardStreams(src ForwardSource, now int64) (string, []any) {
	q := "SELECT f.`date`, 0 AS story, f.dst_channel_id AS peer_id, f.dst_message_id AS id FROM channel_stats_public_forwards f" +
		" JOIN channel_messages m ON m.channel_id = f.dst_channel_id AND m.id = f.dst_message_id AND m.deleted = 0" +
		" WHERE f.src_kind = ? AND f.src_peer_type = ? AND f.src_peer_id = ? AND f.src_id = ? AND " + publicChannel("f.dst_channel_id")
	args := []any{int8(src.Kind), mtproto.PEER_CHANNEL, src.ChannelId, src.Id}
	if src.Reposts {
		q += " UNION ALL SELECT s.`date`, 1 AS story, s.peer_id, s.story_id AS id FROM stories s" +
			" WHERE s.fwd_peer_type = ? AND s.fwd_peer_id = ? AND s.fwd_story_id = ? AND s.peer_type = ?" +
			" AND (s.expire_date > ? OR s.pinned = 1) AND " + publicChannel("s.peer_id")
		args = append(args, mtproto.PEER_CHANNEL, src.ChannelId, src.Id, mtproto.PEER_CHANNEL, now)
	}
	return q, args
}

// PublicForwards are the limit public copies of src after the cursor (nil:
// from the latest one), in the order of ForwardCursor.
func (d *Dao) PublicForwards(ctx context.Context, src ForwardSource, now int64, after *ForwardCursor, limit int) ([]Forward, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("dao: forwards limit %d", limit)
	}
	streams, args := forwardStreams(src, now)
	q := "SELECT t.`date`, t.story, t.peer_id, t.id FROM (" + streams + ") t"
	if after != nil {
		story := 0
		if after.Story {
			story = 1
		}
		q += " WHERE t.`date` < ? OR (t.`date` = ? AND (t.story, t.peer_id, t.id) > (?, ?, ?))"
		args = append(args, after.Date, after.Date, story, after.PeerId, after.Id)
	}
	q += " ORDER BY t.`date` DESC, t.story, t.peer_id, t.id LIMIT ?"
	var rows []Forward
	err := d.db.QueryRowsPartial(ctx, &rows, q, append(args, limit)...)
	return rows, err
}

// PublicForwardsCount is the number of public copies of src.
func (d *Dao) PublicForwardsCount(ctx context.Context, src ForwardSource, now int64) (int32, error) {
	streams, args := forwardStreams(src, now)
	var n []int32
	if err := d.db.QueryRowsPartial(ctx, &n, "SELECT COUNT(*) FROM ("+streams+") t", args...); err != nil {
		return 0, err
	}
	if len(n) == 0 {
		return 0, nil
	}
	return n[0], nil
}

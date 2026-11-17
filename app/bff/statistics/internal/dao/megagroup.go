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
	"fmt"

	"github.com/teamgram/teamgram-server/pkg/channelstats"
)

// The messages of supergroups (channel_messages), the days their members
// read them (channel_stats_readers, from stats_collection's readers on) and
// what their admins did (channel_stats_admin_actions, from admin_actions
// on). A deleted message and a service message are not counted.
//
// The messages are read from the index stats_messages alone (channel_id,
// date2, stats_kind, sender_user_id, send_as_id, stats_chars; migration
// migrate-20261119-channel-stats.sql): stats_kind is the kind a message is
// counted as (the series of the graph of messages), NULL for a deleted or a
// service message. The optimizer takes the primary key (channel_id, id) for
// a range that holds most of a channel's messages, which reads every row
// and its JSON: a year of a group of 200 000 messages takes about three times as
// long, so the queries name the index.
const (
	groupMessages = "channel_messages m FORCE INDEX (stats_messages)"
	isGroupPost   = "m.stats_kind IS NOT NULL"
)

// byMember is the condition that the row m of channel_messages was written
// by its sender as itself: not as a channel or as the group (send_as_id,
// task 079), not the copy of a post of the linked channel (msg
// channel_send.go postCopy), which is from the channel, and not by a ghost.
const byMember = "m.send_as_id = 0" +
	" AND NOT EXISTS (SELECT 1 FROM channel_discussion_posts dp WHERE dp.group_id = m.channel_id AND dp.group_message_id = m.id)" +
	" AND NOT EXISTS (SELECT 1 FROM users g WHERE g.id = m.sender_user_id AND g.ghost = 1)"

// MessageKinds are the messages of the channel posted in r by bucket and
// kind (K: stats_kind, 0 text to 9 other).
func (d *Dao) MessageKinds(ctx context.Context, channelId int64, r Range) ([]Count, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	var rows []Count
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT FLOOR(m.date2 / ?) AS b, m.stats_kind AS k, COUNT(*) AS n FROM "+groupMessages+
			" WHERE m.channel_id = ? AND m.date2 >= ? AND m.date2 < ? AND "+isGroupPost+" GROUP BY b, k",
		r.Bucket, channelId, r.From, r.To)
	return rows, err
}

// MessageCounts are the messages of the channel posted in r by bucket (K is
// 0).
func (d *Dao) MessageCounts(ctx context.Context, channelId int64, r Range) ([]Count, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	var rows []Count
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT FLOOR(m.date2 / ?) AS b, 0 AS k, COUNT(*) AS n FROM "+groupMessages+
			" WHERE m.channel_id = ? AND m.date2 >= ? AND m.date2 < ? AND "+isGroupPost+" GROUP BY b",
		r.Bucket, channelId, r.From, r.To)
	return rows, err
}

// GroupTotals are the messages of a period of a supergroup and the members
// who wrote them as themselves.
type GroupTotals struct {
	Messages int64 `db:"messages"`
	Posters  int64 `db:"posters"`
}

// GroupTotals are the messages of the channel posted in [from, to) and the
// number of the members who wrote them (byMember).
func (d *Dao) GroupTotals(ctx context.Context, channelId, from, to int64) (GroupTotals, error) {
	var t []GroupTotals
	if err := d.db.QueryRowsPartial(ctx, &t,
		"SELECT COUNT(*) AS messages, COUNT(DISTINCT IF("+byMember+", m.sender_user_id, NULL)) AS posters FROM "+groupMessages+
			" WHERE m.channel_id = ? AND m.date2 >= ? AND m.date2 < ? AND "+isGroupPost,
		channelId, from, to); err != nil || len(t) == 0 {
		return GroupTotals{}, err
	}
	return t[0], nil
}

// dayRange checks that r is in days and returns its days [first, end).
func dayRange(r Range) (first, end int32, err error) {
	if err = r.check(); err != nil {
		return 0, 0, err
	}
	if r.Bucket != 86400 || r.From%86400 != 0 || r.To%86400 != 0 {
		return 0, 0, fmt.Errorf("dao: range [%d, %d) in buckets of %d, not in days", r.From, r.To, r.Bucket)
	}
	return channelstats.Day(r.From), channelstats.Day(r.To), nil
}

// ReaderCounts are the members who read or wrote to the channel on each
// day of r (in days: the readers are kept by the day), by day (K is 0).
func (d *Dao) ReaderCounts(ctx context.Context, channelId int64, r Range) ([]Count, error) {
	first, end, err := dayRange(r)
	if err != nil {
		return nil, err
	}
	var rows []Count
	err = d.db.QueryRowsPartial(ctx, &rows,
		"SELECT `day` AS b, 0 AS k, COUNT(*) AS n FROM channel_stats_readers"+
			" WHERE channel_id = ? AND `day` >= ? AND `day` < ? GROUP BY `day`",
		channelId, first, end)
	return rows, err
}

// Readers is the number of the members who read or wrote to the channel on
// any of the days [first, last].
func (d *Dao) Readers(ctx context.Context, channelId int64, first, last int32) (int64, error) {
	var n []int64
	if err := d.db.QueryRowsPartial(ctx, &n,
		"SELECT COUNT(DISTINCT user_id) FROM channel_stats_readers WHERE channel_id = ? AND `day` >= ? AND `day` <= ?",
		channelId, first, last); err != nil || len(n) == 0 {
		return 0, err
	}
	return n[0], nil
}

// TopPoster is a member with the messages it wrote and their mean length
// in characters.
type TopPoster struct {
	UserId   int64 `db:"user_id"`
	Messages int64 `db:"messages"`
	AvgChars int64 `db:"avg_chars"`
}

// TopPosters are the limit members who wrote the most messages to the
// channel as themselves (byMember) in [from, to), the most first.
func (d *Dao) TopPosters(ctx context.Context, channelId, from, to int64, limit int) ([]TopPoster, error) {
	var rows []TopPoster
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT m.sender_user_id AS user_id, COUNT(*) AS messages, CAST(COALESCE(ROUND(AVG(m.stats_chars)), 0) AS SIGNED) AS avg_chars"+
			" FROM "+groupMessages+" WHERE m.channel_id = ? AND m.date2 >= ? AND m.date2 < ? AND "+isGroupPost+" AND "+byMember+
			" GROUP BY m.sender_user_id ORDER BY messages DESC, m.sender_user_id LIMIT ?",
		channelId, from, to, limit)
	return rows, err
}

// TopAdmin is an admin with what it did.
type TopAdmin struct {
	UserId  int64 `db:"user_id"`
	Deleted int64 `db:"deleted"`
	Kicked  int64 `db:"kicked"`
	Banned  int64 `db:"banned"`
}

// TopAdmins are the limit admins of the channel who did the most on the
// days [first, last], the most first.
func (d *Dao) TopAdmins(ctx context.Context, channelId int64, first, last int32, limit int) ([]TopAdmin, error) {
	var rows []TopAdmin
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT admin_id AS user_id, CAST(SUM(deleted) AS SIGNED) AS deleted, CAST(SUM(kicked) AS SIGNED) AS kicked, CAST(SUM(banned) AS SIGNED) AS banned"+
			" FROM channel_stats_admin_actions WHERE channel_id = ? AND `day` >= ? AND `day` <= ?"+
			" GROUP BY admin_id HAVING SUM(deleted + kicked + banned) > 0 ORDER BY SUM(deleted + kicked + banned) DESC, admin_id LIMIT ?",
		channelId, first, last, limit)
	return rows, err
}

// TopInviter is a member with the users it added.
type TopInviter struct {
	UserId      int64 `db:"user_id"`
	Invitations int64 `db:"invitations"`
}

// TopInviters are the limit members who added the most users to the
// channel (channelstats.JoinAdded) in [from, to), the most first.
func (d *Dao) TopInviters(ctx context.Context, channelId, from, to int64, limit int) ([]TopInviter, error) {
	var rows []TopInviter
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT e.inviter_id AS user_id, COUNT(*) AS invitations FROM channel_stats_member_events e"+
			" WHERE e.channel_id = ? AND e.`date` >= ? AND e.`date` < ? AND e.joined = 1 AND e.source = ? AND e.inviter_id <> 0 AND "+notGhost("e.inviter_id")+
			" GROUP BY e.inviter_id ORDER BY invitations DESC, e.inviter_id LIMIT ?",
		channelId, from, to, int8(channelstats.JoinAdded), limit)
	return rows, err
}

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

// The posts of broadcast channels and the stories of channels: their views
// (channel_message_views, one row per viewer, with the source from
// stats_collection's view_sources on), forwards (channel_message_forwards),
// reactions (channel_message_reactions) and the views, shares and reactions
// of stories (story_views, story_shares, the reposts in stories.fwd_*).
// A post deleted since is not counted.

// Totals are the posts or stories of a period with the sums of their
// counters.
type Totals struct {
	Items     int64 `db:"items"`
	Views     int64 `db:"views"`
	Forwards  int64 `db:"forwards"`
	Reactions int64 `db:"reactions"`
}

// PostTotals are the posts of the channel made in [from, to) with their
// views and forwards (the counters the clients show) and the reactions
// they have.
func (d *Dao) PostTotals(ctx context.Context, channelId, from, to int64) (Totals, error) {
	var t []Totals
	if err := d.db.QueryRowsPartial(ctx, &t,
		"SELECT COUNT(*) AS items, COALESCE(SUM(m.views), 0) AS views, COALESCE(SUM(m.forwards), 0) AS forwards, 0 AS reactions"+
			" FROM channel_messages m WHERE m.channel_id = ? AND m.date2 >= ? AND m.date2 < ? AND "+isPost,
		channelId, from, to); err != nil || len(t) == 0 {
		return Totals{}, err
	}
	var reactions []int64
	if err := d.db.QueryRowsPartial(ctx, &reactions,
		"SELECT COUNT(*) FROM channel_messages m JOIN channel_message_reactions r ON r.channel_id = m.channel_id AND r.message_id = m.id"+
			" WHERE m.channel_id = ? AND m.date2 >= ? AND m.date2 < ? AND "+isPost+" AND "+notGhost("r.user_id"),
		channelId, from, to); err != nil {
		return Totals{}, err
	}
	if len(reactions) > 0 {
		t[0].Reactions = reactions[0]
	}
	return t[0], nil
}

// StoryTotals are the stories of the channel posted in [from, to) with
// their views, shares (forwards_count: the messages that shared each and
// its reposts, pkg/stories) and reactions.
func (d *Dao) StoryTotals(ctx context.Context, channelId, from, to int64) (Totals, error) {
	var t []Totals
	if err := d.db.QueryRowsPartial(ctx, &t,
		"SELECT COUNT(*) AS items, 0 AS views, COALESCE(SUM(s.forwards_count), 0) AS forwards, 0 AS reactions"+
			" FROM stories s WHERE s.peer_type = ? AND s.peer_id = ? AND s.`date` >= ? AND s.`date` < ?",
		mtproto.PEER_CHANNEL, channelId, from, to); err != nil || len(t) == 0 {
		return Totals{}, err
	}
	var v []struct {
		Views     int64 `db:"views"`
		Reactions int64 `db:"reactions"`
	}
	if err := d.db.QueryRowsPartial(ctx, &v,
		"SELECT COUNT(*) AS views, COALESCE(SUM(v.reaction <> ''), 0) AS reactions FROM stories s"+
			" JOIN story_views v ON v.peer_type = s.peer_type AND v.peer_id = s.peer_id AND v.story_id = s.story_id"+
			" WHERE s.peer_type = ? AND s.peer_id = ? AND s.`date` >= ? AND s.`date` < ? AND "+notGhost("v.viewer_id"),
		mtproto.PEER_CHANNEL, channelId, from, to); err != nil {
		return Totals{}, err
	}
	if len(v) > 0 {
		t[0].Views, t[0].Reactions = v[0].Views, v[0].Reactions
	}
	return t[0], nil
}

// ViewCounts are the views of the posts of the channel in r by bucket and
// source (pkg/channelstats ViewSource); of the post msgId alone when it is
// not 0.
func (d *Dao) ViewCounts(ctx context.Context, channelId int64, msgId int32, r Range) ([]Count, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	q := "SELECT FLOOR(v.`date` / ?) AS b, v.source AS k, COUNT(*) AS n FROM channel_message_views v" +
		" JOIN channel_messages m ON m.channel_id = v.channel_id AND m.id = v.message_id AND m.deleted = 0" +
		" WHERE v.channel_id = ? AND v.`date` >= ? AND v.`date` < ?"
	args := []any{r.Bucket, channelId, r.From, r.To}
	if msgId != 0 {
		q += " AND v.message_id = ?"
		args = append(args, msgId)
	}
	var rows []Count
	err := d.db.QueryRowsPartial(ctx, &rows, q+" GROUP BY b, k", args...)
	return rows, err
}

// ForwardCounts are the forwards of the posts of the channel in r by
// bucket (K is 0); of the post msgId alone when it is not 0.
func (d *Dao) ForwardCounts(ctx context.Context, channelId int64, msgId int32, r Range) ([]Count, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	q := "SELECT FLOOR(f.`date` / ?) AS b, 0 AS k, COUNT(*) AS n FROM channel_message_forwards f" +
		" JOIN channel_messages m ON m.channel_id = f.channel_id AND m.id = f.message_id AND m.deleted = 0" +
		" WHERE f.channel_id = ? AND f.`date` >= ? AND f.`date` < ? AND " + notGhost("f.user_id")
	args := []any{r.Bucket, channelId, r.From, r.To}
	if msgId != 0 {
		q += " AND f.message_id = ?"
		args = append(args, msgId)
	}
	var rows []Count
	err := d.db.QueryRowsPartial(ctx, &rows, q+" GROUP BY b", args...)
	return rows, err
}

// ReactionCounts are the reactions put on the messages of the channel in r
// by bucket and reaction (the key of pkg/reactions); on the message msgId
// alone when it is not 0.
func (d *Dao) ReactionCounts(ctx context.Context, channelId int64, msgId int32, r Range) ([]KeyCount, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	q := "SELECT FLOOR(r.`date` / ?) AS b, r.reaction AS k, COUNT(*) AS n FROM channel_message_reactions r" +
		" JOIN channel_messages m ON m.channel_id = r.channel_id AND m.id = r.message_id AND m.deleted = 0" +
		" WHERE r.channel_id = ? AND r.`date` >= ? AND r.`date` < ? AND " + notGhost("r.user_id")
	args := []any{r.Bucket, channelId, r.From, r.To}
	if msgId != 0 {
		q += " AND r.message_id = ?"
		args = append(args, msgId)
	}
	var rows []KeyCount
	err := d.db.QueryRowsPartial(ctx, &rows, q+" GROUP BY b, k", args...)
	return rows, err
}

// StoryViewCounts are the views of the stories of the channel in r by
// bucket (K is 0); of the story storyId alone when it is not 0.
func (d *Dao) StoryViewCounts(ctx context.Context, channelId int64, storyId int32, r Range) ([]Count, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	q := "SELECT FLOOR(v.`date` / ?) AS b, 0 AS k, COUNT(*) AS n FROM story_views v" +
		" WHERE v.peer_type = ? AND v.peer_id = ? AND v.`date` >= ? AND v.`date` < ? AND " + notGhost("v.viewer_id")
	args := []any{r.Bucket, mtproto.PEER_CHANNEL, channelId, r.From, r.To}
	if storyId != 0 {
		q += " AND v.story_id = ?"
		args = append(args, storyId)
	}
	var rows []Count
	err := d.db.QueryRowsPartial(ctx, &rows, q+" GROUP BY b", args...)
	return rows, err
}

// StoryShareCounts are the shares of the stories of the channel in r by
// bucket (K is 0): the messages that shared them (story_shares) and their
// reposts as stories (stories.fwd_*), as forwards_count counts them; of the
// story storyId alone when it is not 0.
func (d *Dao) StoryShareCounts(ctx context.Context, channelId int64, storyId int32, r Range) ([]Count, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	shares := "SELECT FLOOR(sh.`date` / ?) AS b, 0 AS k, COUNT(*) AS n FROM story_shares sh" +
		" WHERE sh.peer_type = ? AND sh.peer_id = ? AND sh.`date` >= ? AND sh.`date` < ? AND " + notGhost("sh.user_id")
	reposts := "SELECT FLOOR(s.`date` / ?) AS b, 0 AS k, COUNT(*) AS n FROM stories s" +
		" WHERE s.fwd_peer_type = ? AND s.fwd_peer_id = ? AND s.`date` >= ? AND s.`date` < ?"
	shareArgs := []any{r.Bucket, mtproto.PEER_CHANNEL, channelId, r.From, r.To}
	repostArgs := []any{r.Bucket, mtproto.PEER_CHANNEL, channelId, r.From, r.To}
	if storyId != 0 {
		shares += " AND sh.story_id = ?"
		shareArgs = append(shareArgs, storyId)
		reposts += " AND s.fwd_story_id = ?"
		repostArgs = append(repostArgs, storyId)
	}
	var rows, more []Count
	if err := d.db.QueryRowsPartial(ctx, &rows, shares+" GROUP BY b", shareArgs...); err != nil {
		return nil, err
	}
	if err := d.db.QueryRowsPartial(ctx, &more, reposts+" GROUP BY b", repostArgs...); err != nil {
		return nil, err
	}
	return append(rows, more...), nil
}

// StoryReactionCounts are the reactions put on the stories of the channel
// in r (by the time of the reaction) by bucket and reaction; on the story
// storyId alone when it is not 0.
func (d *Dao) StoryReactionCounts(ctx context.Context, channelId int64, storyId int32, r Range) ([]KeyCount, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	q := "SELECT FLOOR(v.reaction_date / ?) AS b, v.reaction AS k, COUNT(*) AS n FROM story_views v" +
		" WHERE v.peer_type = ? AND v.peer_id = ? AND v.reaction <> '' AND v.reaction_date >= ? AND v.reaction_date < ? AND " + notGhost("v.viewer_id")
	args := []any{r.Bucket, mtproto.PEER_CHANNEL, channelId, r.From, r.To}
	if storyId != 0 {
		q += " AND v.story_id = ?"
		args = append(args, storyId)
	}
	var rows []KeyCount
	err := d.db.QueryRowsPartial(ctx, &rows, q+" GROUP BY b, k", args...)
	return rows, err
}

// Counters are a post's or a story's counters as the list of recent posts
// of a channel's statistics gives them.
type Counters struct {
	Id        int32 `db:"id"`
	Date      int64 `db:"date"`
	Views     int64 `db:"views"`
	Forwards  int64 `db:"forwards"`
	Reactions int64 `db:"reactions"`
}

// RecentPosts are the limit latest posts of the channel, the latest first.
func (d *Dao) RecentPosts(ctx context.Context, channelId int64, limit int) ([]Counters, error) {
	var rows []Counters
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT m.id, m.date2 AS `date`, m.views, m.forwards,"+
			" (SELECT COUNT(*) FROM channel_message_reactions r WHERE r.channel_id = m.channel_id AND r.message_id = m.id AND "+notGhost("r.user_id")+") AS reactions"+
			" FROM channel_messages m WHERE m.channel_id = ? AND "+isPost+" ORDER BY m.date2 DESC, m.id DESC LIMIT ?",
		channelId, limit)
	return rows, err
}

// RecentStories are the limit latest stories of the channel, the latest
// first (story ids grow with their dates).
func (d *Dao) RecentStories(ctx context.Context, channelId int64, limit int) ([]Counters, error) {
	var rows []Counters
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT s.story_id AS id, s.`date`, s.forwards_count AS forwards,"+
			" (SELECT COUNT(*) FROM story_views v WHERE v.peer_type = s.peer_type AND v.peer_id = s.peer_id AND v.story_id = s.story_id AND "+notGhost("v.viewer_id")+") AS views,"+
			" (SELECT COUNT(*) FROM story_views v WHERE v.peer_type = s.peer_type AND v.peer_id = s.peer_id AND v.story_id = s.story_id AND v.reaction <> '' AND "+notGhost("v.viewer_id")+") AS reactions"+
			" FROM stories s WHERE s.peer_type = ? AND s.peer_id = ? ORDER BY s.story_id DESC LIMIT ?",
		mtproto.PEER_CHANNEL, channelId, limit)
	return rows, err
}

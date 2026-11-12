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
	"errors"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
)

// The reads of updates.getChannelDifference (ADR 0034, p. 4). They go to
// the master, as the channel's other reads do: the committed state first,
// then the journal rows up to its pts, which are all committed by then.

// Viewer is what bounds what a user sees of a channel.
type Viewer struct {
	CanRead        bool  // a member, or anyone not kicked from a public channel
	Member         bool  // in the channel now
	AvailableMinId int32 // the first message it sees
	Joined         int64 // when a member joined (unix time), 0 for anyone else
	// Sublist is the one sublist of a monoforum the viewer sees, its own;
	// 0 in any other channel and for an admin of the direct messages, who
	// sees them all (task 084).
	Sublist int64
}

func (v viewer) exported() Viewer {
	return Viewer{CanRead: v.canRead, Member: v.member, AvailableMinId: v.availableMinId, Joined: v.joined, Sublist: v.sublist}
}

// ViewerOf reads what userId may see of channelId (viewerOf).
func (d *Dao) ViewerOf(ctx context.Context, userId, channelId int64) (Viewer, error) {
	v, err := d.viewerOf(ctx, userId, channelId)
	return v.exported(), err
}

// ViewerOfCatchUp is ViewerOf for updates.getChannelDifference, which goes on
// while the channel is blocked (viewerFor).
func (d *Dao) ViewerOfCatchUp(ctx context.Context, userId, channelId int64) (Viewer, error) {
	v, err := d.viewerFor(ctx, userId, channelId, true)
	return v.exported(), err
}

// InSublist are the messages of ids that are in the sublist peerId of the
// monoforum, deleted ones too: what a subscriber is told of an update that
// names messages by their ids (task 084).
func (d *Dao) InSublist(ctx context.Context, channelId, peerId int64, ids []int32) (map[int32]bool, error) {
	out := map[int32]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	args := []any{channelId, peerId}
	for _, id := range ids {
		args = append(args, id)
	}
	var mine []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &mine,
		"SELECT id FROM channel_messages WHERE channel_id = ? AND saved_peer_id = ? AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+")",
		args...); err != nil {
		return nil, err
	}
	for _, id := range mine {
		out[id] = true
	}
	return out, nil
}

// PtsBefore is the pts the channel's box had before the second date (unix
// time) began: that of the last journal row written earlier, 0 when the
// journal keeps none that old. The journal's dates are seconds: a row of
// that very second may come after an event of it, and counts as later.
func (d *Dao) PtsBefore(ctx context.Context, channelId, date int64) (int32, error) {
	var pts []int32
	err := d.Mysql.QueryRowsPartial(ctx, &pts,
		"SELECT pts FROM channel_pts_updates WHERE channel_id = ? AND date2 < ? ORDER BY pts DESC LIMIT 1",
		channelId, date)
	if len(pts) == 0 {
		return 0, err
	}
	return pts[0], err
}

// ChannelPts is the committed pts of the channel's box; ErrChannelInvalid
// for a channel that does not exist.
func (d *Dao) ChannelPts(ctx context.Context, channelId int64) (int32, error) {
	st, err := channelbox.Read(ctx, d.BoxDB, channelId)
	if errors.Is(err, channelbox.ErrNoChannel) {
		return 0, mtproto.ErrChannelInvalid
	}
	return st.Pts, err
}

// JournalRow is a row of a channel's journal.
type JournalRow struct {
	Pts        int32  `db:"pts"`
	PtsCount   int32  `db:"pts_count"`
	UpdateData string `db:"update_data"`
}

// Journal returns the rows of the channel's journal with pts in (after,
// upto], ascending, at most limit of them.
func (d *Dao) Journal(ctx context.Context, channelId int64, after, upto, limit int32) ([]JournalRow, error) {
	var rows []JournalRow
	err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT pts, pts_count, update_data FROM channel_pts_updates WHERE channel_id = ? AND pts > ? AND pts <= ? ORDER BY pts LIMIT ?",
		channelId, after, upto, limit)
	return rows, err
}

type randomIdRow struct {
	Id       int32 `db:"id"`
	RandomId int64 `db:"random_id"`
}

// OwnRandomIds maps the messages of ids that selfId sent to the channel to
// their random_id, 0 for one sent without.
func (d *Dao) OwnRandomIds(ctx context.Context, selfId, channelId int64, ids []int32) (map[int32]int64, error) {
	out := map[int32]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	args := []any{channelId, selfId}
	for _, id := range ids {
		args = append(args, id)
	}
	var rows []randomIdRow
	if err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT id, random_id FROM channel_messages WHERE channel_id = ? AND sender_user_id = ? AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+")",
		args...); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.Id] = r.RandomId
	}
	return out, nil
}

// ViewerDialog is the dialog of selfId with the channel as
// channelDifferenceTooLong gives it: from its row of dialogs, or, for a
// viewer that has none (a public channel it is not in), the channel's
// last message with nothing unread. The fields of the row the caller
// adds (notification settings, pins, folder) are not set.
func (d *Dao) ViewerDialog(ctx context.Context, selfId, channelId int64, v Viewer) (*mtproto.Dialog, error) {
	dialogs, err := d.ChannelDialogs(ctx, selfId, []int64{channelId})
	if err != nil {
		return nil, err
	}
	if len(dialogs) > 0 {
		return dialogs[0], nil
	}
	var rows []channelDialogRow
	if err = d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT id, pts, top_message FROM channels WHERE id = ? AND deleted = 0", channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	top := rows[0].TopMessage
	if top < v.AvailableMinId {
		top = 0
	}
	if v.Sublist != 0 {
		// A subscriber of a monoforum that has not written yet: its
		// sublist, which is empty or holds what the channel wrote to it.
		var own []int32
		if err = d.Mysql.QueryRowsPartial(ctx, &own,
			"SELECT COALESCE(MAX(id), 0) FROM channel_messages WHERE channel_id = ? AND saved_peer_id = ? AND deleted = 0", channelId, v.Sublist); err != nil {
			return nil, err
		}
		top = first(own)
	}
	return mtproto.MakeTLDialog(&mtproto.Dialog{
		Peer:           mtproto.MakePeerChannel(channelId),
		TopMessage:     top,
		ReadInboxMaxId: top,
		Pts:            mtproto.MakeFlagsInt32(rows[0].Pts),
	}).To_Dialog(), nil
}

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
}

// ViewerOf reads what userId may see of channelId (viewerOf).
func (d *Dao) ViewerOf(ctx context.Context, userId, channelId int64) (Viewer, error) {
	v, err := d.viewerOf(ctx, userId, channelId)
	return Viewer{CanRead: v.canRead, Member: v.member, AvailableMinId: v.availableMinId}, err
}

// ViewerOfCatchUp is ViewerOf for updates.getChannelDifference, which goes on
// while the channel is blocked (viewerFor).
func (d *Dao) ViewerOfCatchUp(ctx context.Context, userId, channelId int64) (Viewer, error) {
	v, err := d.viewerFor(ctx, userId, channelId, true)
	return Viewer{CanRead: v.canRead, Member: v.member, AvailableMinId: v.availableMinId}, err
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
// their random_id.
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
		"SELECT id, random_id FROM channel_messages WHERE channel_id = ? AND sender_user_id = ? AND random_id <> 0 AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+")",
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
	return mtproto.MakeTLDialog(&mtproto.Dialog{
		Peer:           mtproto.MakePeerChannel(channelId),
		TopMessage:     top,
		ReadInboxMaxId: top,
		Pts:            mtproto.MakeFlagsInt32(rows[0].Pts),
	}).To_Dialog(), nil
}

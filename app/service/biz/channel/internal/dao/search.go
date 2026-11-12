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
	"github.com/teamgram/teamgram-server/pkg/msgsearch"
)

func (d *Dao) readableSource(ctx context.Context, selfId, channelId int64, q SearchQuery) (msgsearch.Source, viewer, error) {
	v, err := d.viewerOf(ctx, selfId, channelId)
	if err != nil {
		return msgsearch.Source{}, viewer{}, err
	}
	if !v.canRead {
		return msgsearch.Source{}, viewer{}, mtproto.ErrChannelPrivate
	}
	pq := q.pageQuery(selfId, channelId)
	own, ownArgs := v.own("saved_peer_id")
	return msgsearch.Source{
		Table: "channel_messages",
		Id:    "id",
		Where: "channel_id = ? AND deleted = 0 AND id >= ?" + pq.where + own,
		Args:  append(append([]any{channelId, v.availableMinId}, pq.args...), ownArgs...),
	}, v, nil
}

// SearchCount is how many messages of channelId match q for selfId.
func (d *Dao) SearchCount(ctx context.Context, selfId, channelId int64, q SearchQuery) (int32, error) {
	src, _, err := d.readableSource(ctx, selfId, channelId, q)
	if err != nil {
		return 0, err
	}
	return msgsearch.Count(ctx, d.Mysql, src)
}

// SearchCalendar is the days of the messages of channelId of q's kind.
func (d *Dao) SearchCalendar(ctx context.Context, selfId, channelId int64, q SearchQuery, offsetId, offsetDate int32) ([]msgsearch.Day, int32, error) {
	src, _, err := d.readableSource(ctx, selfId, channelId, q)
	if err != nil {
		return nil, 0, err
	}
	return msgsearch.Calendar(ctx, d.Mysql, src, offsetId, offsetDate)
}

// SearchPositions are sparse positions of the messages of channelId of q's kind.
func (d *Dao) SearchPositions(ctx context.Context, selfId, channelId int64, q SearchQuery, offsetId, limit int32) ([]msgsearch.Position, int32, error) {
	src, _, err := d.readableSource(ctx, selfId, channelId, q)
	if err != nil {
		return nil, 0, err
	}
	return msgsearch.Positions(ctx, d.Mysql, src, offsetId, limit)
}

// Search folder modes of AcrossQuery.
const (
	FolderKeep    = 1
	FolderDrop    = 2
	viewBannedBit = 1 // chatBannedRights.view_messages
)

// AcrossQuery is a search over many channels: selfId's, or the public ones
// of the whole server with PublicOnly (channels.searchPosts).
type AcrossQuery struct {
	SelfId                                 int64
	PublicOnly, BroadcastsOnly, GroupsOnly bool
	CommunityId                            int64
	FolderMode                             int32
	FolderChannels                         []int64
	Q                                      string
	MediaType, MinDate, MaxDate            int32
	OffsetRate                             int32
	OffsetChannelId                        int64
	OffsetId, Limit                        int32
}

type acrossRow struct {
	ChannelId int64 `db:"channel_id"`
	Id        int32 `db:"id"`
}

// SearchAcross is a page of the messages of the channels q covers, newest
// first (equal dates by channel and id, biggest first), as selfId sees
// them, the count of all that match, and whether a next page can exist.
// A channel that went private between the two queries is left out.
func (d *Dao) SearchAcross(ctx context.Context, q AcrossQuery) ([]*mtproto.Message, int32, bool, error) {
	limit := min(max(q.Limit, 1), SearchMax)
	from, w, ok := q.scope()
	if !ok {
		return []*mtproto.Message{}, 0, false, nil
	}
	var count []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &count, "SELECT COUNT(*) FROM "+from+" WHERE m.deleted = 0"+w.SQL, w.Args...); err != nil {
		return nil, 0, false, err
	}
	if q.OffsetRate > 0 {
		if q.OffsetChannelId != 0 {
			w.And("(m.date2 < ? OR (m.date2 = ? AND (m.channel_id < ? OR (m.channel_id = ? AND m.id < ?))))",
				q.OffsetRate, q.OffsetRate, q.OffsetChannelId, q.OffsetChannelId, q.OffsetId)
		} else {
			w.And("m.date2 <= ?", q.OffsetRate)
		}
	}
	var rows []acrossRow
	if err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT m.channel_id, m.id FROM "+from+" WHERE m.deleted = 0"+w.SQL+" ORDER BY m.date2 DESC, m.channel_id DESC, m.id DESC LIMIT ?",
		append(w.Args, limit)...); err != nil {
		return nil, 0, false, err
	}
	byChannel := map[int64][]int32{}
	for _, r := range rows {
		byChannel[r.ChannelId] = append(byChannel[r.ChannelId], r.Id)
	}
	found := map[int64]map[int32]*mtproto.Message{}
	for channelId, ids := range byChannel {
		msgs, err := d.Messages(ctx, q.SelfId, channelId, ids)
		if err != nil {
			if errors.Is(err, mtproto.ErrChannelPrivate) {
				continue
			}
			return nil, 0, false, err
		}
		found[channelId] = make(map[int32]*mtproto.Message, len(msgs))
		for _, m := range msgs {
			found[channelId][m.GetId()] = m
		}
	}
	out := make([]*mtproto.Message, 0, len(rows))
	for _, r := range rows {
		if m := found[r.ChannelId][r.Id]; m != nil {
			out = append(out, m)
		}
	}
	return out, first(count), int32(len(rows)) == limit, nil
}

// scope is the FROM and the WHERE of the query; ok is false when nothing
// can match (an empty folder that was asked for).
func (q AcrossQuery) scope() (string, msgsearch.Where, bool) {
	var w msgsearch.Where
	cols := msgsearch.Columns{Prefix: "m."}
	from := "channel_messages m JOIN channels c ON c.id = m.channel_id AND c.deleted = 0"
	if q.PublicOnly {
		// Anyone not kicked may read a public channel (ADR 0034, p. 4).
		from += " JOIN username un ON un.peer_type = ? AND un.peer_id = m.channel_id AND un.editable = 1" +
			" LEFT JOIN channel_participants bp ON bp.channel_id = m.channel_id AND bp.user_id = ? AND bp.state = 2 AND bp.banned_rights & ? <> 0"
		w.Args = append(w.Args, mtproto.PEER_CHANNEL, q.SelfId, viewBannedBit)
		w.And("c.broadcast = 1")
		w.And("bp.user_id IS NULL")
	} else {
		from += " JOIN channel_participants p ON p.channel_id = m.channel_id AND p.user_id = ? AND p.state = 0"
		w.Args = append(w.Args, q.SelfId)
		w.And("m.id >= p.available_min_id")
	}
	if q.CommunityId != 0 {
		w.And("m.channel_id = ?", q.CommunityId)
	}
	if q.BroadcastsOnly {
		w.And("c.broadcast = 1")
	}
	if q.GroupsOnly {
		w.And("c.megagroup = 1")
	}
	switch {
	case q.FolderMode == FolderKeep && len(q.FolderChannels) == 0:
		return "", w, false
	case q.FolderMode == FolderKeep:
		w.And("m.channel_id IN ("+marks(len(q.FolderChannels))+")", int64Args(q.FolderChannels)...)
	case q.FolderMode == FolderDrop && len(q.FolderChannels) > 0:
		w.And("m.channel_id NOT IN ("+marks(len(q.FolderChannels))+")", int64Args(q.FolderChannels)...)
	}
	cols.Text(&w, q.MediaType, q.Q)
	cols.Kind(&w, q.MediaType)
	if q.MediaType == msgsearch.KindMyMentions {
		w.And("m.id IN (SELECT message_id FROM channel_message_mentions mm WHERE mm.channel_id = m.channel_id AND mm.user_id = ?)", q.SelfId)
	}
	cols.Dates(&w, q.MinDate, q.MaxDate)
	return from, w, true
}

func marks(n int) string { return "?" + strings.Repeat(", ?", n-1) }

func int64Args(v []int64) []any {
	out := make([]any, len(v))
	for i, id := range v {
		out[i] = id
	}
	return out
}

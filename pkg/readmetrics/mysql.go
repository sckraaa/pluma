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

package readmetrics

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// Store is the tables message_read_metrics and music_listens.
type Store struct {
	db  *sqlx.DB
	now func() time.Time
}

// New returns the store on db. Writes and the reads of the statistics go to
// the primary.
func New(db *sqlx.DB) *Store {
	return &Store{db: db.Master(), now: time.Now}
}

// ErrNotChannel is an Aggregate of a chat whose message ids are not
// shared: in a private chat and a basic group every reader's copy of a
// message has its own id (the reader's box), so the metrics of one id are
// of different messages.
var ErrNotChannel = errors.New("readmetrics: statistics are of channels only")

const metricColumns = 10

// AddMetrics keeps the metrics of userId's showings of messages of the chat
// (peerType, peerId) as the user names it: a private chat is the other
// user (PEER_USER, the user's own id for Saved Messages), the message ids
// those of the user's box, except in a channel. A showing reported again
// (the same view_id of the same message) is kept once. It returns how many
// rows were added.
func (s *Store) AddMetrics(ctx context.Context, userId int64, peerType int32, peerId int64, ms []Metric) (int64, error) {
	if len(ms) == 0 {
		return 0, nil
	}
	if len(ms) > MaxMetrics {
		return 0, mtproto.ErrLimitInvalid
	}
	date := s.now().Unix()
	var q strings.Builder
	q.WriteString("insert ignore into message_read_metrics(user_id, peer_type, peer_id, msg_id, view_id, time_in_view_ms, active_time_in_view_ms," +
		" height_to_viewport_ratio_permille, seen_range_ratio_permille, date) values ")
	args := make([]any, 0, len(ms)*metricColumns)
	for i, m := range ms {
		if i > 0 {
			q.WriteString(", ")
		}
		q.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args, userId, peerType, peerId, m.MsgId, m.ViewId, m.TimeInViewMs, m.ActiveTimeInViewMs,
			m.HeightToViewportRatioPermille, m.SeenRangeRatioPermille, date)
	}
	r, err := s.db.Exec(ctx, q.String(), args...)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

// AddListen keeps that userId listened to the document for duration
// seconds. The listens of one user and document in one calendar minute
// make one row, with the longest duration of them.
func (s *Store) AddListen(ctx context.Context, userId, documentId int64, duration int32) error {
	date := s.now().Unix()
	_, err := s.db.Exec(ctx,
		"insert into music_listens(user_id, document_id, minute, listened_duration, date) values (?, ?, ?, ?, ?)"+
			" on duplicate key update listened_duration = greatest(listened_duration, values(listened_duration))",
		userId, documentId, date/ListenWindow, duration, date)
	return err
}

// DeleteUser removes the rows of userId, whose account is deleted.
func (s *Store) DeleteUser(ctx context.Context, userId int64) error {
	if _, err := s.db.Exec(ctx, "delete from message_read_metrics where user_id = ?", userId); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, "delete from music_listens where user_id = ?", userId)
	return err
}

type metricRow struct {
	MsgId              int32 `db:"msg_id"`
	UserId             int64 `db:"user_id"`
	TimeInViewMs       int32 `db:"time_in_view_ms"`
	ActiveTimeInViewMs int32 `db:"active_time_in_view_ms"`
	SeenRangePermille  int32 `db:"seen_range_ratio_permille"`
}

// Aggregate adds up the metrics of the messages msgIds of the channel, in
// the order of msgIds; a message without metrics has zero stats. At most
// MaxAggregateMessages messages are asked at once.
func (s *Store) Aggregate(ctx context.Context, peerType int32, peerId int64, msgIds []int32) ([]MessageStats, error) {
	if peerType != mtproto.PEER_CHANNEL {
		return nil, ErrNotChannel
	}
	if len(msgIds) > MaxAggregateMessages {
		return nil, fmt.Errorf("readmetrics: %d messages, at most %d", len(msgIds), MaxAggregateMessages)
	}
	out := make([]MessageStats, len(msgIds))
	for i, id := range msgIds {
		out[i].MsgId = id
	}
	if len(msgIds) == 0 {
		return out, nil
	}
	args := make([]any, 0, 2+len(msgIds))
	args = append(args, peerType, peerId)
	for _, id := range msgIds {
		args = append(args, id)
	}
	var rows []metricRow
	err := s.db.QueryRowsPartial(ctx, &rows,
		"select msg_id, user_id, time_in_view_ms, active_time_in_view_ms, seen_range_ratio_permille from message_read_metrics"+
			" where peer_type = ? and peer_id = ? and msg_id in (?"+strings.Repeat(", ?", len(msgIds)-1)+")", args...)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return aggregate(out, rows), nil
}

// aggregate fills the stats of out from the rows.
func aggregate(out []MessageStats, rows []metricRow) []MessageStats {
	type acc struct {
		readers            map[int64]struct{}
		time, active, seen []int32
	}
	byId := map[int32]*acc{}
	for _, r := range rows {
		a := byId[r.MsgId]
		if a == nil {
			a = &acc{readers: map[int64]struct{}{}}
			byId[r.MsgId] = a
		}
		a.readers[r.UserId] = struct{}{}
		a.time = append(a.time, r.TimeInViewMs)
		a.active = append(a.active, r.ActiveTimeInViewMs)
		a.seen = append(a.seen, r.SeenRangePermille)
	}
	for i := range out {
		a := byId[out[i].MsgId]
		if a == nil {
			continue
		}
		out[i].Views = len(a.time)
		out[i].Readers = len(a.readers)
		for j := range a.time {
			out[i].TimeInViewMsSum += int64(a.time[j])
			out[i].ActiveTimeInViewMsSum += int64(a.active[j])
		}
		out[i].TimeInViewMsMedian = median(a.time)
		out[i].ActiveTimeInViewMsMedian = median(a.active)
		out[i].SeenRangePermilleMedian = median(a.seen)
	}
	return out
}

// median is the middle value of v, the lower of the two middle ones for an
// even count (a value that was reported), 0 for none. It sorts v.
func median(v []int32) int32 {
	if len(v) == 0 {
		return 0
	}
	slices.Sort(v)
	return v[(len(v)-1)/2]
}

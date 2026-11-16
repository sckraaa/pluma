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

// The members of channels and supergroups: their joins and leaves
// (channel_stats_member_events, from stats_collection's member_events on),
// who is in now, their languages and their notifications. A supergroup's
// statistics read them as a channel's.

// MemberCount is the number of joins (Joined) or leaves of a source in
// bucket B.
type MemberCount struct {
	B      int64 `db:"b"`
	Joined bool  `db:"joined"`
	Source int8  `db:"source"`
	N      int64 `db:"n"`
}

// MemberCounts are the joins and leaves of the channel in r, by bucket,
// join or leave, and source.
func (d *Dao) MemberCounts(ctx context.Context, channelId int64, r Range) ([]MemberCount, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	var rows []MemberCount
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT FLOOR(`date` / ?) AS b, joined, source, COUNT(*) AS n FROM channel_stats_member_events"+
			" WHERE channel_id = ? AND `date` >= ? AND `date` < ? GROUP BY b, joined, source",
		r.Bucket, channelId, r.From, r.To)
	return rows, err
}

// MemberDelta is the joins less the leaves of the channel from unix time
// from on.
func (d *Dao) MemberDelta(ctx context.Context, channelId, from int64) (int64, error) {
	var n []int64
	if err := d.db.QueryRowsPartial(ctx, &n,
		"SELECT COALESCE(SUM(IF(joined, 1, -1)), 0) FROM channel_stats_member_events WHERE channel_id = ? AND `date` >= ?",
		channelId, from); err != nil {
		return 0, err
	}
	if len(n) == 0 {
		return 0, nil
	}
	return n[0], nil
}

// MemberEvent is a join or a leave.
type MemberEvent struct {
	UserId int64 `db:"user_id"`
	Date   int64 `db:"date"`
	Joined bool  `db:"joined"`
}

// MemberEvents are the joins and leaves of the channel from unix time from
// on, the latest first.
func (d *Dao) MemberEvents(ctx context.Context, channelId, from int64) ([]MemberEvent, error) {
	var rows []MemberEvent
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT user_id, `date`, joined FROM channel_stats_member_events WHERE channel_id = ? AND `date` >= ? ORDER BY `date` DESC, id DESC",
		channelId, from)
	return rows, err
}

// Participant is a row of channel_participants: who is in (State 0) or was,
// and when they last joined.
type Participant struct {
	UserId int64 `db:"user_id"`
	State  int32 `db:"state"`
	Date   int64 `db:"date"`
}

// Participants are the rows of the channel's participants but the ghosts'
// (hidden).
func (d *Dao) Participants(ctx context.Context, channelId int64) ([]Participant, error) {
	var rows []Participant
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT user_id, state, `date` FROM channel_participants WHERE channel_id = ? AND hidden = 0", channelId)
	return rows, err
}

// langChunk bounds the users of one query of LangCodes.
const langChunk = 500

// LangCodes are the lang_code of initConnection of the session of each
// user of users that was active last; a user without a session has none.
func (d *Dao) LangCodes(ctx context.Context, users []int64) (map[int64]string, error) {
	type row struct {
		UserId     int64  `db:"user_id"`
		LangCode   string `db:"lang_code"`
		DateActive int64  `db:"date_active"`
	}
	out := make(map[int64]string, len(users))
	latest := make(map[int64]int64, len(users))
	for len(users) > 0 {
		chunk := users[:min(len(users), langChunk)]
		users = users[len(chunk):]
		args := make([]any, 0, len(chunk))
		for _, id := range chunk {
			args = append(args, id)
		}
		var rows []row
		if err := d.db.QueryRowsPartial(ctx, &rows,
			"SELECT u.user_id, a.lang_code, a.date_active FROM auth_users u JOIN auths a ON a.auth_key_id = u.auth_key_id AND a.deleted = 0"+
				" WHERE u.deleted = 0 AND u.user_id IN ("+placeholders(len(chunk))+")", args...); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if t, ok := latest[r.UserId]; !ok || r.DateActive > t {
				latest[r.UserId] = r.DateActive
				out[r.UserId] = r.LangCode
			}
		}
	}
	return out, nil
}

// MuteSetting is a member's mute_until of the channel (PeerMute) and of all
// channels or all groups (ClassMute); -1 is none set.
type MuteSetting struct {
	UserId    int64 `db:"user_id"`
	PeerMute  int32 `db:"peer_mute"`
	ClassMute int32 `db:"class_mute"`
}

// MuteSettings are the notify settings of the members of the channel (in
// it now, no ghosts) that its muting comes from: the channel's own, and
// those of all broadcast channels, or of all groups for a supergroup
// (biz user user_notify_settings.go muteInEffect).
func (d *Dao) MuteSettings(ctx context.Context, channelId int64, broadcast bool) ([]MuteSetting, error) {
	class := int32(mtproto.PEER_CHATS)
	if broadcast {
		class = mtproto.PEER_BROADCASTS
	}
	var rows []MuteSetting
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT p.user_id, COALESCE(s.mute_until, -1) AS peer_mute, COALESCE(c.mute_until, -1) AS class_mute FROM channel_participants p"+
			" LEFT JOIN user_notify_settings s ON s.user_id = p.user_id AND s.peer_type = ? AND s.peer_id = p.channel_id AND s.deleted = 0"+
			" LEFT JOIN user_notify_settings c ON c.user_id = p.user_id AND c.peer_type = ? AND c.peer_id = 0 AND c.deleted = 0"+
			" WHERE p.channel_id = ? AND p.state = 0 AND p.hidden = 0",
		mtproto.PEER_CHANNEL, class, channelId)
	return rows, err
}

// MuteEvent is a change of a member's muting in effect: MuteUntil 0 is
// unmuted, a later time muted until it.
type MuteEvent struct {
	UserId    int64 `db:"user_id"`
	Date      int64 `db:"date"`
	MuteUntil int32 `db:"mute_until"`
}

// MuteEvents are the changes of the muting of the channel before unix time
// to, by user and in order.
func (d *Dao) MuteEvents(ctx context.Context, channelId, to int64) ([]MuteEvent, error) {
	var rows []MuteEvent
	err := d.db.QueryRowsPartial(ctx, &rows,
		"SELECT user_id, `date`, mute_until FROM channel_stats_mute_events WHERE channel_id = ? AND `date` < ? ORDER BY user_id, `date`, id",
		channelId, to)
	return rows, err
}

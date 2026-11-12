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

package groupcalls

import (
	"context"
	"database/sql"
	"errors"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// The participants and the version (core.telegram.org/api/group-calls).
// A join and a leave are versioned: each grows the version of the call by
// exactly one and goes out with the new version; every other change goes
// out with the current one. The clients apply an update whose version
// follows theirs and reload the call on a gap (tdesktop
// data_group_call.cpp:413-441, 600-680: getGroupCall after 3 s; Android
// ChatObject.java:1040-1054), and take version 0 for an API error (:515-517).
// So a join or a leave locks the row of its call: two of them never get the
// same version, and none is skipped.

type participantRow struct {
	CallId             int64         `db:"call_id"`
	PeerType           int32         `db:"peer_type"`
	PeerId             int64         `db:"peer_id"`
	UserId             int64         `db:"user_id"`
	AuthKeyId          int64         `db:"auth_key_id"`
	InCall             bool          `db:"in_call"`
	Source             sql.NullInt64 `db:"source"`
	PresentationSource sql.NullInt64 `db:"presentation_source"`
	Muted              bool          `db:"muted"`
	CanSelfUnmute      bool          `db:"can_self_unmute"`
	Volume             int32         `db:"volume"`
	RaiseHandRating    int64         `db:"raise_hand_rating"`
	VideoJoined        bool          `db:"video_joined"`
	VideoStopped       bool          `db:"video_stopped"`
	VideoPaused        bool          `db:"video_paused"`
	Video              string        `db:"video"`
	Presentation       string        `db:"presentation"`
	PresentationPaused bool          `db:"presentation_paused"`
	Date               int32         `db:"date"`
	ActiveDate         int32         `db:"active_date"`
	SeenAt             int64         `db:"seen_at"`
}

const participantColumns = "call_id, peer_type, peer_id, user_id, auth_key_id, in_call, source, presentation_source, muted, can_self_unmute, volume, raise_hand_rating, video_joined, video_stopped, video_paused, video, presentation, presentation_paused, `date`, active_date, seen_at"

func (r *participantRow) participant() (*Participant, error) {
	p := &Participant{
		CallId: r.CallId, PeerType: r.PeerType, PeerId: r.PeerId, UserId: r.UserId, AuthKeyId: r.AuthKeyId,
		InCall: r.InCall, Source: uint32(r.Source.Int64), PresentationSource: uint32(r.PresentationSource.Int64),
		Muted: r.Muted, CanSelfUnmute: r.CanSelfUnmute, Volume: r.Volume, RaiseHandRating: r.RaiseHandRating,
		VideoJoined: r.VideoJoined, VideoStopped: r.VideoStopped, VideoPaused: r.VideoPaused,
		PresentationPaused: r.PresentationPaused, Date: r.Date, ActiveDate: r.ActiveDate, SeenAt: r.SeenAt,
	}
	var err error
	if p.Video, err = unmarshalMedia(r.Video); err != nil {
		return nil, err
	}
	if p.Presentation, err = unmarshalMedia(r.Presentation); err != nil {
		return nil, err
	}
	return p, nil
}

func participants(ctx context.Context, q querier, where string, args ...any) ([]*Participant, error) {
	var rows []participantRow
	err := q.QueryRowsPartial(ctx, &rows, "select "+participantColumns+" from group_call_participants "+where, args...)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make([]*Participant, 0, len(rows))
	for i := range rows {
		p, err := rows[i].participant()
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Change is one participant row as an update tells it.
type Change struct {
	Participant *Participant
	// JustJoined: the peer came into the call (the clients count it);
	// Versioned: the change is a join or a leave.
	JustJoined bool
	Versioned  bool
}

// JoinParams is a join of phone.joinGroupCall.
type JoinParams struct {
	CallId    int64
	UserId    int64
	AuthKeyId int64
	Source    uint32
	// Video is the camera the join params describe; nil without video.
	Video        *Media
	VideoStopped bool
	// State gives muted and can_self_unmute (JoinState) for the call and
	// the row the join finds (nil for none).
	State func(call *Call, prev *Participant) (muted, canSelfUnmute bool)
	// VideoLimit is the cameras the call takes at once.
	VideoLimit int32
	// Node is the SFU node the join connected on; "" checks none.
	Node string
}

// JoinResult is a join done: the call afterwards, the account's row and
// the change to tell.
type JoinResult struct {
	Call    *Call
	Self    *Participant
	Changes []Change
}

// Join puts the account into a call as itself, or connects it again: a
// repeated join (a new connection after a network change or an SFU
// restart) replaces the source. ErrDiscarded, ErrScheduled, ErrFull or
// ErrSourceInUse refuse it.
func (s *Store) Join(ctx context.Context, j JoinParams) (*JoinResult, error) {
	var res *JoinResult
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		res = nil
		call, err := oneCall(ctx, txq{tx}, "where c.id = ? for update", j.CallId)
		if err != nil {
			return err
		}
		switch {
		case call.Discarded:
			return ErrDiscarded
		case call.Scheduled():
			return ErrScheduled
		}
		switch {
		case j.Node == "" || call.SfuNode == j.Node:
		case call.SfuNode == "":
			// The collector forgot the node of a room it found empty
			// while this join opened the room again: the room is there.
			if _, err = tx.Exec("update group_calls set sfu_node = ? where id = ?", j.Node, j.CallId); err != nil {
				return err
			}
		default:
			return ErrMoved
		}
		mine, err := participants(ctx, txq{tx}, "where call_id = ? and peer_type = ? and peer_id = ? for update", j.CallId, mtproto.PEER_USER, j.UserId)
		if err != nil {
			return err
		}
		var prev *Participant
		if len(mine) > 0 {
			prev = mine[0]
		}
		already := prev != nil && prev.InCall
		if !already && call.ParticipantsCount >= MaxParticipants {
			return ErrFull
		}
		now := s.now()
		p := prev
		if p == nil {
			p = &Participant{CallId: j.CallId, PeerType: mtproto.PEER_USER, PeerId: j.UserId}
		}
		p.UserId, p.AuthKeyId, p.Source, p.Video, p.VideoStopped = j.UserId, j.AuthKeyId, j.Source, j.Video, j.VideoStopped
		p.Muted, p.CanSelfUnmute = j.State(call, prev)
		if !p.CanSelfUnmute {
			// No camera without the word; the SFU does not forward it.
			p.VideoStopped = true
		}
		if !already {
			// A camera may go on while the call has room for one more
			// (Android ChatObject.java:420 takes can_start_video for it).
			limit := j.VideoLimit
			if limit <= 0 {
				limit = DefaultVideoLimit
			}
			p.VideoJoined = call.VideoCount < limit
			p.Date, p.RaiseHandRating, p.VideoPaused = int32(now.Unix()), 0, false
			p.PresentationSource, p.Presentation, p.PresentationPaused = 0, nil, false
		}
		p.InCall, p.SeenAt = true, now.UnixMilli()
		video, err := marshalMedia(p.Video)
		if err != nil {
			return err
		}
		presentation, err := marshalMedia(p.Presentation)
		if err != nil {
			return err
		}
		// The source must be free: an upsert would change the row of the
		// participant that holds it.
		var holders []int64
		if err = tx.QueryRowsPartial(&holders, "select peer_id from group_call_participants where call_id = ? and source = ? and not (peer_type = ? and peer_id = ?)",
			p.CallId, int64(p.Source), p.PeerType, p.PeerId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
			return err
		}
		if len(holders) > 0 {
			return ErrSourceInUse
		}
		if prev == nil {
			_, err = tx.Exec("insert into group_call_participants (call_id, peer_type, peer_id, user_id, auth_key_id, in_call, source, presentation_source, muted, can_self_unmute, volume, raise_hand_rating, video_joined, video_stopped, video_paused, video, presentation, presentation_paused, `date`, active_date, seen_at)"+
				" values (?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
				p.CallId, p.PeerType, p.PeerId, p.UserId, p.AuthKeyId, int64(p.Source), nullSource(p.PresentationSource), p.Muted, p.CanSelfUnmute,
				p.Volume, p.RaiseHandRating, p.VideoJoined, p.VideoStopped, p.VideoPaused, video, presentation, p.PresentationPaused,
				p.Date, p.ActiveDate, p.SeenAt)
		} else {
			_, err = tx.Exec("update group_call_participants set user_id = ?, auth_key_id = ?, in_call = 1, source = ?, presentation_source = ?, muted = ?, can_self_unmute = ?,"+
				" raise_hand_rating = ?, video_joined = ?, video_stopped = ?, video_paused = ?, video = ?, presentation = ?, presentation_paused = ?, `date` = ?, seen_at = ?"+
				" where call_id = ? and peer_type = ? and peer_id = ?",
				p.UserId, p.AuthKeyId, int64(p.Source), nullSource(p.PresentationSource), p.Muted, p.CanSelfUnmute,
				p.RaiseHandRating, p.VideoJoined, p.VideoStopped, p.VideoPaused, video, presentation, p.PresentationPaused, p.Date, p.SeenAt,
				p.CallId, p.PeerType, p.PeerId)
		}
		if isDuplicate(err) {
			return ErrSourceInUse
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec("update group_calls set version = version + 1 where id = ?", j.CallId); err != nil {
			return err
		}
		res = &JoinResult{Self: p, Changes: []Change{{Participant: p, JustJoined: !already, Versioned: true}}}
		res.Call, err = oneCall(ctx, txq{tx}, "where c.id = ?", j.CallId)
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func nullSource(s uint32) any {
	if s == 0 {
		return nil
	}
	return int64(s)
}

// leaveRow takes a participant out of the call; the version is the
// caller's.
func leaveRow(tx *sqlx.Tx, p *Participant) error {
	_, err := tx.Exec("update group_call_participants set in_call = 0, source = null, presentation_source = null, presentation = '', presentation_paused = 0, raise_hand_rating = 0"+
		" where call_id = ? and peer_type = ? and peer_id = ?", p.CallId, p.PeerType, p.PeerId)
	return err
}

// LeaveCond is which connection a leave ends.
type LeaveCond struct {
	// Source, when not 0, must be the participant's: a leave of a
	// connection the account replaced since does not end the new one.
	Source uint32
	// SeenBefore, when not 0, leaves only a participant the SFU last held
	// before it (unix ms): the collector's leave of one who is gone.
	SeenBefore int64
}

// Leave takes the account userId out of a call. ok is false (and nothing
// changes) when it is not in the call or cond does not hold.
func (s *Store) Leave(ctx context.Context, callId, userId int64, cond LeaveCond) (call *Call, change *Change, err error) {
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		change = nil
		c, err := oneCall(ctx, txq{tx}, "where c.id = ? for update", callId)
		if err != nil {
			return err
		}
		call = c
		if c.Discarded {
			return nil
		}
		list, err := participants(ctx, txq{tx}, "where call_id = ? and user_id = ? and in_call = 1 for update", callId, userId)
		if err != nil || len(list) == 0 {
			return err
		}
		p := list[0]
		if (cond.Source != 0 && p.Source != cond.Source) || (cond.SeenBefore != 0 && p.SeenAt >= cond.SeenBefore) {
			return nil
		}
		if err = leaveRow(tx, p); err != nil {
			return err
		}
		if _, err = tx.Exec("update group_calls set version = version + 1 where id = ?", callId); err != nil {
			return err
		}
		p.InCall, p.Source, p.PresentationSource, p.Presentation, p.RaiseHandRating = false, 0, 0, nil, 0
		change = &Change{Participant: p, Versioned: true}
		call, err = oneCall(ctx, txq{tx}, "where c.id = ?", callId)
		return err
	})
	return call, change, err
}

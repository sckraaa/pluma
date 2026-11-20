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
	"errors"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// An invitation to a conference is a service message
// messageActionConferenceCall in the private chat of the inviter and the
// invitee; the clients ring on it while it shows neither active, missed nor
// a duration (tdesktop data_media_types.cpp:513-533, Android
// NotificationsController.java:1088-1109, iOS CallSessionManager.swift:
// 486-500), and stop when an edit of it shows one of them. Its state moves
// here by conditional updates, one way only:
//
//	ringing ──the invitee joins──► active ──the conference ends──► ended
//	   └──declined, cancelled, not answered, the conference ends──► missed
//
// Shown is the state the message shows; the message is edited after each
// move and again by the collector while the two differ.

// InviteState is where an invitation is.
type InviteState int8

const (
	InviteRinging InviteState = iota
	InviteActive
	InviteMissed
	InviteEnded
)

const (
	// InviteRingTimeout is how long an invitation rings: call_ring_timeout_ms
	// of help.getConfig (teamgramd/bin/config.json), the clients' own ring
	// time (tdesktop calls_call.cpp:256-260, Android VoIPGroupNotification
	// .java:324), as phonecalls.RingTimeout.
	InviteRingTimeout = 90 * time.Second
	// InvitesPerMinute bounds the invitations one account sends.
	InvitesPerMinute = 30
)

// Invite is one invitation.
type Invite struct {
	Id        int64 `db:"id"`
	CallId    int64 `db:"call_id"`
	InviterId int64 `db:"inviter_id"`
	InviteeId int64 `db:"invitee_id"`
	// InviterMsgId is the message in the inviter's box; DialogMessageId
	// names it in both boxes.
	InviterMsgId    int32       `db:"inviter_msg_id"`
	DialogMessageId int64       `db:"dialog_message_id"`
	Video           bool        `db:"video"`
	State           InviteState `db:"state"`
	Shown           InviteState `db:"shown"`
	// Duration is that of the conference, for InviteEnded.
	Duration  int32 `db:"duration"`
	Date      int32 `db:"date"`
	RingUntil int32 `db:"ring_until"`
}

const inviteColumns = "id, call_id, inviter_id, invitee_id, inviter_msg_id, dialog_message_id, video, state, shown, duration, `date`, ring_until"

func invites(ctx context.Context, q querier, where string, args ...any) ([]*Invite, error) {
	var rows []Invite
	err := q.QueryRowsPartial(ctx, &rows, "select "+inviteColumns+" from group_call_invites "+where, args...)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make([]*Invite, 0, len(rows))
	for i := range rows {
		out = append(out, &rows[i])
	}
	return out, nil
}

// AddInvite stores a new ringing invitation; Id, Date and RingUntil are
// set here.
func (s *Store) AddInvite(ctx context.Context, inv *Invite) error {
	now := s.now()
	inv.Date, inv.RingUntil = int32(now.Unix()), int32(now.Add(InviteRingTimeout).Unix())
	inv.State, inv.Shown = InviteRinging, InviteRinging
	r, err := s.db.Exec(ctx, "insert into group_call_invites (call_id, inviter_id, invitee_id, inviter_msg_id, dialog_message_id, video, state, shown, `date`, ring_until) values (?, ?, ?, ?, ?, ?, 0, 0, ?, ?)",
		inv.CallId, inv.InviterId, inv.InviteeId, inv.InviterMsgId, inv.DialogMessageId, inv.Video, inv.Date, inv.RingUntil)
	if err != nil {
		return err
	}
	inv.Id, err = r.LastInsertId()
	return err
}

// Ringing returns the invitation of inviteeId to a conference that rings
// now; ErrNotFound if none does.
func (s *Store) Ringing(ctx context.Context, callId, inviteeId int64) (*Invite, error) {
	list, err := invites(ctx, dbq{s.db}, "where call_id = ? and invitee_id = ? and state = 0 and ring_until >= ? order by id desc limit 1", callId, inviteeId, int32(s.now().Unix()))
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// InviteOf returns the invitation of a message, dialogMessageId, in the
// private chat of userId as its inviter or its invitee; ErrNotFound if
// the message is no invitation of his.
func (s *Store) InviteOf(ctx context.Context, dialogMessageId, userId int64) (*Invite, error) {
	list, err := invites(ctx, dbq{s.db}, "where dialog_message_id = ? and (inviter_id = ? or invitee_id = ?) limit 1", dialogMessageId, userId, userId)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// Invited reports whether userId has an invitation to a conference, in
// any state.
func (s *Store) Invited(ctx context.Context, callId, userId int64) (bool, error) {
	var ids []int64
	if err := s.db.QueryRowsPartial(ctx, &ids, "select id from group_call_invites where call_id = ? and invitee_id = ? limit 1", callId, userId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return false, err
	}
	return len(ids) > 0, nil
}

// InvitesSent is the invitations userId sent in the last minute.
func (s *Store) InvitesSent(ctx context.Context, userId int64) (int, error) {
	var n []int64
	if err := s.db.QueryRowsPartial(ctx, &n, "select count(*) from group_call_invites where inviter_id = ? and `date` > ?", userId, int32(s.now().Add(-time.Minute).Unix())); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return 0, err
	}
	if len(n) == 0 {
		return 0, nil
	}
	return int(n[0]), nil
}

// MoveInvite moves an invitation from the state from to to (with the
// conference's duration for InviteEnded). moved is false when it was not in
// from: a race another step won; the invitation is returned either way.
func (s *Store) MoveInvite(ctx context.Context, id int64, from, to InviteState, duration int32) (inv *Invite, moved bool, err error) {
	r, err := s.db.Exec(ctx, "update group_call_invites set state = ?, duration = ? where id = ? and state = ?", to, duration, id, from)
	if err != nil {
		return nil, false, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	list, err := invites(ctx, dbq{s.db}, "where id = ?", id)
	if err != nil {
		return nil, false, err
	}
	if len(list) == 0 {
		return nil, false, ErrNotFound
	}
	return list[0], n > 0, nil
}

// InvitesIn returns the invitations to a conference in state.
func (s *Store) InvitesIn(ctx context.Context, callId int64, state InviteState) ([]*Invite, error) {
	return invites(ctx, dbq{s.db}, "where call_id = ? and state = ?", callId, state)
}

// Unanswered returns at most limit invitations that rang out by now.
func (s *Store) Unanswered(ctx context.Context, limit int) ([]*Invite, error) {
	return invites(ctx, dbq{s.db}, "where state = 0 and ring_until < ? order by ring_until limit ?", int32(s.now().Unix()), limit)
}

// Unshown returns at most limit invitations of the last since whose
// messages do not show their state yet.
func (s *Store) Unshown(ctx context.Context, since time.Time, limit int) ([]*Invite, error) {
	return invites(ctx, dbq{s.db}, "where state <> shown and `date` > ? order by id limit ?", int32(since.Unix()), limit)
}

// MarkShown records that the message of an invitation shows state.
func (s *Store) MarkShown(ctx context.Context, id int64, state InviteState) error {
	_, err := s.db.Exec(ctx, "update group_call_invites set shown = ? where id = ? and state = ?", state, id, state)
	return err
}

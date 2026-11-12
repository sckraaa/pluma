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

// Package monoforum is the direct messages of a channel (task 084, ADR
// 0055; core.telegram.org/api/monoforum): a supergroup of its own, the
// monoforum, linked both ways with the broadcast channel. Every message of
// it is in the chat of one subscriber with the channel, a sublist, named by
// the message's saved_peer_id.
//
// Nobody joins a monoforum, and it has no rows of channel_participants.
// What a user is in it follows from the channel, read each time:
//   - the creator of the channel and its admins with manage_direct_messages
//     (the right every client checks on the channel: Android
//     ChatObject.java:1767-1797, tdesktop data_channel.cpp:748-750, iOS
//     PendingMessageManager.swift:1330) see every sublist and answer in
//     them as the channel;
//   - anyone else who sees the channel (a subscriber, or anyone not banned
//     from a public one) sees and writes its own sublist alone.
//
// A change of rights in the channel therefore takes effect at once, with
// nothing to keep in step.
package monoforum

import (
	"context"
	"math"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

// Querier reads rows: a transaction (*sqlx.Tx), or a database with OnDB.
type Querier interface {
	QueryRowsPartial(v any, query string, args ...any) error
}

type onDB struct {
	ctx context.Context
	db  *sqlx.DB
}

func (q onDB) QueryRowsPartial(v any, query string, args ...any) error {
	return q.db.QueryRowsPartial(q.ctx, v, query, args...)
}

// OnDB is a Querier of the database outside a transaction.
func OnDB(ctx context.Context, db *sqlx.DB) Querier {
	return onDB{ctx: ctx, db: db}
}

// Side is what a channel is to direct messages.
type Side struct {
	// Monoforum: the supergroup that holds the direct messages of a
	// channel. Otherwise the row is an ordinary channel or supergroup, a
	// broadcast channel with direct messages when LinkedId is set.
	Monoforum bool
	// LinkedId is the other side: the channel of a monoforum, the monoforum
	// of a channel; 0 for none. It stays when the direct messages are off.
	LinkedId int64
	// Enabled: the channel takes direct messages now
	// (channel.broadcast_messages_allowed); of a monoforum, its channel
	// does and still exists.
	Enabled bool
}

// BroadcastId is the broadcast channel of the monoforum id with side s.
func (s Side) BroadcastId() int64 {
	if s.Monoforum {
		return s.LinkedId
	}
	return 0
}

type sideRow struct {
	Monoforum     bool  `db:"monoforum"`
	LinkedId      int64 `db:"linked_monoforum_id"`
	Allowed       bool  `db:"broadcast_messages_allowed"`
	LinkedAllowed bool  `db:"linked_allowed"`
}

// SideOf reads what channelId is to direct messages; a channel that does
// not exist is no side at all.
func SideOf(q Querier, channelId int64) (Side, error) {
	var rows []sideRow
	if err := q.QueryRowsPartial(&rows,
		"SELECT c.monoforum, c.linked_monoforum_id, c.broadcast_messages_allowed,"+
			" COALESCE(l.broadcast_messages_allowed = 1 AND l.deleted = 0, 0) AS linked_allowed"+
			" FROM channels c LEFT JOIN channels l ON l.id = c.linked_monoforum_id WHERE c.id = ?",
		channelId); err != nil {
		return Side{}, err
	}
	if len(rows) == 0 {
		return Side{}, nil
	}
	r := rows[0]
	if r.Monoforum {
		return Side{Monoforum: true, LinkedId: r.LinkedId, Enabled: r.LinkedAllowed}, nil
	}
	return Side{LinkedId: r.LinkedId, Enabled: r.Allowed && r.LinkedId != 0}, nil
}

// Member is what a user is in the monoforum of a channel.
type Member struct {
	// Access: the user may open the monoforum.
	Access bool
	// Admin: it manages the direct messages, seeing every sublist.
	Admin bool
	// Creator: it created the channel.
	Creator bool
	// AdminRights are its admin rights in the channel (the creator's are
	// channelperm.CreatorRights).
	AdminRights int32
}

// Manages tells whether the row of a participant of a broadcast channel
// makes it an admin of the channel's direct messages.
func Manages(role, state, adminRights int32) bool {
	if state != channelperm.StateIn {
		return false
	}
	return role == channelperm.RoleCreator ||
		role == channelperm.RoleAdmin && adminRights&mtproto.ADMIN_MANAGE_DIRECT_MESSAGES != 0
}

type memberRow struct {
	UserId      int64 `db:"user_id"`
	Role        int32 `db:"role"`
	State       int32 `db:"state"`
	AdminRights int32 `db:"admin_rights"`
	UntilDate   int32 `db:"until_date"`
}

type broadcastRow struct {
	Deleted bool `db:"deleted"`
	Blocked bool `db:"blocked"`
	Public  bool `db:"public"`
}

// Members reads what the users ids are in the monoforum of the broadcast
// channel broadcastId at now (unix time). A user who never joined a public
// channel has access; a private channel gives it to its members alone, and
// a ban of the channel that still runs takes it away. Nobody has access to
// the monoforum of a channel that is gone or blocked.
func Members(q Querier, broadcastId int64, ids []int64, now int64) (map[int64]Member, error) {
	out := make(map[int64]Member, len(ids))
	if broadcastId == 0 || len(ids) == 0 {
		return out, nil
	}
	var ch []broadcastRow
	if err := q.QueryRowsPartial(&ch,
		"SELECT c.deleted, c.blocked, EXISTS (SELECT 1 FROM username u WHERE u.peer_type = ? AND u.peer_id = c.id AND u.editable = 1) AS public"+
			" FROM channels c WHERE c.id = ? AND c.broadcast = 1",
		mtproto.PEER_CHANNEL, broadcastId); err != nil {
		return nil, err
	}
	if len(ch) == 0 || ch[0].Deleted || ch[0].Blocked {
		return out, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, broadcastId)
	for _, id := range ids {
		args = append(args, id)
	}
	var rows []memberRow
	if err := q.QueryRowsPartial(&rows,
		"SELECT user_id, role, state, admin_rights, until_date FROM channel_participants WHERE channel_id = ? AND user_id IN (?"+
			strings.Repeat(", ?", len(ids)-1)+")", args...); err != nil {
		return nil, err
	}
	byId := make(map[int64]memberRow, len(rows))
	for _, r := range rows {
		byId[r.UserId] = r
	}
	for _, id := range ids {
		r, ok := byId[id]
		switch {
		case !ok || r.State == channelperm.StateLeft:
			out[id] = Member{Access: ch[0].Public}
		case r.State == channelperm.StateKicked:
			// A ban that ran out leaves the user out of the channel, as one
			// who left.
			running := r.UntilDate == 0 || r.UntilDate == math.MaxInt32 || now < int64(r.UntilDate)
			out[id] = Member{Access: ch[0].Public && !running}
		case Manages(r.Role, r.State, r.AdminRights):
			m := Member{Access: true, Admin: true, Creator: r.Role == channelperm.RoleCreator, AdminRights: r.AdminRights}
			if m.Creator {
				m.AdminRights = channelperm.CreatorRights
			}
			out[id] = m
		default:
			out[id] = Member{Access: true}
		}
	}
	return out, nil
}

// MemberOf is Members for one user.
func MemberOf(q Querier, broadcastId, userId, now int64) (Member, error) {
	m, err := Members(q, broadcastId, []int64{userId}, now)
	return m[userId], err
}

// AdminIds are the users who manage the direct messages of the broadcast
// channel broadcastId now, in ascending order; none for a channel that is
// gone or blocked.
func AdminIds(q Querier, broadcastId int64) ([]int64, error) {
	var ids []int64
	err := q.QueryRowsPartial(&ids,
		"SELECT p.user_id FROM channel_participants p JOIN channels c ON c.id = p.channel_id AND c.deleted = 0 AND c.blocked = 0 AND c.broadcast = 1"+
			" WHERE p.channel_id = ? AND p.state = ? AND (p.role = ? OR p.role = ? AND p.admin_rights & ? <> 0) ORDER BY p.user_id",
		broadcastId, channelperm.StateIn, channelperm.RoleCreator, channelperm.RoleAdmin, mtproto.ADMIN_MANAGE_DIRECT_MESSAGES)
	return ids, err
}

// Recipients are the users the updates of the sublists peers of the
// monoforum of broadcastId go to: the admins of the direct messages and,
// of each sublist, its subscriber while it still has access. In ascending
// order, each once.
func Recipients(q Querier, broadcastId int64, peers []int64, now int64) ([]int64, error) {
	admins, err := AdminIds(q, broadcastId)
	if err != nil {
		return nil, err
	}
	members, err := Members(q, broadcastId, peers, now)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool, len(admins)+len(peers))
	out := make([]int64, 0, len(admins)+len(peers))
	for _, id := range admins {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range peers {
		if id != 0 && !seen[id] && members[id].Access {
			seen[id] = true
			out = append(out, id)
		}
	}
	sortIds(out)
	return out, nil
}

func sortIds(ids []int64) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j-1] > ids[j]; j-- {
			ids[j-1], ids[j] = ids[j], ids[j-1]
		}
	}
}

// SublistsOf maps the messages ids of the monoforum to the sublist each is
// in, deleted ones too: who an update about them goes to.
func SublistsOf(q Querier, monoforumId int64, ids []int32) (map[int32]int64, error) {
	out := make(map[int32]int64, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, monoforumId)
	for _, id := range ids {
		args = append(args, id)
	}
	var rows []struct {
		Id          int32 `db:"id"`
		SavedPeerId int64 `db:"saved_peer_id"`
	}
	if err := q.QueryRowsPartial(&rows,
		"SELECT id, saved_peer_id FROM channel_messages WHERE channel_id = ? AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+")", args...); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.Id] = r.SavedPeerId
	}
	return out, nil
}

// Peers are the sublists the messages of m are in, each once, in the order
// met.
func Peers(m map[int32]int64) []int64 {
	var out []int64
	seen := map[int64]bool{}
	for _, p := range m {
		if p != 0 && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sortIds(out)
	return out
}

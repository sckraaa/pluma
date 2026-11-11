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

// Package ghost is the support ghost (task 080, ADR 0048): an account
// (users.ghost) that takes part in chats without being seen among their
// members. Its participant rows are hidden (channel_participants.hidden,
// chat_participants.hidden): a member for delivery, history, rights, push
// and unread mentions, left out of everything the other members are shown.
//
// A hidden join writes the row alone: no counter, no service message, no
// recent action, no request, no use of an invite link. A ghost sees the
// whole history of a supergroup or channel it joins (available_min_id 0).
// A ban still keeps it out.
package ghost

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

// Ghosts are the ghosts among ids, in ascending order.
func Ghosts(q Querier, ids []int64) ([]int64, error) {
	ids = positive(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	var out []int64
	if err := q.QueryRowsPartial(&out, "SELECT id FROM users WHERE ghost = 1 AND id IN ("+placeholders(len(ids))+") ORDER BY id", args...); err != nil {
		return nil, err
	}
	return out, nil
}

// IsGhost tells whether the account id is a ghost.
func IsGhost(q Querier, id int64) (bool, error) {
	g, err := Ghosts(q, []int64{id})
	return len(g) > 0, err
}

// Outcome is what a hidden join found.
type Outcome int

const (
	// Joined: the ghost had no live row and has a hidden one now.
	Joined Outcome = iota
	// Member: the ghost was in already; nothing changed.
	Member
	// Banned: the ghost is kicked and its ban runs; nothing changed.
	Banned
)

type channelRow struct {
	State     int32 `db:"state"`
	UntilDate int32 `db:"until_date"`
}

// kicked tells whether a kick runs at now (0 or Int32.max: for good), as
// channelperm reads it.
func (r channelRow) kicked(now int64) bool {
	return r.State == channelperm.StateKicked &&
		(r.UntilDate == 0 || r.UntilDate == math.MaxInt32 || now < int64(r.UntilDate))
}

// JoinChannel makes the ghost userId a hidden member of the channel or
// supergroup channelId at date, in tx, which has the channel locked
// (channelbox.Open). A row of one who left, or whose ban ran out, comes back
// as a plain member with the whole history: no role, no rights, no
// restriction. The counters, the messages and the recent actions stay as
// they are. The caller has checked that userId is a ghost.
func JoinChannel(tx *sqlx.Tx, channelId, userId, date int64) (Outcome, error) {
	var rows []channelRow
	if err := tx.QueryRowsPartial(&rows, "SELECT state, until_date FROM channel_participants WHERE channel_id = ? AND user_id = ? FOR UPDATE",
		channelId, userId); err != nil {
		return 0, err
	}
	if len(rows) > 0 {
		switch {
		case rows[0].State == channelperm.StateIn:
			return Member, nil
		case rows[0].kicked(date):
			return Banned, nil
		}
	}
	if _, err := tx.Exec(joinChannelQuery, channelId, userId, channelperm.RoleMember, channelperm.StateIn, date); err != nil {
		return 0, err
	}
	return Joined, nil
}

// joinChannelQuery writes the hidden row of a ghost joining. A row of a
// sender under the slow mode (msg recordSlowmode) keeps its last send.
const joinChannelQuery = "INSERT INTO channel_participants (channel_id, user_id, role, state, hidden, available_min_id, `date`) VALUES (?, ?, ?, ?, 1, 0, ?)" +
	" ON DUPLICATE KEY UPDATE role = VALUES(role), state = VALUES(state), hidden = 1, admin_rights = 0, `rank` = '', banned_rights = 0, until_date = 0," +
	" inviter_user_id = 0, promoted_by = 0, kicked_by = 0, available_min_id = 0, `date` = VALUES(`date`)"

// LeaveChannel takes the ghost userId out of the channel channelId, in tx
// with the channel locked: its hidden row is left, as nobody saw it come.
// It tells whether the ghost was in.
func LeaveChannel(tx *sqlx.Tx, channelId, userId int64) (bool, error) {
	r, err := tx.Exec("UPDATE channel_participants SET state = ?, admin_rights = 0, `rank` = '', banned_rights = 0, until_date = 0"+
		" WHERE channel_id = ? AND user_id = ? AND state = ? AND hidden = 1",
		channelperm.StateLeft, channelId, userId, channelperm.StateIn)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// Readable tells whether a ghost without a row may read the supergroup or
// channel channelId: a public one, or the discussion group of a broadcast
// channel the ghost may read (public, or one it is in). A deleted channel
// is none, and so is one the ghost is kicked from at now (a ban that ran out
// keeps nobody out, as channelperm reads it).
func Readable(q Querier, channelId, userId, now int64) (bool, error) {
	var ok []int64
	err := q.QueryRowsPartial(&ok, readableQuery, mtproto.PEER_CHANNEL, mtproto.PEER_CHANNEL, userId, channelId, userId,
		channelperm.StateKicked, math.MaxInt32, now)
	return len(ok) > 0, err
}

// readableQuery: the channel itself public, or a broadcast channel it
// discusses, linked both ways, public or with the ghost in it.
const readableQuery = "SELECT c.id FROM channels c WHERE (" +
	"EXISTS (SELECT 1 FROM username u WHERE u.peer_type = ? AND u.peer_id = c.id AND u.editable = 1)" +
	" OR c.megagroup = 1 AND EXISTS (SELECT 1 FROM channels b WHERE b.id = c.linked_chat_id AND b.broadcast = 1 AND b.deleted = 0 AND b.linked_chat_id = c.id" +
	" AND (EXISTS (SELECT 1 FROM username u WHERE u.peer_type = ? AND u.peer_id = b.id AND u.editable = 1)" +
	" OR EXISTS (SELECT 1 FROM channel_participants p WHERE p.channel_id = b.id AND p.user_id = ? AND p.state = 0))))" +
	" AND c.id = ? AND c.deleted = 0" +
	" AND NOT EXISTS (SELECT 1 FROM channel_participants k WHERE k.channel_id = c.id AND k.user_id = ? AND k.state = ?" +
	" AND (k.until_date = 0 OR k.until_date = ? OR k.until_date > ?))"

func positive(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		dup := false
		for _, o := range out {
			if o == id {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, id)
		}
	}
	return out
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return "?" + strings.Repeat(", ?", n-1)
}

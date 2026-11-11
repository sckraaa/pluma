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

//go:build integration

package ghost

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

const (
	alice   = int64(1)
	support = int64(2)
	other   = int64(3)

	privateGroup = int64(100)
	publicGroup  = int64(101)
	discussion   = int64(102)
	post         = int64(200) // the channel discussed in discussion
)

type fixture struct {
	t   *testing.T
	ctx context.Context
	db  *sqlx.DB
	box *sql.DB
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	m, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Terminate(ctx) })
	conf := m.Config()
	box, err := sql.Open("mysql", m.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = box.Close() })
	f := &fixture{t: t, ctx: ctx, db: sqlx.NewMySQL(&conf), box: box}
	f.exec("INSERT INTO users (id, access_hash, phone, country_code, first_name, ghost) VALUES (?, 1, '1', 'US', 'Alice', 0), (?, 1, '2', 'US', 'Support', 1), (?, 1, '3', 'US', 'Other', 0)",
		alice, support, other)
	f.channel(privateGroup, false, 0)
	f.channel(publicGroup, false, 0)
	f.channel(discussion, false, post)
	f.channel(post, true, discussion)
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES ('pg', ?, ?, 1, 1, 0)", mtproto.PEER_CHANNEL, publicGroup)
	return f
}

func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(f.ctx, query, args...); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
}

func (f *fixture) channel(id int64, broadcast bool, linked int64) {
	f.t.Helper()
	f.exec("INSERT INTO channels (id, access_hash, creator_user_id, title, broadcast, megagroup, linked_chat_id, participants_count, admins_count) VALUES (?, ?, ?, 'c', ?, ?, ?, 1, 1)",
		id, id*7, alice, broadcast, !broadcast, linked)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, role, state) VALUES (?, ?, ?, 0)", id, alice, channelperm.RoleCreator)
}

// run runs fn in a box transaction of support.
func (f *fixture) run(fn func(b *userbox.Box) error) {
	f.t.Helper()
	if _, err := userbox.Run(f.ctx, f.box, support, fn); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) join(channelId int64) Outcome {
	f.t.Helper()
	var out Outcome
	f.run(func(b *userbox.Box) error {
		var err error
		out, err = JoinChannel(b.Tx(), channelId, support, b.Date())
		return err
	})
	return out
}

type row struct {
	Role           int32 `db:"role"`
	State          int32 `db:"state"`
	Hidden         bool  `db:"hidden"`
	AvailableMinId int32 `db:"available_min_id"`
	BannedRights   int32 `db:"banned_rights"`
	LastSendDate   int64 `db:"last_send_date"`
}

func (f *fixture) row(channelId int64) row {
	f.t.Helper()
	var rows []row
	if err := f.db.QueryRowsPartial(f.ctx, &rows, "SELECT role, state, hidden, available_min_id, banned_rights, last_send_date FROM channel_participants WHERE channel_id = ? AND user_id = ?", channelId, support); err != nil {
		f.t.Fatal(err)
	}
	if len(rows) == 0 {
		f.t.Fatalf("no row of the ghost in %d", channelId)
	}
	return rows[0]
}

func (f *fixture) counts(channelId int64) (int32, int32) {
	f.t.Helper()
	var c []struct {
		P int32 `db:"participants_count"`
		A int32 `db:"admins_count"`
	}
	if err := f.db.QueryRowsPartial(f.ctx, &c, "SELECT participants_count, admins_count FROM channels WHERE id = ?", channelId); err != nil || len(c) == 0 {
		f.t.Fatalf("counts: %v", err)
	}
	return c[0].P, c[0].A
}

func TestGhosts(t *testing.T) {
	f := newFixture(t)
	q := OnDB(f.ctx, f.db)
	got, err := Ghosts(q, []int64{other, support, alice, support, 0})
	if err != nil || !slices.Equal(got, []int64{support}) {
		t.Errorf("Ghosts: %v, %v", got, err)
	}
	if g, err := IsGhost(q, alice); err != nil || g {
		t.Errorf("IsGhost(alice): %v, %v", g, err)
	}
	if got, err := Ghosts(q, nil); err != nil || len(got) != 0 {
		t.Errorf("Ghosts(nil): %v, %v", got, err)
	}
}

// TestJoinChannel: a hidden join writes a hidden plain row with the whole
// history and nothing else; again it finds the ghost in; a running ban
// keeps it out, one that ran out lets it in; a row that left comes back
// hidden, its last send kept.
func TestJoinChannel(t *testing.T) {
	f := newFixture(t)
	p, a := f.counts(privateGroup)
	if out := f.join(privateGroup); out != Joined {
		t.Fatalf("join: %v", out)
	}
	r := f.row(privateGroup)
	if r.State != channelperm.StateIn || !r.Hidden || r.Role != channelperm.RoleMember || r.AvailableMinId != 0 {
		t.Errorf("row %+v", r)
	}
	if p2, a2 := f.counts(privateGroup); p2 != p || a2 != a {
		t.Errorf("counts %d, %d, want %d, %d", p2, a2, p, a)
	}
	if out := f.join(privateGroup); out != Member {
		t.Errorf("again: %v, want Member", out)
	}

	f.exec("INSERT INTO channel_participants (channel_id, user_id, state, banned_rights, until_date) VALUES (?, ?, ?, ?, 0)",
		publicGroup, support, channelperm.StateKicked, mtproto.BANNED_VIEW_MESSAGES)
	if out := f.join(publicGroup); out != Banned {
		t.Errorf("banned for good: %v", out)
	}
	f.exec("UPDATE channel_participants SET until_date = 1 WHERE channel_id = ? AND user_id = ?", publicGroup, support)
	if out := f.join(publicGroup); out != Joined {
		t.Errorf("the ban ran out: %v", out)
	}
	if r := f.row(publicGroup); r.BannedRights != 0 || !r.Hidden {
		t.Errorf("row after the ban %+v", r)
	}

	f.exec("INSERT INTO channel_participants (channel_id, user_id, state, available_min_id, last_send_date) VALUES (?, ?, ?, 40, 77)",
		discussion, support, channelperm.StateLeft)
	if out := f.join(discussion); out != Joined {
		t.Errorf("after a leave: %v", out)
	}
	if r := f.row(discussion); r.AvailableMinId != 0 || r.LastSendDate != 77 || r.State != channelperm.StateIn {
		t.Errorf("row after a leave %+v", r)
	}

	var left bool
	f.run(func(b *userbox.Box) error {
		var err error
		left, err = LeaveChannel(b.Tx(), discussion, support)
		return err
	})
	if !left || f.row(discussion).State != channelperm.StateLeft {
		t.Errorf("leave: %v, row %+v", left, f.row(discussion))
	}
	f.run(func(b *userbox.Box) error {
		var err error
		left, err = LeaveChannel(b.Tx(), discussion, support)
		return err
	})
	if left {
		t.Error("left twice")
	}
}

// TestReadable: a public chat, the discussion of a public channel or of one
// the ghost is in; not a private chat, nor one it was kicked from.
func TestReadable(t *testing.T) {
	f := newFixture(t)
	q := OnDB(f.ctx, f.db)
	now := time.Now().Unix()
	check := func(what string, channelId int64, want bool) {
		t.Helper()
		got, err := Readable(q, channelId, support, now)
		if err != nil || got != want {
			t.Errorf("%s: %v, %v, want %v", what, got, err, want)
		}
	}
	check("public", publicGroup, true)
	check("private", privateGroup, false)
	check("discussion of a private channel", discussion, false)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, state, hidden) VALUES (?, ?, 0, 1)", post, support)
	check("discussion of a channel it is in", discussion, true)
	f.exec("DELETE FROM channel_participants WHERE channel_id = ? AND user_id = ?", post, support)
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES ('post', ?, ?, 1, 1, 0)", mtproto.PEER_CHANNEL, post)
	check("discussion of a public channel", discussion, true)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, state) VALUES (?, ?, ?)", publicGroup, support, channelperm.StateKicked)
	check("kicked", publicGroup, false)
	f.exec("UPDATE channel_participants SET until_date = ? WHERE channel_id = ? AND user_id = ?", now-1, publicGroup, support)
	check("kicked until a date gone by", publicGroup, true)
	f.exec("UPDATE channel_participants SET until_date = ? WHERE channel_id = ? AND user_id = ?", now+60, publicGroup, support)
	check("kicked until a date to come", publicGroup, false)
	f.exec("UPDATE channels SET deleted = 1 WHERE id = ?", discussion)
	check("deleted", discussion, false)
}

// basicGroup writes a basic group created by alice, with alice in it.
func (f *fixture) basicGroup(id int64) {
	f.t.Helper()
	f.exec("INSERT INTO chats (id, creator_user_id, access_hash, random_id, participant_count, title, version) VALUES (?, ?, 1, 0, 1, 'g', 3)", id, alice)
	f.exec("INSERT INTO chat_participants (chat_id, user_id, participant_type, state, date2) VALUES (?, ?, ?, 0, 1)", id, alice, mtproto.ChatMemberCreator)
}

func (f *fixture) joinChat(chatId int64, kickedStaysOut bool) Outcome {
	f.t.Helper()
	var out Outcome
	f.run(func(b *userbox.Box) error {
		var err error
		out, err = JoinChat(b.Tx(), chatId, support, b.Date(), kickedStaysOut)
		return err
	})
	return out
}

type chatMemberRow struct {
	State           int32  `db:"state"`
	Hidden          bool   `db:"hidden"`
	ParticipantType int32  `db:"participant_type"`
	Rank2           string `db:"rank2"`
}

func (f *fixture) chatRow(chatId int64) chatMemberRow {
	f.t.Helper()
	var rows []chatMemberRow
	if err := f.db.QueryRowsPartial(f.ctx, &rows, "SELECT state, hidden, participant_type, rank2 FROM chat_participants WHERE chat_id = ? AND user_id = ?", chatId, support); err != nil {
		f.t.Fatal(err)
	}
	if len(rows) == 0 {
		return chatMemberRow{State: -1}
	}
	return rows[0]
}

// TestJoinChat: the ghost comes into a basic group as a hidden plain member
// and the group's count and version stay; once in, again changes nothing; a
// ghost an admin removed stays out of a mention's join but not of the
// operator's; a moved or deleted group, or one the ghost created, takes
// nobody; LeaveChat takes it out once.
func TestJoinChat(t *testing.T) {
	f := newFixture(t)
	const group, moved, own = int64(300), int64(301), int64(302)
	f.basicGroup(group)
	f.basicGroup(moved)
	f.exec("UPDATE chats SET migrated_to_id = ? WHERE id = ?", privateGroup, moved)
	f.exec("INSERT INTO chats (id, creator_user_id, access_hash, random_id, participant_count, title) VALUES (?, ?, 1, 0, 0, 'o')", own, support)

	if out := f.joinChat(group, true); out != Joined {
		t.Fatalf("join: %v", out)
	}
	if r := f.chatRow(group); r.State != mtproto.ChatMemberStateNormal || !r.Hidden || r.ParticipantType != mtproto.ChatMemberNormal {
		t.Errorf("row %+v", r)
	}
	var counts []struct {
		N int32 `db:"participant_count"`
		V int32 `db:"version"`
	}
	if err := f.db.QueryRowsPartial(f.ctx, &counts, "SELECT participant_count, version FROM chats WHERE id = ?", group); err != nil || counts[0].N != 1 || counts[0].V != 3 {
		t.Errorf("count and version %+v, %v", counts, err)
	}
	if out := f.joinChat(group, true); out != Member {
		t.Errorf("again: %v", out)
	}

	f.exec("UPDATE chat_participants SET state = ?, participant_type = ?, rank2 = 'x' WHERE chat_id = ? AND user_id = ?", mtproto.ChatMemberStateKicked, mtproto.ChatMemberAdmin, group, support)
	if out := f.joinChat(group, true); out != Banned {
		t.Errorf("removed, by a mention: %v", out)
	}
	if out := f.joinChat(group, false); out != Joined {
		t.Errorf("removed, by the operator: %v", out)
	}
	if r := f.chatRow(group); !r.Hidden || r.ParticipantType != mtproto.ChatMemberNormal || r.Rank2 != "" {
		t.Errorf("back: %+v", r)
	}
	for _, id := range []int64{moved, own, 999} {
		if out := f.joinChat(id, true); out != Closed {
			t.Errorf("chat %d: %v", id, out)
		}
	}

	var left bool
	f.run(func(b *userbox.Box) error {
		var err error
		left, err = LeaveChat(b.Tx(), group, support, b.Date())
		return err
	})
	if !left || f.chatRow(group).State != mtproto.ChatMemberStateLeft {
		t.Errorf("leave: %v, %+v", left, f.chatRow(group))
	}
	f.run(func(b *userbox.Box) error {
		var err error
		left, err = LeaveChat(b.Tx(), group, support, b.Date())
		return err
	})
	if left {
		t.Error("left twice")
	}
}

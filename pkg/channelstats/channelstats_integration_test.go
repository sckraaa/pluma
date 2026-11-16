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

package channelstats

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

const statsMigration = "migrate-20261119-channel-stats.sql"

func openDB(t *testing.T, m *testinfra.MySQL) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", m.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// channelMessage stores m in channel_messages as msg does (jsonx of the
// message).
func channelMessage(t *testing.T, db *sql.DB, m *mtproto.Message, deleted bool) {
	t.Helper()
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_data, date2, deleted) VALUES (?, ?, 1, ?, ?, ?, ?)",
		m.GetPeerId().GetChannelId(), m.Id, m.Id, data, m.Date, deleted)
}

// TestMigration: the migration makes the tables and the indexes, records
// since when each kind of events is collected, fills the index of public
// forwards from the forwarded posts and the shared stories of channels
// already stored (not the automatic copies in discussion groups, nor deleted
// messages, nor forwards from users, nor stories of users) and
// runs again without changing anything.
func TestMigration(t *testing.T) {
	ctx := context.Background()
	files, err := testinfra.SchemaFilesBefore(statsMigration)
	if err != nil {
		t.Fatal(err)
	}
	m, err := testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles(files))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Terminate(ctx) })
	db := openDB(t, m)

	const source, group, other = int64(500), int64(600), int64(700)
	fwd := func(channelId int64, id, date int32, h *mtproto.MessageFwdHeader) *mtproto.Message {
		return mtproto.MakeTLMessage(&mtproto.Message{
			Id: id, PeerId: mtproto.MakePeerChannel(channelId), Date: date, Message: "text",
			FwdFrom: mtproto.MakeTLMessageFwdHeader(h).To_MessageFwdHeader(),
		}).To_Message()
	}
	// A post forwarded into another channel and into a group.
	channelMessage(t, db, fwd(other, 1, 1000, &mtproto.MessageFwdHeader{
		FromId: mtproto.MakePeerChannel(source), ChannelPost: mtproto.MakeFlagsInt32(7), Date: 900,
	}), false)
	channelMessage(t, db, fwd(group, 1, 1100, &mtproto.MessageFwdHeader{
		FromId: mtproto.MakePeerChannel(source), ChannelPost: mtproto.MakeFlagsInt32(8), Date: 900,
	}), false)
	// The automatic copy of post 9 in its discussion group (msg postCopy).
	channelMessage(t, db, fwd(group, 2, 1200, &mtproto.MessageFwdHeader{
		FromId: mtproto.MakePeerChannel(source), ChannelPost: mtproto.MakeFlagsInt32(9), Date: 1200,
		SavedFromPeer: mtproto.MakePeerChannel(source), SavedFromMsgId: mtproto.MakeFlagsInt32(9),
	}), false)
	// A deleted forward, a forward of a user's message, a plain message.
	channelMessage(t, db, fwd(other, 2, 1300, &mtproto.MessageFwdHeader{
		FromId: mtproto.MakePeerChannel(source), ChannelPost: mtproto.MakeFlagsInt32(10), Date: 900,
	}), true)
	channelMessage(t, db, fwd(other, 3, 1400, &mtproto.MessageFwdHeader{FromId: mtproto.MakePeerUser(42), Date: 900}), false)
	channelMessage(t, db, mtproto.MakeTLMessage(&mtproto.Message{Id: 4, PeerId: mtproto.MakePeerChannel(other), Date: 1500, Message: "x"}).To_Message(), false)
	// A story of the channel shared into another, and a story of a user.
	story := func(id, date int32, peer *mtproto.Peer, storyId int32) *mtproto.Message {
		return mtproto.MakeTLMessage(&mtproto.Message{
			Id: id, PeerId: mtproto.MakePeerChannel(other), Date: date,
			Media: mtproto.MakeTLMessageMediaStory(&mtproto.MessageMedia{Peer: peer, Id: storyId}).To_MessageMedia(),
		}).To_Message()
	}
	channelMessage(t, db, story(5, 1600, mtproto.MakePeerChannel(source), 3), false)
	channelMessage(t, db, story(6, 1700, mtproto.MakePeerUser(42), 4), false)

	path, err := testinfra.SchemaFile(statsMigration)
	if err != nil {
		t.Fatal(err)
	}
	type forward struct {
		Kind, PeerType int32
		PeerId         int64
		SrcId          int32
		DstChannel     int64
		DstMessage     int32
		Date           int64
	}
	want := []forward{
		{2, mtproto.PEER_CHANNEL, source, 3, other, 5, 1600},
		{1, mtproto.PEER_CHANNEL, source, 7, other, 1, 1000},
		{1, mtproto.PEER_CHANNEL, source, 8, group, 1, 1100},
	}
	for run := range 2 {
		if err = m.ApplySQLFile(ctx, path); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		rows, err := db.Query("SELECT src_kind, src_peer_type, src_peer_id, src_id, dst_channel_id, dst_message_id, `date` FROM channel_stats_public_forwards ORDER BY src_id")
		if err != nil {
			t.Fatal(err)
		}
		var got []forward
		for rows.Next() {
			var f forward
			if err = rows.Scan(&f.Kind, &f.PeerType, &f.PeerId, &f.SrcId, &f.DstChannel, &f.DstMessage, &f.Date); err != nil {
				t.Fatal(err)
			}
			got = append(got, f)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: public forwards %v, want %v", run, got, want)
		}

		var kinds int
		if err = db.QueryRow("SELECT COUNT(*) FROM stats_collection WHERE since > 0").Scan(&kinds); err != nil {
			t.Fatal(err)
		}
		if kinds != len(Kinds) {
			t.Fatalf("run %d: %d kinds collected, want %d", run, kinds, len(Kinds))
		}
		for _, k := range Kinds {
			if _, err = Since(ghostDB{db}, k); err != nil {
				t.Fatalf("run %d: %s: %v", run, k, err)
			}
		}
		if run == 0 {
			// The second run must keep the date of the first.
			mustExec(t, db, "UPDATE stats_collection SET since = 12345")
		}
	}
	var kept int
	if err = db.QueryRow("SELECT COUNT(*) FROM stats_collection WHERE since = 12345").Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != len(Kinds) {
		t.Fatalf("a second run changed since: %d of %d kept", kept, len(Kinds))
	}

	for _, ix := range []struct{ table, index string }{
		{"channel_stats_member_events", "channel_date"},
		{"channel_stats_mute_events", "channel_date"},
		{"channel_stats_mute_events", "channel_user"},
		{"channel_stats_public_forwards", "src_date"},
		{"channel_message_views", "channel_date"},
		{"channel_messages", "channel_date"},
		{"channel_message_reactions", "channel_date"},
		{"channel_message_forwards", "channel_date"},
		{"story_views", "peer_date"},
		{"stories", "fwd_source"},
	} {
		var n int
		if err = db.QueryRow("SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?",
			ix.table, ix.index).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Errorf("no index %s on %s", ix.index, ix.table)
		}
	}
	var source0 int
	if err = db.QueryRow("SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'channel_message_views' AND COLUMN_NAME = 'source' AND COLUMN_DEFAULT = '0'").Scan(&source0); err != nil {
		t.Fatal(err)
	}
	if source0 != 1 {
		t.Error("channel_message_views.source missing")
	}
}

// ghostDB is a Querier of a plain database.
type ghostDB struct{ db *sql.DB }

func (q ghostDB) QueryRowsPartial(v any, query string, args ...any) error {
	out := v.(*[]int64)
	rows, err := q.db.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var n int64
		if err = rows.Scan(&n); err != nil {
			return err
		}
		*out = append(*out, n)
	}
	return rows.Err()
}

// TestEventsWritten: every writer's statement fits its table on the whole
// schema, in a box transaction as the writers run them: the counters of the
// admins add up, a reader counts once a day, a forward once a copy, and a
// ghost writes nothing.
func TestEventsWritten(t *testing.T) {
	ctx := context.Background()
	m, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Terminate(ctx) })
	db := openDB(t, m)
	const alice, bob, ghostId, channelId = int64(1), int64(2), int64(3), int64(900)
	mustExec(t, db, "INSERT INTO users (id, access_hash, phone, country_code, first_name, ghost) VALUES (?, 1, '1', 'US', 'A', 0), (?, 1, '2', 'US', 'B', 0), (?, 1, '3', 'US', 'G', 1)",
		alice, bob, ghostId)

	const day = int64(20000 * 86400)
	run := func(fn func(tx *sqlx.Tx) error) {
		t.Helper()
		if _, err := userbox.Run(ctx, db, alice, func(b *userbox.Box) error { return fn(b.Tx()) }); err != nil {
			t.Fatal(err)
		}
	}
	run(func(tx *sqlx.Tx) error {
		for _, step := range []func() error{
			func() error { return MemberJoined(tx, channelId, bob, day+10, JoinLink, alice) },
			func() error { return MembersJoined(tx, channelId, []int64{bob, ghostId}, day+20, JoinAdded, alice) },
			func() error { return MemberLeft(tx, channelId, bob, day+30) },
			func() error { return MemberLeft(tx, channelId, ghostId, day+30) },
			func() error { return MuteChanged(tx, channelId, bob, day+40, MuteForever) },
			func() error { return MuteChanged(tx, channelId, ghostId, day+40, 0) },
			func() error { return Reader(tx, channelId, bob, day+50) },
			func() error { return Reader(tx, channelId, bob, day+60) },
			func() error { return Reader(tx, channelId, bob, day+86400) },
			func() error { return Reader(tx, channelId, ghostId, day+60) },
			func() error { return AdminAction(tx, channelId, alice, day+70, Deleted, 3) },
			func() error { return AdminAction(tx, channelId, alice, day+80, Deleted, 2) },
			func() error { return AdminAction(tx, channelId, alice, day+90, Kicked, 1) },
			func() error { return AdminAction(tx, channelId, alice, day+95, Banned, 1) },
			func() error {
				return PublicForward(tx, ForwardPost, mtproto.PEER_CHANNEL, channelId, 5, 901, 10, day+100)
			},
			func() error {
				return PublicForward(tx, ForwardPost, mtproto.PEER_CHANNEL, channelId, 5, 901, 10, day+200)
			},
			func() error {
				return PublicForward(tx, ForwardStory, mtproto.PEER_CHANNEL, channelId, 2, 902, 11, day+300)
			},
		} {
			if err := step(); err != nil {
				return err
			}
		}
		return nil
	})

	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	if n := count("SELECT COUNT(*) FROM channel_stats_member_events WHERE channel_id = ? AND user_id = ?", channelId, ghostId); n != 0 {
		t.Fatalf("ghost member events: %d", n)
	}
	if n := count("SELECT COUNT(*) FROM channel_stats_member_events WHERE channel_id = ? AND user_id = ? AND joined = 1 AND source = 1 AND inviter_id = ? AND `date` = ?", channelId, bob, alice, day+10); n != 1 {
		t.Fatalf("join by link: %d", n)
	}
	if n := count("SELECT COUNT(*) FROM channel_stats_member_events WHERE channel_id = ? AND user_id = ? AND joined = 1 AND source = 5", channelId, bob); n != 1 {
		t.Fatalf("join by being added: %d", n)
	}
	if n := count("SELECT COUNT(*) FROM channel_stats_member_events WHERE channel_id = ? AND user_id = ? AND joined = 0 AND source = 0 AND inviter_id = 0", channelId, bob); n != 1 {
		t.Fatalf("leave: %d", n)
	}
	if n := count("SELECT COUNT(*) FROM channel_stats_mute_events WHERE channel_id = ?", channelId); n != 1 {
		t.Fatalf("mute events: %d, want 1 (the ghost's is not written)", n)
	}
	if n := count("SELECT COUNT(*) FROM channel_stats_mute_events WHERE channel_id = ? AND user_id = ? AND mute_until = ?", channelId, bob, MuteForever); n != 1 {
		t.Fatalf("mute for good: %d", n)
	}
	if n := count("SELECT COUNT(*) FROM channel_stats_readers WHERE channel_id = ? AND user_id = ?", channelId, bob); n != 2 {
		t.Fatalf("reader days: %d, want 2", n)
	}
	if n := count("SELECT COUNT(*) FROM channel_stats_readers WHERE channel_id = ? AND user_id = ? AND `day` = 20000", channelId, bob); n != 1 {
		t.Fatalf("reader of day 20000: %d", n)
	}
	if n := count("SELECT COUNT(*) FROM channel_stats_readers WHERE user_id = ?", ghostId); n != 0 {
		t.Fatalf("ghost reader: %d", n)
	}
	var deleted, kicked, banned int
	if err := db.QueryRow("SELECT deleted, kicked, banned FROM channel_stats_admin_actions WHERE channel_id = ? AND `day` = 20000 AND admin_id = ?", channelId, alice).
		Scan(&deleted, &kicked, &banned); err != nil {
		t.Fatal(err)
	}
	if deleted != 5 || kicked != 1 || banned != 1 {
		t.Fatalf("admin actions %d/%d/%d, want 5/1/1", deleted, kicked, banned)
	}
	if n := count("SELECT COUNT(*) FROM channel_stats_public_forwards WHERE src_kind = 1 AND src_peer_id = ? AND src_id = 5 AND `date` = ?", channelId, day+100); n != 1 {
		t.Fatalf("post forward: %d (a copy is recorded once, at its first date)", n)
	}
	if n := count("SELECT COUNT(*) FROM channel_stats_public_forwards WHERE src_kind = 2 AND src_id = 2 AND dst_channel_id = 902"); n != 1 {
		t.Fatalf("story forward: %d", n)
	}
}

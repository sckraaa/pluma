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

package sendas

import (
	"context"
	"errors"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

const (
	alice = int64(1)
	bob   = int64(2)
	carol = int64(3)

	privateGroup = int64(100)
	publicGroup  = int64(101)
	discussion   = int64(102)
	post         = int64(200) // Alice's channel, discussed in discussion
	other        = int64(201) // Alice's second channel
	bobs         = int64(202) // Bob's channel
	gone         = int64(203) // Alice's deleted channel
)

func newStore(ctx context.Context, t *testing.T) (*Store, *sqlx.DB) {
	t.Helper()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	conn := sqlx.NewMySQL(&conf)
	return New(conn), conn
}

func exec(ctx context.Context, t *testing.T, db *sqlx.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(ctx, query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func channel(ctx context.Context, t *testing.T, db *sqlx.DB, id int64, broadcast bool, linked int64, deleted bool) {
	t.Helper()
	exec(ctx, t, db, "INSERT INTO channels (id, access_hash, creator_user_id, title, broadcast, megagroup, linked_chat_id, deleted) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, id*7, alice, "c", broadcast, !broadcast, linked, deleted)
}

func participant(ctx context.Context, t *testing.T, db *sqlx.DB, channelId, userId int64, role, state, adminRights int32) {
	t.Helper()
	exec(ctx, t, db, "INSERT INTO channel_participants (channel_id, user_id, role, state, admin_rights) VALUES (?, ?, ?, ?, ?)",
		channelId, userId, role, state, adminRights)
}

func ids(peers []*mtproto.Peer) []int64 {
	out := []int64{}
	for _, p := range peers {
		if id := p.GetUserId(); id != 0 {
			out = append(out, id)
		} else {
			out = append(out, -p.GetChannelId())
		}
	}
	return out
}

func equal(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Who may write as whom, read from the tables (migrate-20261103-channels.sql,
// migrate-20261111-channel-send-as.sql), and the default each keeps.
func TestPeersAndDefault(t *testing.T) {
	ctx := context.Background()
	s, db := newStore(ctx, t)

	channel(ctx, t, db, privateGroup, false, 0, false)
	channel(ctx, t, db, publicGroup, false, 0, false)
	exec(ctx, t, db, "INSERT INTO username (id, username, peer_type, peer_id) VALUES (1, 'publicgroup', ?, ?)", mtproto.PEER_CHANNEL, publicGroup)
	channel(ctx, t, db, discussion, false, post, false)
	channel(ctx, t, db, post, true, discussion, false)
	channel(ctx, t, db, other, true, 0, false)
	channel(ctx, t, db, bobs, true, 0, false)
	channel(ctx, t, db, gone, true, 0, true)
	for _, id := range []int64{post, other, gone} {
		participant(ctx, t, db, id, alice, channelperm.RoleCreator, channelperm.StateIn, channelperm.CreatorRights)
	}
	participant(ctx, t, db, bobs, bob, channelperm.RoleCreator, channelperm.StateIn, channelperm.CreatorRights)
	// Carol reads Alice's channel and comments as a guest of its group.
	participant(ctx, t, db, post, carol, channelperm.RoleMember, channelperm.StateIn, 0)

	participant(ctx, t, db, privateGroup, alice, channelperm.RoleCreator, channelperm.StateIn, channelperm.CreatorRights|mtproto.ADMIN_ANONYMOUS)
	participant(ctx, t, db, privateGroup, bob, channelperm.RoleMember, channelperm.StateIn, 0)
	participant(ctx, t, db, publicGroup, bob, channelperm.RoleMember, channelperm.StateIn, 0)
	participant(ctx, t, db, publicGroup, carol, channelperm.RoleMember, channelperm.StateKicked, 0)

	for _, tc := range []struct {
		name          string
		user, channel int64
		want          []int64
		err           error
	}{
		{"anonymous creator of a private group", alice, privateGroup, []int64{-privateGroup, -post, -other}, nil},
		{"member of a private group", bob, privateGroup, []int64{bob}, nil},
		{"member of a public group", bob, publicGroup, []int64{bob, -bobs}, nil},
		{"guest of a discussion group", carol, discussion, []int64{carol}, nil},
		{"broadcast channel", alice, post, []int64{-post}, nil},
		{"banned", carol, publicGroup, nil, mtproto.ErrChannelPrivate},
		{"not in it", carol, privateGroup, nil, mtproto.ErrChannelPrivate},
		{"no such channel", alice, 999, nil, mtproto.ErrChannelInvalid},
	} {
		got, err := s.Peers(ctx, tc.user, tc.channel)
		if tc.err != nil {
			if !errors.Is(err, tc.err) {
				t.Errorf("%s: %v, want %v", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil || !equal(ids(got), tc.want) {
			t.Errorf("%s: %v, %v; want %v", tc.name, ids(got), err, tc.want)
		}
	}

	if err := s.Check(ctx, bob, publicGroup, mtproto.MakePeerChannel(post)); !errors.Is(err, mtproto.ErrSendAsPeerInvalid) {
		t.Errorf("Bob as Alice's channel: %v", err)
	}
	if err := s.SaveDefault(ctx, bob, publicGroup, mtproto.MakePeerChannel(bobs)); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Default(ctx, bob, publicGroup); err != nil || p.GetChannelId() != bobs {
		t.Fatalf("default %v, %v; want Bob's channel", p, err)
	}
	// The group stops being public: Bob may no longer write as his
	// channel, and the default goes with it.
	exec(ctx, t, db, "DELETE FROM username WHERE peer_id = ?", publicGroup)
	if p, err := s.Default(ctx, bob, publicGroup); err != nil || p != nil {
		t.Errorf("default %v, %v after the group became private; want none", p, err)
	}
	if err := s.SaveDefault(ctx, bob, publicGroup, mtproto.MakePeerUser(bob)); err != nil {
		t.Fatal(err)
	}
	var rows []int64
	if err := db.QueryRowsPartial(ctx, &rows, "SELECT send_as_id FROM channel_default_send_as WHERE user_id = ?", bob); err != nil || len(rows) != 0 {
		t.Errorf("choosing himself kept %v, %v", rows, err)
	}
}

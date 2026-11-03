//go:build integration

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
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// itDB is MySQL with the whole schema.
var itDB *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	code := 1
	if itDB, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
		_ = itDB.Terminate(ctx)
	}
	os.Exit(code)
}

// itSeq gives each test its own channels and users, so the tests run in
// any order and again (-count=N) on the same database.
var itSeq atomic.Int64

func itId() int64 { return 30_000_000 + itSeq.Add(1) }

// photos serves the photos of ids, or fails with err.
type photos struct {
	ids map[int64]bool
	err error
}

func (p *photos) MediaGetPhoto(_ context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error) {
	if p.err != nil {
		return nil, p.err
	}
	if !p.ids[in.PhotoId] {
		return mtproto.MakeTLPhotoEmpty(nil).To_Photo(), nil
	}
	return mtproto.MakeTLPhoto(&mtproto.Photo{
		Id:    in.PhotoId,
		Sizes: []*mtproto.PhotoSize{mtproto.MakeTLPhotoSize(&mtproto.PhotoSize{Type: "a", W: 160, H: 160}).To_PhotoSize()},
	}).To_Photo(), nil
}

type fixture struct {
	t      *testing.T
	box    *sql.DB
	d      *Dao
	photos *photos
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	// A pool of its own: userbox.Open shares go-zero's pool of the DSN,
	// which closing would close for the tests after this one.
	box, err := sql.Open("mysql", itDB.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = box.Close() })
	conf := itDB.Config()
	p := &photos{ids: map[int64]bool{}}
	return &fixture{t: t, box: box, d: NewDao(sqlx.NewMySQL(&conf), p), photos: p}
}

// create makes a channel of creator in its box transaction, as the writers
// do (pkg/channelbox), with the creator's participant row.
func (f *fixture) create(creator int64, megagroup bool) int64 {
	f.t.Helper()
	id := itId()
	_, err := userbox.Run(context.Background(), f.box, creator, func(b *userbox.Box) error {
		if _, err := channelbox.Create(b, channelbox.NewChannel{
			Id: id, AccessHash: id * 11, CreatorUserId: creator, Title: fmt.Sprintf("channel %d", id),
			About: "about", Broadcast: !megagroup, Megagroup: megagroup, Date: b.Date(),
		}); err != nil {
			return err
		}
		_, err := b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, ?)",
			id, creator, channel.RoleCreator, mtproto.ADMIN_CHANGE_INFO|mtproto.ADMIN_ADD_ADMINS, b.Date())
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.box.Exec(query, args...); err != nil {
		f.t.Fatal(err)
	}
}

func TestGetMutableChannel(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	creator, kicked, never := itId(), itId(), itId()
	id := f.create(creator, true)
	until := int32(time.Now().Add(time.Hour).Unix())
	f.exec("INSERT INTO channel_participants (channel_id, user_id, state, banned_rights, until_date, kicked_by, `rank`, `date`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, kicked, channel.StateBanned, mtproto.BANNED_VIEW_MESSAGES|mtproto.BANNED_SEND_MESSAGES, until, creator, "r", 42)
	photoId := itId()
	f.photos.ids[photoId] = true
	f.exec("UPDATE channels SET photo_id = ?, participants_count = 1, kicked_count = 1, slowmode_seconds = 10, default_banned_rights = ? WHERE id = ?",
		photoId, mtproto.BANNED_SEND_POLLS, id)
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)",
		fmt.Sprintf("ch%d", id), mtproto.PEER_CHANNEL, id)

	// The rows of the users asked for that have one, each once.
	mc, err := f.d.GetMutableChannel(ctx, id, []int64{creator, kicked, never, creator, 0})
	if err != nil {
		t.Fatal(err)
	}
	c := mc.GetChannel()
	if c.GetId() != id || c.GetAccessHash() != id*11 || c.GetCreatorUserId() != creator || c.GetTitle() != fmt.Sprintf("channel %d", id) ||
		c.GetAbout() != "about" || !c.GetMegagroup() || c.GetBroadcast() || c.GetPts() != channelbox.BasePts ||
		c.GetParticipantsCount() != 1 || c.GetKickedCount() != 1 || c.GetSlowmodeSeconds() != 10 ||
		c.GetDefaultBannedRights() != mtproto.BANNED_SEND_POLLS || c.GetDate() == 0 || c.GetDeleted() {
		t.Errorf("channel %v", c)
	}
	if c.GetUsername() != fmt.Sprintf("ch%d", id) {
		t.Errorf("username %q", c.GetUsername())
	}
	if c.GetPhoto().GetId() != photoId {
		t.Errorf("photo %v, want %d", c.GetPhoto(), photoId)
	}
	if len(mc.GetParticipants()) != 2 {
		t.Fatalf("participants %v", mc.GetParticipants())
	}
	p, ok := mc.GetParticipant(creator)
	if !ok || p.GetRole() != channel.RoleCreator || p.GetState() != channel.StateIn || p.GetAdminRights() != mtproto.ADMIN_CHANGE_INFO|mtproto.ADMIN_ADD_ADMINS {
		t.Errorf("creator %v", p)
	}
	p, ok = mc.GetParticipant(kicked)
	if !ok || p.GetState() != channel.StateBanned || p.GetUntilDate() != until || p.GetKickedBy() != creator || p.GetRank() != "r" || p.GetDate() != 42 {
		t.Errorf("kicked %v", p)
	}

	// The objects of the viewers, from the rows read.
	now := int32(time.Now().Unix())
	if got := mc.ToChat(creator, now); got.GetPredicateName() != mtproto.Predicate_channel || !got.GetCreator() || got.GetUsername().GetValue() == "" {
		t.Errorf("creator's view %v", got)
	}
	if got := mc.ToChat(kicked, now); got.GetPredicateName() != mtproto.Predicate_channelForbidden || got.GetUntilDate().GetValue() != until {
		t.Errorf("kicked's view %v", got)
	}
	if got := mc.ToChat(never, now); got.GetPredicateName() != mtproto.Predicate_channel || !got.GetLeft() {
		t.Errorf("view of one who never joined the public channel %v", got)
	}

	// No users asked for: no rows.
	if mc, err = f.d.GetMutableChannel(ctx, id, nil); err != nil || len(mc.GetParticipants()) != 0 {
		t.Errorf("no users: %v, %v", mc, err)
	}
	// No such channel.
	if _, err = f.d.GetMutableChannel(ctx, itId(), []int64{creator}); !errors.Is(err, mtproto.ErrChannelInvalid) {
		t.Errorf("unknown channel: %v, want CHANNEL_INVALID", err)
	}
	// A photo that cannot be loaded fails the read, never a channel without
	// its photo.
	f.photos.err = errors.New("media unavailable")
	if _, err = f.d.GetMutableChannel(ctx, id, nil); !errors.Is(err, f.photos.err) {
		t.Errorf("photo failed: %v", err)
	}
}

func TestGetChannelListByIdList(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	creator, member := itId(), itId()
	group := f.create(creator, true)
	broadcast := f.create(creator, false)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, `date`) VALUES (?, ?, ?)", group, member, 7)
	f.exec("UPDATE channels SET deleted = 1 WHERE id = ?", broadcast)

	// The channels that exist in the order asked, each once, with the
	// viewer's row where it has one.
	missing := itId()
	list, err := f.d.GetChannelListByIdList(ctx, member, []int64{broadcast, missing, group, broadcast, 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].GetChannel().GetId() != broadcast || list[1].GetChannel().GetId() != group {
		t.Fatalf("channels %v", list)
	}
	if b := list[0]; !b.GetChannel().GetBroadcast() || !b.GetChannel().GetDeleted() || len(b.GetParticipants()) != 0 {
		t.Errorf("broadcast %v", b)
	}
	if g := list[1]; len(g.GetParticipants()) != 1 || g.GetParticipants()[0].GetUserId() != member || g.GetParticipants()[0].GetDate() != 7 {
		t.Errorf("group %v", g)
	}

	// As the resolver gives them to the member: the deleted channel is
	// forbidden, the group is the member's.
	chats := (&channel.Vector_MutableChannel{Datas: list}).GetChannelListByIdList(member)
	if chats[0].GetPredicateName() != mtproto.Predicate_channelForbidden ||
		chats[1].GetPredicateName() != mtproto.Predicate_channel || chats[1].GetLeft() || chats[1].GetDate() != 7 {
		t.Errorf("views %v", chats)
	}

	// Nothing asked, nothing found.
	if list, err = f.d.GetChannelListByIdList(ctx, member, nil); err != nil || len(list) != 0 {
		t.Errorf("no ids: %v, %v", list, err)
	}
	if list, err = f.d.GetChannelListByIdList(ctx, member, []int64{missing}); err != nil || len(list) != 0 {
		t.Errorf("unknown ids: %v, %v", list, err)
	}
}

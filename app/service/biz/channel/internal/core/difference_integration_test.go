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

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/config"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

var (
	itDB  *testinfra.MySQL
	itSeq atomic.Int64
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	code := 1
	if err = outbox.Register(outbox.Topics{Push: []string{itSyncTopic}, Inbox: []string{itInboxTopic}}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if itDB, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		code = m.Run()
		_ = itDB.Terminate(ctx)
	}
	os.Exit(code)
}

// The topics of the events the tests' transactions write.
const (
	itSyncTopic  = "Sync-T"
	itInboxTopic = "Inbox-T"
)

func itId() int64 { return 50_000_000 + itSeq.Add(1) }

type diffFixture struct {
	t   *testing.T
	box *sql.DB
	c   *ChannelCore
}

func newDiffFixture(t *testing.T) *diffFixture {
	t.Helper()
	// A pool of its own: closing go-zero's shared pool of the DSN would
	// close it for the tests after this one.
	box, err := sql.Open("mysql", itDB.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = box.Close() })
	conf := itDB.Config()
	d := dao.NewDao(sqlx.NewMySQL(&conf), nil)
	d.BoxDB = box
	d.Topics = config.Topics{Sync: itSyncTopic, Inbox: itInboxTopic}
	return &diffFixture{t: t, box: box, c: New(context.Background(), &svc.ServiceContext{Dao: d})}
}

func (f *diffFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.box.Exec(query, args...); err != nil {
		f.t.Fatal(err)
	}
}

// create makes a supergroup of creator, with its dialog, at pts BasePts.
func (f *diffFixture) create(creator int64) int64 {
	f.t.Helper()
	id := itId()
	_, err := userbox.Run(context.Background(), f.box, creator, func(b *userbox.Box) error {
		if _, err := channelbox.Create(b, channelbox.NewChannel{
			Id: id, AccessHash: id * 3, CreatorUserId: creator, Title: "t", Megagroup: true, Date: b.Date(),
		}); err != nil {
			return err
		}
		if _, err := b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, `date`) VALUES (?, ?, ?, ?)",
			id, creator, channel.RoleCreator, b.Date()); err != nil {
			return err
		}
		_, err := b.Tx().Exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, draft_message_data, date2) VALUES (?, ?, ?, ?, 'null', ?)",
			creator, mtproto.PEER_CHANNEL, id, mtproto.MakePeerDialogId(mtproto.PEER_CHANNEL, id), b.Date())
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

// send writes n messages of from to the channel, one pts each, with
// random ids, as msg does; it returns the channel's pts.
func (f *diffFixture) send(channelId, from int64, n int) int32 {
	f.t.Helper()
	var pts int32
	_, err := userbox.Run(context.Background(), f.box, from, func(b *userbox.Box) error {
		ch, err := channelbox.Open(b, channelId)
		if err != nil {
			return err
		}
		for range n {
			m := mtproto.MakeTLMessage(&mtproto.Message{
				Id:      ch.NextMessageId(),
				FromId:  mtproto.MakePeerUser(from),
				PeerId:  mtproto.MakePeerChannel(channelId),
				Date:    int32(b.Date()),
				Message: "text",
			}).To_Message()
			ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
			data, err := jsonx.MarshalToString(m)
			if err != nil {
				return err
			}
			if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, random_id, pts, message_data, date2) VALUES (?, ?, ?, ?, ?, ?, ?)",
				channelId, m.Id, from, 1000+int64(m.Id), ch.Pts(), data, b.Date()); err != nil {
				return err
			}
			if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", m.Id, channelId); err != nil {
				return err
			}
		}
		pts = ch.Pts()
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return pts
}

// journal lists the pts of the channel's journal rows.
func (f *diffFixture) journal(channelId int64) []int32 {
	f.t.Helper()
	r, err := f.box.Query("SELECT pts FROM channel_pts_updates WHERE channel_id = ? ORDER BY pts", channelId)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var out []int32
	for r.Next() {
		var p int32
		if err = r.Scan(&p); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, p)
	}
	if err = r.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *diffFixture) diff(self, channelId int64, pts, limit int32) *mtproto.Updates_ChannelDifference {
	f.t.Helper()
	d, err := f.c.ChannelGetChannelDifference(&channel.TLChannelGetChannelDifference{SelfId: self, ChannelId: channelId, Pts: pts, Limit: limit})
	if err != nil {
		f.t.Fatal(err)
	}
	return d
}

// TestDifferencePages: limit 2 (iOS in DEBUG) walks the journal in slices
// that always move pts forward, without timeout, to a final answer with
// one; limit 100 takes it at once; updateMessageID comes for the viewer's
// own messages; every final answer has a timeout above zero.
func TestDifferencePages(t *testing.T) {
	f := newDiffFixture(t)
	creator, member := itId(), itId()
	id := f.create(creator)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, `date`) VALUES (?, ?, 0)", id, member)
	s := f.send(id, creator, 5)
	if s != channelbox.BasePts+5 {
		t.Fatalf("pts %d", s)
	}

	var got []int32
	pts := int32(channelbox.BasePts)
	for page := 0; ; page++ {
		if page > 5 {
			t.Fatal("no final answer")
		}
		d := f.diff(member, id, pts, 2)
		if d.GetPredicateName() != mtproto.Predicate_updates_channelDifference {
			t.Fatalf("page %d: %s", page, d.GetPredicateName())
		}
		if d.GetPts() <= pts {
			t.Fatalf("page %d: pts %d does not move from %d", page, d.GetPts(), pts)
		}
		pts = d.GetPts()
		got = append(got, ids(d.GetNewMessages())...)
		if !d.GetFinal() {
			if d.GetTimeout() != nil {
				t.Errorf("page %d: a slice with timeout %v", page, d.GetTimeout())
			}
			continue
		}
		if d.GetTimeout().GetValue() != memberTimeout || pts != s {
			t.Errorf("final page: timeout %v, pts %d (want %d)", d.GetTimeout(), pts, s)
		}
		break
	}
	if !slices.Equal(got, []int32{1, 2, 3, 4, 5}) {
		t.Errorf("messages %v", got)
	}

	d := f.diff(creator, id, channelbox.BasePts, 100)
	if !d.GetFinal() || d.GetPts() != s || len(d.GetNewMessages()) != 5 {
		t.Fatalf("limit 100: final %v pts %d, %d messages", d.GetFinal(), d.GetPts(), len(d.GetNewMessages()))
	}
	other := d.GetOtherUpdates()
	if len(other) != 5 || other[0].GetPredicateName() != mtproto.Predicate_updateMessageID || other[0].GetRandomId() != 1001 {
		t.Errorf("the creator's message ids: %v", names(other))
	}
	if !d.GetNewMessages()[0].GetOut() {
		t.Error("the creator's own message is not out")
	}

	for _, pts := range []int32{s, s + 5} {
		e := f.diff(member, id, pts, 100)
		if e.GetPredicateName() != mtproto.Predicate_updates_channelDifferenceEmpty || e.GetPts() != s || !e.GetFinal() || e.GetTimeout().GetValue() != memberTimeout {
			t.Errorf("from %d: %s pts %d final %v timeout %v", pts, e.GetPredicateName(), e.GetPts(), e.GetFinal(), e.GetTimeout())
		}
	}
}

// TestDifferenceTooLong: past the journal's window, the client gets the
// dialog with the channel's pts and the last messages; within it, the
// rows as usual.
func TestDifferenceTooLong(t *testing.T) {
	defer func(w int32) { channelbox.JournalWindow = w }(channelbox.JournalWindow)
	channelbox.JournalWindow = 3
	f := newDiffFixture(t)
	creator := itId()
	id := f.create(creator)
	s := f.send(id, creator, 6)

	rows := f.journal(id)
	if !slices.Equal(rows, []int32{s - 2, s - 1, s}) {
		t.Fatalf("journal after the trim %v, want the last 3 of pts %d", rows, s)
	}

	d := f.diff(creator, id, channelbox.BasePts, 2)
	if d.GetPredicateName() != mtproto.Predicate_updates_channelDifferenceTooLong {
		t.Fatalf("%s", d.GetPredicateName())
	}
	if d.GetDialog().GetPts().GetValue() != s || d.GetDialog().GetTopMessage() != 6 || !d.GetFinal() || d.GetTimeout().GetValue() != memberTimeout {
		t.Errorf("dialog %v, final %v, timeout %v", d.GetDialog(), d.GetFinal(), d.GetTimeout())
	}
	if got := ids(d.GetMessages()); !slices.Equal(got, []int32{6, 5}) {
		t.Errorf("messages %v, want the last 2", got)
	}

	if d = f.diff(creator, id, s-3, 100); d.GetPredicateName() != mtproto.Predicate_updates_channelDifference || len(d.GetNewMessages()) != 3 {
		t.Errorf("within the window: %s, %d messages", d.GetPredicateName(), len(d.GetNewMessages()))
	}
	if d = f.diff(creator, id, s-4, 100); d.GetPredicateName() != mtproto.Predicate_updates_channelDifferenceTooLong {
		t.Errorf("just past the window: %s", d.GetPredicateName())
	}
}

// post writes a post to the channel as msg does, with the counters a new
// post has, and returns its id and pts.
func (f *diffFixture) post(channelId, from int64) (id, pts int32) {
	f.t.Helper()
	_, err := userbox.Run(context.Background(), f.box, from, func(b *userbox.Box) error {
		ch, err := channelbox.Open(b, channelId)
		if err != nil {
			return err
		}
		m := f.postMessage(ch.NextMessageId(), channelId, int32(b.Date()))
		ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
		data, err := jsonx.MarshalToString(m)
		if err != nil {
			return err
		}
		id, pts = m.Id, ch.Pts()
		_, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_data, date2) VALUES (?, ?, ?, ?, ?, ?)",
			channelId, m.Id, from, pts, data, b.Date())
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id, pts
}

func (f *diffFixture) postMessage(id int32, channelId int64, date int32) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{
		Id: id, Post: true, PeerId: mtproto.MakePeerChannel(channelId), Date: date, Message: "post",
		Views: wrapperspb.Int32(1), Forwards: wrapperspb.Int32(0),
	}).To_Message()
}

// editPost journals an edit of the post with the counters it had then.
func (f *diffFixture) editPost(channelId, from int64, id int32) {
	f.t.Helper()
	_, err := userbox.Run(context.Background(), f.box, from, func(b *userbox.Box) error {
		ch, err := channelbox.Open(b, channelId)
		if err != nil {
			return err
		}
		ch.AddPtsUpdate(mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: f.postMessage(id, channelId, int32(b.Date()))}).To_Update(), 1)
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// TestDifferencePostCounters: the journal holds a post as it was; the
// difference gives the new posts and the edits with the counters of now.
func TestDifferencePostCounters(t *testing.T) {
	f := newDiffFixture(t)
	creator := itId()
	id := f.create(creator)
	post, pts := f.post(id, creator)
	f.editPost(id, creator, post)
	f.exec("UPDATE channel_messages SET views = 7, forwards = 2 WHERE channel_id = ? AND id = ?", id, post)

	check := func(what string, m *mtproto.Message) {
		t.Helper()
		if m.GetViews().GetValue() != 7 || m.GetForwards().GetValue() != 2 {
			t.Errorf("%s: views %v forwards %v, want 7 and 2", what, m.GetViews(), m.GetForwards())
		}
	}
	d := f.diff(creator, id, channelbox.BasePts, 100)
	if len(d.GetNewMessages()) != 1 {
		t.Fatalf("new messages %v", ids(d.GetNewMessages()))
	}
	check("the new post", d.GetNewMessages()[0])

	edits := 0
	for _, u := range f.diff(creator, id, pts, 100).GetOtherUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateEditChannelMessage {
			edits++
			check("the edit", u.GetMessage_MESSAGE())
		}
	}
	if edits != 1 {
		t.Errorf("%d edits, want 1", edits)
	}
}

// TestDifferenceAccess: a private channel is CHANNEL_PRIVATE to one not in
// it; a public one answers with the shorter timeout of a non-member.
func TestDifferenceAccess(t *testing.T) {
	f := newDiffFixture(t)
	creator, outsider := itId(), itId()
	id := f.create(creator)
	s := f.send(id, creator, 1)
	_, err := f.c.ChannelGetChannelDifference(&channel.TLChannelGetChannelDifference{SelfId: outsider, ChannelId: id, Pts: channelbox.BasePts, Limit: 10})
	if !errors.Is(err, mtproto.ErrChannelPrivate) {
		t.Fatalf("private channel: %v", err)
	}
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)",
		fmt.Sprintf("diff%d", id), mtproto.PEER_CHANNEL, id)
	d := f.diff(outsider, id, channelbox.BasePts, 10)
	if len(d.GetNewMessages()) != 1 || d.GetTimeout().GetValue() != viewerTimeout || d.GetPts() != s {
		t.Errorf("public channel: %d messages, timeout %v, pts %d", len(d.GetNewMessages()), d.GetTimeout(), d.GetPts())
	}
	if d.GetNewMessages()[0].GetOut() {
		t.Error("another's message is out")
	}
}

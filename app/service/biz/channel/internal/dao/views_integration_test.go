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
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	sync_ "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/config"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/jsonx"
)

const (
	viewsSyncTopic  = "views-it-sync"
	viewsInboxTopic = "views-it-inbox"
)

var registerViewsTopics sync.Once

// viewsFixture is a fixture able to run box transactions and write events,
// with a window long enough that a test sees only the first push.
func viewsFixture(t *testing.T) *fixture {
	t.Helper()
	registerViewsTopics.Do(func() {
		if err := outbox.Register(outbox.Topics{Push: []string{viewsSyncTopic}, Inbox: []string{viewsInboxTopic}}); err != nil {
			t.Fatal(err)
		}
	})
	f := newFixture(t)
	f.d.BoxDB = f.box
	f.d.Topics = config.Topics{Sync: viewsSyncTopic, Inbox: viewsInboxTopic}
	return f
}

// post adds post id of author to the channel, as msg stores it.
func (f *fixture) post(channelId, author int64, id int32) {
	f.t.Helper()
	data, err := jsonx.MarshalToString(mtproto.MakeTLMessage(&mtproto.Message{
		Id: id, Post: true, PeerId: mtproto.MakePeerChannel(channelId), Message: "post",
	}).To_Message())
	if err != nil {
		f.t.Fatal(err)
	}
	f.exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_data, date2) VALUES (?, ?, ?, ?, ?, 0)",
		channelId, id, author, id, data)
}

func (f *fixture) counters(channelId int64, id int32) (views, forwards int32) {
	f.t.Helper()
	if err := f.box.QueryRow("SELECT views, forwards FROM channel_messages WHERE channel_id = ? AND id = ?", channelId, id).Scan(&views, &forwards); err != nil {
		f.t.Fatal(err)
	}
	return views, forwards
}

func (f *fixture) join(channelId, userId int64) {
	f.exec("INSERT INTO channel_participants (channel_id, user_id, `date`) VALUES (?, ?, 0)", channelId, userId)
}

// TestViewsOncePerUser: many views of many users at once count each user
// once; the author's view and the views of a user asking again do not.
func TestViewsOncePerUser(t *testing.T) {
	ctx := context.Background()
	f := viewsFixture(t)
	author := itId()
	id := f.create(author, false)
	f.post(id, author, 1)
	f.post(id, author, 2)

	const viewers, repeats = 8, 6
	users := make([]int64, viewers)
	for i := range users {
		users[i] = itId()
		f.join(id, users[i])
	}
	var wg sync.WaitGroup
	errs := make(chan error, viewers*repeats+repeats)
	for _, u := range users {
		for range repeats {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- f.d.IncrementViews(ctx, u, id, []int32{2, 1, 1})
			}()
		}
	}
	for range repeats {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- f.d.IncrementViews(ctx, author, id, []int32{1, 2})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []int32{1, 2} {
		if v, fw := f.counters(id, m); v != viewers+1 || fw != 0 {
			t.Errorf("post %d: views %d forwards %d, want %d and 0", m, v, fw, viewers+1)
		}
	}
	var rows int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_message_views WHERE channel_id = ? AND user_id = ?", id, author).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("the author has %d rows", rows)
	}

	if err := f.d.IncrementViews(ctx, users[0], id, []int32{1}); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.counters(id, 1); v != viewers+1 {
		t.Errorf("a repeated view: %d", v)
	}

	got, err := f.d.Messages(ctx, users[0], id, []int32{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].GetViews().GetValue() != viewers+1 || got[0].GetForwards() == nil {
		t.Fatalf("the post as read: %v", got)
	}
}

// TestViewsOnlyPostsTheyMaySee: a user who may not read the channel is
// refused; deleted posts, service messages and supergroups count nothing.
func TestViewsOnlyPostsTheyMaySee(t *testing.T) {
	ctx := context.Background()
	f := viewsFixture(t)
	author, stranger, member := itId(), itId(), itId()
	id := f.create(author, false)
	f.join(id, member)
	f.post(id, author, 1)
	f.post(id, author, 2)
	f.exec("UPDATE channel_messages SET deleted = 1 WHERE channel_id = ? AND id = 2", id)

	if err := f.d.IncrementViews(ctx, stranger, id, []int32{1}); !errors.Is(err, mtproto.ErrChannelPrivate) {
		t.Errorf("one out of the channel: %v, want CHANNEL_PRIVATE", err)
	}
	if err := f.d.IncrementViews(ctx, member, id, []int32{1, 2, 99}); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.counters(id, 1); v != 2 {
		t.Errorf("the post: views %d", v)
	}
	if v, _ := f.counters(id, 2); v != 1 {
		t.Errorf("the deleted post: views %d", v)
	}

	group := f.create(author, true)
	f.join(group, member)
	f.post(group, author, 1)
	if err := f.d.IncrementViews(ctx, member, group, []int32{1}); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.counters(group, 1); v != 1 {
		t.Errorf("a message of a supergroup: views %d", v)
	}
}

// TestForwardsOncePerRandomId: a repeated forward counts once, another
// forward by the same user counts again.
func TestForwardsOncePerRandomId(t *testing.T) {
	ctx := context.Background()
	f := viewsFixture(t)
	author, member := itId(), itId()
	id := f.create(author, false)
	f.join(id, member)
	f.post(id, author, 1)

	for range 3 {
		if err := f.d.AddForwards(ctx, member, id, []int32{1}, []int64{111}); err != nil {
			t.Fatal(err)
		}
	}
	if _, fw := f.counters(id, 1); fw != 1 {
		t.Errorf("a forward asked for three times: %d", fw)
	}
	if err := f.d.AddForwards(ctx, member, id, []int32{1}, []int64{112}); err != nil {
		t.Fatal(err)
	}
	if _, fw := f.counters(id, 1); fw != 2 {
		t.Errorf("a second forward: %d", fw)
	}
	if err := f.d.AddForwards(ctx, member, id, []int32{1, 1}, []int64{1}); !errors.Is(err, mtproto.ErrInputRequestInvalid) {
		t.Errorf("ids and random ids of different lengths: %v", err)
	}
}

// TestPushCountersEvent: the push is one event to the channel with the
// counters of that moment, without pts.
func TestPushCountersEvent(t *testing.T) {
	ctx := context.Background()
	f := viewsFixture(t)
	author, member := itId(), itId()
	id := f.create(author, false)
	f.join(id, member)
	f.post(id, author, 1)
	f.exec("UPDATE channel_messages SET views = 7, forwards = 3 WHERE channel_id = ? AND id = 1", id)

	if err := f.d.pushCounters(ctx, id, member, []int32{1}, []int32{1}); err != nil {
		t.Fatal(err)
	}
	row := f.box.QueryRow("SELECT payload FROM mq_outbox WHERE topic = ? AND recipient_id = ? ORDER BY id DESC LIMIT 1", viewsSyncTopic, id)
	var data []byte
	if err := row.Scan(&data); err != nil {
		t.Fatal(err)
	}
	in := new(sync_.TLSyncPushChannelUpdates)
	if err := jsonx.Unmarshal(data, in); err != nil {
		t.Fatal(err)
	}
	updates := in.GetUpdates().GetUpdates()
	if len(updates) != 2 {
		t.Fatalf("updates %v", updates)
	}
	if u := updates[0]; u.GetPredicateName() != mtproto.Predicate_updateChannelMessageViews || u.GetChannelId() != id || u.GetId_INT32() != 1 || u.GetViews() != 7 {
		t.Errorf("views %v", u)
	}
	if u := updates[1]; u.GetPredicateName() != mtproto.Predicate_updateChannelMessageForwards || u.GetForwards() != 3 {
		t.Errorf("forwards %v", u)
	}
	if updates[0].GetPts_INT32() != 0 {
		t.Errorf("the update has a pts: %v", updates[0])
	}
}

const viewsMigration = "migrate-20261108-channel-views.sql"

// TestViewsMigration: on a database with posts the counters start at 1 and
// 0, and running the migration again changes nothing.
func TestViewsMigration(t *testing.T) {
	ctx := context.Background()
	files, err := testinfra.SchemaFilesBefore(viewsMigration)
	if err != nil {
		t.Fatal(err)
	}
	old, err := testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles(files))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Terminate(ctx) })
	db, err := sql.Open("mysql", old.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_data, date2) VALUES (5, 1, 2, 1, '{}', 0)"); err != nil {
		t.Fatal(err)
	}
	path, err := testinfra.SchemaFile(viewsMigration)
	if err != nil {
		t.Fatal(err)
	}
	for run := range 2 {
		if err = old.ApplySQLFile(ctx, path); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		var v, fw int32
		if err = db.QueryRow("SELECT views, forwards FROM channel_messages WHERE channel_id = 5 AND id = 1").Scan(&v, &fw); err != nil {
			t.Fatal(err)
		}
		if v != 1 || fw != 0 {
			t.Errorf("run %d: views %d forwards %d, want 1 and 0", run, v, fw)
		}
		if _, err = db.Exec("INSERT IGNORE INTO channel_message_views (channel_id, message_id, user_id, `date`) VALUES (5, 1, 3, 0)"); err != nil {
			t.Errorf("run %d: channel_message_views: %v", run, err)
		}
		if _, err = db.Exec("INSERT IGNORE INTO channel_message_forwards (channel_id, message_id, user_id, random_id, `date`) VALUES (5, 1, 3, 9, 0)"); err != nil {
			t.Errorf("run %d: channel_message_forwards: %v", run, err)
		}
	}
}

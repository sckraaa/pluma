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

package dao_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	msg_helper "github.com/teamgram/teamgram-server/app/messenger/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// The automatic replies of Telegram Business through the inbox and the
// worker of msg (task 105): the inbox queues a reply in the owner's box
// transaction that takes the message, the worker sends the shortcut from
// the owner, marked offline, and takes the row off the queue.

// businessUsers is what the automatic replies ask the user service beyond
// the fakes of the world: contacts and the end of the online window.
var businessUsers = struct {
	sync.Mutex
	contacts  map[[2]int64]bool // {user, contact}
	seenUntil map[int64]int64
}{contacts: map[[2]int64]bool{}, seenUntil: map[int64]int64{}}

func (f *fakeUsers) UserCheckContact(_ context.Context, in *userpb.TLUserCheckContact) (*mtproto.Bool, error) {
	businessUsers.Lock()
	defer businessUsers.Unlock()
	return mtproto.ToBool(businessUsers.contacts[[2]int64{in.UserId, in.Id}]), nil
}

func (f *fakeUsers) UserGetLastSeens(_ context.Context, in *userpb.TLUserGetLastSeens) (*userpb.Vector_LastSeenData, error) {
	businessUsers.Lock()
	defer businessUsers.Unlock()
	out := &userpb.Vector_LastSeenData{}
	for _, id := range in.Id {
		if until, ok := businessUsers.seenUntil[id]; ok {
			out.Datas = append(out.Datas, userpb.MakeTLLastSeenData(&userpb.LastSeenData{UserId: id, LastSeenAt: until}).To_LastSeenData())
		}
	}
	return out, nil
}

func (f *fakeUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	return f.mark([]*mtproto.ImmutableUser{immutableUser(in.Id)})[0], nil
}

func setSeenUntil(userId, until int64) {
	businessUsers.Lock()
	defer businessUsers.Unlock()
	businessUsers.seenUntil[userId] = until
}

// startAutoReplyWorkers runs n workers of the automatic replies to the end
// of the test.
func (w *world) startAutoReplyWorkers(n int) {
	w.t.Helper()
	for range n {
		r := msg_helper.NewAutoReplyWorker(w.d, nil)
		r.Start()
		w.t.Cleanup(r.Stop)
	}
}

// hello makes the shortcut of owner with texts and returns its id.
func (w *world) hello(owner int64, name string, texts ...string) int32 {
	w.t.Helper()
	a := w.mustAddQuickReplies(owner, byName(name), texts...)
	if a.created == nil {
		w.t.Fatalf("shortcut %q of %d not created", name, owner)
	}
	return a.created.GetShortcutId()
}

// setAutoReply turns a on, as account.updateBusiness*Message does.
func (w *world) setAutoReply(a *business.AutoReply) {
	w.t.Helper()
	if err := w.d.Mysql.DB.Transact(context.Background(), func(tx *sqlx.Tx) error {
		return business.SetAutoReplyTx(tx, a, int32(time.Now().Unix()))
	}); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) autoRepliesQueued(owner int64) int {
	w.t.Helper()
	var n int
	if err := itCheck.QueryRow("SELECT COUNT(*) FROM business_auto_replies WHERE user_id = ?", owner).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// received are the messages of text from sender in the box of userId.
func (w *world) received(userId, sender int64, text string) []messageRow {
	w.t.Helper()
	var out []messageRow
	for _, m := range w.messages(userId) {
		if m.Sender == sender && m.Text == text && !m.Deleted {
			out = append(out, m)
		}
	}
	return out
}

// waitAutoReplies waits until the queue of owner is empty and the replies
// it sent are delivered.
func (w *world) waitAutoReplies(owner int64) {
	w.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for w.autoRepliesQueued(owner) > 0 {
		if time.Now().After(deadline) {
			w.t.Fatalf("%d automatic replies of %d still queued", w.autoRepliesQueued(owner), owner)
		}
		time.Sleep(100 * time.Millisecond)
	}
	w.pub.drain()
}

func offlineOf(t *testing.T, m messageRow) bool {
	t.Helper()
	var data struct {
		Offline bool `json:"offline"`
	}
	if err := json.Unmarshal([]byte(m.Data), &data); err != nil {
		t.Fatal(err)
	}
	return data.Offline
}

// A new chat gets the greeting once, from the owner, marked offline, on
// both sides; the next message of the chat gets none, and a message that
// is itself an automatic reply triggers none of the other side's.
func TestAutoReplyGreeting(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	owner, peer := newUser(), newUser()
	shortcut := w.hello(owner, "hello", "Hello! We answer within a day.")
	w.setAutoReply(&business.AutoReply{UserID: owner, Kind: business.KindGreeting, ShortcutID: shortcut, NoActivityDays: 7,
		Recipients: business.Recipients{ExcludeSelected: true}})
	// The peer greets back whoever greets it: its greeting must not
	// answer the owner's.
	peerShortcut := w.hello(peer, "hello", "Hi from the peer.")
	w.setAutoReply(&business.AutoReply{UserID: peer, Kind: business.KindGreeting, ShortcutID: peerShortcut, NoActivityDays: 7,
		Recipients: business.Recipients{ExcludeSelected: true}})
	w.startAutoReplyWorkers(1)

	w.send(peer, mtproto.PEER_USER, owner, "is anyone there?")
	w.pub.drain()
	w.waitAutoReplies(owner)

	got := w.received(peer, owner, "Hello! We answer within a day.")
	if len(got) != 1 {
		t.Fatalf("the peer got %d greetings, want 1", len(got))
	}
	if !offlineOf(t, got[0]) {
		t.Errorf("the greeting the peer got is not offline: %s", got[0].Data)
	}
	if mine := w.received(owner, owner, "Hello! We answer within a day."); len(mine) != 1 || !offlineOf(t, mine[0]) {
		t.Errorf("the owner's copies of the greeting: %+v", mine)
	}
	if n := len(w.received(owner, peer, "Hi from the peer.")); n != 0 {
		t.Errorf("the greeting of the owner triggered the peer's: %d", n)
	}

	w.send(peer, mtproto.PEER_USER, owner, "hello?")
	w.pub.drain()
	time.Sleep(2 * time.Second)
	w.waitAutoReplies(owner)
	if n := len(w.received(peer, owner, "Hello! We answer within a day.")); n != 1 {
		t.Errorf("after the second message the peer has %d greetings, want 1", n)
	}
	w.checkBox(owner)
	w.checkBox(peer)
}

// Three workers on one queue: every reply goes exactly once.
func TestAutoReplySentOnce(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	owner := newUser()
	shortcut := w.hello(owner, "hello", "one", "two")
	w.setAutoReply(&business.AutoReply{UserID: owner, Kind: business.KindGreeting, ShortcutID: shortcut, NoActivityDays: 7,
		Recipients: business.Recipients{NewChats: true}})
	peers := make([]int64, 12)
	for i := range peers {
		peers[i] = newUser()
		w.send(peers[i], mtproto.PEER_USER, owner, "hi")
	}
	w.pub.drain()
	if n := w.autoRepliesQueued(owner); n != len(peers) {
		t.Fatalf("%d replies queued, want %d", n, len(peers))
	}
	w.startAutoReplyWorkers(3)
	w.waitAutoReplies(owner)
	for _, p := range peers {
		if a, b := len(w.received(p, owner, "one")), len(w.received(p, owner, "two")); a != 1 || b != 1 {
			t.Errorf("peer %d got the greeting %d and %d times", p, a, b)
		}
	}
}

// A reply that can never go is dropped unsent: the peer blocked the owner,
// or the owner turned the greeting off meanwhile.
func TestAutoReplyDropped(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	owner, blocker, late := newUser(), newUser(), newUser()
	shortcut := w.hello(owner, "hello", "greetings")
	w.setAutoReply(&business.AutoReply{UserID: owner, Kind: business.KindGreeting, ShortcutID: shortcut, NoActivityDays: 7,
		Recipients: business.Recipients{ExcludeSelected: true}})
	w.users.mu.Lock()
	w.users.blocked[[2]int64{blocker, owner}] = true
	w.users.mu.Unlock()
	w.send(blocker, mtproto.PEER_USER, owner, "hi")
	w.pub.drain()
	if n := w.autoRepliesQueued(owner); n != 1 {
		t.Fatalf("%d replies queued, want 1", n)
	}
	w.startAutoReplyWorkers(1)
	w.waitAutoReplies(owner)
	if n := len(w.received(owner, owner, "greetings")) + len(w.received(blocker, owner, "greetings")); n != 0 {
		t.Errorf("a greeting went to a user who blocked the owner: %d", n)
	}

	// Turned off before the worker came: the queued row goes with it.
	if err := w.d.Mysql.DB.Transact(context.Background(), func(tx *sqlx.Tx) error {
		return business.DeleteAutoReplyTx(tx, owner, business.KindGreeting)
	}); err != nil {
		t.Fatal(err)
	}
	w.send(late, mtproto.PEER_USER, owner, "hi")
	w.pub.drain()
	time.Sleep(2 * time.Second)
	if n := len(w.received(late, owner, "greetings")); n != 0 || w.autoRepliesQueued(owner) != 0 {
		t.Errorf("a greeting turned off: sent %d, queued %d", n, w.autoRepliesQueued(owner))
	}
}

// The away message with offline_only: none while the owner is online, one
// when it has been away for ten minutes, none again for the next message.
func TestAutoReplyAway(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	owner, peer := newUser(), newUser()
	shortcut := w.hello(owner, "away", "We are away.")
	w.setAutoReply(&business.AutoReply{UserID: owner, Kind: business.KindAway, ShortcutID: shortcut, Schedule: business.AwayAlways,
		OfflineOnly: true, Recipients: business.Recipients{ExcludeSelected: true}})
	w.startAutoReplyWorkers(1)

	setSeenUntil(owner, time.Now().Unix()+60)
	w.send(peer, mtproto.PEER_USER, owner, "online?")
	w.pub.drain()
	if n := w.autoRepliesQueued(owner); n != 0 {
		t.Fatalf("an away message queued while the owner is online: %d", n)
	}

	setSeenUntil(owner, time.Now().Add(-11*time.Minute).Unix())
	w.send(peer, mtproto.PEER_USER, owner, "away?")
	w.pub.drain()
	w.waitAutoReplies(owner)
	if n := len(w.received(peer, owner, "We are away.")); n != 1 {
		t.Fatalf("the peer got %d away messages, want 1", n)
	}

	w.send(peer, mtproto.PEER_USER, owner, "still away?")
	w.pub.drain()
	if n := w.autoRepliesQueued(owner); n != 0 {
		t.Errorf("a second away message queued within a day: %d", n)
	}
}

// Deleting a shortcut turns off the automatic replies that send it, and
// every device of the owner hears of it (updateUser with seq).
func TestAutoReplyShortcutDeleted(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	owner := newUser()
	shortcut := w.hello(owner, "hello", "hi")
	w.setAutoReply(&business.AutoReply{UserID: owner, Kind: business.KindGreeting, ShortcutID: shortcut, NoActivityDays: 7,
		Recipients: business.Recipients{ExcludeSelected: true}})
	before := w.state(owner).Seq
	if _, err := w.rpc.MsgDeleteQuickReplyShortcut(context.Background(), &msg.TLMsgDeleteQuickReplyShortcut{
		UserId: owner, AuthKeyId: owner * 10, ShortcutId: shortcut,
	}); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()
	list, err := w.d.Business().AutoReplies(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("settings left on a deleted shortcut: %+v", list)
	}
	// updateDeleteQuickReply and updateUser.
	if after := w.state(owner).Seq; after != before+2 {
		t.Errorf("seq %d after the delete, want %d", after, before+2)
	}
}

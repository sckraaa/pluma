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
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

func byName(name string) *mtproto.InputQuickReplyShortcut {
	return mtproto.MakeTLInputQuickReplyShortcut(&mtproto.InputQuickReplyShortcut{Shortcut: name}).To_InputQuickReplyShortcut()
}

func byShortcutId(id int32) *mtproto.InputQuickReplyShortcut {
	return mtproto.MakeTLInputQuickReplyShortcutId(&mtproto.InputQuickReplyShortcut{ShortcutId: id}).To_InputQuickReplyShortcut()
}

// addQuickReplies adds the texts to a shortcut of userId in one request.
func (w *world) addQuickReplies(userId int64, shortcut *mtproto.InputQuickReplyShortcut, texts ...string) (*mtproto.Updates, []*msg.OutboxMessage, error) {
	w.t.Helper()
	var list []*msg.OutboxMessage
	for _, s := range texts {
		list = append(list, outbox(textMessage(userId, mtproto.MakePeerUser(userId), s)))
	}
	r, err := w.rpc.MsgAddQuickReplyMessages(context.Background(), &msg.TLMsgAddQuickReplyMessages{
		UserId: userId, AuthKeyId: userId * 10, Shortcut: shortcut, Message: list,
	})
	return r, list, err
}

// quickReplyAnswer is what an add answered: the shortcut created, if any,
// and the messages, with the random ids they were matched to.
type quickReplyAnswer struct {
	created  *mtproto.QuickReply
	messages []*mtproto.Message
	randomOf map[int32]int64
}

func readQuickReplyAnswer(t *testing.T, r *mtproto.Updates) quickReplyAnswer {
	t.Helper()
	a := quickReplyAnswer{randomOf: map[int32]int64{}}
	for i, u := range r.GetUpdates() {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateNewQuickReply:
			if i != 0 {
				t.Errorf("updateNewQuickReply at %d, want first", i)
			}
			a.created = u.GetQuickReply()
		case mtproto.Predicate_updateMessageID:
			a.randomOf[u.GetId_INT32()] = u.GetRandomId()
		case mtproto.Predicate_updateQuickReplyMessage:
			a.messages = append(a.messages, u.GetMessage_MESSAGE())
		default:
			t.Errorf("unexpected %s in the answer", u.GetPredicateName())
		}
	}
	return a
}

func (w *world) mustAddQuickReplies(userId int64, shortcut *mtproto.InputQuickReplyShortcut, texts ...string) quickReplyAnswer {
	w.t.Helper()
	r, list, err := w.addQuickReplies(userId, shortcut, texts...)
	if err != nil {
		w.t.Fatalf("add %v to a shortcut of %d: %v", texts, userId, err)
	}
	a := readQuickReplyAnswer(w.t, r)
	if len(a.messages) != len(texts) {
		w.t.Fatalf("add %v: %d messages answered", texts, len(a.messages))
	}
	for i, m := range a.messages {
		if a.randomOf[m.Id] != list[i].RandomId {
			w.t.Errorf("message %d: random id %d, want %d", m.Id, a.randomOf[m.Id], list[i].RandomId)
		}
	}
	return a
}

func (w *world) quickReplies(userId int64, hash int64) *mtproto.Messages_QuickReplies {
	w.t.Helper()
	r, err := w.rpc.MsgGetQuickReplies(context.Background(), &msg.TLMsgGetQuickReplies{UserId: userId, Hash: hash})
	if err != nil {
		w.t.Fatal(err)
	}
	return r
}

func shortcutNames(r *mtproto.Messages_QuickReplies) []string {
	var out []string
	for _, q := range r.GetQuickReplies() {
		out = append(out, fmt.Sprintf("%s:%d", q.Shortcut, q.Count))
	}
	return out
}

// seqKinds are the kinds of userId's seq journal rows from the n-th on.
func (w *world) seqKinds(userId int64, from int) []string {
	w.t.Helper()
	var out []string
	for i, r := range w.seqJournal(userId) {
		if i >= from {
			out = append(out, r.kind)
		}
	}
	return out
}

// A send by name creates the shortcut, the same name in another case adds
// to it, and so does its id; the clients read it back with the hashes they
// count, and the other devices get the same updates with seq.
func TestQuickRepliesAdd(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a := newUser()
	other := newUser()

	first := w.mustAddQuickReplies(a, byName("Hello"), "hi", "how are you")
	if first.created == nil || first.created.Shortcut != "Hello" || first.created.Count != 2 || first.created.TopMessage != first.messages[0].Id {
		t.Fatalf("created %v, messages %v", first.created, first.messages)
	}
	id := first.created.ShortcutId
	for _, m := range first.messages {
		if m.GetQuickReplyShortcutId().GetValue() != id || !m.Out || m.GetPeerId().GetUserId() != a || m.SavedPeerId != nil {
			t.Errorf("message %v", m)
		}
	}
	second := w.mustAddQuickReplies(a, byName("hELLO"), "third")
	third := w.mustAddQuickReplies(a, byShortcutId(id), "fourth")
	if second.created != nil || third.created != nil {
		t.Errorf("a known shortcut created again: %v, %v", second.created, third.created)
	}
	if second.messages[0].GetQuickReplyShortcutId().GetValue() != id || third.messages[0].GetQuickReplyShortcutId().GetValue() != id {
		t.Errorf("added to another shortcut: %v, %v", second.messages[0], third.messages[0])
	}
	if _, _, err := w.addQuickReplies(a, byShortcutId(id+1000), "x"); !errors.Is(err, mtproto.ErrShortcutInvalid) {
		t.Errorf("an unknown id: %v", err)
	}
	if _, _, err := w.addQuickReplies(other, byShortcutId(id), "x"); !errors.Is(err, mtproto.ErrShortcutInvalid) {
		t.Errorf("another account's shortcut: %v", err)
	}
	if _, _, err := w.addQuickReplies(a, byName("hello world"), "x"); !errors.Is(err, mtproto.ErrShortcutInvalid) {
		t.Errorf("an invalid name: %v", err)
	}

	list := w.quickReplies(a, 0)
	if got := shortcutNames(list); !slices.Equal(got, []string{"Hello:4"}) {
		t.Fatalf("shortcuts %v", got)
	}
	if top := list.GetQuickReplies()[0].TopMessage; top != first.messages[0].Id || len(list.GetMessages()) != 1 || list.GetMessages()[0].Id != top {
		t.Errorf("top %d, messages %v", top, list.GetMessages())
	}
	rows := []*dao.QuickReplyRow{}
	for _, m := range list.GetMessages() {
		rows = append(rows, &dao.QuickReplyRow{Id: m.Id})
	}
	hash := dao.QuickRepliesHash([]*dao.QuickReply{{Id: id, Shortcut: "Hello", Top: rows[0]}})
	if r := w.quickReplies(a, hash); r.GetPredicateName() != mtproto.Predicate_messages_quickRepliesNotModified {
		t.Errorf("same hash: %s", r.GetPredicateName())
	}
	if r := w.quickReplies(other, 0); len(r.GetQuickReplies()) != 0 {
		t.Errorf("another account sees %v", shortcutNames(r))
	}

	msgs, err := w.rpc.MsgGetQuickReplyMessages(context.Background(), &msg.TLMsgGetQuickReplyMessages{UserId: a, ShortcutId: id})
	if err != nil {
		t.Fatal(err)
	}
	var ids, texts []string
	all := []*dao.QuickReplyRow{}
	for _, m := range msgs.GetMessages() {
		ids = append(ids, fmt.Sprint(m.Id))
		texts = append(texts, m.Message)
		all = append([]*dao.QuickReplyRow{{Id: m.Id}}, all...)
	}
	if !slices.Equal(texts, []string{"fourth", "third", "how are you", "hi"}) {
		t.Errorf("messages %v, want newest first", texts)
	}
	notModified, err := w.rpc.MsgGetQuickReplyMessages(context.Background(), &msg.TLMsgGetQuickReplyMessages{UserId: a, ShortcutId: id, Hash: dao.QuickReplyMessagesHash(all)})
	if err != nil || notModified.GetPredicateName() != mtproto.Predicate_messages_messagesNotModified || notModified.GetCount() != 4 {
		t.Errorf("same hash: %v %v", notModified, err)
	}
	some, err := w.rpc.MsgGetQuickReplyMessages(context.Background(), &msg.TLMsgGetQuickReplyMessages{UserId: a, ShortcutId: id, Id: []int32{first.messages[1].Id, 999999}})
	if err != nil || len(some.GetMessages()) != 1 || some.GetMessages()[0].Id != first.messages[1].Id {
		t.Errorf("by id: %v %v", some.GetMessages(), err)
	}
	if _, err = w.rpc.MsgGetQuickReplyMessages(context.Background(), &msg.TLMsgGetQuickReplyMessages{UserId: other, ShortcutId: id}); !errors.Is(err, mtproto.ErrShortcutInvalid) {
		t.Errorf("another account's shortcut: %v (ids %v)", err, ids)
	}

	want := []string{mtproto.Predicate_updateNewQuickReply}
	for range 4 {
		want = append(want, mtproto.Predicate_updateQuickReplyMessage)
	}
	if got := w.seqKinds(a, 0); !slices.Equal(got, want) {
		t.Errorf("seq journal %v, want %v", got, want)
	}
	w.pub.drain()
	for _, p := range w.pub.pushesTo(a) {
		if !p.notMe || p.updates.GetSeq() == 0 {
			t.Errorf("push %v: not me %v, seq %d", p.updates.GetUpdates(), p.notMe, p.updates.GetSeq())
		}
	}
}

// A send repeated after a lost answer gets the first answer, the shortcut
// it created included; a partial repeat is RANDOM_ID_DUPLICATE.
func TestQuickRepliesRepeat(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a := newUser()
	var list []*msg.OutboxMessage
	for _, s := range []string{"one", "two"} {
		list = append(list, outbox(textMessage(a, mtproto.MakePeerUser(a), s)))
	}
	add := func(l []*msg.OutboxMessage) (*mtproto.Updates, error) {
		return w.rpc.MsgAddQuickReplyMessages(context.Background(), &msg.TLMsgAddQuickReplyMessages{
			UserId: a, AuthKeyId: a * 10, Shortcut: byName("repeat"), Message: l,
		})
	}
	r1, err := add(list)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := add(list)
	if err != nil {
		t.Fatal(err)
	}
	a1, a2 := readQuickReplyAnswer(t, r1), readQuickReplyAnswer(t, r2)
	if a2.created == nil || a2.created.ShortcutId != a1.created.ShortcutId {
		t.Errorf("repeat: created %v, first %v", a2.created, a1.created)
	}
	for i := range a1.messages {
		if a1.messages[i].Id != a2.messages[i].Id {
			t.Errorf("repeat: message %d is %d, was %d", i, a2.messages[i].Id, a1.messages[i].Id)
		}
	}
	if got := shortcutNames(w.quickReplies(a, 0)); !slices.Equal(got, []string{"repeat:2"}) {
		t.Errorf("shortcuts %v", got)
	}
	if got := len(w.seqJournal(a)); got != 3 {
		t.Errorf("%d seq rows, want 3", got)
	}
	partial := []*msg.OutboxMessage{list[0], outbox(textMessage(a, mtproto.MakePeerUser(a), "three"))}
	if _, err = add(partial); !errors.Is(err, mtproto.ErrRandomIdDuplicate) {
		t.Errorf("partial repeat: %v", err)
	}
	// A later add to the same shortcut repeated does not say it created it.
	more := []*msg.OutboxMessage{outbox(textMessage(a, mtproto.MakePeerUser(a), "four"))}
	if _, err = add(more); err != nil {
		t.Fatal(err)
	}
	r4, err := add(more)
	if err != nil {
		t.Fatal(err)
	}
	if a4 := readQuickReplyAnswer(t, r4); a4.created != nil {
		t.Errorf("a repeated add to a known shortcut created it: %v", a4.created)
	}
}

// Sends racing to create one shortcut make one; sends racing past the
// message limit stop at quick_reply_messages_limit.
func TestQuickRepliesRace(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a := newUser()
	const n = business.QuickReplyMessagesLimit + 5
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		ok, full int
		others   []error
	)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := w.addQuickReplies(a, byName("race"), fmt.Sprint("m", i))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, mtproto.ErrReplyMessagesTooMuch):
				full++
			default:
				others = append(others, err)
			}
		}()
	}
	wg.Wait()
	if ok != business.QuickReplyMessagesLimit || full != n-ok || len(others) > 0 {
		t.Fatalf("%d added, %d refused as too many, other errors %v", ok, full, others)
	}
	if got := shortcutNames(w.quickReplies(a, 0)); !slices.Equal(got, []string{fmt.Sprint("race:", business.QuickReplyMessagesLimit)}) {
		t.Errorf("shortcuts %v", got)
	}
	created := 0
	for _, k := range w.seqKinds(a, 0) {
		if k == mtproto.Predicate_updateNewQuickReply {
			created++
		}
	}
	if created != 1 {
		t.Errorf("%d shortcuts created", created)
	}
	check, err := w.rpc.MsgCheckQuickReplyShortcut(context.Background(), &msg.TLMsgCheckQuickReplyShortcut{UserId: a, Shortcut: "RACE"})
	if err != nil || mtproto.FromBool(check) {
		t.Errorf("check of a full shortcut: %v %v", check, err)
	}
}

// An account has at most quick_replies_limit shortcuts.
func TestQuickRepliesShortcutLimit(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a := newUser()
	for i := range business.QuickRepliesLimit {
		w.mustAddQuickReplies(a, byName(fmt.Sprint("s", i)), "x")
	}
	if _, _, err := w.addQuickReplies(a, byName("onemore"), "x"); !errors.Is(err, mtproto.ErrQuickRepliesTooMuch) {
		t.Errorf("one shortcut too many: %v", err)
	}
	check := func(name string) bool {
		r, err := w.rpc.MsgCheckQuickReplyShortcut(context.Background(), &msg.TLMsgCheckQuickReplyShortcut{UserId: a, Shortcut: name})
		if err != nil {
			t.Fatal(err)
		}
		return mtproto.FromBool(r)
	}
	if check("onemore") || !check("S0") || check("bad name") {
		t.Errorf("check: new %v, existing %v, invalid %v", check("onemore"), check("S0"), check("bad name"))
	}
	// Adding to a shortcut the account has is not creating one.
	w.mustAddQuickReplies(a, byName("s7"), "y")
}

// Renaming, ordering and deleting, with what the other devices get.
func TestQuickRepliesEdit(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a := newUser()
	other := newUser()
	ctx := context.Background()
	one := w.mustAddQuickReplies(a, byName("one"), "1").created.ShortcutId
	two := w.mustAddQuickReplies(a, byName("two"), "2").created.ShortcutId
	three := w.mustAddQuickReplies(a, byName("three"), "3").created.ShortcutId
	journal := len(w.seqJournal(a))

	rename := func(userId int64, id int32, name string) error {
		_, err := w.rpc.MsgEditQuickReplyShortcut(ctx, &msg.TLMsgEditQuickReplyShortcut{UserId: userId, AuthKeyId: userId * 10, ShortcutId: id, Shortcut: name})
		return err
	}
	for _, c := range []struct {
		userId int64
		id     int32
		name   string
		want   error
	}{
		{a, two, "ONE", mtproto.ErrShortcutOccupied},
		{a, two, "bad name", mtproto.ErrShortcutInvalid},
		{a, 999999, "free", mtproto.ErrShortcutInvalid},
		{other, two, "free", mtproto.ErrShortcutInvalid},
		{a, two, "Two", nil},
		{a, two, "Two", nil}, // no change: no update
		{a, one, "first", nil},
	} {
		if err := rename(c.userId, c.id, c.name); !errors.Is(err, c.want) {
			t.Errorf("rename %d to %q by %d: %v, want %v", c.id, c.name, c.userId, err, c.want)
		}
	}
	if got := shortcutNames(w.quickReplies(a, 0)); !slices.Equal(got, []string{"first:1", "Two:1", "three:1"}) {
		t.Errorf("after renames %v", got)
	}

	reorder := func(order ...int32) {
		if _, err := w.rpc.MsgReorderQuickReplies(ctx, &msg.TLMsgReorderQuickReplies{UserId: a, AuthKeyId: a * 10, Order: order}); err != nil {
			t.Fatal(err)
		}
	}
	reorder(three, 999999, one)
	if got := shortcutNames(w.quickReplies(a, 0)); !slices.Equal(got, []string{"three:1", "first:1", "Two:1"}) {
		t.Errorf("after reorder %v", got)
	}
	reorder(three, one, two) // the order it has: no update

	if _, err := w.rpc.MsgDeleteQuickReplyShortcut(ctx, &msg.TLMsgDeleteQuickReplyShortcut{UserId: other, AuthKeyId: other * 10, ShortcutId: one}); !errors.Is(err, mtproto.ErrShortcutInvalid) {
		t.Errorf("delete of another account's shortcut: %v", err)
	}
	if _, err := w.rpc.MsgDeleteQuickReplyShortcut(ctx, &msg.TLMsgDeleteQuickReplyShortcut{UserId: a, AuthKeyId: a * 10, ShortcutId: one}); err != nil {
		t.Fatal(err)
	}
	if got := shortcutNames(w.quickReplies(a, 0)); !slices.Equal(got, []string{"three:1", "Two:1"}) {
		t.Errorf("after delete %v", got)
	}
	if _, err := w.rpc.MsgGetQuickReplyMessages(ctx, &msg.TLMsgGetQuickReplyMessages{UserId: a, ShortcutId: one}); !errors.Is(err, mtproto.ErrShortcutInvalid) {
		t.Errorf("messages of a deleted shortcut: %v", err)
	}
	var left int
	if err := w.check.QueryRow("SELECT COUNT(*) FROM quick_reply_messages WHERE shortcut_id = ?", one).Scan(&left); err != nil || left != 0 {
		t.Errorf("%d messages of the deleted shortcut left, %v", left, err)
	}
	// The name is free again.
	if again := w.mustAddQuickReplies(a, byName("FIRST"), "x"); again.created == nil {
		t.Errorf("a deleted name is not free")
	}

	want := []string{
		mtproto.Predicate_updateQuickReplies, // Two
		mtproto.Predicate_updateQuickReplies, // first
		mtproto.Predicate_updateQuickReplies, // the order
		mtproto.Predicate_updateDeleteQuickReply,
		mtproto.Predicate_updateNewQuickReply,
		mtproto.Predicate_updateQuickReplyMessage,
	}
	if got := w.seqKinds(a, journal); !slices.Equal(got, want) {
		t.Errorf("seq journal %v, want %v", got, want)
	}
}

// The migration again, twice, over shortcuts that exist: they stay.
func TestQuickRepliesMigrationAgain(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a := newUser()
	w.mustAddQuickReplies(a, byName("kept"), "1", "2")
	path, err := testinfra.SchemaFile("migrate-20261119-quick-replies.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = itDB.ApplySQLFile(context.Background(), path); err != nil {
			t.Fatalf("the migration again: %v", err)
		}
	}
	if got := shortcutNames(w.quickReplies(a, 0)); !slices.Equal(got, []string{"kept:2"}) {
		t.Errorf("after the migration again: %v", got)
	}
}

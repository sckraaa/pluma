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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// shortcutTexts are the texts of userId's shortcut, oldest first.
func (w *world) shortcutTexts(userId int64, shortcutId int32) []string {
	w.t.Helper()
	r, err := w.rpc.MsgGetQuickReplyMessages(context.Background(), &msg.TLMsgGetQuickReplyMessages{UserId: userId, ShortcutId: shortcutId})
	if err != nil {
		w.t.Fatal(err)
	}
	var out []string
	for _, m := range r.GetMessages() {
		out = append([]string{m.Message}, out...)
	}
	return out
}

// An edit replaces a message of the shortcut with its edit date, a delete
// removes those of its messages the request names; both answer the update
// the other devices get with seq. Another account's shortcut, or a message
// the shortcut does not hold, is refused. A shortcut left empty stays for
// adds by id.
func TestQuickReplyMessagesEditDelete(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a := newUser()
	other := newUser()
	ctx := context.Background()
	added := w.mustAddQuickReplies(a, byName("hello"), "one", "two", "three")
	id := added.created.ShortcutId
	journal := len(w.seqJournal(a))

	edit := func(userId int64, shortcutId int32, m *mtproto.Message, text string) (*mtproto.Updates, error) {
		e := proto.Clone(m).(*mtproto.Message)
		e.Message = text
		e.EditDate = wrapperspb.Int32(1)
		return w.rpc.MsgEditQuickReplyMessage(ctx, &msg.TLMsgEditQuickReplyMessage{
			UserId: userId, AuthKeyId: userId * 10, ShortcutId: shortcutId,
			Message: msg.MakeTLOutboxMessage(&msg.OutboxMessage{NoWebpage: true, Message: e}).To_OutboxMessage(),
		})
	}
	r, err := edit(a, id, added.messages[0], "one edited")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateQuickReplyMessage {
		t.Fatalf("answer %v", r.GetUpdates())
	}
	m := r.GetUpdates()[0].GetMessage_MESSAGE()
	if m.Id != added.messages[0].Id || m.Message != "one edited" || m.GetEditDate().GetValue() < added.messages[0].Date ||
		m.GetQuickReplyShortcutId().GetValue() != id || m.GetPeerId().GetUserId() != a {
		t.Errorf("edited %v", m)
	}
	if got := w.shortcutTexts(a, id); !slices.Equal(got, []string{"one edited", "two", "three"}) {
		t.Errorf("after the edit %v", got)
	}
	if _, err = edit(other, id, added.messages[1], "x"); !errors.Is(err, mtproto.ErrShortcutInvalid) {
		t.Errorf("edit of another account's shortcut: %v", err)
	}
	gone := proto.Clone(added.messages[1]).(*mtproto.Message)
	gone.Id = 999999
	if _, err = edit(a, id, gone, "x"); !errors.Is(err, mtproto.ErrMessageIdInvalid) {
		t.Errorf("edit of a message the shortcut does not hold: %v", err)
	}

	del := func(userId int64, ids ...int32) (*mtproto.Updates, error) {
		return w.rpc.MsgDeleteQuickReplyMessages(ctx, &msg.TLMsgDeleteQuickReplyMessages{UserId: userId, AuthKeyId: userId * 10, ShortcutId: id, Id: ids})
	}
	if _, err = del(other, added.messages[1].Id); !errors.Is(err, mtproto.ErrShortcutInvalid) {
		t.Errorf("delete in another account's shortcut: %v", err)
	}
	r, err = del(a, added.messages[1].Id, 999999)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateDeleteQuickReplyMessages ||
		r.GetUpdates()[0].GetShortcutId() != id || !slices.Equal(r.GetUpdates()[0].GetMessages(), []int32{added.messages[1].Id}) {
		t.Fatalf("answer %v", r.GetUpdates())
	}
	if got := w.shortcutTexts(a, id); !slices.Equal(got, []string{"one edited", "three"}) {
		t.Errorf("after the delete %v", got)
	}
	// Ids it no longer holds: nothing to tell.
	if r, err = del(a, added.messages[1].Id); err != nil || len(r.GetUpdates()) != 0 {
		t.Errorf("delete again: %v %v", r.GetUpdates(), err)
	}
	want := []string{mtproto.Predicate_updateQuickReplyMessage, mtproto.Predicate_updateDeleteQuickReplyMessages}
	if got := w.seqKinds(a, journal); !slices.Equal(got, want) {
		t.Errorf("seq journal %v, want %v", got, want)
	}

	if _, err = del(a, added.messages[0].Id, added.messages[2].Id); err != nil {
		t.Fatal(err)
	}
	if got := shortcutNames(w.quickReplies(a, 0)); len(got) != 0 {
		t.Errorf("an empty shortcut listed: %v", got)
	}
	if again := w.mustAddQuickReplies(a, byShortcutId(id), "four"); again.created != nil || again.messages[0].GetQuickReplyShortcutId().GetValue() != id {
		t.Errorf("add to the emptied shortcut: created %v, message %v", again.created, again.messages)
	}
}

// sentMessage decodes a message of a box.
func sentMessage(t *testing.T, r messageRow) *mtproto.Message {
	t.Helper()
	var m mtproto.Message
	if err := jsonx.UnmarshalFromString(r.Data, &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

// A shortcut goes to a private chat as new messages of its owner, in the
// order they were added: an album as one new album, without the shortcut's
// id. A send repeated with the same random ids is sent once. Saved
// Messages, a bot, another account's shortcut and random ids that do not
// match are refused.
func TestQuickRepliesSend(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a := newUser()
	b := newUser()
	bot := newUser()
	w.users.mu.Lock()
	w.users.bots[bot] = true
	w.users.mu.Unlock()
	ctx := context.Background()

	album := func(text string) *msg.OutboxMessage {
		m := textMessage(a, mtproto.MakePeerUser(a), text)
		m.GroupedId = wrapperspb.Int64(4242)
		return outbox(m)
	}
	r, err := w.rpc.MsgAddQuickReplyMessages(ctx, &msg.TLMsgAddQuickReplyMessages{
		UserId: a, AuthKeyId: a * 10, Shortcut: byName("send"),
		Message: []*msg.OutboxMessage{outbox(textMessage(a, mtproto.MakePeerUser(a), "first")), album("second"), album("third")},
	})
	if err != nil {
		t.Fatal(err)
	}
	added := readQuickReplyAnswer(t, r)
	id := added.created.ShortcutId

	send := func(userId, peerId int64, shortcutId int32, ids []int32, randomIds []int64) (*mtproto.Updates, error) {
		return w.rpc.MsgSendQuickReplyMessages(ctx, &msg.TLMsgSendQuickReplyMessages{
			UserId: userId, AuthKeyId: 0, PeerId: peerId, ShortcutId: shortcutId, Id: ids, RandomId: randomIds,
		})
	}
	for _, c := range []struct {
		name      string
		userId    int64
		peerId    int64
		shortcut  int32
		ids       []int32
		randomIds []int64
		want      error
	}{
		{"Saved Messages", a, a, id, nil, nil, mtproto.ErrPeerIdInvalid},
		{"a bot", a, bot, id, nil, nil, mtproto.ErrPeerIdInvalid},
		{"another account's shortcut", b, a, id, nil, nil, mtproto.ErrShortcutInvalid},
		{"random ids for other ids", a, b, id, []int32{added.messages[0].Id}, []int64{1, 2}, mtproto.ErrRandomIdInvalid},
		{"random ids for fewer messages", a, b, id, nil, []int64{1, 2}, mtproto.ErrRandomIdInvalid},
		{"no message of the shortcut", a, b, id, []int32{999999}, nil, mtproto.ErrMessageIdInvalid},
	} {
		if _, err := send(c.userId, c.peerId, c.shortcut, c.ids, c.randomIds); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	if got := w.messages(a); len(got) != 0 {
		t.Fatalf("refused sends wrote %d messages", len(got))
	}

	// tdesktop and iOS: no ids, no random ids.
	if _, err = send(a, b, id, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Android: the ids newest first, as getQuickReplyMessages gives them,
	// each with its random id; sent twice after a lost answer.
	ids := []int32{added.messages[2].Id, added.messages[1].Id, added.messages[0].Id}
	randomIds := []int64{randomId(), randomId(), randomId()}
	r1, err := send(a, b, id, ids, randomIds)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := send(a, b, id, ids, randomIds)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.GetUpdates()) == 0 || len(r1.GetUpdates()) != len(r2.GetUpdates()) {
		t.Errorf("repeat answered %d updates, first %d", len(r2.GetUpdates()), len(r1.GetUpdates()))
	}
	w.pub.drain()

	mine := alive(w.messages(a))
	theirs := alive(w.messages(b))
	if len(mine) != 6 || len(theirs) != 6 {
		t.Fatalf("%d messages sent, %d received; want 6", len(mine), len(theirs))
	}
	for _, rows := range [][]messageRow{mine, theirs} {
		var texts []string
		albums := map[int64]int{}
		for _, row := range rows {
			m := sentMessage(t, row)
			texts = append(texts, m.Message)
			if m.GetQuickReplyShortcutId() != nil || row.Sender != a {
				t.Errorf("message %q: shortcut %v, sender %d", m.Message, m.GetQuickReplyShortcutId(), row.Sender)
			}
			if g := m.GetGroupedId().GetValue(); g != 0 {
				if g == 4242 {
					t.Errorf("message %q keeps the shortcut's album", m.Message)
				}
				albums[g]++
			}
		}
		if !slices.Equal(texts, []string{"first", "second", "third", "first", "second", "third"}) {
			t.Errorf("texts %v", texts)
		}
		if len(albums) != 2 {
			t.Errorf("albums %v, want two of two messages", albums)
		}
		for g, n := range albums {
			if n != 2 {
				t.Errorf("album %d of %d messages", g, n)
			}
		}
	}
	// The shortcut is kept as it was.
	if got := w.shortcutTexts(a, id); !slices.Equal(got, []string{"first", "second", "third"}) {
		t.Errorf("shortcut after the sends %v", got)
	}
}

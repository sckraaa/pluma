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
	"fmt"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const self = int64(7)

func message(id int32, text string, peer *mtproto.Peer) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{Id: id, Message: text, PeerId: peer}).To_Message()
}

func newMessage(id int32, text string, pts int32) *mtproto.Update {
	return mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: message(id, text, mtproto.MakePeerUser(self)), Pts_INT32: pts, PtsCount: 1}).To_Update()
}

func editMessage(id int32, text string, pts int32) *mtproto.Update {
	m := message(id, text, mtproto.MakePeerUser(self))
	m.EditDate = wrapperspb.Int32(1700000000 + pts)
	return mtproto.MakeTLUpdateEditMessage(&mtproto.Update{Message_MESSAGE: m, Pts_INT32: pts, PtsCount: 1}).To_Update()
}

func deleteMessages(pts int32, ids ...int32) *mtproto.Update {
	return mtproto.MakeTLUpdateDeleteMessages(&mtproto.Update{Messages: ids, Pts_INT32: pts, PtsCount: int32(len(ids))}).To_Update()
}

func readInbox(peer *mtproto.Peer, maxId, pts int32) *mtproto.Update {
	return mtproto.MakeTLUpdateReadHistoryInbox(&mtproto.Update{Peer_PEER: peer, MaxId: maxId, Pts_INT32: pts, PtsCount: 1}).To_Update()
}

func pinned(peer *mtproto.Peer, pin bool, pts int32, ids ...int32) *mtproto.Update {
	return mtproto.MakeTLUpdatePinnedMessages(&mtproto.Update{Peer_PEER: peer, Pinned: pin, Messages: ids, Pts_INT32: pts, PtsCount: 1}).To_Update()
}

// fold folds updates ending at pts; randomIds are the account's own
// messages.
func fold(pts int32, randomIds map[int32]int64, updates ...*mtproto.Update) *differenceBuilder {
	b := newDifferenceBuilder(pts)
	for _, u := range updates {
		b.add(u)
	}
	b.addMessageIds(randomIds)
	return b
}

// describe writes new_messages and other_updates as id:text and
// predicate(details) lists.
func describe(b *differenceBuilder) (string, string) {
	var ms, us []string
	for _, m := range b.messages() {
		ms = append(ms, fmt.Sprintf("%d:%s", m.GetId(), m.GetMessage()))
	}
	for _, u := range b.otherUpdates() {
		s := strings.TrimPrefix(u.GetPredicateName(), "update")
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateMessageID:
			s += fmt.Sprintf("(%d,%d)", u.GetId_INT32(), u.GetRandomId())
		case mtproto.Predicate_updateEditMessage:
			s += fmt.Sprintf("(%d:%s)", u.GetMessage_MESSAGE().GetId(), u.GetMessage_MESSAGE().GetMessage())
		case mtproto.Predicate_updateDeleteMessages, mtproto.Predicate_updatePinnedMessages:
			s += fmt.Sprint(u.GetMessages())
		case mtproto.Predicate_updateReadHistoryInbox:
			s += fmt.Sprintf("(%d,%d)", u.GetPeer_PEER().GetUserId(), u.GetMaxId())
		}
		us = append(us, s)
	}
	return strings.Join(ms, " "), strings.Join(us, " ")
}

// The two answers of the Telegram fixture saved_messages: a message sent to
// Saved Messages (read at once), edited, then deleted.
func TestDifferenceAsTelegram(t *testing.T) {
	journal := []*mtproto.Update{
		newMessage(1, "sent", 2), readInbox(mtproto.MakePeerUser(self), 1, 3), editMessage(1, "edited", 4),
	}
	b := fold(4, map[int32]int64{1: 555}, journal...)
	ms, us := describe(b)
	if ms != "1:edited" || us != "MessageID(1,555) ReadHistoryInbox(7,1)" {
		t.Errorf("after the edit: messages %q, updates %q", ms, us)
	}
	if b.messages()[0].GetEditDate().GetValue() == 0 {
		t.Error("the new message has no edit_date")
	}
	for _, u := range b.otherUpdates() {
		if u.GetPredicateName() != mtproto.Predicate_updateMessageID && (u.GetPts_INT32() != 4 || u.GetPtsCount() != 0) {
			t.Errorf("%s: pts %d, pts_count %d; want the final pts and none", u.GetPredicateName(), u.GetPts_INT32(), u.GetPtsCount())
		}
	}

	b = fold(5, map[int32]int64{1: 555}, append(journal, deleteMessages(5, 1))...)
	if ms, us = describe(b); ms != "" || us != "DeleteMessages[1] ReadHistoryInbox(7,1)" {
		t.Errorf("after the deletion: messages %q, updates %q", ms, us)
	}
	if ids := b.newMessageIds(); len(ids) != 0 {
		t.Errorf("ids to look up for a deleted message: %v", ids)
	}
}

func TestDifferenceFold(t *testing.T) {
	alice := mtproto.MakePeerUser(9)
	bob := mtproto.MakePeerUser(10)
	for _, c := range []struct {
		name           string
		randomIds      map[int32]int64
		journal        []*mtproto.Update
		messages, rest string
	}{
		{
			name:     "an older message edited twice: once, the last state",
			journal:  []*mtproto.Update{editMessage(1, "a", 2), newMessage(2, "x", 3), editMessage(1, "b", 4)},
			messages: "2:x", rest: "EditMessage(1:b)",
		},
		{
			name:    "an older message edited and deleted: the deletion only",
			journal: []*mtproto.Update{editMessage(1, "a", 2), deleteMessages(3, 1)},
			rest:    "DeleteMessages[1]",
		},
		{
			name:    "deletions merge where the last one was; a deleted new message goes",
			journal: []*mtproto.Update{deleteMessages(2, 1), newMessage(3, "x", 3), pinned(alice, true, 4, 9), deleteMessages(5, 3)},
			rest:    "PinnedMessages[9] DeleteMessages[1 3]",
		},
		{
			name: "the read history of a dialog once, last, with its last max_id",
			journal: []*mtproto.Update{
				readInbox(alice, 5, 2), readInbox(bob, 3, 3), readInbox(alice, 6, 4), readInbox(alice, 8, 5), newMessage(9, "x", 6),
			},
			messages: "9:x", rest: "ReadHistoryInbox(9,8) ReadHistoryInbox(10,3)",
		},
		{
			name:    "pins of other dialogs and unpins are all kept",
			journal: []*mtproto.Update{pinned(alice, true, 2, 1), pinned(bob, true, 3, 2), pinned(alice, false, 4, 1)},
			rest:    "PinnedMessages[1] PinnedMessages[2] PinnedMessages[1]",
		},
		{
			name:      "updateMessageID of own messages only, first, ascending",
			randomIds: map[int32]int64{3: 33, 1: 11},
			journal:   []*mtproto.Update{newMessage(3, "c", 2), newMessage(1, "a", 3), newMessage(2, "incoming", 4), readInbox(alice, 1, 5)},
			messages:  "1:a 2:incoming 3:c", rest: "MessageID(1,11) MessageID(3,33) ReadHistoryInbox(9,1)",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			ms, us := describe(fold(10, c.randomIds, c.journal...))
			if ms != c.messages || us != c.rest {
				t.Errorf("messages %q, updates %q; want %q, %q", ms, us, c.messages, c.rest)
			}
		})
	}
}

// A difference slice folds its own rows: an edit in the next slice is an
// update, the message is the client's already.
func TestDifferenceSliceFold(t *testing.T) {
	first := fold(2, nil, newMessage(1, "a", 2))
	next := fold(3, nil, editMessage(1, "b", 3))
	if ms, _ := describe(first); ms != "1:a" {
		t.Errorf("first slice: %q", ms)
	}
	if ms, us := describe(next); ms != "" || us != "EditMessage(1:b)" {
		t.Errorf("next slice: %q, %q", ms, us)
	}
}

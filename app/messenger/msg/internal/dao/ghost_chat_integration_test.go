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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// basicGroupRows writes the basic group chat with its members, the first
// its creator, as biz chat keeps them: the rows a hidden join locks and
// changes.
func (w *world) basicGroupRows(chat int64, members ...int64) {
	w.t.Helper()
	w.exec("INSERT INTO chats (id, creator_user_id, access_hash, random_id, participant_count, title, version) VALUES (?, ?, 1, 0, ?, 'g', 5)", chat, members[0], len(members))
	for i, u := range members {
		kind := mtproto.ChatMemberNormal
		if i == 0 {
			kind = mtproto.ChatMemberCreator
		}
		w.exec("INSERT INTO chat_participants (chat_id, user_id, participant_type, state, invited_at, date2) VALUES (?, ?, ?, 0, ?, ?)", chat, u, kind, i+1, i+1)
	}
	w.chats.members[chat] = members
}

// sendChat sends m from from to the basic group chat.
func (w *world) sendChat(from, chat int64, m *mtproto.Message) {
	w.t.Helper()
	if _, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		UserId: from, AuthKeyId: from * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, Message: []*msg.OutboxMessage{outbox(m)},
	}); err != nil {
		w.t.Fatalf("send from %d to chat %d: %v", from, chat, err)
	}
}

// mentionInChat is a message from from to chat that mentions userId by name.
func mentionInChat(from, chat, userId int64) *mtproto.Message {
	m := textMessage(from, mtproto.MakePeerChat(chat), "ask Support")
	m.Entities = []*mtproto.MessageEntity{mtproto.MakeTLMessageEntityMentionName(&mtproto.MessageEntity{Offset: 4, Length: 7, UserId_INT64: userId}).To_MessageEntity()}
	return m
}

func (w *world) outboxCount(recipient int64, method string) int {
	w.t.Helper()
	return w.countRows("SELECT COUNT(*) FROM mq_outbox WHERE recipient_id = ? AND method = ?", recipient, method)
}

// TestMentionBringsGhostToBasicGroup: a mention of the ghost in a basic
// group it is not in makes it a hidden member in the transaction of the
// send: the group's count, version and messages stay as the members know
// them. Its box gets the whole history first, from whichever member still
// has each message, its own replies pointing to its own copies, read; then
// the mention, unread and mentioned; the group is muted for it. A second
// copy of the history changes nothing; the ghost's read reaches no sender.
// A ghost an admin removed stays out of the next mention.
func TestMentionBringsGhostToBasicGroup(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b, g := newUser(), newUser(), w.newGhost()
	chat := newChat()
	w.basicGroupRows(chat, a, b)

	w.send(a, mtproto.PEER_CHAT, chat, "1", "2")
	w.pub.drain()
	bMsgs := w.messages(b)
	reply := textMessage(b, mtproto.MakePeerChat(chat), "3")
	reply.ReplyTo = mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
		ReplyToMsgId:           bMsgs[0].Id,
		ReplyToMsgId_INT32:     bMsgs[0].Id,
		ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(bMsgs[0].Id),
	}).To_MessageReplyHeader()
	w.sendChat(b, chat, reply)
	w.pub.drain()
	// A deleted "2" for itself: B's copy is the one to take.
	w.exec("UPDATE messages SET deleted = 1 WHERE user_id = ? AND message = '2'", a)

	w.sendChat(a, chat, mentionInChat(a, chat, g))
	w.pub.drain()

	var state int32
	var hidden bool
	if err := w.check.QueryRow("SELECT state, hidden FROM chat_participants WHERE chat_id = ? AND user_id = ?", chat, g).Scan(&state, &hidden); err != nil || state != 0 || !hidden {
		t.Fatalf("the ghost's row: %d %v %v", state, hidden, err)
	}
	var count, version int32
	if err := w.check.QueryRow("SELECT participant_count, version FROM chats WHERE id = ?", chat).Scan(&count, &version); err != nil || count != 2 || version != 5 {
		t.Errorf("chat count %d, version %d, %v", count, version, err)
	}
	if n := len(w.messages(b)); n != 4 {
		t.Errorf("B has %d messages, want 4: no service message", n)
	}

	got := w.messages(g)
	texts := make([]string, len(got))
	for i, m := range got {
		texts[i] = m.Text
	}
	if len(got) != 4 || texts[0] != "1" || texts[1] != "2" || texts[2] != "3" || texts[3] != "ask Support" {
		t.Fatalf("the ghost's box %v", texts)
	}
	var m3 mtproto.Message
	if err := json.Unmarshal([]byte(got[2].Data), &m3); err != nil {
		t.Fatal(err)
	}
	if m3.GetReplyTo().GetReplyToMsgId() != got[0].Id || m3.GetOut() || m3.GetId() != got[2].Id {
		t.Errorf("the copy of the reply %+v", &m3)
	}
	for i, m := range got[:3] {
		if m.Mentioned || m.MediaUnread {
			t.Errorf("history copy %d mentions", i)
		}
	}
	if !got[3].Mentioned {
		t.Error("the mention is not marked")
	}
	if d := w.dialog(g, mtproto.PEER_CHAT, chat); d.TopMessage != got[3].Id || d.ReadInboxMaxId != got[2].Id || d.UnreadCount != 1 || d.UnreadMentions != 1 {
		t.Errorf("the ghost's dialog %+v", d)
	}
	if !w.users.isMuted(g, chat) {
		t.Error("the group is not muted for the ghost")
	}
	w.checkBox(g)

	// The history again: nothing more.
	if _, err := w.d.RunBox(ctx, g, nil, func(bx *userbox.Box) error {
		done, err := w.d.CopyChatHistory(bx, chat, nil)
		if err == nil && !done {
			t.Error("a second copy is not done at once")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := len(w.messages(g)); n != 4 {
		t.Errorf("after a second copy the ghost has %d messages", n)
	}

	// The ghost's read reaches nobody; B's reaches A.
	const readOutbox = "inbox.TL_inbox_readOutboxHistory"
	before := w.outboxCount(a, readOutbox)
	if _, err := w.rpc.MsgReadHistoryV2(ctx, &msg.TLMsgReadHistoryV2{UserId: g, AuthKeyId: g * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, MaxId: got[3].Id}); err != nil {
		t.Fatal(err)
	}
	if n := w.outboxCount(a, readOutbox); n != before {
		t.Errorf("the ghost's read told A: %d events, %d before", n, before)
	}
	if _, err := w.rpc.MsgReadHistoryV2(ctx, &msg.TLMsgReadHistoryV2{UserId: b, AuthKeyId: b * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, MaxId: w.messages(b)[3].Id}); err != nil {
		t.Fatal(err)
	}
	if n := w.outboxCount(a, readOutbox); n == before {
		t.Error("B's read did not tell A: the check sees nothing")
	}
	w.pub.drain()

	// Removed by an admin: the next mention leaves it out.
	w.exec("UPDATE chat_participants SET state = ? WHERE chat_id = ? AND user_id = ?", mtproto.ChatMemberStateKicked, chat, g)
	w.sendChat(b, chat, mentionInChat(b, chat, g))
	w.pub.drain()
	if err := w.check.QueryRow("SELECT state FROM chat_participants WHERE chat_id = ? AND user_id = ?", chat, g).Scan(&state); err != nil || state != mtproto.ChatMemberStateKicked {
		t.Errorf("removed ghost: state %d, %v", state, err)
	}
	if n := len(w.messages(g)); n != 4 {
		t.Errorf("the removed ghost got the mention: %d messages", n)
	}
}

// TestChatDeliveryKeepsHistoryCopy: a message a ghost's history copy took
// before its delivery came lands once.
func TestChatDeliveryKeepsHistoryCopy(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, g := newUser(), w.newGhost()
	chat := newChat()
	w.basicGroupRows(chat, a)
	w.exec("INSERT INTO chat_participants (chat_id, user_id, participant_type, state, hidden, invited_at, date2) VALUES (?, ?, 0, 0, 1, UNIX_TIMESTAMP() + 60, 9)", chat, g)
	w.send(a, mtproto.PEER_CHAT, chat, "x")
	w.pub.drain()
	if _, err := w.d.RunBox(ctx, g, nil, func(bx *userbox.Box) error {
		_, err := w.d.CopyChatHistory(bx, chat, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	src := w.messages(a)
	if _, err := w.d.RunBox(ctx, g, nil, func(bx *userbox.Box) error {
		in, err := w.d.ReceiveIncoming(bx, a, mtproto.PEER_CHAT, chat, []*mtproto.MessageBox{{
			DialogMessageId: src[0].DialogMessageId,
			Message:         textMessage(a, mtproto.MakePeerChat(chat), "x"),
		}})
		if err == nil && len(in.Boxes) != 0 {
			t.Errorf("delivered again: %d", len(in.Boxes))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := len(w.messages(g)); n != 1 {
		t.Errorf("the ghost has %d copies", n)
	}
}

// TestChatHistoryEndsAtJoin: the history a ghost's box gets ends at its
// join; a message sent after it is left to its own delivery (unread, with
// its mention), even when the copy runs late.
func TestChatHistoryEndsAtJoin(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	a, b, g := newUser(), newUser(), w.newGhost()
	chat := newChat()
	w.basicGroupRows(chat, a, b)
	w.send(a, mtproto.PEER_CHAT, chat, "before")
	w.pub.drain()
	w.exec("UPDATE messages SET date2 = date2 - 200 WHERE peer_type = ? AND peer_id = ?", mtproto.PEER_CHAT, chat)
	w.exec("INSERT INTO chat_participants (chat_id, user_id, participant_type, state, hidden, invited_at, date2) VALUES (?, ?, 0, 0, 1, UNIX_TIMESTAMP() - 100, 9)", chat, g)
	// Sent after the join by a sender that read the chat before it: here
	// the delivery to the ghost would follow the history event.
	w.send(b, mtproto.PEER_CHAT, chat, "after")
	w.pub.drain()
	if _, err := w.d.RunBox(ctx, g, nil, func(bx *userbox.Box) error {
		done, err := w.d.CopyChatHistory(bx, chat, nil)
		if err == nil && !done {
			t.Error("the copy is not done")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got := w.messages(g)
	if len(got) != 1 || got[0].Text != "before" {
		t.Fatalf("the ghost's box %+v", got)
	}
}

// TestMentionReachesGhostAlreadyIn: a mention sent with the chat read before
// another send brought the ghost in reaches it, with no second history.
func TestMentionReachesGhostAlreadyIn(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b, g := newUser(), newUser(), w.newGhost()
	chat := newChat()
	w.basicGroupRows(chat, a, b)
	w.send(a, mtproto.PEER_CHAT, chat, "old")
	w.pub.drain()
	// The ghost is in; the chat the send reads does not list it yet.
	w.exec("INSERT INTO chat_participants (chat_id, user_id, participant_type, state, hidden, invited_at, date2) VALUES (?, ?, 0, 0, 1, UNIX_TIMESTAMP(), 9)", chat, g)
	w.sendChat(b, chat, mentionInChat(b, chat, g))
	w.pub.drain()
	got := w.messages(g)
	// No history: "old" came before the other send's history event did.
	if len(got) != 1 || got[0].Text != "ask Support" || !got[0].Mentioned {
		t.Fatalf("the ghost's box %+v", got)
	}
}

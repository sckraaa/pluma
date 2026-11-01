//go:build integration

package dao_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// storedMessage is the message of a messages row.
func storedMessage(t *testing.T, r messageRow) *mtproto.Message {
	t.Helper()
	m := &mtproto.Message{}
	if err := json.Unmarshal([]byte(r.Data), m); err != nil {
		t.Fatal(err)
	}
	return m
}

// pushedUsers are the ids of the users pushed to userId.
func pushedUsers(w *world, userId int64) []int64 {
	var ids []int64
	for _, p := range w.pub.pushesTo(userId) {
		for _, u := range p.updates.GetUsers() {
			ids = append(ids, u.GetId())
		}
	}
	return ids
}

// TestOtherChatReply: the header of a reply to a message of another chat
// names no message; it reaches the sender's box and every member's box as
// it was sent, mentions no one, and the users it names come with it.
func TestOtherChatReply(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b, c, author, contact := newUser(), newUser(), newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{a, b, c}

	// B writes to A; A answers it in the group.
	w.send(b, mtproto.PEER_USER, a, "original")
	w.pub.drain()
	header := mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
		ReplyFrom: mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{
			FromId: mtproto.MakePeerUser(author),
			Date:   1700000000,
		}).To_MessageFwdHeader(),
		ReplyMedia: mtproto.MakeTLMessageMediaContact(&mtproto.MessageMedia{
			PhoneNumber: "", FirstName: "C", UserId: contact,
		}).To_MessageMedia(),
		QuoteText: wrapperspb.String("original"),
	}).To_MessageReplyHeader()
	reply := textMessage(a, mtproto.MakePeerChat(chat), "an answer")
	reply.ReplyTo = proto.Clone(header).(*mtproto.MessageReplyHeader)
	r, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, Message: []*msg.OutboxMessage{outbox(reply)},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.pub.drain()

	// The sender's reply carries the stored message and its users.
	var sent *mtproto.Message
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			sent = u.GetMessage_MESSAGE()
		}
	}
	if !proto.Equal(sent.GetReplyTo(), header) {
		t.Errorf("sender's reply: header %v", sent.GetReplyTo())
	}
	var replyUsers []int64
	for _, u := range r.GetUsers() {
		replyUsers = append(replyUsers, u.GetId())
	}
	if !slices.Contains(replyUsers, author) || !slices.Contains(replyUsers, contact) {
		t.Errorf("sender's reply users %v, want %d and %d", replyUsers, author, contact)
	}

	for _, u := range []int64{a, b, c} {
		msgs := w.messages(u)
		last := msgs[len(msgs)-1]
		m := storedMessage(t, last)
		if m.GetMessage() != "an answer" {
			t.Fatalf("user %d: last message %q", u, m.GetMessage())
		}
		if !proto.Equal(m.GetReplyTo(), header) {
			t.Errorf("user %d: header %v, want %v", u, m.GetReplyTo(), header)
		}
		if last.Mentioned || m.GetMentioned() {
			t.Errorf("user %d: mentioned", u)
		}
		if d := w.dialog(u, mtproto.PEER_CHAT, chat); d.UnreadMentions != 0 {
			t.Errorf("user %d: %d unread mentions", u, d.UnreadMentions)
		}
		w.checkBox(u)
	}
	for _, u := range []int64{b, c} {
		users := pushedUsers(w, u)
		if !slices.Contains(users, author) || !slices.Contains(users, contact) {
			t.Errorf("user %d: pushed users %v, want %d and %d", u, users, author, contact)
		}
	}
}

// TestReplyToCopyInOtherDialog: a reply in a group to a message id whose
// copy lies in another chat of a member is no reply for that member, and
// does not mention them.
func TestReplyToCopyInOtherDialog(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b, c := newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{a, b, c}

	// B writes to A in private; A sends to the group a reply naming the id
	// of that private message.
	w.send(b, mtproto.PEER_USER, a, "private")
	w.pub.drain()
	private := w.messages(a)[0].Id
	reply := textMessage(a, mtproto.MakePeerChat(chat), "in the group")
	reply.ReplyTo = mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
		ReplyToMsgId:           private,
		ReplyToMsgId_INT32:     private,
		ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(private),
	}).To_MessageReplyHeader()
	if _, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, Message: []*msg.OutboxMessage{outbox(reply)},
	}); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()

	for _, u := range []int64{b, c} {
		msgs := w.messages(u)
		last := msgs[len(msgs)-1]
		m := storedMessage(t, last)
		if m.GetMessage() != "in the group" {
			t.Fatalf("user %d: last message %q", u, m.GetMessage())
		}
		if m.GetReplyTo() != nil {
			t.Errorf("user %d: reply to %d", u, m.GetReplyTo().GetFixedReplyToMsgId())
		}
		if last.Mentioned {
			t.Errorf("user %d: mentioned", u)
		}
		if d := w.dialog(u, mtproto.PEER_CHAT, chat); d.UnreadMentions != 0 {
			t.Errorf("user %d: %d unread mentions", u, d.UnreadMentions)
		}
	}

	// A reply to a message of the group still names each member's copy.
	ids := sentIds(w.send(b, mtproto.PEER_CHAT, chat, "in group"))
	w.pub.drain()
	reply = textMessage(c, mtproto.MakePeerChat(chat), "answer")
	cMsgs := w.messages(c)
	cCopy := cMsgs[len(cMsgs)-1].Id
	reply.ReplyTo = mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
		ReplyToMsgId:           cCopy,
		ReplyToMsgId_INT32:     cCopy,
		ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(cCopy),
	}).To_MessageReplyHeader()
	if _, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		UserId: c, AuthKeyId: c * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, Message: []*msg.OutboxMessage{outbox(reply)},
	}); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()
	bMsgs := w.messages(b)
	if m := storedMessage(t, bMsgs[len(bMsgs)-1]); m.GetReplyTo().GetReplyToMsgId() != ids[0] || !bMsgs[len(bMsgs)-1].Mentioned {
		t.Errorf("B's copy of the answer: reply to %d (want %d), mentioned %v", m.GetReplyTo().GetReplyToMsgId(), ids[0], bMsgs[len(bMsgs)-1].Mentioned)
	}
}

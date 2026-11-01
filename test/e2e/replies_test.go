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

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// messages.getMessages with inputMessageReplyTo gives the message the given
// one replies to, as iOS asks for replies out of the loaded history
// (task 020).
func TestGetRepliedMessage(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob := peerOf(ctx, t, alice, bob)

	question := fmt.Sprintf("question %d", randomID())
	first := sendMessageID(ctx, t, alice, toBob, question, randomID())
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        toBob,
		Message:     "and a reply",
		RandomId:    randomID(),
		ReplyTo:     mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: first}).To_InputReplyTo(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	var reply int32
	for _, up := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if up.GetPredicateName() == mtproto.Predicate_updateMessageID {
			reply = up.GetId_INT32()
		}
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getMessages),
		Id_VECTORINPUTMESSAGE: []*mtproto.InputMessage{
			mtproto.MakeTLInputMessageReplyTo(&mtproto.InputMessage{Id: reply}).To_InputMessage(),
		},
	})
	if err != nil {
		t.Fatalf("messages.getMessages: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	})
	if !ok {
		t.Fatalf("messages.getMessages returned %T", o)
	}
	msgs := r.To_Messages_Messages().GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("messages.getMessages(inputMessageReplyTo %d): %d messages", reply, len(msgs))
	}
	if msgs[0].GetId() != first || msgs[0].GetMessage() != question {
		t.Errorf("messages.getMessages(inputMessageReplyTo %d): message %d, want %d", reply, msgs[0].GetId(), first)
	}
}

// sendWithReply sends text from u to peer as a reply by replyTo and returns
// the server's answer.
func sendWithReply(ctx context.Context, u *user, peer *mtproto.InputPeer, text string, replyTo *mtproto.InputReplyTo) (*mtproto.Updates, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer,
		Message:     text,
		RandomId:    randomID(),
		ReplyTo:     replyTo,
	})
	if err != nil {
		return nil, err
	}
	return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
}

// newMessageWith is the message with text of an updateNewMessage of us.
func newMessageWith(us *mtproto.Updates, text string) *mtproto.Message {
	for _, up := range us.GetUpdates() {
		if up.GetPredicateName() == mtproto.Predicate_updateNewMessage && up.GetMessage_MESSAGE().GetMessage() == text {
			return up.GetMessage_MESSAGE()
		}
	}
	return nil
}

// waitNewMessage waits on u for the new message with text and returns it
// with the users of its update.
func waitNewMessage(ctx context.Context, t *testing.T, u *user, text string) (*mtproto.Message, []*mtproto.User) {
	t.Helper()
	us, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool { return newMessageWith(us, text) != nil })
	if err != nil {
		t.Fatalf("no new message %q: %v", text, err)
	}
	return newMessageWith(us, text), us.GetUsers()
}

// historyMessage returns the message with text of u's history with peer
// and the users of the history. A recipient's copy is written by the inbox
// consumer after the sender's call returns, so it asks again until the
// message is there.
func historyMessage(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, text string) (*mtproto.Message, []*mtproto.User) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
			Peer:        peer,
			Limit:       100,
		})
		if err != nil {
			t.Fatalf("messages.getHistory: %v", err)
		}
		h := o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages()
		for _, m := range h.GetMessages() {
			if m.GetMessage() == text {
				return m, h.GetUsers()
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("messages.getHistory: no message %q", text)
			return nil, nil
		}
	}
}

// differenceMessage returns the new message with text of u's
// updates.getDifference from st and the users of the difference.
func differenceMessage(ctx context.Context, t *testing.T, u *user, st *mtproto.TLUpdatesState, text string) (*mtproto.Message, []*mtproto.User) {
	t.Helper()
	d := differenceOnce(ctx, t, u, st)
	for _, m := range d.GetNewMessages() {
		if m.GetMessage() == text {
			return m, d.GetUsers()
		}
	}
	t.Fatalf("updates.getDifference: no message %q (%s)", text, d.GetPredicateName())
	return nil, nil
}

func hasUser(users []*mtproto.User, id int64) bool {
	return slices.ContainsFunc(users, func(u *mtproto.User) bool { return u.GetId() == id })
}

// checkOtherChatReply checks the header of a reply to a message of another
// chat as where shows it: no message, chat or thread; the author and the
// date of the original; its media (a predicate, or "" for none); the quote,
// with the quote flag when it is manual.
func checkOtherChatReply(t *testing.T, where string, h *mtproto.MessageReplyHeader, author int64, quote string, manual bool, media string) {
	t.Helper()
	if h.GetPredicateName() != mtproto.Predicate_messageReplyHeader {
		t.Errorf("%s: reply header %v", where, h)
		return
	}
	if h.GetReplyToMsgId_FLAGINT32() != nil || h.GetReplyToMsgId() != 0 || h.GetReplyToPeerId() != nil || h.GetReplyToTopId() != nil {
		t.Errorf("%s: reply names message %v in %v, top %v", where, h.GetReplyToMsgId_FLAGINT32(), h.GetReplyToPeerId(), h.GetReplyToTopId())
	}
	if from := h.GetReplyFrom(); from.GetFromId().GetUserId() != author || from.GetDate() <= 0 {
		t.Errorf("%s: reply_from %v, want user %d", where, from, author)
	}
	if got := h.GetReplyMedia().GetPredicateName(); got != media {
		t.Errorf("%s: reply_media %q, want %q", where, got, media)
	}
	if h.GetQuoteText().GetValue() != quote || h.GetQuote() != manual {
		t.Errorf("%s: quote %q (flag %v), want %q (flag %v)", where, h.GetQuoteText().GetValue(), h.GetQuote(), quote, manual)
	}
}

// A reply to a message of another chat (reply_to_peer_id, task 020) shows
// its author, media and quote to every reader the same way, whether the
// reader has the original or not, in the update, the history and the
// difference; it names no message, as tdlib expects for private chats and
// basic groups. The sender's result carries the stored message. A photo and
// an album carry the same header.
func TestReplyInAnotherChat(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	toBob := peerOf(ctx, t, alice, bob)
	toAlice := peerOf(ctx, t, bob, alice)

	// Bob sends Alice a photo with a caption.
	caption := fmt.Sprintf("a photo %d", randomID())
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        toAlice,
		Media:       uploadPhoto(ctx, t, bob),
		Message:     caption,
		RandomId:    randomID(),
	}); err != nil {
		t.Fatalf("messages.sendMedia: %v", err)
	}
	photo, _ := historyMessage(ctx, t, alice, toBob, caption)

	chat := createChat(ctx, t, alice, fmt.Sprintf("replies %d", randomID()), bob, carol)
	group := chatPeer(alice, chat)
	stBob, stCarol := getState(ctx, t, bob), getState(ctx, t, carol)

	// Alice answers the photo in the group: Bob has the photo, Carol has
	// not.
	answer := fmt.Sprintf("about the photo %d", randomID())
	r, err := sendWithReply(ctx, alice, group, answer, mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
		ReplyToMsgId: photo.GetId(), ReplyToPeerId: toBob,
	}).To_InputReplyTo())
	if err != nil {
		t.Fatalf("messages.sendMessage with a reply to the private chat: %v", err)
	}
	sent := newMessageWith(r, answer)
	if sent == nil {
		t.Fatalf("messages.sendMessage: no updateNewMessage in %s", r.GetPredicateName())
	}
	checkOtherChatReply(t, "Alice's result", sent.GetReplyTo(), bob.self.GetId(), caption, false, mtproto.Predicate_messageMediaPhoto)
	if !hasUser(r.GetUsers(), bob.self.GetId()) {
		t.Errorf("Alice's result: no user %d", bob.self.GetId())
	}
	for _, c := range []struct {
		name string
		u    *user
		st   *mtproto.TLUpdatesState
	}{{"Bob", bob, stBob}, {"Carol", carol, stCarol}} {
		m, users := waitNewMessage(ctx, t, c.u, answer)
		checkOtherChatReply(t, c.name+"'s update", m.GetReplyTo(), bob.self.GetId(), caption, false, mtproto.Predicate_messageMediaPhoto)
		if !hasUser(users, bob.self.GetId()) {
			t.Errorf("%s's update: no user %d", c.name, bob.self.GetId())
		}
		if m.GetMentioned() {
			t.Errorf("%s's update: mentioned", c.name)
		}
		if m.GetReplyTo().GetReplyMedia().GetPhoto_FLAGPHOTO().GetId() != sent.GetReplyTo().GetReplyMedia().GetPhoto_FLAGPHOTO().GetId() {
			t.Errorf("%s's update: another photo", c.name)
		}
		m, _ = historyMessage(ctx, t, c.u, chatPeer(c.u, chat), answer)
		checkOtherChatReply(t, c.name+"'s history", m.GetReplyTo(), bob.self.GetId(), caption, false, mtproto.Predicate_messageMediaPhoto)
		m, users = differenceMessage(ctx, t, c.u, c.st, answer)
		checkOtherChatReply(t, c.name+"'s difference", m.GetReplyTo(), bob.self.GetId(), caption, false, mtproto.Predicate_messageMediaPhoto)
		if !hasUser(users, bob.self.GetId()) {
			t.Errorf("%s's difference: no user %d", c.name, bob.self.GetId())
		}
	}

	// From the group to the private chat, with a quote Alice chose of
	// Carol's text: Android sends the start of the selection, here the
	// space before the word, with the selection trimmed of it.
	said := fmt.Sprintf("Carol says %d", randomID())
	sendMessageID(ctx, t, carol, chatPeer(carol, chat), said, randomID())
	inGroup, _ := historyMessage(ctx, t, alice, group, said)
	stBob = getState(ctx, t, bob)
	answer = fmt.Sprintf("about Carol %d", randomID())
	r, err = sendWithReply(ctx, alice, toBob, answer, mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
		ReplyToMsgId:  inGroup.GetId(),
		ReplyToPeerId: group,
		QuoteText:     wrapperspb.String("says"),
		QuoteOffset:   wrapperspb.Int32(5),
	}).To_InputReplyTo())
	if err != nil {
		t.Fatalf("messages.sendMessage with a reply to the group: %v", err)
	}
	checkOtherChatReply(t, "Alice's result", newMessageWith(r, answer).GetReplyTo(), carol.self.GetId(), "says", true, "")
	m, users := waitNewMessage(ctx, t, bob, answer)
	checkOtherChatReply(t, "Bob's update", m.GetReplyTo(), carol.self.GetId(), "says", true, "")
	if got := m.GetReplyTo().GetQuoteOffset().GetValue(); got != 6 {
		t.Errorf("Bob's update: quote offset %d, want 6", got)
	}
	if !hasUser(users, carol.self.GetId()) {
		t.Errorf("Bob's update: no user %d", carol.self.GetId())
	}
	m, users = historyMessage(ctx, t, bob, toAlice, answer)
	checkOtherChatReply(t, "Bob's history", m.GetReplyTo(), carol.self.GetId(), "says", true, "")
	if !hasUser(users, carol.self.GetId()) {
		t.Errorf("Bob's history: no user %d", carol.self.GetId())
	}
	m, _ = differenceMessage(ctx, t, bob, stBob, answer)
	checkOtherChatReply(t, "Bob's difference", m.GetReplyTo(), carol.self.GetId(), "says", true, "")

	// A message of another chat than the one named, and a quote that is
	// not in the message.
	if _, err = sendWithReply(ctx, alice, group, "wrong chat", mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
		ReplyToMsgId: inGroup.GetId(), ReplyToPeerId: toBob,
	}).To_InputReplyTo()); !rpcErrorIs(err, "REPLY_MESSAGE_ID_INVALID") {
		t.Errorf("a reply naming the wrong chat: %v, want REPLY_MESSAGE_ID_INVALID", err)
	}
	if _, err = sendWithReply(ctx, alice, group, "wrong quote", mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
		ReplyToMsgId: photo.GetId(), ReplyToPeerId: toBob, QuoteText: wrapperspb.String("not in the caption"),
	}).To_InputReplyTo()); !rpcErrorIs(err, "QUOTE_TEXT_INVALID") {
		t.Errorf("a quote not in the message: %v, want QUOTE_TEXT_INVALID", err)
	}

	checkMediaReplyInAnotherChat(ctx, t, alice, bob, carol, photo, caption, chat)
}

// waitNewMessages waits on u for the new messages with texts, in one update
// or in several, and returns them by text with the users of their updates.
func waitNewMessages(ctx context.Context, t *testing.T, u *user, texts ...string) (map[string]*mtproto.Message, []*mtproto.User) {
	t.Helper()
	got := make(map[string]*mtproto.Message, len(texts))
	var users []*mtproto.User
	for len(got) < len(texts) {
		us, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
			for _, text := range texts {
				if _, ok := got[text]; !ok && newMessageWith(us, text) != nil {
					return true
				}
			}
			return false
		})
		if err != nil {
			t.Fatalf("no new messages %q: %v", texts, err)
		}
		for _, text := range texts {
			if m := newMessageWith(us, text); m != nil {
				got[text] = m
			}
		}
		users = append(users, us.GetUsers()...)
	}
	return got, users
}

// checkMediaReplyInAnotherChat checks that a photo and an album Alice sends
// to chat with a reply to photo, Bob's message with caption in their
// private chat (messages.sendMedia and messages.sendMultiMedia), carry the
// same header as a text: every message of the album, for Alice and for
// Carol, who has not the original and whose updates are pushed.
func checkMediaReplyInAnotherChat(ctx context.Context, t *testing.T, alice, bob, carol *user, photo *mtproto.Message, caption string, chat int64) {
	t.Helper()
	toBob := peerOf(ctx, t, alice, bob)
	group := chatPeer(alice, chat)
	replyTo := func() *mtproto.InputReplyTo {
		return mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
			ReplyToMsgId: photo.GetId(), ReplyToPeerId: toBob,
		}).To_InputReplyTo()
	}
	check := func(where string, ms map[string]*mtproto.Message, users []*mtproto.User, texts ...string) {
		t.Helper()
		for _, text := range texts {
			m := ms[text]
			if m == nil {
				t.Errorf("%s: no message %q", where, text)
				continue
			}
			checkOtherChatReply(t, where, m.GetReplyTo(), bob.self.GetId(), caption, false, mtproto.Predicate_messageMediaPhoto)
		}
		if !hasUser(users, bob.self.GetId()) {
			t.Errorf("%s: no user %d", where, bob.self.GetId())
		}
	}

	single := fmt.Sprintf("a photo in answer %d", randomID())
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        group,
		Media:       uploadPhoto(ctx, t, alice),
		Message:     single,
		RandomId:    randomID(),
		ReplyTo:     replyTo(),
	})
	if err != nil {
		t.Fatalf("messages.sendMedia with a reply to the private chat: %v", err)
	}
	r := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	check("Alice's sendMedia result", map[string]*mtproto.Message{single: newMessageWith(r, single)}, r.GetUsers(), single)
	ms, users := waitNewMessages(ctx, t, carol, single)
	check("Carol's photo update", ms, users, single)

	first, second := fmt.Sprintf("album one %d", randomID()), fmt.Sprintf("album two %d", randomID())
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMultiMedia{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMultiMedia),
		Peer:        group,
		ReplyTo:     replyTo(),
		MultiMedia: []*mtproto.InputSingleMedia{
			mtproto.MakeTLInputSingleMedia(&mtproto.InputSingleMedia{Media: uploadPhoto(ctx, t, alice), RandomId: randomID(), Message: first}).To_InputSingleMedia(),
			mtproto.MakeTLInputSingleMedia(&mtproto.InputSingleMedia{Media: uploadPhoto(ctx, t, alice), RandomId: randomID(), Message: second}).To_InputSingleMedia(),
		},
	})
	if err != nil {
		t.Fatalf("messages.sendMultiMedia with a reply to the private chat: %v", err)
	}
	r = o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	check("Alice's sendMultiMedia result", map[string]*mtproto.Message{first: newMessageWith(r, first), second: newMessageWith(r, second)}, r.GetUsers(), first, second)
	ms, users = waitNewMessages(ctx, t, carol, first, second)
	check("Carol's album update", ms, users, first, second)
	for _, text := range []string{single, first, second} {
		m, users := historyMessage(ctx, t, carol, chatPeer(carol, chat), text)
		check("Carol's history", map[string]*mtproto.Message{text: m}, users, text)
	}

	// The message must be of the chat named.
	_, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        toBob,
		Media:       uploadPhoto(ctx, t, alice),
		Message:     "wrong chat",
		RandomId:    randomID(),
		ReplyTo: mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
			ReplyToMsgId: photo.GetId(), ReplyToPeerId: group,
		}).To_InputReplyTo(),
	})
	if !rpcErrorIs(err, "REPLY_MESSAGE_ID_INVALID") {
		t.Errorf("messages.sendMedia with a reply naming the wrong chat: %v, want REPLY_MESSAGE_ID_INVALID", err)
	}
}

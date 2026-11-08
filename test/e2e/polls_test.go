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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func pollText(u *user, s string) *mtproto.TextWithEntities {
	return &mtproto.TextWithEntities{Constructor: u.c.Constructor(mtproto.Predicate_textWithEntities), Text: s, Entities: []*mtproto.MessageEntity{}}
}

func pollAnswer(u *user, s string) *mtproto.PollAnswer {
	return &mtproto.PollAnswer{Constructor: u.c.Constructor(mtproto.Predicate_inputPollAnswer), Text_TEXTWITHENTITIES: pollText(u, s)}
}

// inputPoll is a poll of three answers, options 0, 1, 2 as the server gives
// them to answers without one.
func inputPoll(u *user, set func(*mtproto.Poll)) *mtproto.InputMedia {
	p := &mtproto.Poll{
		Constructor:               u.c.Constructor(mtproto.Predicate_poll),
		Question_TEXTWITHENTITIES: pollText(u, "Pick one"),
		Answers:                   []*mtproto.PollAnswer{pollAnswer(u, "A"), pollAnswer(u, "B"), pollAnswer(u, "C")},
	}
	if set != nil {
		set(p)
	}
	return &mtproto.InputMedia{Constructor: u.c.Constructor(mtproto.Predicate_inputMediaPoll), Poll: p}
}

func sendPollMedia(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, media *mtproto.InputMedia) (*mtproto.Updates, int32) {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        peer,
		Media:       media,
		RandomId:    randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMedia: %v", err)
	}
	r := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	for _, x := range r.GetUpdates() {
		if p := x.GetPredicateName(); p == mtproto.Predicate_updateNewMessage || p == mtproto.Predicate_updateNewChannelMessage {
			return r, x.GetMessage_MESSAGE().GetId()
		}
	}
	t.Fatal("messages.sendMedia reply without the new message")
	return nil, 0
}

func vote(ctx context.Context, u *user, peer *mtproto.InputPeer, msgId int32, options ...string) (*mtproto.Updates, error) {
	opts := [][]byte{}
	for _, o := range options {
		opts = append(opts, []byte(o))
	}
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendVote{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendVote),
		Peer:        peer,
		MsgId:       msgId,
		Options:     opts,
	})
	if err != nil {
		return nil, err
	}
	return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
}

// pollUpdateOf finds updateMessagePoll in updates.
func pollUpdateOf(updates []*mtproto.Update) *mtproto.Update {
	for _, u := range updates {
		if u.GetPredicateName() == mtproto.Predicate_updateMessagePoll {
			return u
		}
	}
	return nil
}

func chosenOptions(r *mtproto.PollResults) []string {
	var out []string
	for _, a := range r.GetResults() {
		if a.GetChosen() {
			out = append(out, string(a.GetOption()))
		}
	}
	return out
}

func votersOf(r *mtproto.PollResults, option string) int32 {
	for _, a := range r.GetResults() {
		if string(a.GetOption()) == option {
			return a.GetVoters_FLAGINT32().GetValue()
		}
	}
	return -1
}

func historyPoll(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, msgId int32) *mtproto.MessageMedia {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer,
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	h, ok := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	})
	if !ok {
		t.Fatalf("messages.getHistory returned %T", o)
	}
	for _, m := range h.To_Messages_Messages().GetMessages() {
		if m.GetId() == msgId {
			return m.GetMedia()
		}
	}
	t.Fatalf("history without message %d", msgId)
	return nil
}

// TestPollPrivateChat (task 062): a poll in a private chat. The sender is
// its creator; a vote reaches the creator without the voter's choice and the
// voter with it; a retract is seen as well; the results come for each viewer
// in the history and through getDifference; a vote repeated changes nothing.
func TestPollPrivateChat(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	aliceTwo := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	stateTwo := getState(ctx, t, aliceTwo)

	reply, msgId := sendPollMedia(ctx, t, alice, inputPeerOf(ctx, t, alice, bob), inputPoll(alice, nil))
	var sent *mtproto.Message
	for _, u := range reply.GetUpdates() {
		if p := u.GetPredicateName(); p == mtproto.Predicate_updateNewMessage {
			sent = u.GetMessage_MESSAGE()
		}
	}
	if !sent.GetMedia().GetPoll().GetCreator() || sent.GetMedia().GetPoll().GetId() == 0 {
		t.Fatalf("sender's reply: %+v", sent.GetMedia().GetPoll())
	}
	var bobsId int32
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		if u.GetMessage_MESSAGE().GetMedia().GetPoll().GetId() == sent.GetMedia().GetPoll().GetId() {
			bobsId = u.GetMessage_MESSAGE().GetId()
			return true
		}
		return false
	})

	toAlice := inputPeerOf(ctx, t, bob, alice)
	r, err := vote(ctx, bob, toAlice, bobsId, "1")
	if err != nil {
		t.Fatalf("messages.sendVote: %v", err)
	}
	mine := pollUpdateOf(r.GetUpdates())
	if mine == nil || len(chosenOptions(mine.GetResults())) != 1 || mine.GetResults().GetMin() {
		t.Fatalf("voter's answer: %+v", mine)
	}

	waitUpdate(ctx, t, alice, mtproto.Predicate_updateMessagePoll, func(u *mtproto.Update) bool {
		if u.GetPollId() != sent.GetMedia().GetPoll().GetId() || votersOf(u.GetResults(), "1") != 1 {
			return false
		}
		if len(chosenOptions(u.GetResults())) != 0 {
			t.Errorf("the creator's update names the voter's choice as its own")
		}
		return true
	})

	// The same vote: nothing new, no error.
	if r, err = vote(ctx, bob, toAlice, bobsId, "1"); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if !equalOptions(chosenOptions(pollUpdateOf(r.GetUpdates()).GetResults()), []string{"1"}) {
		t.Fatalf("repeat answer: %+v", r.GetUpdates())
	}

	// Each viewer's history.
	bm := historyPoll(ctx, t, bob, toAlice, bobsId)
	if !equalOptions(chosenOptions(bm.GetResults()), []string{"1"}) || bm.GetResults().GetTotalVoters().GetValue() != 1 {
		t.Fatalf("bob's history: %+v", bm.GetResults())
	}
	if am := historyPoll(ctx, t, alice, inputPeerOf(ctx, t, alice, bob), msgId); len(chosenOptions(am.GetResults())) != 0 || am.GetResults().GetTotalVoters().GetValue() != 1 || !am.GetPoll().GetCreator() {
		t.Fatalf("alice's history: %+v", am.GetResults())
	}

	// Alice's second device, offline: the difference carries the state.
	found := pollUpdateOf(otherUpdates(ctx, t, aliceTwo, stateTwo))
	if found == nil || found.GetPollId() != sent.GetMedia().GetPoll().GetId() || votersOf(found.GetResults(), "1") != 1 {
		t.Fatalf("difference: %+v", found)
	}

	// Retract.
	if r, err = vote(ctx, bob, toAlice, bobsId); err != nil {
		t.Fatalf("retract: %v", err)
	}
	if res := pollUpdateOf(r.GetUpdates()).GetResults(); len(chosenOptions(res)) != 0 || res.GetTotalVoters().GetValue() != 0 {
		t.Fatalf("after retract: %+v", res)
	}

	// A bad option.
	if _, err = vote(ctx, bob, toAlice, bobsId, "7"); err == nil {
		t.Fatal("OPTION_INVALID expected")
	}
}

// A quiz: the right answer and the solution only after voting; one vote,
// not changed afterwards; a wrong answer shows which was right.
func TestPollQuiz(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	chatId := createChat(ctx, t, alice, "quiz", bob, carol)
	getState(ctx, t, bob)
	getState(ctx, t, carol)
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChat), ChatId: chatId}
	}
	m := inputPoll(alice, func(p *mtproto.Poll) { p.Quiz = true })
	m.CorrectAnswers_FLAGVECTORINT32 = []int32{1}
	m.Solution = wrapperspb.String("Because")
	_, msgId := sendPollMedia(ctx, t, alice, peer(alice), m)
	bobId := pollMessageIn(ctx, t, bob, peer(bob))

	if carolMedia := historyPoll(ctx, t, carol, peer(carol), pollMessageIn(ctx, t, carol, peer(carol))); carolMedia.GetResults().GetSolution() != nil {
		t.Fatalf("carol sees the solution before voting: %+v", carolMedia.GetResults())
	}
	r, err := vote(ctx, bob, peer(bob), bobId, "0")
	if err != nil {
		t.Fatalf("vote: %v", err)
	}
	res := pollUpdateOf(r.GetUpdates()).GetResults()
	var right bool
	for _, a := range res.GetResults() {
		if string(a.GetOption()) == "1" {
			right = a.GetCorrect()
		}
	}
	if !right || res.GetSolution().GetValue() != "Because" {
		t.Fatalf("after voting: %+v", res)
	}
	// Carol has not voted: she gets the counts and nothing of the answer.
	waitUpdate(ctx, t, carol, mtproto.Predicate_updateMessagePoll, func(u *mtproto.Update) bool {
		if !u.GetResults().GetMin() || votersOf(u.GetResults(), "0") != 1 {
			return false
		}
		if u.GetResults().GetSolution() != nil {
			t.Errorf("min update has the solution")
		}
		for _, a := range u.GetResults().GetResults() {
			if a.GetCorrect() || a.GetChosen() {
				t.Errorf("min update names an answer: %+v", a)
			}
		}
		return true
	})
	if _, err = vote(ctx, bob, peer(bob), bobId, "1"); err == nil {
		t.Fatal("REVOTE_NOT_ALLOWED expected")
	}
	// The creator closes it; the voter's next look shows it closed and
	// votes are refused.
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesEditMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_editMessage),
		Peer:        peer(alice),
		Id:          msgId,
		Media:       inputPoll(alice, func(p *mtproto.Poll) { p.Closed, p.Quiz = true, true }),
	}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err = vote(ctx, carol, peer(carol), pollMessageIn(ctx, t, carol, peer(carol)), "0"); err == nil {
		t.Fatal("MESSAGE_POLL_CLOSED expected")
	}
	if got := historyPoll(ctx, t, bob, peer(bob), bobId); !got.GetPoll().GetClosed() {
		t.Fatalf("not closed: %+v", got.GetPoll())
	}
}

// mediaMessageIn is the id of the last message with media of predicate in
// u's dialog with peer. u's copy arrives with an inbox event after the
// sender's answer, so it is waited for.
func mediaMessageIn(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, predicate string) int32 {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
			Peer:        peer,
			Limit:       5,
		})
		if err != nil {
			t.Fatalf("messages.getHistory: %v", err)
		}
		for _, m := range o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages().GetMessages() {
			if m.GetMedia().GetPredicateName() == predicate {
				return m.GetId()
			}
		}
	}
	t.Fatalf("no %s in the dialog", predicate)
	return 0
}

func pollMessageIn(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) int32 {
	t.Helper()
	return mediaMessageIn(ctx, t, u, peer, mtproto.Predicate_messageMediaPoll)
}

// A poll in a supergroup: members vote, the others get min; the channel
// keeps the count through parallel votes.
func TestPollSupergroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	_, ch := createChannel(ctx, t, alice, "polls", true)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	invite := channelInviteHash(ctx, t, alice, input(alice))
	for _, u := range []*user{bob, carol} {
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E), Hash: invite,
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}
	getState(ctx, t, alice)
	_, msgId := sendPollMedia(ctx, t, alice, peer(alice), inputPoll(alice, nil))

	errs := make(chan error, 2)
	for _, u := range []*user{bob, carol} {
		go func(u *user) {
			_, err := vote(ctx, u, peer(u), msgId, "2")
			errs <- err
		}(u)
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("vote: %v", err)
		}
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateMessagePoll, func(u *mtproto.Update) bool {
		return votersOf(u.GetResults(), "2") >= 1
	})
	if got := historyPoll(ctx, t, alice, peer(alice), msgId); got.GetResults().GetTotalVoters().GetValue() != 2 {
		t.Fatalf("total %+v", got.GetResults())
	}
}

// A poll past its close_date is closed for votes and for readers before
// the sweep, and the sweep closes it in the row: updateMessagePoll with the
// poll closed reaches the members.
func TestPollCloseDate(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)
	closes := int32(time.Now().Unix()) + 2
	_, _ = sendPollMedia(ctx, t, alice, inputPeerOf(ctx, t, alice, bob), inputPoll(alice, func(p *mtproto.Poll) { p.CloseDate = wrapperspb.Int32(closes) }))
	toAlice := inputPeerOf(ctx, t, bob, alice)
	bobsId := pollMessageIn(ctx, t, bob, toAlice)
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateMessagePoll, func(u *mtproto.Update) bool {
		return u.GetPoll().GetClosed()
	})
	if _, err := vote(ctx, bob, toAlice, bobsId, "0"); err == nil {
		t.Fatal("MESSAGE_POLL_CLOSED expected")
	}
}

func equalOptions(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func todoItem(u *user, id int32, s string) *mtproto.TodoItem {
	return &mtproto.TodoItem{Constructor: u.c.Constructor(mtproto.Predicate_todoItem), Id: id, Title: pollText(u, s)}
}

// A checklist in a basic group: the members tick items, the state reaches
// every copy, the service message says what changed; only the list's
// author appends when others may not.
func TestTodoList(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	chatId := createChat(ctx, t, alice, "list", bob)
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChat), ChatId: chatId}
	}
	media := &mtproto.InputMedia{Constructor: alice.c.Constructor(mtproto.Predicate_inputMediaTodo), Todo: &mtproto.TodoList{
		Constructor:       alice.c.Constructor(mtproto.Predicate_todoList),
		OthersCanComplete: true,
		Title:             pollText(alice, "Trip"),
		List:              []*mtproto.TodoItem{todoItem(alice, 1, "Tickets"), todoItem(alice, 2, "Hotel")},
	}}
	_, msgId := sendPollMedia(ctx, t, alice, peer(alice), media)

	bobId := mediaMessageIn(ctx, t, bob, peer(bob), mtproto.Predicate_messageMediaToDo)
	o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesToggleTodoCompleted{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_toggleTodoCompleted),
		Peer:        peer(bob), MsgId: bobId, Completed: []int32{1}, Incompleted: []int32{},
	})
	if err != nil {
		t.Fatalf("toggle: %v", err)
	}
	var edited, service bool
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateEditMessage:
			edited = len(u.GetMessage_MESSAGE().GetMedia().GetCompletions()) == 1
		case mtproto.Predicate_updateNewMessage:
			service = u.GetMessage_MESSAGE().GetAction().GetPredicateName() == mtproto.Predicate_messageActionTodoCompletions
		}
	}
	if !edited || !service {
		t.Fatalf("reply: edited %v, service %v", edited, service)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateEditMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetId() == msgId && len(u.GetMessage_MESSAGE().GetMedia().GetCompletions()) == 1
	})

	// Others may not append here.
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesAppendTodoList{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_appendTodoList),
		Peer:        peer(bob), MsgId: bobId, List: []*mtproto.TodoItem{todoItem(bob, 9, "x")},
	}); err == nil {
		t.Fatal("append by a stranger accepted")
	}
}

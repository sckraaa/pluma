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
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	msg_helper "github.com/teamgram/teamgram-server/app/messenger/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/polls"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

func pollText(s string) *mtproto.TextWithEntities {
	return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: s, Entities: []*mtproto.MessageEntity{}}).To_TextWithEntities()
}

// pollMessage is a message carrying a new poll of three answers.
func pollMessage(t *testing.T, from int64, peer *mtproto.Peer, set func(*mtproto.Poll)) *mtproto.Message {
	t.Helper()
	answers := []*mtproto.PollAnswer{}
	for _, s := range []string{"A", "B", "C"} {
		answers = append(answers, mtproto.MakeTLInputPollAnswer(&mtproto.PollAnswer{Text_TEXTWITHENTITIES: pollText(s)}).To_PollAnswer())
	}
	p := mtproto.MakeTLPoll(&mtproto.Poll{Question_TEXTWITHENTITIES: pollText("Which?"), Answers: answers}).To_Poll()
	if set != nil {
		set(p)
	}
	c, err := polls.Check(mtproto.MakeTLInputMediaPoll(&mtproto.InputMedia{Poll: p}).To_InputMedia(), time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	m := textMessage(from, peer, "")
	m.Media = polls.NewMedia(c, nil, nil)
	return m
}

// sentPoll sends a poll to a group and returns the poll id and the copy id
// of the sender and of each member.
func (w *world) sentPoll(from, chat int64, set func(*mtproto.Poll)) (int64, map[int64]int32) {
	w.t.Helper()
	r, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		UserId: from, AuthKeyId: from * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat,
		Message: []*msg.OutboxMessage{outbox(pollMessage(w.t, from, mtproto.MakePeerChat(chat), set))},
	})
	if err != nil {
		w.t.Fatal(err)
	}
	var id int64
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			id = u.GetMessage_MESSAGE().GetMedia().GetPoll().GetId()
		}
	}
	if id == 0 {
		w.t.Fatal("no poll id in the reply")
	}
	w.pub.drain()
	copies := map[int64]int32{}
	for _, u := range w.chats.members[chat] {
		for _, m := range w.messages(u) {
			if storedMessage(w.t, m).GetMedia().GetPoll().GetId() == id {
				copies[u] = m.Id
			}
		}
	}
	return id, copies
}

func (w *world) vote(user, chat int64, msgId int32, options ...[]byte) error {
	_, err := w.rpc.MsgSendVote(context.Background(), &msg.TLMsgSendVote{
		UserId: user, AuthKeyId: user * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, MsgId: msgId, Options: options,
	})
	return err
}

func (w *world) pollVotes(id int64) []polls.Vote {
	w.t.Helper()
	var rows []polls.VoteRow
	if err := w.d.Mysql.DB.QueryRowsPartial(context.Background(), &rows, "SELECT poll_id, user_id, opt, `date` FROM poll_votes WHERE poll_id = ?", id); err != nil {
		w.t.Fatal(err)
	}
	out := make([]polls.Vote, len(rows))
	for i, r := range rows {
		out[i] = polls.Vote{UserId: r.UserId, Option: r.Opt, Date: r.Date}
	}
	return out
}

// TestPollParallelVotes: many voters at once keep the totals right, each
// vote is idempotent, a change of option replaces the old one.
func TestPollParallelVotes(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	members := []int64{newUser(), newUser(), newUser(), newUser(), newUser(), newUser(), newUser(), newUser()}
	chat := newChat()
	w.chats.members[chat] = members
	id, copies := w.sentPoll(members[0], chat, nil)

	var wg sync.WaitGroup
	for _, u := range members {
		wg.Add(2)
		// Each voter twice at once: the second is the same vote.
		for range 2 {
			go func(u int64) {
				defer wg.Done()
				if err := w.vote(u, chat, copies[u], polls.OptionOf(0)); err != nil {
					t.Errorf("vote %d: %v", u, err)
				}
			}(u)
		}
	}
	wg.Wait()
	w.pub.drain()
	if rows := w.pollVotes(id); len(rows) != len(members) {
		t.Fatalf("%d vote rows for %d voters", len(rows), len(members))
	}
	if err := w.vote(members[1], chat, copies[members[1]], polls.OptionOf(2)); err != nil {
		t.Fatal(err)
	}
	var mine []polls.Vote
	for _, v := range w.pollVotes(id) {
		if v.UserId == members[1] {
			mine = append(mine, v)
		}
	}
	if len(mine) != 1 || string(mine[0].Option) != string(polls.OptionOf(2)) {
		t.Fatalf("changed vote: %+v", mine)
	}
	if err := w.vote(members[1], chat, copies[members[1]]); err != nil {
		t.Fatal(err)
	}
	if len(w.pollVotes(id)) != len(members)-1 {
		t.Fatal("retract did not remove the vote")
	}
	for _, u := range members {
		w.checkBox(u)
	}
}

// TestPollQuizAndClose: a poll refusing a change does; closing stops the
// votes; two sweeps at once close a poll past its date in the row.
func TestPollQuizAndClose(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b, c := newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{a, b, c}
	id, copies := w.sentPoll(a, chat, func(p *mtproto.Poll) { p.RevotingDisabled = true })
	if err := w.vote(b, chat, copies[b], polls.OptionOf(1)); err != nil {
		t.Fatal(err)
	}
	if err := w.vote(b, chat, copies[b], polls.OptionOf(2)); err == nil {
		t.Fatal("revote accepted on a revoting_disabled poll")
	}
	if _, err := w.rpc.MsgClosePoll(context.Background(), &msg.TLMsgClosePoll{
		UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, MsgId: copies[a],
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.vote(c, chat, copies[c], polls.OptionOf(0)); err == nil {
		t.Fatal("vote accepted on a closed poll")
	}
	if _, err := w.rpc.MsgClosePoll(context.Background(), &msg.TLMsgClosePoll{
		UserId: b, AuthKeyId: b * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, MsgId: copies[b],
	}); err == nil {
		t.Fatal("a member closed another's poll")
	}
	// A date a little ahead: Consume stores a poll already past it as closed.
	// Past the date a vote is refused whether or not a sweep got there.
	dueId, dueCopies := w.sentPoll(a, chat, func(p *mtproto.Poll) { p.CloseDate = wrapperspb.Int32(int32(time.Now().Unix()) + 1) })
	if id == 0 || dueId == 0 {
		t.Fatal("no poll ids")
	}
	time.Sleep(2 * time.Second)
	if err := w.vote(c, chat, dueCopies[c], polls.OptionOf(0)); err == nil {
		t.Fatal("vote accepted past close_date")
	}
	for range 2 {
		t.Cleanup(msg_helper.StartPollSweeper(w.d).Stop)
	}
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		var closed []bool
		if err := w.d.Mysql.DB.QueryRowsPartial(context.Background(), &closed, "SELECT closed FROM polls WHERE id = ?", dueId); err != nil {
			t.Fatal(err)
		}
		if len(closed) == 1 && closed[0] {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sweep did not close the poll")
		}
	}
	w.pub.drain()
	for _, u := range []int64{a, b, c} {
		w.checkBox(u)
	}
}

// TestTodoParallelToggle: two members ticking different items at once lose
// neither, every copy ends with both ticks and with the service messages.
func TestTodoParallelToggle(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	a, b, c := newUser(), newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{a, b, c}
	items := []*mtproto.TodoItem{
		mtproto.MakeTLTodoItem(&mtproto.TodoItem{Id: 1, Title: pollText("one")}).To_TodoItem(),
		mtproto.MakeTLTodoItem(&mtproto.TodoItem{Id: 2, Title: pollText("two")}).To_TodoItem(),
	}
	m := textMessage(a, mtproto.MakePeerChat(chat), "")
	m.Media = polls.TodoMedia(mtproto.MakeTLTodoList(&mtproto.TodoList{
		Title: pollText("list"), List: items, OthersCanComplete: true, OthersCanAppend: true,
	}).To_TodoList(), nil)
	if _, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		UserId: a, AuthKeyId: a * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, Message: []*msg.OutboxMessage{outbox(m)},
	}); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()
	copyOf := func(u int64) int32 {
		for _, r := range w.messages(u) {
			if storedMessage(t, r).GetMedia().GetPredicateName() == mtproto.Predicate_messageMediaToDo {
				return r.Id
			}
		}
		t.Fatalf("user %d: no checklist", u)
		return 0
	}
	var wg sync.WaitGroup
	for i, u := range []int64{a, b} {
		wg.Add(1)
		go func(i int, u int64) {
			defer wg.Done()
			if _, err := w.rpc.MsgToggleTodoCompleted(context.Background(), &msg.TLMsgToggleTodoCompleted{
				UserId: u, AuthKeyId: u * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, MsgId: copyOf(u),
				Completed: []int32{int32(i + 1)}, Incompleted: []int32{},
			}); err != nil {
				t.Errorf("toggle by %d: %v", u, err)
			}
		}(i, u)
	}
	wg.Wait()
	w.pub.drain()
	for _, u := range []int64{a, b, c} {
		var got *mtproto.MessageMedia
		var services int
		for _, r := range w.messages(u) {
			m := storedMessage(t, r)
			if m.GetMedia().GetPredicateName() == mtproto.Predicate_messageMediaToDo {
				got = m.GetMedia()
			}
			if m.GetAction().GetPredicateName() == mtproto.Predicate_messageActionTodoCompletions {
				services++
			}
		}
		if len(got.GetCompletions()) != 2 {
			t.Errorf("user %d: completions %+v", u, got.GetCompletions())
		}
		if services != 2 {
			t.Errorf("user %d: %d service messages", u, services)
		}
		w.checkBox(u)
	}
}

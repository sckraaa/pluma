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

package polls

import (
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

func text(s string) *mtproto.TextWithEntities {
	return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: s, Entities: []*mtproto.MessageEntity{}}).To_TextWithEntities()
}

func answer(s string, o string) *mtproto.PollAnswer {
	return mtproto.MakeTLPollAnswer(&mtproto.PollAnswer{Text_TEXTWITHENTITIES: text(s), Option: []byte(o)}).To_PollAnswer()
}

func inputPoll(p *mtproto.Poll) *mtproto.InputMedia {
	return mtproto.MakeTLInputMediaPoll(&mtproto.InputMedia{Poll: p}).To_InputMedia()
}

func poll(flags func(*mtproto.Poll)) *mtproto.Poll {
	p := mtproto.MakeTLPoll(&mtproto.Poll{
		Question_TEXTWITHENTITIES: text("Why?"),
		Answers:                   []*mtproto.PollAnswer{answer("A", "a"), answer("B", "b"), answer("C", "c")},
	}).To_Poll()
	if flags != nil {
		flags(p)
	}
	return p
}

// A quiz has one right answer unless it is a multiple choice; the errors
// are the method's.
func TestCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   func() *mtproto.InputMedia
		err  error
	}{
		{"two answers ok", func() *mtproto.InputMedia { return inputPoll(poll(nil)) }, nil},
		{"one answer", func() *mtproto.InputMedia {
			return inputPoll(poll(func(p *mtproto.Poll) { p.Answers = p.Answers[:1] }))
		}, mtproto.ErrPollAnswersInvalid},
		{"open answers from one", func() *mtproto.InputMedia {
			return inputPoll(poll(func(p *mtproto.Poll) { p.Answers = p.Answers[:1]; p.OpenAnswers = true }))
		}, nil},
		{"blank answer", func() *mtproto.InputMedia {
			return inputPoll(poll(func(p *mtproto.Poll) { p.Answers[0] = answer("", "a") }))
		}, mtproto.ErrPollAnswerInvalid},
		{"duplicate option", func() *mtproto.InputMedia {
			return inputPoll(poll(func(p *mtproto.Poll) { p.Answers[0] = answer("A", "b") }))
		}, mtproto.ErrPollOptionDuplicate},
		{"blank question", func() *mtproto.InputMedia {
			return inputPoll(poll(func(p *mtproto.Poll) { p.Question_TEXTWITHENTITIES = text("") }))
		}, mtproto.ErrPollQuestionInvalid},
		{"quiz without correct", func() *mtproto.InputMedia {
			return inputPoll(poll(func(p *mtproto.Poll) { p.Quiz = true }))
		}, mtproto.ErrQuizCorrectAnswersEmpty},
		{"quiz with two correct", func() *mtproto.InputMedia {
			m := inputPoll(poll(func(p *mtproto.Poll) { p.Quiz = true }))
			m.CorrectAnswers_FLAGVECTORINT32 = []int32{0, 1}
			return m
		}, mtproto.ErrQuizCorrectAnswersTooMuch},
		{"quiz two correct multiple", func() *mtproto.InputMedia {
			m := inputPoll(poll(func(p *mtproto.Poll) { p.Quiz, p.MultipleChoice = true, true }))
			m.CorrectAnswers_FLAGVECTORINT32 = []int32{0, 1}
			return m
		}, nil},
		{"quiz correct out of range", func() *mtproto.InputMedia {
			m := inputPoll(poll(func(p *mtproto.Poll) { p.Quiz = true }))
			m.CorrectAnswers_FLAGVECTORINT32 = []int32{9}
			return m
		}, mtproto.ErrQuizCorrectAnswerInvalid},
		{"correct without quiz", func() *mtproto.InputMedia {
			m := inputPoll(poll(nil))
			m.CorrectAnswers_FLAGVECTORINT32 = []int32{0}
			return m
		}, mtproto.ErrQuizCorrectAnswerInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Check(tc.in(), 1000); !errors.Is(err, tc.err) {
				t.Fatalf("got %v, want %v", err, tc.err)
			}
		})
	}
}

// An input answer carries no option: each gets its position.
func TestCheckGivesOptions(t *testing.T) {
	in := inputPoll(poll(nil))
	for i, a := range in.Poll.Answers {
		in.Poll.Answers[i] = mtproto.MakeTLInputPollAnswer(&mtproto.PollAnswer{Text_TEXTWITHENTITIES: a.Text_TEXTWITHENTITIES}).To_PollAnswer()
	}
	c, err := Check(in, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range c.Poll.GetAnswers() {
		if string(a.GetOption()) != string(OptionOf(int32(i))) || a.GetPredicateName() != mtproto.Predicate_pollAnswer {
			t.Errorf("answer %d: %q %s", i, a.GetOption(), a.GetPredicateName())
		}
	}
}

// A close_period gives the date the poll closes.
func TestClosePeriodSetsDate(t *testing.T) {
	c, err := Check(inputPoll(poll(func(p *mtproto.Poll) { p.ClosePeriod = wrapperspb.Int32(60) })), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if c.Poll.GetCloseDate().GetValue() != 1060 {
		t.Fatalf("close_date = %d", c.Poll.GetCloseDate().GetValue())
	}
	if _, err = Check(inputPoll(poll(func(p *mtproto.Poll) { p.ClosePeriod = wrapperspb.Int32(ClosePeriodMax + 1) })), 1000); err == nil {
		t.Fatal("close_period over the limit accepted")
	}
}

func record(flags func(*mtproto.Poll)) *Record {
	return &Record{Id: 7, Creator: 1, Poll: poll(flags), Correct: [][]byte{[]byte("b")},
		Solution: wrapperspb.String("because")}
}

func votes(pairs ...any) []Vote {
	var out []Vote
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Vote{UserId: int64(pairs[i].(int)), Option: []byte(pairs[i+1].(string)), Date: int64(10 + i)})
	}
	return out
}

func resultsOf(r *mtproto.PollResults) (voters, chosen map[string]int32, correct map[string]bool) {
	voters, chosen, correct = map[string]int32{}, map[string]int32{}, map[string]bool{}
	for _, a := range r.GetResults() {
		voters[string(a.GetOption())] = a.GetVoters_FLAGINT32().GetValue()
		if a.GetChosen() {
			chosen[string(a.GetOption())] = 1
		}
		correct[string(a.GetOption())] = a.GetCorrect()
	}
	return
}

// Only one who voted sees the right answer and the solution; a min view
// has neither, and no chosen.
func TestViewQuiz(t *testing.T) {
	rec := record(func(p *mtproto.Poll) { p.Quiz = true })
	rows := votes(2, "a", 3, "b")
	// A voter.
	_, r := rec.View(Tally(rows, 2), 2, Full, 100)
	v, c, ok := resultsOf(r)
	if v["a"] != 1 || v["b"] != 1 || c["a"] != 1 || !ok["b"] || r.GetTotalVoters().GetValue() != 2 {
		t.Fatalf("voter: %v %v %v %v", v, c, ok, r.GetTotalVoters())
	}
	if r.GetSolution().GetValue() != "because" {
		t.Fatalf("voter's solution: %v", r.GetSolution())
	}
	// Someone who did not vote.
	_, r = rec.View(Tally(rows, 9), 9, Full, 100)
	v, c, ok = resultsOf(r)
	if ok["b"] || r.GetSolution() != nil || len(c) != 0 {
		t.Fatalf("not a voter: %v %v %v", v, ok, r.GetSolution())
	}
	// Min: counts, nothing personal or revealing.
	_, r = rec.View(Tally(rows, 2), 2, Min, 100)
	if !r.GetMin() {
		t.Fatal("not min")
	}
	v, c, ok = resultsOf(r)
	if len(c) != 0 || ok["b"] || r.GetSolution() != nil || v["a"] != 1 {
		t.Fatalf("min: %v %v %v", c, ok, r.GetSolution())
	}
	// Once closed everyone sees the right answer, in a min view too.
	rec.Closed = true
	_, r = rec.View(Tally(rows, 9), 9, Min, 100)
	if _, _, ok = resultsOf(r); !ok["b"] {
		t.Fatalf("closed min: %v", ok)
	}
}

// A poll hiding its results until closed: others see only their own choice,
// and no counts; its creator sees them; closing shows them all.
func TestViewHideResultsUntilClose(t *testing.T) {
	rec := record(func(p *mtproto.Poll) { p.HideResultsUntilClose = true })
	rows := votes(2, "a", 3, "b")
	_, r := rec.View(Tally(rows, 2), 2, Full, 100)
	if r.GetTotalVoters() != nil || len(r.GetResults()) != 1 || !r.GetResults()[0].GetChosen() || r.GetResults()[0].GetVoters_FLAGINT32() != nil {
		t.Fatalf("voter: %+v", r)
	}
	_, r = rec.View(Tally(rows, 9), 9, Full, 100)
	if r.GetTotalVoters() != nil || len(r.GetResults()) != 0 {
		t.Fatalf("not a voter: %+v", r)
	}
	if _, r = rec.View(Tally(rows, 1), 1, Full, 100); r.GetTotalVoters().GetValue() != 2 || len(r.GetResults()) != 3 {
		t.Fatalf("creator: %+v", r)
	}
	rec.Closed = true
	if _, r = rec.View(Tally(rows, 9), 9, Min, 100); r.GetTotalVoters().GetValue() != 2 {
		t.Fatalf("closed: %+v", r)
	}
}

// A closing date in the past closes the poll for readers before the sweep.
func TestIsClosed(t *testing.T) {
	rec := &Record{Poll: poll(nil), CloseDate: 50}
	if rec.IsClosed(49) || !rec.IsClosed(50) {
		t.Fatal("close date not honoured")
	}
	p, _ := rec.View(Tally(nil, 1), 1, Full, 60)
	if !p.GetClosed() {
		t.Fatal("view of a past-date poll not closed")
	}
}

// The poll's hash changes with its definition and its closing, never with
// the votes or the viewer; it is not zero.
func TestHash(t *testing.T) {
	rec := record(nil)
	p1, _ := rec.View(Tally(votes(2, "a"), 2), 2, Full, 100)
	p2, _ := rec.View(Tally(nil, 9), 9, Full, 100)
	if p1.GetHash() == 0 || p1.GetHash() != p2.GetHash() {
		t.Fatalf("hash %d vs %d", p1.GetHash(), p2.GetHash())
	}
	rec.Closed = true
	p3, _ := rec.View(Tally(nil, 9), 9, Full, 100)
	if p3.GetHash() == p1.GetHash() {
		t.Fatal("hash did not change on close")
	}
}

// A public poll names recent voters, the most recent first, only there;
// has_unread_votes is for its creator.
func TestViewPublicVoters(t *testing.T) {
	rec := record(func(p *mtproto.Poll) { p.PublicVoters = true })
	rec.HasUnread = true
	rows := []Vote{{UserId: 2, Option: []byte("a"), Date: 10}, {UserId: 3, Option: []byte("a"), Date: 20}}
	_, r := rec.View(Tally(rows, 1), 1, Full, 100)
	if !r.GetHasUnreadVotes() {
		t.Fatal("creator without has_unread_votes")
	}
	rv := r.GetRecentVoters_FLAGVECTORPEER()
	if len(rv) != 2 || rv[0].GetUserId() != 3 {
		t.Fatalf("recent voters %+v", rv)
	}
	if _, r = rec.View(Tally(rows, 2), 2, Full, 100); r.GetHasUnreadVotes() {
		t.Fatal("has_unread_votes for a non-creator")
	}
	anon := record(nil)
	_, r = anon.View(Tally(rows, 1), 1, Full, 100)
	if r.GetRecentVoters_FLAGVECTORPEER() != nil || r.GetResults()[0].GetRecentVoters() != nil && len(r.GetResults()[0].GetRecentVoters()) != 0 {
		t.Fatalf("anonymous poll names voters: %+v", r)
	}
}

func TestVoteOptions(t *testing.T) {
	p := poll(nil)
	if got, err := VoteOptions(p, [][]byte{[]byte("a")}); err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := VoteOptions(p, [][]byte{[]byte("z")}); !errors.Is(err, mtproto.ErrOptionInvalid) {
		t.Fatalf("invalid option: %v", err)
	}
	if _, err := VoteOptions(p, [][]byte{[]byte("a"), []byte("b")}); !errors.Is(err, mtproto.ErrOptionsTooMuch) {
		t.Fatalf("two options: %v", err)
	}
	mp := poll(func(p *mtproto.Poll) { p.MultipleChoice = true })
	got, err := VoteOptions(mp, [][]byte{[]byte("b"), []byte("a"), []byte("b")})
	if err != nil || len(got) != 2 || string(got[0]) != "a" {
		t.Fatalf("multiple: %v %v", got, err)
	}
	if got, err = VoteOptions(p, nil); err != nil || len(got) != 0 {
		t.Fatalf("retract: %v %v", got, err)
	}
}

func TestMayChange(t *testing.T) {
	if !MayChange(poll(nil)) {
		t.Fatal("normal poll locked")
	}
	if MayChange(poll(func(p *mtproto.Poll) { p.Quiz = true })) {
		t.Fatal("quiz unlocked")
	}
	if MayChange(poll(func(p *mtproto.Poll) { p.RevotingDisabled = true })) {
		t.Fatal("revoting_disabled unlocked")
	}
}

// The answer of an open poll gets the next option, its author may take it
// back only soon after; the creator always.
func TestOpenAnswers(t *testing.T) {
	rec := record(func(p *mtproto.Poll) { p.OpenAnswers = true })
	rec.NextOption = 3
	added, err := AppendAnswer(rec, answer("D", ""), 2, 100)
	if err != nil {
		t.Fatal(err)
	}
	if string(added.GetOption()) != "3" || added.GetAddedBy().GetUserId() != 2 || added.GetDate().GetValue() != 100 || rec.NextOption != 4 {
		t.Fatalf("added %+v", added)
	}
	if _, err = AppendAnswer(rec, answer("D", ""), 2, 100); !errors.Is(err, mtproto.ErrPollOptionDuplicate) {
		t.Fatalf("duplicate text: %v", err)
	}
	if _, err = RemoveAnswer(rec, []byte("3"), 2, 100+AnswerDeleteAfter+1); !errors.Is(err, mtproto.ErrMessageAuthorRequired) {
		t.Fatalf("too late: %v", err)
	}
	if _, err = RemoveAnswer(rec, []byte("3"), 2, 100+AnswerDeleteAfter); err != nil {
		t.Fatalf("in time: %v", err)
	}
	if _, err = RemoveAnswer(rec, []byte("3"), 1, 9999); !errors.Is(err, mtproto.ErrOptionInvalid) {
		t.Fatalf("already gone: %v", err)
	}
	if _, err = RemoveAnswer(rec, []byte("a"), 1, 9999); err != nil {
		t.Fatalf("creator: %v", err)
	}
}

// Check, Consume and the view round-trip: the copies of a message hold no
// right answer, the record does.
func TestConsumeStripsSecrets(t *testing.T) {
	m := inputPoll(poll(func(p *mtproto.Poll) { p.Quiz = true }))
	m.CorrectAnswers_FLAGVECTORINT32 = []int32{1}
	m.Solution = wrapperspb.String("because")
	c, err := Check(m, 1000)
	if err != nil {
		t.Fatal(err)
	}
	media := NewMedia(c, nil, nil)
	if !IsNew(media) {
		t.Fatal("not new")
	}
	rec := Consume(media, 42, 1, 1000)
	if IsNew(media) || media.GetPoll().GetId() != 42 {
		t.Fatalf("media after consume: %+v", media.GetPoll())
	}
	if media.GetResults().GetSolution() != nil || len(media.GetResults().GetResults()) != 0 {
		t.Fatalf("copies keep the answer: %+v", media.GetResults())
	}
	if string(rec.Correct[0]) != "b" || rec.Solution.GetValue() != "because" {
		t.Fatalf("record: %+v", rec)
	}
}

func TestForForward(t *testing.T) {
	media := mtproto.MakeTLMessageMediaPoll(&mtproto.MessageMedia{
		Poll:    poll(func(p *mtproto.Poll) { p.Id = 5; p.Creator = true }),
		Results: mtproto.MakeTLPollResults(&mtproto.PollResults{TotalVoters: wrapperspb.Int32(3)}).To_PollResults(),
	}).To_MessageMedia()
	out := ForForward(media)
	if out.GetPoll().GetCreator() || out.GetPoll().GetId() != 5 || out.GetResults().GetTotalVoters().GetValue() != 0 {
		t.Fatalf("forward: %+v %+v", out.GetPoll(), out.GetResults())
	}
	if !media.GetPoll().GetCreator() {
		t.Fatal("the source was changed")
	}
}

func item(id int32, s string) *mtproto.TodoItem {
	return mtproto.MakeTLTodoItem(&mtproto.TodoItem{Id: id, Title: text(s)}).To_TodoItem()
}

func list(items ...*mtproto.TodoItem) *mtproto.TodoList {
	return mtproto.MakeTLTodoList(&mtproto.TodoList{Title: text("Shopping"), List: items}).To_TodoList()
}

// The rules of a checklist as sent.
func TestCheckTodo(t *testing.T) {
	if err := CheckTodo(list()); !errors.Is(err, ErrTodoItemsEmpty) {
		t.Fatalf("empty: %v", err)
	}
	if err := CheckTodo(list(item(1, "a"), item(1, "b"))); !errors.Is(err, ErrTodoItemDup) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := CheckTodo(list(item(0, "a"))); !errors.Is(err, ErrTodoItemDup) {
		t.Fatalf("zero id: %v", err)
	}
	many := make([]*mtproto.TodoItem, TodoItemsMax+1)
	for i := range many {
		many[i] = item(int32(i+1), "x")
	}
	if err := CheckTodo(list(many...)); !errors.Is(err, ErrTodoItemsTooMuch) {
		t.Fatalf("too many: %v", err)
	}
	if err := CheckTodo(list(item(1, "a"))); err != nil {
		t.Fatalf("ok: %v", err)
	}
}

// Toggling marks and unmarks; repeats change nothing; ids off the list are
// ignored; the first completer stays.
func TestToggle(t *testing.T) {
	l := list(item(1, "a"), item(2, "b"))
	by := mtproto.MakePeerUser(9)
	out, done, undone := Toggle(l, nil, []int32{1, 99}, nil, by, 50)
	if len(out) != 1 || len(done) != 1 || done[0] != 1 || len(undone) != 0 {
		t.Fatalf("complete: %v %v %v", out, done, undone)
	}
	// Already done, by another now: nothing changes, the first stays.
	again, done, _ := Toggle(l, out, []int32{1}, nil, mtproto.MakePeerUser(8), 60)
	if len(done) != 0 || again[0].GetCompletedBy_PEER().GetUserId() != 9 {
		t.Fatalf("repeat: %v", again)
	}
	_, _, undone = Toggle(l, again, nil, []int32{1}, by, 70)
	if len(undone) != 1 {
		t.Fatalf("undo: %v", undone)
	}
}

func TestAppendItems(t *testing.T) {
	l := list(item(1, "a"))
	if _, err := AppendItems(l, nil); !errors.Is(err, ErrTodoNotModified) {
		t.Fatalf("nothing: %v", err)
	}
	if _, err := AppendItems(l, []*mtproto.TodoItem{item(1, "b")}); !errors.Is(err, ErrTodoItemDup) {
		t.Fatalf("duplicate: %v", err)
	}
	out, err := AppendItems(l, []*mtproto.TodoItem{item(2, "b")})
	if err != nil || len(out.GetList()) != 2 || len(l.GetList()) != 1 {
		t.Fatalf("append: %v %v", out, err)
	}
}

func TestEditListKeepsCompletions(t *testing.T) {
	comps := []*mtproto.TodoCompletion{
		mtproto.MakeTLTodoCompletion(&mtproto.TodoCompletion{Id: 1}).To_TodoCompletion(),
		mtproto.MakeTLTodoCompletion(&mtproto.TodoCompletion{Id: 2}).To_TodoCompletion(),
	}
	_, kept, err := EditList(list(item(1, "a")), comps)
	if err != nil || len(kept) != 1 || kept[0].GetId() != 1 {
		t.Fatalf("%v %v", kept, err)
	}
}

func TestCountry(t *testing.T) {
	if c := Country("79001234567"); c != "RU" {
		t.Fatalf("got %q", c)
	}
	if c := Country("not-a-phone"); c != "" {
		t.Fatalf("got %q", c)
	}
}

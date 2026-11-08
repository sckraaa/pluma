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

// Package polls holds the rules of polls and quizzes (task 062): checking a
// poll as sent, how a vote changes a voter's options, and the view of a poll
// for one viewer or for everyone (results min). The sources of each rule are
// in docs/tasks/062-polls-and-checklists.md.
package polls

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"hash/fnv"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Limits. core.telegram.org/api/config: poll_answers_max 12,
// poll_close_period_max 2628000, poll_answer_delete_period 300,
// poll_countries_max 12. The lengths are what the official clients enforce:
// tdesktop create_poll_box.cpp:107,109 (question 255, answer 100), Android
// ChatAttachAlertPollLayout.java:191-193 (and solution 200). OptionMax bounds
// the option's column.
const (
	AnswersMax        = 12
	ClosePeriodMax    = 2628000
	AnswerDeleteAfter = 300
	CountriesMax      = 12
	QuestionMax       = 255
	AnswerMax         = 100
	SolutionMax       = 200
	OptionMax         = 100
	// RecentVotersMax is the most voters of recent_voters; the clients draw
	// three under a public poll (Telegram's number is not verified).
	RecentVotersMax = 3
	// SubscriberAge is how long a user must have been in a channel before
	// a subscribers-only poll was posted, in seconds (api/poll).
	SubscriberAge = 86400
)

// Errors core.telegram.org/method/messages.sendVote lists that the shared
// schema (third_party/teamgram-proto) has no constant for.
var (
	ErrPollMemberRestricted  = status.Error(mtproto.ErrNotAcceptable, "POLL_MEMBER_RESTRICTED")
	ErrPollCountryRestricted = status.Error(mtproto.ErrNotAcceptable, "POLL_COUNTRY_RESTRICTED")
)

// Record is what the server knows of a poll: a polls row.
type Record struct {
	Id              int64
	Creator         int64
	PeerType        int32
	PeerId          int64
	MessageId       int32
	DialogMessageId int64
	Poll            *mtproto.Poll // as sent: id, flags, answers
	Correct         [][]byte
	Solution        *wrapperspb.StringValue
	Entities        []*mtproto.MessageEntity
	SolutionMedia   *mtproto.MessageMedia
	NextOption      int32
	Closed          bool
	CloseDate       int64 // 0: no closing date
	HasUnread       bool
	Date            int64
}

// Vote is a row of poll_votes.
type Vote struct {
	UserId int64
	Option []byte
	Date   int64
}

// Votes are the rows of a poll tallied for one viewer.
type Votes struct {
	Counts   map[string]int32
	Total    int32
	Mine     [][]byte           // the viewer's options
	Recent   []int64            // recent voters of the poll, most recent first
	ByOption map[string][]int64 // recent voters of each option
}

// Tally groups rows (in any order) for viewer.
func Tally(rows []Vote, viewer int64) *Votes {
	v := &Votes{Counts: map[string]int32{}, ByOption: map[string][]int64{}}
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b Vote) int {
		if c := cmp.Compare(b.Date, a.Date); c != 0 {
			return c
		}
		if c := cmp.Compare(b.UserId, a.UserId); c != 0 {
			return c
		}
		return bytes.Compare(a.Option, b.Option)
	})
	seen := map[int64]bool{}
	for _, r := range sorted {
		key := string(r.Option)
		v.Counts[key]++
		if r.UserId == viewer {
			v.Mine = append(v.Mine, r.Option)
		}
		if !seen[r.UserId] {
			seen[r.UserId] = true
			v.Total++
			if len(v.Recent) < RecentVotersMax {
				v.Recent = append(v.Recent, r.UserId)
			}
		}
		if len(v.ByOption[key]) < RecentVotersMax {
			v.ByOption[key] = append(v.ByOption[key], r.UserId)
		}
	}
	return v
}

// IsClosed tells whether the poll takes no more votes now: closed already,
// or past its closing date while the sweep has not reached it. Readers and
// votes never wait for the sweep.
func (r *Record) IsClosed(now int64) bool {
	return r.Closed || (r.CloseDate > 0 && r.CloseDate <= now)
}

// Mode selects the results a view holds.
type Mode int

const (
	// Full: the viewer's own results (chosen, correct when it may see them,
	// the solution).
	Full Mode = iota
	// Min: what every member may see, without anything personal
	// (constructor/pollResults: min). The clients keep the chosen answers
	// they have (tdesktop PollData::applyResultToAnswers, Android
	// MessageObject.updatePollResults, Web K mergeMinAnswerResults).
	Min
)

func optionKey(o []byte) string { return string(o) }

func answerSeen(answers []*mtproto.PollAnswer, option []byte) bool {
	return slices.ContainsFunc(answers, func(a *mtproto.PollAnswer) bool { return bytes.Equal(a.GetOption(), option) })
}

// Hash is poll.hash. It changes when the definition does, also when the poll
// closes: api/poll, and messages.getPollResults, where the server skips what
// did not change for the hash the client holds. The results are not part of
// it: they are always sent.
func (r *Record) Hash(now int64) int64 {
	p := proto.Clone(r.Poll).(*mtproto.Poll)
	p.Closed = r.IsClosed(now)
	p.Creator = false
	p.Hash = 0
	data, err := jsonx.Marshal(p)
	if err != nil {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write(data)
	v := int64(binary.BigEndian.Uint64(h.Sum(nil)) & 0x7fffffffffffffff)
	if v == 0 {
		return 1
	}
	return v
}

// View returns the poll and its results for viewer at now. Min leaves out
// everything personal and what only those who voted may see.
func (r *Record) View(votes *Votes, viewer int64, mode Mode, now int64) (*mtproto.Poll, *mtproto.PollResults) {
	closed := r.IsClosed(now)
	poll := proto.Clone(r.Poll).(*mtproto.Poll)
	poll.Closed = closed
	poll.Creator = mode == Full && viewer == r.Creator
	poll.Hash = r.Hash(now)

	creator := poll.Creator
	// The creator always sees the results; with hide_results_until_close
	// others do once it closes (tdesktop history_view_poll.cpp Poll::showVotes,
	// iOS ChatMessagePollBubbleContentNode.swift:3582).
	hidden := poll.GetHideResultsUntilClose() && !closed && !creator
	voted := mode == Full && len(votes.Mine) > 0
	// A viewer sees the right answers and the explanation once it has
	// voted, once the poll is closed, or if it made the poll: api/poll,
	// "chosen options carry the chosen flag, correct answers the correct
	// flag" in the answer to the vote.
	reveal := closed || voted || creator

	mine := map[string]bool{}
	if mode == Full {
		for _, o := range votes.Mine {
			mine[optionKey(o)] = true
		}
	}
	correct := map[string]bool{}
	for _, o := range r.Correct {
		correct[optionKey(o)] = true
	}
	results := &mtproto.PollResults{Min: mode == Min}
	// The list goes out with its zeros as well: for a non-min update
	// without it Android keeps the answers it holds, the chosen mark
	// included, which is what the last voter taking its vote back would
	// leave (MessageObject.updatePollResults).
	if !hidden || voted {
		for _, a := range poll.GetAnswers() {
			key := optionKey(a.GetOption())
			row := &mtproto.PollAnswerVoters{
				Option:  a.GetOption(),
				Chosen:  mine[key],
				Correct: reveal && correct[key],
			}
			if hidden {
				// Only the viewer's own choice, with no numbers: the
				// clients clear what they hold for an empty list (Android
				// MessageObject.updatePollResults; Web K
				// appPollsManager.saveResults).
				if !row.Chosen {
					continue
				}
			} else {
				row.Voters_FLAGINT32 = wrapperspb.Int32(votes.Counts[key])
				row.RecentVoters = []*mtproto.Peer{}
				if poll.GetPublicVoters() {
					row.RecentVoters = peers(votes.ByOption[key])
				}
			}
			results.Results = append(results.Results, mtproto.MakeTLPollAnswerVoters(row).To_PollAnswerVoters())
		}
	}
	if !hidden {
		results.TotalVoters = wrapperspb.Int32(votes.Total)
		if poll.GetPublicVoters() {
			results.RecentVoters_FLAGVECTORPEER = peers(votes.Recent)
		}
	}
	if reveal && r.Solution != nil {
		results.Solution = r.Solution
		results.SolutionEntities = r.Entities
		results.SolutionMedia = r.SolutionMedia
	}
	if creator && poll.GetPublicVoters() {
		results.HasUnreadVotes = r.HasUnread
	}
	return poll, mtproto.MakeTLPollResults(results).To_PollResults()
}

func peers(ids []int64) []*mtproto.Peer {
	out := make([]*mtproto.Peer, 0, len(ids))
	for _, id := range ids {
		out = append(out, mtproto.MakePeerUser(id))
	}
	return out
}

// UsersIn returns the users results name.
func UsersIn(results *mtproto.PollResults) []int64 {
	var ids []int64
	add := func(list []*mtproto.Peer) {
		for _, p := range list {
			if id := p.GetUserId(); id != 0 && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	add(results.GetRecentVoters_FLAGVECTORPEER())
	for _, r := range results.GetResults() {
		add(r.GetRecentVoters())
	}
	return ids
}

// Update is updateMessagePoll of poll pollId for message msgId of peer; a
// nil peer leaves both out, for a push to the subscribers of a channel
// (api/poll). withPoll adds the poll itself: a change of its definition.
func Update(peer *mtproto.Peer, msgId int32, pollId int64, poll *mtproto.Poll, results *mtproto.PollResults, withPoll bool) *mtproto.Update {
	u := &mtproto.Update{PollId: pollId, Results: results}
	if peer != nil {
		u.Peer_PEER = peer
		u.MsgId_FLAGINT32 = wrapperspb.Int32(msgId)
	}
	if withPoll {
		u.Poll = poll
	}
	return mtproto.MakeTLUpdateMessagePoll(u).To_Update()
}

// checkText checks a text of at most limit characters, not blank.
func checkText(t *mtproto.TextWithEntities, limit int, bad error) error {
	text := t.GetText()
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > limit {
		return bad
	}
	return nil
}

// Checked is a poll as sent, checked and in canonical form: each answer a
// pollAnswer with its option.
type Checked struct {
	Poll     *mtproto.Poll
	Correct  [][]byte
	Solution *wrapperspb.StringValue
	Entities []*mtproto.MessageEntity
}

// Check checks a poll as a client sent it (inputMediaPoll) and gives each
// answer its option: the client's for a pollAnswer of the older layers, the
// answer's position for an inputPollAnswer, which carries none. The errors
// are those of messages.sendMedia.
func Check(in *mtproto.InputMedia, now int64) (*Checked, error) {
	p := in.GetPoll()
	if p == nil {
		return nil, mtproto.ErrPollAnswersInvalid
	}
	if err := checkText(p.GetQuestion_TEXTWITHENTITIES(), QuestionMax, mtproto.ErrPollQuestionInvalid); err != nil {
		return nil, err
	}
	answers := p.GetAnswers()
	if len(answers) < 2 && !p.GetOpenAnswers() {
		return nil, mtproto.ErrPollAnswersInvalid
	}
	if len(answers) > AnswersMax {
		return nil, mtproto.ErrPollAnswersInvalid
	}
	out := proto.Clone(p).(*mtproto.Poll)
	out.Answers = make([]*mtproto.PollAnswer, 0, len(answers))
	for i, a := range answers {
		if err := checkText(a.GetText_TEXTWITHENTITIES(), AnswerMax, mtproto.ErrPollAnswerInvalid); err != nil {
			return nil, err
		}
		option := a.GetOption()
		if a.GetPredicateName() == mtproto.Predicate_inputPollAnswer || len(option) == 0 {
			option = OptionOf(int32(i))
		}
		if len(option) > OptionMax {
			return nil, mtproto.ErrPollOptionInvalid
		}
		if answerSeen(out.Answers, option) {
			return nil, mtproto.ErrPollOptionDuplicate
		}
		out.Answers = append(out.Answers, mtproto.MakeTLPollAnswer(&mtproto.PollAnswer{
			Text_TEXTWITHENTITIES:  a.GetText_TEXTWITHENTITIES(),
			Option:                 option,
			Media_FLAGMESSAGEMEDIA: a.GetMedia_FLAGMESSAGEMEDIA(),
		}).To_PollAnswer())
	}
	if len(p.GetCountriesIso2()) > CountriesMax {
		return nil, mtproto.ErrPollAnswersInvalid
	}
	checked := &Checked{Poll: out}

	correct := in.GetCorrectAnswers_FLAGVECTORINT32()
	byOption := in.GetCorrectAnswers_FLAGVECTORBYTES()
	if p.GetQuiz() {
		if len(correct) == 0 && len(byOption) == 0 {
			return nil, mtproto.ErrQuizCorrectAnswersEmpty
		}
		// One right answer unless the quiz is a multiple choice, which
		// iOS builds and Telegram's own api/poll allows
		// (Polls.swift: .quiz(multipleAnswers:)).
		if !p.GetMultipleChoice() && len(correct)+len(byOption) > 1 {
			return nil, mtproto.ErrQuizCorrectAnswersTooMuch
		}
		for _, n := range correct {
			if n < 0 || int(n) >= len(out.Answers) {
				return nil, mtproto.ErrQuizCorrectAnswerInvalid
			}
			checked.Correct = append(checked.Correct, out.Answers[n].GetOption())
		}
		for _, o := range byOption {
			if !answerSeen(out.Answers, o) {
				return nil, mtproto.ErrQuizCorrectAnswerInvalid
			}
			checked.Correct = append(checked.Correct, o)
		}
	} else if len(correct) > 0 || len(byOption) > 0 {
		return nil, mtproto.ErrQuizCorrectAnswerInvalid
	}
	if s := in.GetSolution(); s != nil && s.GetValue() != "" {
		if utf8.RuneCountInString(s.GetValue()) > SolutionMax {
			return nil, mtproto.ErrPollAnswerInvalid
		}
		checked.Solution = s
		checked.Entities = in.GetSolutionEntities()
	}
	// The closing date: close_date as given, or close_period from now
	// (constructor/poll).
	if p.GetClosePeriod().GetValue() > ClosePeriodMax {
		return nil, mtproto.ErrPollAnswersInvalid
	}
	if p.GetCloseDate() == nil && p.GetClosePeriod().GetValue() > 0 {
		out.CloseDate = wrapperspb.Int32(int32(now) + p.GetClosePeriod().GetValue())
	}
	return checked, nil
}

// OptionOf is the option given to the answer at this index when the client
// sends none (inputPollAnswer).
func OptionOf(index int32) []byte { return []byte(strconv.Itoa(int(index))) }

// NewMedia is the message media of a poll as sent: the poll marked new, its
// correct options and solution in the results, for msg to store and take
// out in the send transaction (Consume).
func NewMedia(c *Checked, attached, solutionMedia *mtproto.MessageMedia) *mtproto.MessageMedia {
	results := &mtproto.PollResults{Solution: c.Solution, SolutionEntities: c.Entities, SolutionMedia: solutionMedia}
	for _, o := range c.Correct {
		results.Results = append(results.Results, mtproto.MakeTLPollAnswerVoters(&mtproto.PollAnswerVoters{Correct: true, Option: o}).To_PollAnswerVoters())
	}
	p := proto.Clone(c.Poll).(*mtproto.Poll)
	p.Creator = true
	p.Id = 0
	return mtproto.MakeTLMessageMediaPoll(&mtproto.MessageMedia{
		Poll:          p,
		Results:       mtproto.MakeTLPollResults(results).To_PollResults(),
		AttachedMedia: attached,
	}).To_MessageMedia()
}

// IsNew tells whether media is a poll being created (NewMedia), not a
// forward of an existing one.
func IsNew(media *mtproto.MessageMedia) bool {
	return media.GetPredicateName() == mtproto.Predicate_messageMediaPoll && media.GetPoll().GetCreator()
}

// Consume takes the poll out of its NewMedia form: it returns the record to
// store with id, and leaves media as the copies of the message hold it, with
// empty results and nothing personal.
func Consume(media *mtproto.MessageMedia, id, creator, now int64) *Record {
	p := proto.Clone(media.GetPoll()).(*mtproto.Poll)
	p.Id = id
	p.Creator = false
	r := &Record{
		Id:            id,
		Creator:       creator,
		Poll:          p,
		Solution:      media.GetResults().GetSolution(),
		Entities:      media.GetResults().GetSolutionEntities(),
		SolutionMedia: media.GetResults().GetSolutionMedia(),
		NextOption:    int32(len(p.GetAnswers())),
		Closed:        p.GetClosed(),
		Date:          now,
	}
	if d := p.GetCloseDate(); d != nil {
		r.CloseDate = int64(d.GetValue())
		r.Closed = r.Closed || r.CloseDate <= now
	}
	p.Hash = r.Hash(now)
	for _, a := range media.GetResults().GetResults() {
		if a.GetCorrect() {
			r.Correct = append(r.Correct, a.GetOption())
		}
	}
	media.Poll = proto.Clone(p).(*mtproto.Poll)
	media.Results = EmptyResults()
	return r
}

// EmptyResults are the results of a poll in a copy of its message: the
// readers fill them for the viewer (Attach). total_voters alone is what the
// clients accept before anyone voted (the other fields are optional:
// tdesktop PollData::applyResults, Android MessageObject.updatePollResults).
func EmptyResults() *mtproto.PollResults {
	return mtproto.MakeTLPollResults(&mtproto.PollResults{TotalVoters: wrapperspb.Int32(0)}).To_PollResults()
}

// ForForward prepares media, a poll as its forwarder sees it, for a
// forward: the same poll, nothing of the forwarder's.
func ForForward(media *mtproto.MessageMedia) *mtproto.MessageMedia {
	if media.GetPredicateName() != mtproto.Predicate_messageMediaPoll {
		return media
	}
	out := proto.Clone(media).(*mtproto.MessageMedia)
	out.Poll.Creator = false
	out.Results = EmptyResults()
	return out
}

// VoteOptions checks options against p and returns what the voter will have
// chosen, in the order of the poll's answers: OPTION_INVALID for one the
// poll does not have, OPTIONS_TOO_MUCH for more than one when the poll takes
// one (messages.sendVote).
func VoteOptions(p *mtproto.Poll, options [][]byte) ([][]byte, error) {
	for _, o := range options {
		if !answerSeen(p.GetAnswers(), o) {
			return nil, mtproto.ErrOptionInvalid
		}
	}
	var out [][]byte
	for _, a := range p.GetAnswers() {
		if slices.ContainsFunc(options, func(o []byte) bool { return bytes.Equal(o, a.GetOption()) }) {
			out = append(out, a.GetOption())
		}
	}
	if len(out) > 1 && !p.GetMultipleChoice() {
		return nil, mtproto.ErrOptionsTooMuch
	}
	return out, nil
}

// AppendAnswer adds answer to the open poll of rec, with the next option:
// the text is checked as a poll's answers are and must be new.
func AppendAnswer(rec *Record, answer *mtproto.PollAnswer, by, now int64) (*mtproto.PollAnswer, error) {
	if err := checkText(answer.GetText_TEXTWITHENTITIES(), AnswerMax, mtproto.ErrPollAnswerInvalid); err != nil {
		return nil, err
	}
	if len(rec.Poll.GetAnswers()) >= AnswersMax {
		return nil, mtproto.ErrPollAnswersInvalid
	}
	if slices.ContainsFunc(rec.Poll.GetAnswers(), func(a *mtproto.PollAnswer) bool {
		return a.GetText_TEXTWITHENTITIES().GetText() == answer.GetText_TEXTWITHENTITIES().GetText()
	}) {
		return nil, mtproto.ErrPollOptionDuplicate
	}
	added := mtproto.MakeTLPollAnswer(&mtproto.PollAnswer{
		Text_TEXTWITHENTITIES:  answer.GetText_TEXTWITHENTITIES(),
		Option:                 OptionOf(rec.NextOption),
		Media_FLAGMESSAGEMEDIA: answer.GetMedia_FLAGMESSAGEMEDIA(),
		AddedBy:                mtproto.MakePeerUser(by),
		Date:                   wrapperspb.Int32(int32(now)),
	}).To_PollAnswer()
	rec.Poll.Answers = append(rec.Poll.Answers, added)
	rec.NextOption++
	return added, nil
}

// RemoveAnswer takes option out of the open poll of rec: its creator always,
// the user who added it within AnswerDeleteAfter seconds (api/poll).
func RemoveAnswer(rec *Record, option []byte, by, now int64) (*mtproto.PollAnswer, error) {
	i := slices.IndexFunc(rec.Poll.GetAnswers(), func(a *mtproto.PollAnswer) bool { return bytes.Equal(a.GetOption(), option) })
	if i < 0 {
		return nil, mtproto.ErrOptionInvalid
	}
	answer := rec.Poll.Answers[i]
	mine := answer.GetAddedBy().GetUserId() == by && now-int64(answer.GetDate().GetValue()) <= AnswerDeleteAfter
	if by != rec.Creator && !mine {
		return nil, mtproto.ErrMessageAuthorRequired
	}
	rec.Poll.Answers = slices.Delete(slices.Clone(rec.Poll.Answers), i, i+1)
	return answer, nil
}

// MayListVoters tells whether viewer may read the voters of a public poll:
// its creator or anyone once it is closed; one who has voted, unless the
// poll hides its results until then (View shows that voter no counts
// either).
func (r *Record) MayListVoters(voted bool, viewer, now int64) bool {
	if viewer == r.Creator || r.IsClosed(now) {
		return true
	}
	return voted && !r.Poll.GetHideResultsUntilClose()
}

// SameOptions tells whether two lists hold the same options.
func SameOptions(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for _, o := range a {
		if !slices.ContainsFunc(b, func(x []byte) bool { return bytes.Equal(x, o) }) {
			return false
		}
	}
	return true
}

// MayChange tells whether a voter may change a vote it has cast: not in a
// quiz, not in a poll with revoting_disabled (constructor/poll,
// REVOTE_NOT_ALLOWED).
func MayChange(p *mtproto.Poll) bool { return !p.GetQuiz() && !p.GetRevotingDisabled() }

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
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nyaruka/phonenumbers"
	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Querier reads rows outside a box transaction: the *sqlx.DB of a service.
type Querier interface {
	QueryRowsPartial(ctx context.Context, v interface{}, query string, args ...interface{}) error
}

// Columns are the columns of a polls row, in the order Row scans them.
const Columns = "id, creator_user_id, peer_type, peer_id, message_id, dialog_message_id, poll_data, correct_options, solution_data, next_option, closed, close_date, has_unread, `date`"

// Row is a row of polls as go-zero scans it.
type Row struct {
	Id              int64  `db:"id"`
	CreatorUserId   int64  `db:"creator_user_id"`
	PeerType        int32  `db:"peer_type"`
	PeerId          int64  `db:"peer_id"`
	MessageId       int32  `db:"message_id"`
	DialogMessageId int64  `db:"dialog_message_id"`
	PollData        string `db:"poll_data"`
	CorrectOptions  string `db:"correct_options"`
	SolutionData    string `db:"solution_data"`
	NextOption      int32  `db:"next_option"`
	Closed          bool   `db:"closed"`
	CloseDate       int64  `db:"close_date"`
	HasUnread       bool   `db:"has_unread"`
	Date            int64  `db:"date"`
}

// VoteRow is a row of poll_votes as go-zero scans it.
type VoteRow struct {
	PollId int64  `db:"poll_id"`
	UserId int64  `db:"user_id"`
	Opt    []byte `db:"opt"`
	Date   int64  `db:"date"`
}

// solutionData is the column solution_data.
type solutionData struct {
	Solution *string                  `json:"solution,omitempty"`
	Entities []*mtproto.MessageEntity `json:"entities,omitempty"`
	Media    *mtproto.MessageMedia    `json:"media,omitempty"`
}

// Decode makes the record of a row.
func (r *Row) Decode() (*Record, error) {
	rec := &Record{
		Id: r.Id, Creator: r.CreatorUserId, PeerType: r.PeerType, PeerId: r.PeerId, MessageId: r.MessageId,
		DialogMessageId: r.DialogMessageId, NextOption: r.NextOption, Closed: r.Closed, CloseDate: r.CloseDate,
		HasUnread: r.HasUnread, Date: r.Date,
	}
	if err := jsonx.UnmarshalFromString(r.PollData, &rec.Poll); err != nil {
		return nil, fmt.Errorf("polls: poll %d: %w", r.Id, err)
	}
	if err := jsonx.UnmarshalFromString(r.CorrectOptions, &rec.Correct); err != nil {
		return nil, fmt.Errorf("polls: correct options of poll %d: %w", r.Id, err)
	}
	var s solutionData
	if err := jsonx.UnmarshalFromString(r.SolutionData, &s); err != nil {
		return nil, fmt.Errorf("polls: solution of poll %d: %w", r.Id, err)
	}
	if s.Solution != nil {
		rec.Solution = wrapperspb.String(*s.Solution)
	}
	rec.Entities, rec.SolutionMedia = s.Entities, s.Media
	return rec, nil
}

// Args are the values of Columns, for an INSERT.
func (r *Record) Args() ([]any, error) {
	poll, err := jsonx.MarshalToString(r.Poll)
	if err != nil {
		return nil, err
	}
	correct := "[]"
	if len(r.Correct) > 0 {
		if correct, err = jsonx.MarshalToString(r.Correct); err != nil {
			return nil, err
		}
	}
	s := solutionData{Entities: r.Entities, Media: r.SolutionMedia}
	if r.Solution != nil {
		v := r.Solution.GetValue()
		s.Solution = &v
	}
	solution, err := jsonx.MarshalToString(s)
	if err != nil {
		return nil, err
	}
	return []any{r.Id, r.Creator, r.PeerType, r.PeerId, r.MessageId, r.DialogMessageId, poll, correct, solution, r.NextOption, r.Closed, r.CloseDate, r.HasUnread, r.Date}, nil
}

// InList makes the placeholders of n values.
func InList(n int) string { return "?" + strings.Repeat(", ?", n-1) }

func anyInts(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// Load reads the polls ids that exist, with their votes.
func Load(ctx context.Context, q Querier, ids []int64) (map[int64]*Record, map[int64][]Vote, error) {
	records := map[int64]*Record{}
	votes := map[int64][]Vote{}
	if len(ids) == 0 {
		return records, votes, nil
	}
	var rows []Row
	if err := q.QueryRowsPartial(ctx, &rows, "SELECT "+Columns+" FROM polls WHERE id IN ("+InList(len(ids))+")", anyInts(ids)...); err != nil {
		return nil, nil, err
	}
	for i := range rows {
		rec, err := rows[i].Decode()
		if err != nil {
			return nil, nil, err
		}
		records[rec.Id] = rec
	}
	var vr []VoteRow
	if err := q.QueryRowsPartial(ctx, &vr, "SELECT poll_id, user_id, opt, `date` FROM poll_votes WHERE poll_id IN ("+InList(len(ids))+")", anyInts(ids)...); err != nil {
		return nil, nil, err
	}
	for _, v := range vr {
		votes[v.PollId] = append(votes[v.PollId], Vote{UserId: v.UserId, Option: v.Opt, Date: v.Date})
	}
	return records, votes, nil
}

// AttachMessages gives each message with a poll the poll and its results as
// viewer sees them now. A poll the database does not have (a message older
// than this code) stays as stored.
func AttachMessages(ctx context.Context, q Querier, viewer int64, messages ...*mtproto.Message) error {
	var ids []int64
	for _, m := range messages {
		if id := pollIdOf(m); id != 0 && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	records, votes, err := Load(ctx, q, ids)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, m := range messages {
		rec := records[pollIdOf(m)]
		if rec == nil {
			continue
		}
		m.Media.Poll, m.Media.Results = rec.View(Tally(votes[rec.Id], viewer), viewer, Full, now)
	}
	return nil
}

// Attach is AttachMessages for message boxes, each for its owner.
func Attach(ctx context.Context, q Querier, boxes ...*mtproto.MessageBox) error {
	var ids []int64
	for _, box := range boxes {
		if id := pollIdOf(box.GetMessage()); id != 0 && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	records, votes, err := Load(ctx, q, ids)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, box := range boxes {
		rec := records[pollIdOf(box.GetMessage())]
		if rec == nil {
			continue
		}
		m := box.GetMessage()
		m.Media.Poll, m.Media.Results = rec.View(Tally(votes[rec.Id], box.GetUserId()), box.GetUserId(), Full, now)
	}
	return nil
}

func pollIdOf(m *mtproto.Message) int64 {
	if m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaPoll {
		return 0
	}
	return m.GetMedia().GetPoll().GetId()
}

// AttachUnreadDialogs sets unread_poll_votes_count of each dialog of userId:
// the polls of the dialog made by userId with unread votes (api/poll: "the
// poll owner's dialog").
func AttachUnreadDialogs(ctx context.Context, q Querier, userId int64, dialogs ...*mtproto.Dialog) error {
	if len(dialogs) == 0 {
		return nil
	}
	var rows []struct {
		PeerType int32 `db:"peer_type"`
		PeerId   int64 `db:"peer_id"`
		Count    int32 `db:"n"`
	}
	if err := q.QueryRowsPartial(ctx, &rows,
		"SELECT peer_type, peer_id, COUNT(*) AS n FROM polls WHERE creator_user_id = ? AND has_unread = 1 GROUP BY peer_type, peer_id", userId); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	for _, d := range dialogs {
		p := mtproto.FromPeer(d.GetPeer())
		for _, r := range rows {
			if r.PeerType == p.PeerType && r.PeerId == p.PeerId {
				d.UnreadPollVotesCount = r.Count
			}
		}
	}
	return nil
}

// Voter is the rows of one voter, for messages.getPollVotes.
type Voter struct {
	UserId  int64
	Options [][]byte
	Date    int64
}

// VotersPage reads a page of the voters of poll id, most recent vote first,
// after the cursor offset; option narrows it to the voters of one option.
// count is every voter of the filter, next the offset of the next page ("" at
// the end). Telegram's offset is opaque to clients (method/messages.getPollVotes).
func VotersPage(ctx context.Context, q Querier, id int64, option []byte, offset string, limit int32) (voters []Voter, count int32, next string, err error) {
	where := "poll_id = ?"
	args := []any{id}
	if len(option) > 0 {
		where += " AND opt = ?"
		args = append(args, option)
	}
	var counts []int32
	if err = q.QueryRowsPartial(ctx, &counts, "SELECT COUNT(DISTINCT user_id) FROM poll_votes WHERE "+where, args...); err != nil {
		return nil, 0, "", err
	}
	if len(counts) > 0 {
		count = counts[0]
	}
	limit = min(max(limit, 1), 100)
	havingArgs := []any{}
	having := ""
	if offset != "" {
		date, user, ok := parseOffset(offset)
		if !ok {
			return nil, 0, "", mtproto.ErrOffsetInvalid
		}
		having = " HAVING (d < ? OR (d = ? AND user_id < ?))"
		havingArgs = append(havingArgs, date, date, user)
	}
	var rows []struct {
		UserId int64 `db:"user_id"`
		D      int64 `db:"d"`
	}
	full := append(append(append([]any{}, args...), havingArgs...), limit+1)
	if err = q.QueryRowsPartial(ctx, &rows,
		"SELECT user_id, MAX(`date`) AS d FROM poll_votes WHERE "+where+" GROUP BY user_id"+having+" ORDER BY d DESC, user_id DESC LIMIT ?", full...); err != nil {
		return nil, 0, "", err
	}
	more := len(rows) > int(limit)
	if more {
		rows = rows[:limit]
	}
	if len(rows) == 0 {
		return nil, count, "", nil
	}
	// All the options of each voter of the page, when not filtered by one.
	options := map[int64][][]byte{}
	dates := map[int64]int64{}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.UserId
		dates[r.UserId] = r.D
	}
	if len(option) == 0 {
		var vr []VoteRow
		a := append([]any{id}, anyInts(ids)...)
		if err = q.QueryRowsPartial(ctx, &vr, "SELECT poll_id, user_id, opt, `date` FROM poll_votes WHERE poll_id = ? AND user_id IN ("+InList(len(ids))+") ORDER BY opt", a...); err != nil {
			return nil, 0, "", err
		}
		for _, v := range vr {
			options[v.UserId] = append(options[v.UserId], v.Opt)
		}
	} else {
		for _, u := range ids {
			options[u] = [][]byte{option}
		}
	}
	for _, r := range rows {
		voters = append(voters, Voter{UserId: r.UserId, Options: options[r.UserId], Date: r.D})
	}
	if more {
		last := rows[len(rows)-1]
		next = strconv.FormatInt(last.D, 10) + ":" + strconv.FormatInt(last.UserId, 10)
	}
	return voters, count, next, nil
}

func parseOffset(s string) (date, user int64, ok bool) {
	d, u, found := strings.Cut(s, ":")
	if !found {
		return 0, 0, false
	}
	date, err1 := strconv.ParseInt(d, 10, 64)
	user, err2 := strconv.ParseInt(u, 10, 64)
	return date, user, err1 == nil && err2 == nil
}

// Country is the ISO 3166-1 alpha-2 code of a phone number, as digits with
// or without the plus, "" when it cannot be told: the country of
// countries_iso2 (api/poll: phone_country_iso2).
func Country(phone string) string {
	if phone == "" {
		return ""
	}
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}
	n, err := phonenumbers.Parse(phone, "")
	if err != nil {
		return ""
	}
	return phonenumbers.GetRegionCodeForNumber(n)
}

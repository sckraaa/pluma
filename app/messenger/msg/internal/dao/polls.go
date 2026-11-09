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

package dao

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/polls"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// Polls (task 062). A poll is one row of polls, under the poll's id, which
// every copy of the message and every forward of it hold; the votes are rows
// of poll_votes, so a count never drifts. A vote is written in the voter's
// box transaction, with the poll row locked in share mode and, in a channel,
// the channel locked; the changes of the poll itself (closing, an added or
// removed answer) lock it exclusively. The copies of a message hold only
// the poll's definition: the readers build the results for the viewer from
// these rows (polls.Attach).

// PollRef is the poll of a message as a user addresses it.
type PollRef struct {
	Rec             *polls.Record
	Peer            boxPeer // the user's dialog, or the channel
	MessageId       int32   // the user's copy, or the channel's message
	DialogMessageId int64   // 0 in a channel
	AuthorId        int64
	Message         *mtproto.Message
	sender          *sender             // in a channel
	Channel         *channelbox.Channel // in a channel, locked
	Broadcast       bool
}

// PollChange is what an operation on a poll did.
type PollChange struct {
	// Changed is false when the operation changed nothing (the same vote
	// again): nothing is journaled, the answer shows the current state.
	Changed bool
	// WithPoll: the definition changed; updateMessagePoll carries the poll.
	WithPoll bool
	// Service is the service message that goes with the change.
	Service *mtproto.MessageAction
}

// lockPoll reads poll id in b's transaction, locked for writing when
// exclusive, for reading otherwise: votes of different users go on at once,
// a change of the poll waits for them and they for it.
func lockPoll(b *userbox.Box, id int64, exclusive bool) (*polls.Record, error) {
	lock := " LOCK IN SHARE MODE"
	if exclusive {
		lock = " FOR UPDATE"
	}
	var rows []polls.Row
	if err := b.Tx().QueryRowsPartial(&rows, "SELECT "+polls.Columns+" FROM polls WHERE id = ?"+lock, id); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].Decode()
}

// pollVotes reads the votes of poll id.
func pollVotes(b *userbox.Box, id int64) ([]polls.Vote, error) {
	var rows []polls.VoteRow
	if err := b.Tx().QueryRowsPartial(&rows, "SELECT poll_id, user_id, opt, `date` FROM poll_votes WHERE poll_id = ?", id); err != nil {
		return nil, err
	}
	out := make([]polls.Vote, len(rows))
	for i, r := range rows {
		out[i] = polls.Vote{UserId: r.UserId, Option: r.Opt, Date: r.Date}
	}
	return out, nil
}

// storeNewPoll writes the poll of a message being sent and returns a copy of
// message with the poll as the copies keep it. A message with a poll that is
// not new (a forward) is returned as it is. The original stays untouched: a
// transaction that is retried sends it again.
func (d *Dao) storeNewPoll(b *userbox.Box, message *mtproto.Message, peerType int32, peerId int64, messageId int32, dialogMessageId, pollId int64) (*mtproto.Message, error) {
	if !polls.IsNew(message.GetMedia()) {
		return message, nil
	}
	m := proto.Clone(message).(*mtproto.Message)
	rec := polls.Consume(m.Media, pollId, b.UserId(), b.Date())
	rec.PeerType, rec.PeerId, rec.MessageId, rec.DialogMessageId = peerType, peerId, messageId, dialogMessageId
	args, err := rec.Args()
	if err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("INSERT INTO polls ("+polls.Columns+") VALUES ("+polls.InList(14)+")", args...); err != nil {
		return nil, err
	}
	return m, nil
}

// PollRefOf reads b's owner's copy msgId of its dialog with peerType/peerId
// and its poll. MESSAGE_ID_INVALID when there is no such copy or it has no
// poll; lock the poll exclusively for a change of its definition.
func (d *Dao) PollRefOf(b *userbox.Box, peerType int32, peerId int64, msgId int32, exclusive bool) (*PollRef, error) {
	if peerType != mtproto.PEER_USER && peerType != mtproto.PEER_CHAT {
		return nil, mtproto.ErrPeerIdInvalid
	}
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	row, err := messageById(b, msgId)
	if err != nil {
		return nil, err
	}
	did := peer.dialogId(b.UserId())
	if row == nil || row.DialogId1 != did.A || row.DialogId2 != did.B {
		return nil, mtproto.ErrMessageIdInvalid
	}
	var m *mtproto.Message
	if err = jsonx.UnmarshalFromString(row.MessageData, &m); err != nil {
		return nil, err
	}
	m = m.FixData()
	rec, err := messagePoll(b, m, exclusive)
	if err != nil {
		return nil, err
	}
	return &PollRef{Rec: rec, Peer: peer, MessageId: row.UserMessageBoxId, DialogMessageId: row.DialogMessageId, AuthorId: row.SenderUserId, Message: m}, nil
}

// ChannelPollRefOf reads message msgId of the channel and its poll, with the
// channel locked (openChannel): CHANNEL_PRIVATE for a user out of it.
func (d *Dao) ChannelPollRefOf(b *userbox.Box, channelId int64, msgId int32, exclusive bool) (*PollRef, error) {
	ch, s, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	var rows []channelMessageRow
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT sender_user_id, message_data FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0", channelId, msgId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrMessageIdInvalid
	}
	var m *mtproto.Message
	if err = jsonx.UnmarshalFromString(rows[0].MessageData, &m); err != nil {
		return nil, err
	}
	rec, err := messagePoll(b, m, exclusive)
	if err != nil {
		return nil, err
	}
	return &PollRef{
		Rec: rec, Peer: boxPeer{PeerType: mtproto.PEER_CHANNEL, PeerId: channelId}, MessageId: msgId,
		AuthorId: rows[0].SenderUserId, Message: m, sender: s, Channel: ch, Broadcast: s.ch.Broadcast,
	}, nil
}

func messagePoll(b *userbox.Box, m *mtproto.Message, exclusive bool) (*polls.Record, error) {
	if m.GetPredicateName() != mtproto.Predicate_message || m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaPoll {
		return nil, mtproto.ErrMessageIdInvalid
	}
	rec, err := lockPoll(b, m.GetMedia().GetPoll().GetId(), exclusive)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return rec, nil
}

// PollViewFor returns the poll of ref as viewer sees it at b's date.
func (d *Dao) PollViewFor(b *userbox.Box, ref *PollRef, viewer int64, mode polls.Mode) (*mtproto.Poll, *mtproto.PollResults, error) {
	rows, err := pollVotes(b, ref.Rec.Id)
	if err != nil {
		return nil, nil, err
	}
	poll, results := ref.Rec.View(polls.Tally(rows, viewer), viewer, mode, b.Date())
	return poll, results, nil
}

// PollUpdate is updateMessagePoll of ref for its peer and message.
func PollUpdate(ref *PollRef, poll *mtproto.Poll, results *mtproto.PollResults, withPoll bool) *mtproto.Update {
	return polls.Update(ref.Peer.toPeer(), ref.MessageId, ref.Rec.Id, poll, results, withPoll)
}

// CastVote makes options b's owner's vote in the poll of ref, in the order
// of the answers; none takes the vote back. A vote that repeats the voter's
// current options changes nothing and costs nothing (a retried request), a
// change is refused where the poll forbids it. country is the voter's, as
// the caller read it before the transaction, "" when unknown.
func (d *Dao) CastVote(b *userbox.Box, ref *PollRef, options [][]byte, country string) (*PollChange, error) {
	rec := ref.Rec
	if rec.IsClosed(b.Date()) {
		return nil, mtproto.ErrMessagePollClosed
	}
	if err := d.checkPollRestriction(b, ref, country); err != nil {
		return nil, err
	}
	chosen, err := polls.VoteOptions(rec.Poll, options)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Opt []byte `db:"opt"`
	}
	if err = b.Tx().QueryRowsPartial(&rows, "SELECT opt FROM poll_votes WHERE poll_id = ? AND user_id = ?", rec.Id, b.UserId()); err != nil {
		return nil, err
	}
	mine := make([][]byte, len(rows))
	for i, r := range rows {
		mine[i] = r.Opt
	}
	if polls.SameOptions(mine, chosen) {
		return &PollChange{}, nil
	}
	if len(mine) > 0 && !polls.MayChange(rec.Poll) {
		return nil, mtproto.ErrRevoteNotAllowed
	}
	if _, err = b.Tx().Exec("DELETE FROM poll_votes WHERE poll_id = ? AND user_id = ?", rec.Id, b.UserId()); err != nil {
		return nil, err
	}
	for _, o := range chosen {
		if _, err = b.Tx().Exec("INSERT INTO poll_votes (poll_id, user_id, opt, `date`) VALUES (?, ?, ?, ?)", rec.Id, b.UserId(), o, b.Date()); err != nil {
			return nil, err
		}
	}
	return &PollChange{Changed: true}, nil
}

// checkPollRestriction applies subscribers_only (a voter long enough in the
// channel the poll was posted to) and countries_iso2 (the voter's country
// is listed): api/poll.
func (d *Dao) checkPollRestriction(b *userbox.Box, ref *PollRef, country string) error {
	rec := ref.Rec
	if list := rec.Poll.GetCountriesIso2(); len(list) > 0 && !slices.ContainsFunc(list, func(c string) bool { return strings.EqualFold(c, country) }) {
		return polls.ErrPollCountryRestricted
	}
	if !rec.Poll.GetSubscribersOnly() || rec.PeerType != mtproto.PEER_CHANNEL {
		return nil
	}
	var rows []struct {
		State int32 `db:"state"`
		Date  int64 `db:"date"`
	}
	if err := b.Tx().QueryRowsPartial(&rows, "SELECT state, `date` FROM channel_participants WHERE channel_id = ? AND user_id = ?", rec.PeerId, b.UserId()); err != nil {
		return err
	}
	if len(rows) == 0 || rows[0].State != channelperm.StateIn {
		return polls.ErrPollMemberRestricted
	}
	posted := int64(ref.Message.GetDate())
	if f := ref.Message.GetFwdFrom(); f != nil {
		posted = int64(f.GetDate())
	}
	if posted-rows[0].Date < polls.SubscriberAge {
		return polls.ErrPollMemberRestricted
	}
	return nil
}

// mayEdit tells whether b's owner may edit the message of ref, as for an
// edit of the message (messages.editMessage, EditChannel).
func (ref *PollRef) mayEdit(b *userbox.Box) error {
	// A forward shares the poll of its source and is not its author's to
	// stop (tdesktop HistoryItem::canBeEdited, canStopPoll).
	if ref.Message.GetFwdFrom() != nil {
		return mtproto.ErrMessageAuthorRequired
	}
	own := ref.AuthorId == b.UserId()
	if ref.sender == nil {
		if !own {
			return mtproto.ErrMessageAuthorRequired
		}
		return nil
	}
	if ref.sender.can(channelperm.SendPlain, b.Date()) != nil {
		return mtproto.ErrMessageAuthorRequired
	}
	if !own && (!ref.Broadcast || ref.sender.can(channelperm.EditOthers, b.Date()) != nil) {
		return mtproto.ErrMessageAuthorRequired
	}
	return nil
}

// ClosePoll closes the poll of ref: api/poll, messages.editMessage with
// poll.closed. It is not undone. A poll already closed changes nothing.
func (d *Dao) ClosePoll(b *userbox.Box, ref *PollRef) (*PollChange, error) {
	if err := ref.mayEdit(b); err != nil {
		return nil, err
	}
	if ref.Rec.IsClosed(b.Date()) {
		return &PollChange{}, nil
	}
	if err := closeRecord(b, ref.Rec); err != nil {
		return nil, err
	}
	return &PollChange{Changed: true, WithPoll: true}, nil
}

func closeRecord(b *userbox.Box, rec *polls.Record) error {
	if _, err := b.Tx().Exec("UPDATE polls SET closed = 1 WHERE id = ?", rec.Id); err != nil {
		return err
	}
	rec.Closed = true
	return nil
}

// AddPollAnswer adds answer to the open-answer poll of ref, by b's owner
// (messages.addPollAnswer, api/poll). It gets the next option; the media
// the client sent comes from bff as message media.
func (d *Dao) AddPollAnswer(b *userbox.Box, ref *PollRef, answer *mtproto.PollAnswer) (*PollChange, error) {
	rec := ref.Rec
	if !rec.Poll.GetOpenAnswers() {
		return nil, mtproto.ErrPollAnswerInvalid
	}
	if rec.IsClosed(b.Date()) {
		return nil, mtproto.ErrMessagePollClosed
	}
	added, err := polls.AppendAnswer(rec, answer, b.UserId(), b.Date())
	if err != nil {
		return nil, err
	}
	if err = writePollData(b, rec); err != nil {
		return nil, err
	}
	return &PollChange{Changed: true, WithPoll: true,
		Service: mtproto.MakeTLMessageActionPollAppendAnswer(&mtproto.MessageAction{Answer: added}).To_MessageAction()}, nil
}

// DeletePollAnswer takes option out of the open-answer poll of ref, with
// the votes for it (messages.deletePollAnswer): its creator always, the user
// who added it in the first poll_answer_delete_period seconds.
func (d *Dao) DeletePollAnswer(b *userbox.Box, ref *PollRef, option []byte) (*PollChange, error) {
	rec := ref.Rec
	if !rec.Poll.GetOpenAnswers() {
		return nil, mtproto.ErrPollAnswerInvalid
	}
	if rec.IsClosed(b.Date()) {
		return nil, mtproto.ErrMessagePollClosed
	}
	removed, err := polls.RemoveAnswer(rec, option, b.UserId(), b.Date())
	if err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("DELETE FROM poll_votes WHERE poll_id = ? AND opt = ?", rec.Id, option); err != nil {
		return nil, err
	}
	if err = writePollData(b, rec); err != nil {
		return nil, err
	}
	return &PollChange{Changed: true, WithPoll: true,
		Service: mtproto.MakeTLMessageActionPollDeleteAnswer(&mtproto.MessageAction{Answer: removed}).To_MessageAction()}, nil
}

func writePollData(b *userbox.Box, rec *polls.Record) error {
	data, err := jsonx.MarshalToString(rec.Poll)
	if err != nil {
		return err
	}
	_, err = b.Tx().Exec("UPDATE polls SET poll_data = ?, next_option = ? WHERE id = ?", data, rec.NextOption, rec.Id)
	return err
}

// ReceivePollUpdate builds, in b's transaction, updateMessagePoll of the
// poll as b's owner sees it after a change by fromId: the owner's copy, or
// the poll's first place when the owner is its creator and has no copy of
// the message voted on. A vote on a public poll marks the creator's unread
// votes; the creator gets its results in full (has_unread_votes,
// api/poll), everyone else min. nil when the owner has no copy.
func (d *Dao) ReceivePollUpdate(b *userbox.Box, fromId int64, dialogMessageId, pollId int64, withPoll bool) (*mtproto.Update, error) {
	owner := b.UserId()
	rec, err := lockPoll(b, pollId, false)
	if err != nil || rec == nil {
		return nil, err
	}
	creator := owner == rec.Creator
	peer, msgId := mtproto.MakePeer(rec.PeerType, rec.PeerId), rec.MessageId
	if dialogMessageId != 0 {
		t, err := d.OwnCopy(b, dialogMessageId)
		if err != nil {
			return nil, err
		}
		switch {
		case t != nil:
			peer, msgId = t.Peer.toPeer(), t.MessageId
		case !creator:
			return nil, nil
		}
	} else if !creator {
		return nil, nil
	}
	if creator && !withPoll && fromId != owner && rec.Poll.GetPublicVoters() && !rec.HasUnread {
		if _, err = b.Tx().Exec("UPDATE polls SET has_unread = 1 WHERE id = ?", rec.Id); err != nil {
			return nil, err
		}
		rec.HasUnread = true
	}
	rows, err := pollVotes(b, rec.Id)
	if err != nil {
		return nil, err
	}
	mode := polls.Min
	if creator {
		mode = polls.Full
	}
	poll, results := rec.View(polls.Tally(rows, owner), owner, mode, b.Date())
	return polls.Update(peer, msgId, pollId, poll, results, withPoll), nil
}

// ReadPollVotes clears b's owner's unread votes of its polls of the dialog
// with peerType/peerId; in a channel top_msg_id narrows it to a topic
// (messages.readPollVotes).
func (d *Dao) ReadPollVotes(b *userbox.Box, peerType int32, peerId int64, topMsgId int32) error {
	where := "creator_user_id = ? AND has_unread = 1 AND peer_type = ? AND peer_id = ?"
	args := []any{b.UserId(), peerType, peerId}
	if topMsgId != 0 && peerType == mtproto.PEER_CHANNEL {
		where += " AND message_id IN (SELECT m.id FROM channel_messages m JOIN channels c ON c.id = m.channel_id WHERE m.channel_id = ? AND IF(c.forum, m.topic_id = ?, m.reply_top_id = ?))"
		args = append(args, peerId, channelbox.TopicColumn(topMsgId), topMsgId)
	}
	_, err := b.Tx().Exec("UPDATE polls SET has_unread = 0 WHERE "+where, args...)
	return err
}

// readOwnPollVotes clears the unread votes of b's owner's own polls among
// rows: Desktop marks them seen with messages.readMessageContents as they
// scroll by (history_inner_widget.cpp:1558), iOS with readPollVotes.
func readOwnPollVotes(b *userbox.Box, rows []dataobject.MessagesDO) error {
	var ids []any
	for _, r := range rows {
		if r.SenderUserId == b.UserId() {
			ids = append(ids, r.DialogMessageId)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := b.Tx().Exec("UPDATE polls SET has_unread = 0 WHERE creator_user_id = ? AND has_unread = 1 AND dialog_message_id IN ("+inList(len(ids))+")",
		append([]any{b.UserId()}, ids...)...)
	return err
}

// DuePolls are the polls whose closing date has passed and which are not
// closed yet, for the sweep. Several instances may read the same polls;
// DueClose claims each one under its lock.
func (d *Dao) DuePolls(ctx context.Context, now int64, limit int) ([]*polls.Record, error) {
	var rows []polls.Row
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &rows, "SELECT "+polls.Columns+" FROM polls WHERE closed = 0 AND close_date > 0 AND close_date <= ? LIMIT ?", now, limit); err != nil {
		return nil, err
	}
	out := make([]*polls.Record, 0, len(rows))
	for i := range rows {
		rec, err := rows[i].Decode()
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// errPollClosed: another sweep or a client closed the poll first.
var errPollClosed = errors.New("msg: poll already closed")

// DueClose closes poll id in b's transaction and returns it, with the
// results for everyone; errPollClosed when it was closed meanwhile (the
// caller just moves on).
func (d *Dao) DueClose(b *userbox.Box, id int64) (*polls.Record, *mtproto.Poll, *mtproto.PollResults, error) {
	rec, err := lockPoll(b, id, true)
	if err != nil {
		return nil, nil, nil, err
	}
	if rec == nil || rec.Closed {
		return nil, nil, nil, errPollClosed
	}
	if err = closeRecord(b, rec); err != nil {
		return nil, nil, nil, err
	}
	rows, err := pollVotes(b, rec.Id)
	if err != nil {
		return nil, nil, nil, err
	}
	poll, results := rec.View(polls.Tally(rows, b.UserId()), b.UserId(), polls.Min, b.Date())
	return rec, poll, results, nil
}

// IsPollClosed tells whether err is errPollClosed.
func IsPollClosed(err error) bool { return errors.Is(err, errPollClosed) }

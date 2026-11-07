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
	"errors"
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Messages of channels and supergroups (task 039, ADR 0034): one copy in
// channel_messages, one row of the channel's journal each, in the sender's
// box transaction with the channel locked after the sender.

// ChannelSent is a message sent to a channel.
type ChannelSent struct {
	RandomId int64
	Message  *mtproto.Message // as stored: out not set
	Pts      int32
}

type senderRow struct {
	Broadcast           bool  `db:"broadcast"`
	Deleted             bool  `db:"deleted"`
	DefaultBannedRights int32 `db:"default_banned_rights"`
	Role                int32 `db:"role"`
	State               int32 `db:"state"`
	AdminRights         int32 `db:"admin_rights"`
	BannedRights        int32 `db:"banned_rights"`
	UntilDate           int32 `db:"until_date"`
}

// sender is b's owner as a participant of a channel, for channelperm.
type sender struct {
	ch channelperm.Channel
	p  channelperm.Participant
}

func (s *sender) can(a channelperm.Action, now int64) error {
	return channelperm.Can(s.ch, s.p, a, int32(now))
}

// channelSender reads b's owner as a participant of the channel. It is
// called with the channel locked (channelbox.Open), so that a change of
// rights either comes before the check or waits for the send. Out of the
// channel is CHANNEL_PRIVATE.
func channelSender(b *userbox.Box, channelId int64) (*sender, error) {
	var rows []senderRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT c.broadcast, c.deleted, c.default_banned_rights, COALESCE(p.role, 0) AS role, COALESCE(p.state, ?) AS state,"+
			" COALESCE(p.admin_rights, 0) AS admin_rights, COALESCE(p.banned_rights, 0) AS banned_rights, COALESCE(p.until_date, 0) AS until_date"+
			" FROM channels c LEFT JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = ? WHERE c.id = ?",
		channelperm.StateLeft, b.UserId(), channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].Deleted {
		return nil, mtproto.ErrChannelInvalid
	}
	r := rows[0]
	if r.State != channelperm.StateIn {
		return nil, mtproto.ErrChannelPrivate
	}
	return &sender{
		ch: channelperm.Channel{Broadcast: r.Broadcast, Default: r.DefaultBannedRights},
		p: channelperm.Participant{Role: r.Role, State: r.State, AdminRights: r.AdminRights,
			BannedRights: r.BannedRights, UntilDate: r.UntilDate},
	}, nil
}

// openChannel locks the channel in b's transaction and reads its sender.
func openChannel(b *userbox.Box, channelId int64) (*channelbox.Channel, *sender, error) {
	ch, err := channelbox.Open(b, channelId)
	if err != nil {
		if errors.Is(err, channelbox.ErrNoChannel) {
			return nil, nil, mtproto.ErrChannelInvalid
		}
		return nil, nil, err
	}
	s, err := channelSender(b, channelId)
	if err != nil {
		return nil, nil, err
	}
	return ch, s, nil
}

// checkSend checks that s may send m, each kind of it with its own error
// (chatBannedRights), and drops its link preview when s may not embed
// links: the official clients make none then, and a preview the server
// made is not one the member chose.
func checkSend(s *sender, m *mtproto.Message, now int64) error {
	for _, a := range channelperm.MessageActions(m) {
		if err := s.can(a, now); err != nil {
			return err
		}
	}
	if m.GetMedia().GetPredicateName() == mtproto.Predicate_messageMediaWebPage && s.can(channelperm.EmbedLinks, now) != nil {
		m.Media = nil
	}
	return nil
}

type channelSentRow struct {
	Id          int32  `db:"id"`
	RandomId    int64  `db:"random_id"`
	Pts         int32  `db:"pts"`
	MessageData string `db:"message_data"`
}

// ChannelSentBefore returns the messages b's owner already sent to the
// channel with randomIds, all of them, or nil when it sent none of them:
// a repeated send gets the first answer, a partial repeat is
// RANDOM_ID_DUPLICATE (ADR 0034, p. 11; the unique key of out_random_id
// keeps it beyond any cache).
func (d *Dao) ChannelSentBefore(b *userbox.Box, channelId int64, randomIds []int64) ([]ChannelSent, error) {
	if len(randomIds) == 0 {
		return nil, nil
	}
	args := []any{channelId, b.UserId()}
	for _, id := range randomIds {
		args = append(args, id)
	}
	var rows []channelSentRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT id, random_id, pts, message_data FROM channel_messages WHERE channel_id = ? AND sender_user_id = ? AND out_random_id IN (?"+strings.Repeat(", ?", len(randomIds)-1)+") ORDER BY id",
		args...); err != nil {
		return nil, err
	}
	switch {
	case len(rows) == 0:
		return nil, nil
	case len(rows) != len(randomIds):
		return nil, mtproto.ErrRandomIdDuplicate
	}
	out := make([]ChannelSent, 0, len(rows))
	for _, r := range rows {
		var m mtproto.Message
		if err := jsonx.UnmarshalFromString(r.MessageData, &m); err != nil {
			return nil, err
		}
		out = append(out, ChannelSent{RandomId: r.RandomId, Message: &m, Pts: r.Pts})
	}
	return out, nil
}

// SendChannel writes the messages of b's owner to the channel: each gets the
// channel's next id and pts, as a post in a channel (no author) or from the
// owner in a supergroup. mentioned are the users the messages mention or
// reply to, other than the owner, who get rows of channel_message_mentions.
// The owner's dialog moves to the last message, read.
func (d *Dao) SendChannel(b *userbox.Box, channelId int64, outgoing []Outgoing) (sent []ChannelSent, mentioned []int64, disc *Discussion, err error) {
	// The discussion group of a broadcast channel is locked with it, in
	// ascending order: its link is read first, and checked once locked.
	linked, err := linkedGroup(b, channelId)
	if err != nil {
		return nil, nil, nil, err
	}
	if linked != 0 && linked < channelId {
		if _, err = channelbox.Open(b, linked); err != nil {
			return nil, nil, nil, err
		}
	}
	ch, s, err := openChannel(b, channelId)
	if err != nil {
		return nil, nil, nil, err
	}
	var group *channelbox.Channel
	if s.ch.Broadcast && linked != 0 {
		now, err := linkedGroup(b, channelId)
		if err != nil {
			return nil, nil, nil, err
		}
		if now == linked {
			if group, err = channelbox.Open(b, linked); err != nil {
				return nil, nil, nil, err
			}
			disc = &Discussion{GroupId: linked}
		}
	}
	for _, o := range outgoing {
		m := proto.Clone(o.Message).(*mtproto.Message)
		if err = checkSend(s, m, b.Date()); err != nil {
			return nil, nil, nil, err
		}
		m.Id = ch.NextMessageId()
		m.Out = false
		m.PeerId = mtproto.MakePeerChannel(channelId)
		m.Date = int32(b.Date())
		if s.ch.Broadcast {
			m.Post = true
			m.FromId = nil
		} else {
			m.FromId = mtproto.MakePeerUser(b.UserId())
		}
		// Mentions: the users of the text's mentions, the author of the
		// message replied to.
		var who []int64
		for _, e := range m.GetEntities() {
			if id := e.GetUserId_INT64(); id != 0 {
				who = append(who, id)
			}
		}
		// A reply is in the thread of the message replied to: the thread
		// that message is in, or the one it starts (task 048).
		var top int32
		if r := m.GetReplyTo().GetReplyToMsgId(); r != 0 {
			var replied []struct {
				SenderUserId int64 `db:"sender_user_id"`
				ReplyTopId   int32 `db:"reply_top_id"`
			}
			if err = b.Tx().QueryRowsPartial(&replied,
				"SELECT sender_user_id, reply_top_id FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0", channelId, r); err != nil {
				return nil, nil, nil, err
			}
			switch {
			case len(replied) == 0:
				m.ReplyTo = nil
			case !s.ch.Broadcast:
				who = append(who, replied[0].SenderUserId)
				top = r
				m.ReplyTo.ReplyToTopId = nil
				if replied[0].ReplyTopId != 0 {
					top = replied[0].ReplyTopId
					m.ReplyTo.ReplyToTopId = wrapperspb.Int32(top)
				}
			}
		}
		ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
		data, err := jsonx.MarshalToString(m)
		if err != nil {
			return nil, nil, nil, err
		}
		if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, random_id, pts, message_filter_type, message_data, message, reply_top_id, date2) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			channelId, m.Id, b.UserId(), o.RandomId, ch.Pts(), mtproto.GetMediaType(m), data, m.GetMessage(), top, b.Date()); err != nil {
			return nil, nil, nil, err
		}
		for _, id := range who {
			if id == b.UserId() {
				continue
			}
			if _, err = b.Tx().Exec("INSERT IGNORE INTO channel_message_mentions (channel_id, user_id, message_id) VALUES (?, ?, ?)", channelId, id, m.Id); err != nil {
				return nil, nil, nil, err
			}
			if !slices.Contains(mentioned, id) {
				mentioned = append(mentioned, id)
			}
		}
		sent = append(sent, ChannelSent{RandomId: o.RandomId, Message: m, Pts: ch.Pts()})
		if group != nil {
			c, err := postCopy(b, group, channelId, m)
			if err != nil {
				return nil, nil, nil, err
			}
			disc.Copies = append(disc.Copies, c)
		}
	}
	if err = movedTo(b, channelId, sent[len(sent)-1].Message.Id); err != nil {
		return nil, nil, nil, err
	}
	if disc != nil && len(disc.Copies) > 0 {
		if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", disc.Copies[len(disc.Copies)-1].Message.Id, disc.GroupId); err != nil {
			return nil, nil, nil, err
		}
	}
	return sent, mentioned, disc, nil
}

// Discussion is what a post wrote into the discussion group of its channel:
// the copy of each message (task 048).
type Discussion struct {
	GroupId int64
	Copies  []ChannelSent
}

// linkedGroup is the discussion group of a broadcast channel, 0 for none.
func linkedGroup(b *userbox.Box, channelId int64) (int64, error) {
	var linked []int64
	if err := b.Tx().QueryRowsPartial(&linked,
		"SELECT linked_chat_id FROM channels WHERE id = ? AND broadcast = 1 AND deleted = 0", channelId); err != nil {
		return 0, err
	}
	if len(linked) == 0 {
		return 0, nil
	}
	return linked[0], nil
}

// postCopy writes the copy of post m of channelId into its discussion
// group, locked: from the channel and forwarded from it
// (core.telegram.org/api/discussion: the thread of the "automatically
// forwarded channel message"), with the post's text and media. Its thread
// is the post's comments.
func postCopy(b *userbox.Box, group *channelbox.Channel, channelId int64, post *mtproto.Message) (ChannelSent, error) {
	m := proto.Clone(post).(*mtproto.Message)
	from := mtproto.MakePeerChannel(channelId)
	m.Id = group.NextMessageId()
	m.PeerId = mtproto.MakePeerChannel(group.Id())
	m.FromId = from
	m.Post = false
	m.ReplyTo = nil
	m.Replies = nil
	m.Pinned = false
	m.FwdFrom = mtproto.MakeTLMessageFwdHeader(&mtproto.MessageFwdHeader{
		FromId:         from,
		Date:           post.Date,
		ChannelPost:    mtproto.MakeFlagsInt32(post.Id),
		SavedFromPeer:  from,
		SavedFromMsgId: mtproto.MakeFlagsInt32(post.Id),
	}).To_MessageFwdHeader()
	group.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		return ChannelSent{}, err
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_filter_type, message_data, message, date2) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		group.Id(), m.Id, b.UserId(), group.Pts(), mtproto.GetMediaType(m), data, m.GetMessage(), b.Date()); err != nil {
		return ChannelSent{}, err
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_discussion_posts (channel_id, post_id, group_id, group_message_id) VALUES (?, ?, ?, ?)",
		channelId, post.Id, group.Id(), m.Id); err != nil {
		return ChannelSent{}, err
	}
	return ChannelSent{Message: m, Pts: group.Pts()}, nil
}

// movedTo moves the channel's top message to last, b's owner's message, and
// the owner's dialog with it, read.
func movedTo(b *userbox.Box, channelId int64, last int32) error {
	if _, err := b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", last, channelId); err != nil {
		return err
	}
	b.InvalidateCache(dialog.GetDialogCacheKeyByPeer(b.UserId(), mtproto.PEER_CHANNEL, channelId))
	res, err := b.Tx().Exec("UPDATE dialogs SET top_message = ?, read_inbox_max_id = ?, date2 = ?, deleted = 0 WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		last, last, b.Date(), b.UserId(), mtproto.PEER_CHANNEL, channelId)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err = b.Tx().Exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, top_message, read_inbox_max_id, draft_message_data, date2) VALUES (?, ?, ?, ?, ?, ?, 'null', ?)",
			b.UserId(), mtproto.PEER_CHANNEL, channelId, mtproto.MakePeerDialogId(mtproto.PEER_CHANNEL, channelId), last, last, b.Date()); err != nil {
			return err
		}
	}
	return nil
}

// EmitChannelPush adds the push of the new messages of the channel to its
// members (sync.pushChannelUpdates, keyed by the channel).
func (d *Dao) EmitChannelPush(b *userbox.Box, channelId, permAuthKeyId int64, sent []ChannelSent, mentioned []int64) error {
	updates := make([]*mtproto.Update, 0, len(sent))
	for _, s := range sent {
		updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: s.Message, Pts_INT32: s.Pts, PtsCount: 1}).To_Update())
	}
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId:     channelId,
		FromId:        b.UserId(),
		FromAuthKeyId: permAuthKeyId,
		Mentioned:     mentioned,
		Updates:       mtproto.MakeUpdatesByUpdates(updates...),
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: channelId, Method: string(proto.MessageName(in)), Data: data})
	return nil
}

// InvitedToChannel shows the channel in b's owner's dialogs when it is in
// it (inbox.channelInvitedToInbox, task 041): the dialog is read up to the
// last message, as a join leaves it. It tells whether the owner is in the
// channel.
func (d *Dao) InvitedToChannel(b *userbox.Box, channelId int64) (bool, error) {
	var tops []int32
	if err := b.Tx().QueryRowsPartial(&tops,
		"SELECT c.top_message FROM channels c JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = ? AND p.state = 0 WHERE c.id = ? AND c.deleted = 0",
		b.UserId(), channelId); err != nil {
		return false, err
	}
	if len(tops) == 0 {
		return false, nil
	}
	b.InvalidateCache(dialog.GetDialogCacheKeyByPeer(b.UserId(), mtproto.PEER_CHANNEL, channelId))
	res, err := b.Tx().Exec("UPDATE dialogs SET top_message = ?, read_inbox_max_id = GREATEST(read_inbox_max_id, ?), date2 = ?, deleted = 0 WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		tops[0], tops[0], b.Date(), b.UserId(), mtproto.PEER_CHANNEL, channelId)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		if _, err = b.Tx().Exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, top_message, read_inbox_max_id, draft_message_data, date2) VALUES (?, ?, ?, ?, ?, ?, 'null', ?)",
			b.UserId(), mtproto.PEER_CHANNEL, channelId, mtproto.MakePeerDialogId(mtproto.PEER_CHANNEL, channelId), tops[0], tops[0], b.Date()); err != nil {
			return false, err
		}
	}
	return true, nil
}

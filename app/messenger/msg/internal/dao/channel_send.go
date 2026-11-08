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
	Signatures          bool  `db:"signatures"`
	SignatureProfiles   bool  `db:"signature_profiles"`
	SlowmodeSeconds     int32 `db:"slowmode_seconds"`
	LastSendDate        int64 `db:"last_send_date"`
	Forum               bool  `db:"forum"`
}

// sender is b's owner as a participant of a channel, for channelperm.
type sender struct {
	ch channelperm.Channel
	p  channelperm.Participant
	// guest: not in the discussion group, writing to it as a member of
	// its channel (task 048); a member with the group's default rights,
	// without the group in its dialogs.
	guest bool
	// signatures: the channel signs its posts with their authors (task 044).
	signatures bool
	// signatureProfiles: the signature links to the author (task 045).
	signatureProfiles bool
	// slowmodeSeconds and lastSend: the slow mode of the group and the
	// owner's last message under it (task 045).
	slowmodeSeconds int32
	lastSend        int64
	// forum: its messages are in topics (task 060).
	forum bool
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
		"SELECT c.broadcast, c.deleted, c.default_banned_rights, c.signatures, c.signature_profiles, c.slowmode_seconds, c.forum, COALESCE(p.role, 0) AS role, COALESCE(p.state, ?) AS state,"+
			" COALESCE(p.admin_rights, 0) AS admin_rights, COALESCE(p.banned_rights, 0) AS banned_rights, COALESCE(p.until_date, 0) AS until_date,"+
			" COALESCE(p.last_send_date, 0) AS last_send_date"+
			" FROM channels c LEFT JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = ? WHERE c.id = ?",
		channelperm.StateLeft, b.UserId(), channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].Deleted {
		return nil, mtproto.ErrChannelInvalid
	}
	r := rows[0]
	if r.State != channelperm.StateIn {
		if r.State == channelperm.StateKicked {
			return nil, mtproto.ErrChannelPrivate
		}
		guest, err := discussionGuest(b, channelId)
		if err != nil {
			return nil, err
		}
		if !guest {
			return nil, mtproto.ErrChannelPrivate
		}
		return &sender{
			ch:    channelperm.Channel{Broadcast: r.Broadcast, Default: r.DefaultBannedRights},
			p:     channelperm.Participant{Role: channelperm.RoleMember, State: channelperm.StateIn},
			guest: true,

			slowmodeSeconds: r.SlowmodeSeconds,
			lastSend:        r.LastSendDate,
		}, nil
	}
	return &sender{
		signatures:        r.Signatures,
		signatureProfiles: r.SignatureProfiles,
		slowmodeSeconds:   r.SlowmodeSeconds,
		lastSend:          r.LastSendDate,
		forum:             r.Forum,
		ch:                channelperm.Channel{Broadcast: r.Broadcast, Default: r.DefaultBannedRights},
		p: channelperm.Participant{Role: r.Role, State: r.State, AdminRights: r.AdminRights,
			BannedRights: r.BannedRights, UntilDate: r.UntilDate},
	}, nil
}

// discussionGuest tells whether b's owner, not in the supergroup channelId,
// may write to it: it is the discussion group of a broadcast channel the
// owner is in, and it does not ask commenters to join (join_to_send,
// core.telegram.org/method/channels.toggleJoinToSend).
func discussionGuest(b *userbox.Box, channelId int64) (bool, error) {
	var ok []int64
	err := b.Tx().QueryRowsPartial(&ok,
		"SELECT b.id FROM channels g JOIN channels b ON b.id = g.linked_chat_id AND b.broadcast = 1 AND b.deleted = 0 AND b.linked_chat_id = g.id"+
			" JOIN channel_participants p ON p.channel_id = b.id AND p.user_id = ? AND p.state = 0"+
			" WHERE g.id = ? AND g.megagroup = 1 AND g.join_to_send = 0",
		b.UserId(), channelId)
	return len(ok) > 0, err
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

// checkSlowmode answers whether s may send outgoing now under the slow mode
// of the group: one message, or one album (a forwarded one too: tdesktop
// history_item_helpers.cpp:158-173; the rest is
// SLOWMODE_MULTI_MSGS_DISABLED, core.telegram.org/method/messages.forwardMessages),
// every so many seconds (SLOWMODE_WAIT_X). It runs with the channel locked,
// and the send records its time in the same transaction: of two sends at
// once, the second finds the first's.
func checkSlowmode(s *sender, outgoing []Outgoing, now int64) error {
	if !channelperm.SlowmodeApplies(s.slowmodeSeconds, s.p.Role) {
		return nil
	}
	if !oneSend(outgoing) {
		return mtproto.ErrSlowmodeMultiMsgsDisabled
	}
	if wait := channelperm.SlowmodeWait(s.slowmodeSeconds, s.lastSend, now); wait > 0 {
		return mtproto.NewErrSlowModeWaitX(wait)
	}
	return nil
}

// oneSend reports whether outgoing is one message or one album.
func oneSend(outgoing []Outgoing) bool {
	if len(outgoing) <= 1 {
		return true
	}
	album := outgoing[0].Message.GetGroupedId().GetValue()
	if album == 0 {
		return false
	}
	for _, o := range outgoing[1:] {
		if o.Message.GetGroupedId().GetValue() != album {
			return false
		}
	}
	return true
}

// recordSlowmode remembers b's owner's send under the slow mode. A guest of a
// discussion group has no row of its own: it gets one as out of the group,
// which is what no row reads as, and joining keeps the date.
func recordSlowmode(b *userbox.Box, channelId int64) error {
	_, err := b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, state, last_send_date) VALUES (?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE last_send_date = VALUES(last_send_date)",
		channelId, b.UserId(), channelperm.StateLeft, b.Date())
	return err
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
func (d *Dao) SendChannel(b *userbox.Box, channelId int64, outgoing []Outgoing, author string) (sent []ChannelSent, mentioned []int64, disc *Discussion, err error) {
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
	if err = checkSlowmode(s, outgoing, b.Date()); err != nil {
		return nil, nil, nil, err
	}
	var lastTopic int32
	for _, o := range outgoing {
		m := proto.Clone(o.Message).(*mtproto.Message)
		// from_scheduled is the sender's alone, pushed by flagFromScheduled;
		// the one copy of the message does not hold it.
		m.FromScheduled = false
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
			// A signed post names its author (channels.toggleSignatures).
			// With signature_profiles it also comes from the author, whose
			// profile the clients then open, showing the sender in place of
			// the signature (tdesktop history_view_bottom_info.cpp:677-685,
			// Android ChatMessageCell.java:18375-18377).
			m.PostAuthor = nil
			if s.signatures && author != "" {
				m.PostAuthor = wrapperspb.String(author)
			}
			if s.signatures && s.signatureProfiles {
				m.FromId = mtproto.MakePeerUser(b.UserId())
			}
		} else {
			m.FromId = mtproto.MakePeerUser(b.UserId())
		}
		if s.ch.Broadcast {
			// A post starts with its author's view (task 046): the clients
			// show 1 on a post they have just sent (Android
			// SendMessagesHelper.java:2424, tdesktop history_item.cpp:698,
			// iOS EnqueueMessage.swift:1012, Web K appMessagesManager.ts:
			// 4093). Both are one flag of message.
			m.Views, m.Forwards = wrapperspb.Int32(1), wrapperspb.Int32(0)
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
		var top, topicId int32
		if s.forum {
			var replied int64
			if topicId, replied, err = routeToTopic(b, channelId, s, m, b.Date()); err != nil {
				return nil, nil, nil, err
			}
			if replied != 0 {
				who = append(who, replied)
			}
		} else if r := m.GetReplyTo().GetReplyToMsgId(); r == 0 {
			// A top_msg_id means nothing here: nothing is a topic.
			if m.GetReplyTo().GetReplyToTopId() != nil {
				if replyStandsAlone(m.GetReplyTo()) {
					m.ReplyTo.ReplyToTopId = nil
				} else {
					m.ReplyTo = nil
				}
			}
		} else {
			var replied []struct {
				SenderUserId int64 `db:"sender_user_id"`
				ReplyTopId   int32 `db:"reply_top_id"`
				Deleted      bool  `db:"deleted"`
			}
			if err = b.Tx().QueryRowsPartial(&replied,
				"SELECT sender_user_id, reply_top_id, deleted FROM channel_messages WHERE channel_id = ? AND id = ?", channelId, r); err != nil {
				return nil, nil, nil, err
			}
			alive := len(replied) > 0 && !replied[0].Deleted
			inThread := alive
			if len(replied) > 0 && !alive && !s.ch.Broadcast {
				// A thread outlives its deleted message: tdesktop keeps it
				// open by its root's id and replies to that id
				// (ChatWidget::replyTo, history_view_chat_section.cpp).
				if inThread, err = threadStays(b, channelId, r, replied[0].ReplyTopId); err != nil {
					return nil, nil, nil, err
				}
			}
			switch {
			case !inThread:
				m.ReplyTo = nil
			case !s.ch.Broadcast:
				if alive {
					who = append(who, replied[0].SenderUserId)
				}
				top = r
				m.ReplyTo.ReplyToTopId = nil
				if replied[0].ReplyTopId != 0 {
					top = replied[0].ReplyTopId
					m.ReplyTo.ReplyToTopId = wrapperspb.Int32(top)
				}
			}
		}
		if group != nil {
			// A post with comments says so itself, as every view of it
			// (the push, the difference, the dialogs) carries it: the
			// clients show the comments button by it (Android
			// MessageObject.isComments, messageOwner.replies.comments).
			// The history fills in the counts when read.
			m.Replies = mtproto.MakeTLMessageReplies(&mtproto.MessageReplies{
				Comments:  true,
				ChannelId: mtproto.MakeFlagsInt64(group.Id()),
				// The pts of the post itself, which AddPtsUpdate below
				// gives it ("PTS of the message that started this
				// thread", constructor/messageReplies).
				RepliesPts:     ch.Pts() + 1,
				RecentRepliers: []*mtproto.Peer{},
			}).To_MessageReplies()
		}
		ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
		data, err := jsonx.MarshalToString(m)
		if err != nil {
			return nil, nil, nil, err
		}
		if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, random_id, pts, message_filter_type, message_data, message, reply_top_id, topic_id, date2) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			channelId, m.Id, b.UserId(), o.RandomId, ch.Pts(), mtproto.GetMediaType(m), data, m.GetMessage(), top, channelbox.TopicColumn(topicId), b.Date()); err != nil {
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
		lastTopic = topicId
		if group != nil {
			c, err := postCopy(b, group, channelId, m)
			if err != nil {
				return nil, nil, nil, err
			}
			disc.Copies = append(disc.Copies, c)
		}
	}
	if channelperm.SlowmodeApplies(s.slowmodeSeconds, s.p.Role) {
		if err = recordSlowmode(b, channelId); err != nil {
			return nil, nil, nil, err
		}
	}
	switch {
	case s.forum:
		err = channelbox.ForumMoved(b, channelId, lastTopic, sent[len(sent)-1].Message.Id)
		b.InvalidateCache(dialog.GetDialogCacheKeyByPeer(b.UserId(), mtproto.PEER_CHANNEL, channelId))
	case s.guest:
		// The group's top message moves; the guest has no dialog with it.
		_, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", sent[len(sent)-1].Message.Id, channelId)
	default:
		err = movedTo(b, channelId, sent[len(sent)-1].Message.Id)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if disc != nil && len(disc.Copies) > 0 {
		if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", disc.Copies[len(disc.Copies)-1].Message.Id, disc.GroupId); err != nil {
			return nil, nil, nil, err
		}
	}
	return sent, mentioned, disc, nil
}

// threadStays tells whether the deleted message id of a supergroup, itself
// a reply in the thread top (0: it was in none), leaves a thread to reply
// in.
func threadStays(b *userbox.Box, channelId int64, id, top int32) (bool, error) {
	if top != 0 {
		return true, nil
	}
	var replies []int32
	if err := b.Tx().QueryRowsPartial(&replies,
		"SELECT id FROM channel_messages WHERE channel_id = ? AND reply_top_id = ? AND deleted = 0 LIMIT 1", channelId, id); err != nil {
		return false, err
	}
	return len(replies) > 0, nil
}

// routeToTopic puts m in its topic of the forum and writes its reply_to as
// the clients read it (Android MessageObject.getTopicId, tdesktop
// HistoryItem::topicRootId): in a topic other than General forum_topic is
// set, and reply_to_msg_id is the topic's id or, for a reply within the
// topic, the message replied to with reply_to_top_id the topic; in General
// neither. The topic comes from the message replied to; if that is gone,
// from top_msg_id (core.telegram.org/api/forum); else General. A reply to a
// message of another topic names the forum as reply_to_peer_id (tdesktop
// data_histories.cpp:75-77, Android SendMessagesHelper.java:4947-4953): it
// stays in the topic of top_msg_id. It returns the topic and the author of
// the message replied to (0 for none).
// TOPIC_ID_INVALID for a topic that never was, TOPIC_DELETED for a deleted
// one, TOPIC_CLOSED for a closed one unless its author or one who manages
// topics writes (tdesktop data_peer_values.cpp:207).
func routeToTopic(b *userbox.Box, channelId int64, s *sender, m *mtproto.Message, now int64) (int32, int64, error) {
	h := m.GetReplyTo()
	r, top := h.GetReplyToMsgId(), h.GetReplyToTopId().GetValue()
	external := h.GetReplyToPeerId().GetChannelId() == channelId
	topicId := int32(channelbox.GeneralTopicId)
	if top != 0 {
		topicId = top
	}
	var author int64
	if r != 0 {
		var replied []struct {
			SenderUserId int64 `db:"sender_user_id"`
			TopicId      int32 `db:"topic_id"`
		}
		if err := b.Tx().QueryRowsPartial(&replied,
			"SELECT sender_user_id, topic_id FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0", channelId, r); err != nil {
			return 0, 0, err
		}
		if len(replied) == 0 {
			r = 0
		} else {
			if !external {
				topicId = max(replied[0].TopicId, channelbox.GeneralTopicId)
			}
			if r != topicId {
				author = replied[0].SenderUserId
			} else {
				r = 0
			}
		}
	}
	if r == 0 && external {
		h.ReplyToPeerId = nil
	}
	var rows []struct {
		Creator int64 `db:"creator_user_id"`
		Closed  bool  `db:"closed"`
		Deleted bool  `db:"deleted"`
	}
	if err := b.Tx().QueryRowsPartial(&rows, "SELECT creator_user_id, closed, deleted FROM channel_forum_topics WHERE channel_id = ? AND topic_id = ?", channelId, topicId); err != nil {
		return 0, 0, err
	}
	switch {
	case len(rows) == 0:
		return 0, 0, mtproto.ErrTopicIdInvalid
	case rows[0].Deleted:
		return 0, 0, mtproto.ErrTopicDeleted
	case rows[0].Closed && rows[0].Creator != b.UserId() && !topicManager(s, now):
		return 0, 0, mtproto.ErrTopicClosed
	}
	if topicId == channelbox.GeneralTopicId {
		if h != nil {
			h.ForumTopic = false
			h.ReplyToTopId = nil
		}
		if r == 0 && !replyStandsAlone(h) {
			m.ReplyTo = nil
		}
		return topicId, author, nil
	}
	if h == nil {
		h = mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{}).To_MessageReplyHeader()
		m.ReplyTo = h
	}
	h.ForumTopic = true
	h.ReplyToTopId = nil
	id := topicId
	if r != 0 {
		id = r
		h.ReplyToTopId = wrapperspb.Int32(topicId)
	}
	h.ReplyToMsgId, h.ReplyToMsgId_INT32, h.ReplyToMsgId_FLAGINT32 = id, id, mtproto.MakeFlagsInt32(id)
	return topicId, author, nil
}

// replyStandsAlone tells whether a reply header without a message of this
// chat says what it replies to: a story, or a message of another chat, whose
// origin is reply_from (bff otherChatReply).
func replyStandsAlone(h *mtproto.MessageReplyHeader) bool {
	return h.GetPredicateName() == mtproto.Predicate_messageReplyStoryHeader || h.GetReplyFrom() != nil
}

// topicManager tells whether s manages the topics: the creator, an admin
// with manage_topics (Android ChatObject.canManageTopics).
func topicManager(s *sender, now int64) bool {
	return s.p.Role == channelperm.RoleCreator ||
		s.p.Role == channelperm.RoleAdmin && s.can(channelperm.ManageTopics, now) == nil
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
	m.Views, m.Forwards = nil, nil
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

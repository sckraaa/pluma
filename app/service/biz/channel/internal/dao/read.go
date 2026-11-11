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
	"fmt"
	"github.com/teamgram/teamgram-server/pkg/adminlog"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/readmarks"
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelcontents"
	"github.com/teamgram/teamgram-server/pkg/polls"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// The reading of a channel (task 043, ADR 0034, p. 1 and 3): the reader's
// state is its row of dialogs, written in its own box transaction; the
// channel is not locked and its pts does not move.

// readAuthorsMax is the most authors one read tells that their messages
// were read: those of the newest messages of the range.
const readAuthorsMax = 1000

type readRow struct {
	ReadInboxMaxId int32 `db:"read_inbox_max_id"`
	FolderId       int32 `db:"folder_id"`
	TopMessage     int32 `db:"top_message"`
	Pts            int32 `db:"pts"`
	Broadcast      bool  `db:"broadcast"`
	Forum          bool  `db:"forum"`
	AvailableMinId int32 `db:"available_min_id"`
}

// Read marks the channel read by b's owner up to maxId (0 or above the
// last message: up to the last message). It moves the dialog's
// read_inbox_max_id forward only, marks the mentions up to it read and
// journals updateReadChannelInbox with the channel's pts and the messages
// still unread for the owner's devices (ADR 0034, p. 3: tdesktop and iOS
// trust still_unread_count at an equal pts, history.cpp:2183,
// AccountStateManagementUtils.swift:4725-4764). In a supergroup the authors
// of the messages read get updateReadChannelOutbox by push. A read that
// moves nothing writes nothing. CHANNEL_PRIVATE when the owner has no
// dialog with the channel (not in it).
func (d *Dao) Read(b *userbox.Box, channelId int64, maxId int32) error {
	r, err := readerRow(b, channelId)
	if err != nil {
		return err
	}
	if maxId <= 0 || maxId > r.TopMessage {
		maxId = r.TopMessage
	}
	if maxId <= r.ReadInboxMaxId {
		return nil
	}
	if err = d.moveRead(b, channelId, maxId, r); err != nil {
		return err
	}
	if r.Broadcast {
		// The posts of a channel show views, not reads.
		return nil
	}
	// When the owner read up to maxId, for the senders' "read by" list
	// (messages.getMessageReadParticipants); kept as long as the clients
	// ask for it.
	if _, err = b.Tx().Exec("INSERT IGNORE INTO channel_read_marks (channel_id, user_id, max_id, read_at) VALUES (?, ?, ?, ?)",
		channelId, b.UserId(), maxId, b.Date()); err != nil {
		return err
	}
	if _, err = b.Tx().Exec("DELETE FROM channel_read_marks WHERE channel_id = ? AND user_id = ? AND read_at < ?",
		channelId, b.UserId(), b.Date()-readmarks.KeepPeriod); err != nil {
		return err
	}
	var authors []int64
	if err = b.Tx().QueryRowsPartial(&authors,
		"SELECT sender_user_id FROM channel_messages WHERE channel_id = ? AND id > ? AND id <= ? AND sender_user_id <> ? AND deleted = 0"+
			" GROUP BY sender_user_id ORDER BY MAX(id) DESC LIMIT ?",
		channelId, r.ReadInboxMaxId, maxId, b.UserId(), readAuthorsMax); err != nil {
		return err
	}
	outbox := mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateReadChannelOutbox(&mtproto.Update{ChannelId: channelId, MaxId: maxId}).To_Update())
	for _, author := range authors {
		in := &sync.TLSyncPushUpdates{UserId: author, Updates: outbox}
		data, err := jsonx.Marshal(in)
		if err != nil {
			return err
		}
		b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: author, Method: string(proto.MessageName(in)), Data: data})
	}
	return nil
}

// readerRow reads the row of b's owner's dialog with the channel, locked,
// and what the channel gives it: CHANNEL_PRIVATE when it has none (not in
// the channel).
func readerRow(b *userbox.Box, channelId int64) (*readRow, error) {
	var rows []readRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT g.read_inbox_max_id, g.folder_id, c.top_message, c.pts, c.broadcast, c.forum, COALESCE(p.available_min_id, 0) AS available_min_id"+
			" FROM dialogs g JOIN channels c ON c.id = g.peer_id AND c.deleted = 0"+
			" JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = g.user_id AND p.state <> 1"+
			" WHERE g.user_id = ? AND g.peer_type = ? AND g.peer_id = ? AND g.deleted = 0 FOR UPDATE OF g",
		b.UserId(), mtproto.PEER_CHANNEL, channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrChannelPrivate
	}
	return &rows[0], nil
}

// moveRead moves the read mark of b's owner forward to maxId, marks its
// mentions up to it read and journals updateReadChannelInbox for its
// devices.
func (d *Dao) moveRead(b *userbox.Box, channelId int64, maxId int32, r *readRow) error {
	if _, err := b.Tx().Exec("UPDATE dialogs SET read_inbox_max_id = ? WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		maxId, b.UserId(), mtproto.PEER_CHANNEL, channelId); err != nil {
		return err
	}
	InvalidateDialog(b, channelId)
	if _, err := b.Tx().Exec("UPDATE channel_message_mentions SET `read` = 1 WHERE channel_id = ? AND user_id = ? AND message_id <= ? AND `read` = 0",
		channelId, b.UserId(), maxId); err != nil {
		return err
	}
	var unread []int32
	query, args := unreadQuery(b.UserId(), channelId, r.Forum, maxId, r.AvailableMinId)
	if err := b.Tx().QueryRowsPartial(&unread, query, args...); err != nil {
		return err
	}
	return d.EmitSeqUpdate(b, mtproto.MakeTLUpdateReadChannelInbox(&mtproto.Update{
		FolderId:         mtproto.MakeFlagsInt32(r.FolderId),
		ChannelId:        channelId,
		MaxId:            maxId,
		StillUnreadCount: first(unread),
		Pts_INT32:        r.Pts,
	}).To_Update(), nil, nil)
}

// ReadMentions marks every unread mention of b's owner in the channel
// read, and its devices get updateChannelReadMessagesContents with seq of
// those messages (core.telegram.org/method/messages.readMentions).
// CHANNEL_PRIVATE when the owner is not in the channel.
func (d *Dao) ReadMentions(b *userbox.Box, channelId int64) error {
	if err := readerIn(b, channelId); err != nil {
		return err
	}
	var ids []int32
	if err := b.Tx().QueryRowsPartial(&ids,
		"SELECT message_id FROM channel_message_mentions WHERE channel_id = ? AND user_id = ? AND `read` = 0 ORDER BY message_id FOR UPDATE",
		channelId, b.UserId()); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := b.Tx().Exec("UPDATE channel_message_mentions SET `read` = 1 WHERE channel_id = ? AND user_id = ? AND `read` = 0",
		channelId, b.UserId()); err != nil {
		return err
	}
	InvalidateDialog(b, channelId)
	u := mtproto.MakeTLUpdateChannelReadMessagesContents(&mtproto.Update{ChannelId: channelId, Messages: ids}).To_Update()
	return d.EmitSeqUpdate(b, u, nil, nil)
}

// ReadReactions marks seen the reactions to b's owner's messages of the
// channel, of the topic topicId unless it is 0: the dialog's and the topic's
// counters are counted from these rows. Nothing is journaled, as for a
// private chat (msg ReadReactions, task 053): the other devices read the
// counters again with the dialogs.
func (d *Dao) ReadReactions(b *userbox.Box, channelId int64, topicId int32) error {
	if err := readerIn(b, channelId); err != nil {
		return err
	}
	query := "UPDATE channel_message_reactions r JOIN channel_messages m ON m.channel_id = r.channel_id AND m.id = r.message_id" +
		" SET r.unread = 0 WHERE r.channel_id = ? AND r.unread = 1 AND m.sender_user_id = ?"
	args := []any{channelId, b.UserId()}
	if topicId != 0 {
		query += " AND m.topic_id = ?"
		args = append(args, channelbox.TopicColumn(topicId))
	}
	if _, err := b.Tx().Exec(query, args...); err != nil {
		return err
	}
	InvalidateDialog(b, channelId)
	return nil
}

// readerIn checks that b's owner is in the channel with its dialog, locked:
// CHANNEL_PRIVATE otherwise.
func readerIn(b *userbox.Box, channelId int64) error {
	var in []int32
	if err := b.Tx().QueryRowsPartial(&in,
		"SELECT g.top_message FROM dialogs g JOIN channels c ON c.id = g.peer_id AND c.deleted = 0"+
			" JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = g.user_id AND p.state = 0"+
			" WHERE g.user_id = ? AND g.peer_type = ? AND g.peer_id = ? AND g.deleted = 0 FOR UPDATE OF g",
		b.UserId(), mtproto.PEER_CHANNEL, channelId); err != nil {
		return err
	}
	if len(in) == 0 {
		return mtproto.ErrChannelPrivate
	}
	return nil
}

// ReadContentsMax is the most messages of one channels.readMessageContents,
// as of messages.readMessageContents.
const ReadContentsMax = 100

// ReadContents marks what b's owner has seen of messages ids of the
// channel: its mentions among them are read, the voice and round video
// messages among them are played (playedBy), and its devices get
// updateChannelReadMessagesContents with seq (core.telegram.org/method/
// channels.readMessageContents). Ids that are not the channel's messages
// are left out. CHANNEL_PRIVATE when the owner has no dialog with the
// channel (not in it).
func (d *Dao) ReadContents(b *userbox.Box, channelId int64, ids []int32) error {
	if err := readerIn(b, channelId); err != nil {
		return err
	}
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	ids = slices.DeleteFunc(ids, func(id int32) bool { return id <= 0 })
	if len(ids) > ReadContentsMax {
		ids = ids[:ReadContentsMax]
	}
	if len(ids) == 0 {
		return nil
	}
	args := []any{channelId}
	for _, id := range ids {
		args = append(args, id)
	}
	var rows []messageRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT id, sender_user_id, message_data FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+") ORDER BY id",
		args...); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	found := make([]int32, 0, len(rows))
	for _, r := range rows {
		found = append(found, r.Id)
	}
	if err := playedBy(b, channelId, rows); err != nil {
		return err
	}
	args = append([]any{channelId, b.UserId()}, args[1:]...)
	if _, err := b.Tx().Exec("UPDATE channel_message_mentions SET `read` = 1 WHERE channel_id = ? AND user_id = ? AND `read` = 0 AND message_id IN (?"+strings.Repeat(", ?", len(ids)-1)+")",
		args...); err != nil {
		return err
	}
	// Its messages among them: the reactions to them are seen, as for a
	// private chat (msg readOwnReactions, task 053).
	if _, err := b.Tx().Exec("UPDATE channel_message_reactions r JOIN channel_messages m ON m.channel_id = r.channel_id AND m.id = r.message_id"+
		" SET r.unread = 0 WHERE r.channel_id = ? AND r.unread = 1 AND m.sender_user_id = ? AND r.message_id IN (?"+strings.Repeat(", ?", len(ids)-1)+")",
		args...); err != nil {
		return err
	}
	// A poll of the owner among them: its votes are seen (Desktop reads
	// them this way, history_inner_widget.cpp:1558, task 062).
	if _, err := b.Tx().Exec("UPDATE polls SET has_unread = 0 WHERE creator_user_id = ? AND has_unread = 1 AND peer_type = ? AND peer_id = ? AND message_id IN (?"+strings.Repeat(", ?", len(ids)-1)+")",
		append([]any{b.UserId(), mtproto.PEER_CHANNEL, channelId}, args[2:]...)...); err != nil {
		return err
	}
	InvalidateDialog(b, channelId)
	u := mtproto.MakeTLUpdateChannelReadMessagesContents(&mtproto.Update{ChannelId: channelId, Messages: found}).To_Update()
	return d.EmitSeqUpdate(b, u, nil, nil)
}

// playedBy records that b's owner played the voice and round video
// messages among rows that others wrote: they are no longer media_unread for
// it (channelcontents).
func playedBy(b *userbox.Box, channelId int64, rows []messageRow) error {
	var played []any
	for _, r := range rows {
		if r.SenderUserId == b.UserId() {
			continue
		}
		var m mtproto.Message
		if err := jsonx.UnmarshalFromString(r.MessageData, &m); err != nil {
			return fmt.Errorf("channel: message %d of channel %d: %w", r.Id, channelId, err)
		}
		if channelcontents.Consumable(&m) {
			played = append(played, channelId, b.UserId(), r.Id)
		}
	}
	if len(played) == 0 {
		return nil
	}
	_, err := b.Tx().Exec("INSERT IGNORE INTO channel_contents_read (channel_id, user_id, message_id) VALUES (?, ?, ?)"+
		strings.Repeat(", (?, ?, ?)", len(played)/3-1), played...)
	return err
}

// ReadParticipants are the members who read the message msgId of selfId in
// a supergroup, with the date each read it, the latest first: kept for
// messages of the last readmarks.ChatExpirePeriod in groups of up to
// readmarks.ChatSizeThreshold members (core.telegram.org/api/config), as in
// a basic group. Members who left are not listed, nor reads before the
// dates were kept.
func (d *Dao) ReadParticipants(ctx context.Context, selfId, channelId int64, msgId int32, now int64) ([]*mtproto.ReadParticipantDate, error) {
	var rows []struct {
		Broadcast         bool  `db:"broadcast"`
		ParticipantsCount int32 `db:"participants_count"`
		SenderUserId      int64 `db:"sender_user_id"`
		Date              int64 `db:"date"`
		State             int32 `db:"state"`
	}
	if err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT c.broadcast, c.participants_count, m.sender_user_id, m.date2 AS `date`, p.state FROM channels c"+
			" JOIN channel_messages m ON m.channel_id = c.id AND m.id = ? AND m.deleted = 0"+
			" JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = ?"+
			" WHERE c.id = ? AND c.deleted = 0",
		msgId, selfId, channelId); err != nil {
		return nil, err
	}
	switch {
	case len(rows) == 0 || rows[0].State != channelperm.StateIn:
		return nil, mtproto.ErrMsgIdInvalid
	case rows[0].Broadcast || rows[0].SenderUserId != selfId:
		return nil, mtproto.ErrMsgIdInvalid
	case rows[0].ParticipantsCount > readmarks.ChatSizeThreshold:
		return nil, mtproto.ErrChatTooBig
	case readmarks.TooOld(rows[0].Date, now, readmarks.ChatExpirePeriod):
		return nil, mtproto.ErrMsgTooOld
	}
	var readers []struct {
		UserId int64 `db:"user_id"`
		ReadAt int64 `db:"read_at"`
	}
	if err := d.Mysql.QueryRowsPartial(ctx, &readers,
		"SELECT r.user_id, MIN(r.read_at) AS read_at FROM channel_read_marks r"+
			" JOIN channel_participants p ON p.channel_id = r.channel_id AND p.user_id = r.user_id AND p.state = ?"+
			" WHERE r.channel_id = ? AND r.max_id >= ? AND r.user_id <> ? GROUP BY r.user_id ORDER BY read_at DESC",
		channelperm.StateIn, channelId, msgId, selfId); err != nil {
		return nil, err
	}
	out := make([]*mtproto.ReadParticipantDate, 0, len(readers))
	for _, r := range readers {
		out = append(out, mtproto.MakeTLReadParticipantDate(&mtproto.ReadParticipantDate{UserId: r.UserId, Date: int32(r.ReadAt)}).To_ReadParticipantDate())
	}
	return out, nil
}

// adminLogMax is the most actions one page holds.
const adminLogMax = 100

// AdminLog is a page of the recent actions of the channel, the newest first,
// for one of its admins (CHAT_ADMIN_REQUIRED otherwise): those of the kinds
// filter asks for, by admins when given, matching q, with ids in
// (minId, maxId) when they are given.
func (d *Dao) AdminLog(ctx context.Context, selfId, channelId int64, q string, filter int64, admins []int64, maxId, minId int64, limit int32, now int64) ([]*mtproto.ChannelAdminLogEvent, error) {
	var me []struct {
		Role  int32 `db:"role"`
		State int32 `db:"state"`
	}
	if err := d.Mysql.QueryRowsPartial(ctx, &me, "SELECT role, state FROM channel_participants WHERE channel_id = ? AND user_id = ?", channelId, selfId); err != nil {
		return nil, err
	}
	if len(me) == 0 || me[0].State != channelperm.StateIn || me[0].Role == channelperm.RoleMember {
		return nil, mtproto.ErrChatAdminRequired
	}
	if limit <= 0 || limit > adminLogMax {
		limit = adminLogMax
	}
	query := "SELECT id, user_id, `date`, action FROM channel_admin_log WHERE channel_id = ? AND `date` >= ? AND (kind & ?) <> 0"
	args := []any{channelId, now - adminlog.Keep, filter}
	if maxId > 0 {
		query += " AND id < ?"
		args = append(args, maxId)
	}
	if minId > 0 {
		query += " AND id > ?"
		args = append(args, minId)
	}
	if q != "" {
		query += " AND `text` LIKE ?"
		args = append(args, "%"+strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q)+"%")
	}
	if len(admins) > 0 {
		query += " AND user_id IN (?" + strings.Repeat(", ?", len(admins)-1) + ")"
		for _, a := range admins {
			args = append(args, a)
		}
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	var rows []struct {
		Id     int64  `db:"id"`
		UserId int64  `db:"user_id"`
		Date   int64  `db:"date"`
		Action []byte `db:"action"`
	}
	if err := d.Mysql.QueryRowsPartial(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	out := make([]*mtproto.ChannelAdminLogEvent, 0, len(rows))
	var messages []*mtproto.Message
	for _, r := range rows {
		action, err := adminlog.Decode(r.Action)
		if err != nil {
			return nil, err
		}
		for _, m := range []*mtproto.Message{action.GetMessage(), action.GetPrevMessage(), action.GetNewMessage()} {
			if m != nil {
				messages = append(messages, m)
			}
		}
		out = append(out, mtproto.MakeTLChannelAdminLogEvent(&mtproto.ChannelAdminLogEvent{
			Id: r.Id, Date: int32(r.Date), UserId: r.UserId, Action: action,
		}).To_ChannelAdminLogEvent())
	}
	// A message keeps the definition of its poll only: the poll and its
	// results are read now, as the history reads them (a stopped poll shows
	// closed).
	if err := polls.AttachMessages(ctx, d.Mysql, selfId, messages...); err != nil {
		return nil, err
	}
	return out, nil
}

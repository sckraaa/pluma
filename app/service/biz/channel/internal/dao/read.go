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
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
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
// channel: its mentions among them are read, and its devices get
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
	var found []int32
	if err := b.Tx().QueryRowsPartial(&found,
		"SELECT id FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+") ORDER BY id",
		args...); err != nil {
		return err
	}
	if len(found) == 0 {
		return nil
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

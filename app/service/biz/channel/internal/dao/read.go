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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
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
	var rows []readRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT g.read_inbox_max_id, g.folder_id, c.top_message, c.pts, c.broadcast, COALESCE(p.available_min_id, 0) AS available_min_id"+
			" FROM dialogs g JOIN channels c ON c.id = g.peer_id AND c.deleted = 0"+
			" JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = g.user_id AND p.state <> 1"+
			" WHERE g.user_id = ? AND g.peer_type = ? AND g.peer_id = ? AND g.deleted = 0 FOR UPDATE",
		b.UserId(), mtproto.PEER_CHANNEL, channelId); err != nil {
		return err
	}
	if len(rows) == 0 {
		return mtproto.ErrChannelPrivate
	}
	r := rows[0]
	if maxId <= 0 || maxId > r.TopMessage {
		maxId = r.TopMessage
	}
	if maxId <= r.ReadInboxMaxId {
		return nil
	}
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
	if err := b.Tx().QueryRowsPartial(&unread,
		"SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND id > ? AND id >= ? AND sender_user_id <> ? AND deleted = 0",
		channelId, maxId, r.AvailableMinId, b.UserId()); err != nil {
		return err
	}
	u := mtproto.MakeTLUpdateReadChannelInbox(&mtproto.Update{
		FolderId:         mtproto.MakeFlagsInt32(r.FolderId),
		ChannelId:        channelId,
		MaxId:            maxId,
		StillUnreadCount: first(unread),
		Pts_INT32:        r.Pts,
	}).To_Update()
	if err := d.EmitSeqUpdate(b, u, nil, nil); err != nil {
		return err
	}
	if r.Broadcast {
		// The posts of a channel show views, not reads.
		return nil
	}
	var authors []int64
	if err := b.Tx().QueryRowsPartial(&authors,
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

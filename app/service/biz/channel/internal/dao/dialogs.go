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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/channelperm"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The state of a user in a channel is its row of dialogs; the rest of what
// its dialog shows is computed from the channel at each read (ADR 0034,
// p. 1): no copy is written per member and message. Reads go to the master,
// as the channel's own reads do.

type channelDialogRow struct {
	Id             int64 `db:"id"`
	Pts            int32 `db:"pts"`
	TopMessage     int32 `db:"top_message"`
	ReadInboxMaxId int32 `db:"read_inbox_max_id"`
	AvailableMinId int32 `db:"available_min_id"`
	Forum          bool  `db:"forum"`
	ViewAsMessages bool  `db:"view_forum_as_messages"`
	TtlPeriod      int32 `db:"ttl_period"`
}

// unreadQuery counts what selfId has not read of the channel above read, its
// mark of the whole channel. In a forum a message is also read by how far its
// topic was read.
func unreadQuery(selfId, channelId int64, forum bool, read, availableMinId int32) (string, []any) {
	if forum {
		return "SELECT COUNT(*) FROM channel_messages m LEFT JOIN channel_thread_reads t ON t.user_id = ? AND t.channel_id = m.channel_id AND t.top_id = IF(m.topic_id = 0, 1, m.topic_id)" +
				" WHERE m.channel_id = ? AND m.id > GREATEST(?, COALESCE(t.read_max_id, 0)) AND m.id >= ? AND m.sender_user_id <> ? AND m.deleted = 0",
			[]any{selfId, channelId, read, availableMinId, selfId}
	}
	return "SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND id > ? AND id >= ? AND sender_user_id <> ? AND deleted = 0",
		[]any{channelId, read, availableMinId, selfId}
}

// unreadReactionsQuery counts the messages of a user in a channel, from an
// id on, with reactions of others it has not seen (task 067).
const unreadReactionsQuery = "SELECT COUNT(DISTINCT r.message_id) FROM channel_message_reactions r" +
	" JOIN channel_messages m ON m.channel_id = r.channel_id AND m.id = r.message_id AND m.deleted = 0" +
	" WHERE r.channel_id = ? AND r.unread = 1 AND m.sender_user_id = ? AND r.message_id >= ?"

// ChannelDialogs returns the dialogs of selfId with the channels idList that
// it has, with what the channel gives them: pts, top_message,
// read_inbox_max_id, read_outbox_max_id, unread_count,
// unread_mentions_count. The rest of the dialog (pins, folders, marks,
// notification settings) is its row, which biz dialog reads.
func (d *Dao) ChannelDialogs(ctx context.Context, selfId int64, idList []int64) ([]*mtproto.Dialog, error) {
	out := make([]*mtproto.Dialog, 0, len(idList))
	for _, id := range dedupe(idList) {
		var rows []channelDialogRow
		if err := d.Mysql.QueryRowsPartial(ctx, &rows,
			"SELECT c.id, c.pts, c.top_message, g.read_inbox_max_id, COALESCE(p.available_min_id, 0) AS available_min_id, c.forum, g.view_forum_as_messages, c.ttl_period"+
				" FROM channels c JOIN dialogs g ON g.user_id = ? AND g.peer_type = ? AND g.peer_id = c.id AND g.deleted = 0"+
				" LEFT JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = g.user_id"+
				" WHERE c.id = ? AND c.deleted = 0",
			selfId, mtproto.PEER_CHANNEL, id); err != nil {
			return nil, fmt.Errorf("channel: dialog of user %d with channel %d: %w", selfId, id, err)
		}
		if len(rows) == 0 {
			continue
		}
		r := rows[0]
		var unread, mentions, outbox, reactions []int32
		query, args := unreadQuery(selfId, id, r.Forum, r.ReadInboxMaxId, r.AvailableMinId)
		if err := d.Mysql.QueryRowsPartial(ctx, &unread, query, args...); err != nil {
			return nil, err
		}
		if err := d.Mysql.QueryRowsPartial(ctx, &mentions,
			"SELECT COUNT(*) FROM channel_message_mentions WHERE channel_id = ? AND user_id = ? AND `read` = 0 AND message_id >= ?",
			id, selfId, r.AvailableMinId); err != nil {
			return nil, err
		}
		if err := d.Mysql.QueryRowsPartial(ctx, &reactions, unreadReactionsQuery, id, selfId, r.AvailableMinId); err != nil {
			return nil, err
		}
		// What the others read of the channel: the outbox of the viewer
		// is read up to the furthest of them (ADR 0034, p. 1; UNVERIFIED
		// against Telegram, U5). A ghost's read is not one of them (ADR
		// 0048, p. 5).
		if err := d.Mysql.QueryRowsPartial(ctx, &outbox,
			"SELECT COALESCE(MAX(g.read_inbox_max_id), 0) FROM dialogs g WHERE g.peer_type = ? AND g.peer_id = ? AND g.user_id <> ? AND g.deleted = 0"+
				" AND NOT EXISTS (SELECT 1 FROM channel_participants p WHERE p.channel_id = g.peer_id AND p.user_id = g.user_id AND p.hidden = 1)",
			mtproto.PEER_CHANNEL, id, selfId); err != nil {
			return nil, err
		}
		// All of the channel the owner has hidden: no last message, as
		// TopMessages gives none (ViewerDialog does the same).
		top := r.TopMessage
		if top < r.AvailableMinId {
			top = 0
		}
		out = append(out, mtproto.MakeTLDialog(&mtproto.Dialog{
			Peer:                 mtproto.MakePeerChannel(id),
			TopMessage:           top,
			ReadInboxMaxId:       r.ReadInboxMaxId,
			ReadOutboxMaxId:      first(outbox),
			UnreadCount:          first(unread),
			UnreadMentionsCount:  first(mentions),
			UnreadReactionsCount: first(reactions),
			Pts:                  mtproto.MakeFlagsInt32(r.Pts),
			ViewForumAsMessages:  r.ViewAsMessages,
			TtlPeriod:            mtproto.MakeFlagsInt32(r.TtlPeriod),
		}).To_Dialog())
	}
	return out, nil
}

type topMessageRow struct {
	ChannelId      int64  `db:"channel_id"`
	SenderUserId   int64  `db:"sender_user_id"`
	MessageData    string `db:"message_data"`
	AvailableMinId int32  `db:"available_min_id"`
	Member         bool   `db:"member"`
	Id             int32  `db:"id"`
	Views          int32  `db:"views"`
	Forwards       int32  `db:"forwards"`
}

// TopMessages returns the last message of each channel of idList that
// selfId may see, as it sees it.
func (d *Dao) TopMessages(ctx context.Context, selfId int64, idList []int64) ([]*mtproto.Message, error) {
	out := make([]*mtproto.Message, 0, len(idList))
	for _, id := range dedupe(idList) {
		var rows []topMessageRow
		if err := d.Mysql.QueryRowsPartial(ctx, &rows,
			"SELECT m.channel_id, m.id, m.sender_user_id, m.message_data, m.views, m.forwards, COALESCE(p.available_min_id, 0) AS available_min_id,"+
				" COALESCE(p.state = ?, 0) AS member"+
				" FROM channels c JOIN channel_messages m ON m.channel_id = c.id AND m.id = c.top_message AND m.deleted = 0"+
				" LEFT JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = ?"+
				" WHERE c.id = ? AND c.deleted = 0",
			channelperm.StateIn, selfId, id); err != nil {
			return nil, fmt.Errorf("channel: top message of channel %d: %w", id, err)
		}
		if len(rows) == 0 || rows[0].Id < rows[0].AvailableMinId {
			continue
		}
		var m mtproto.Message
		if err := jsonx.UnmarshalFromString(rows[0].MessageData, &m); err != nil {
			return nil, fmt.Errorf("channel: message %d of channel %d: %w", rows[0].Id, id, err)
		}
		if m.GetPredicateName() == mtproto.Predicate_message && m.GetPost() {
			// The stored copy holds them as they were (task 046).
			channel.SetPostCounters(&m, rows[0].Views, rows[0].Forwards)
		}
		own := rows[0].SenderUserId == selfId
		shown := channel.ViewSentMessage(&m, selfId, own)
		if err := d.withContents(ctx, id, selfId, rows[0].Member, []messageRow{{Id: rows[0].Id, SenderUserId: rows[0].SenderUserId}}, []*mtproto.Message{shown}); err != nil {
			return nil, fmt.Errorf("channel: top message of channel %d: %w", id, err)
		}
		out = append(out, shown)
	}
	return out, nil
}

func first(v []int32) int32 {
	if len(v) == 0 {
		return 0
	}
	return v[0]
}

func dedupe(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

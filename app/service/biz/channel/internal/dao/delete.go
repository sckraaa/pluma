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
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// DeleteMax is the most ids of one channels.deleteMessages (the clients
// send 100 at most, as for messages.deleteMessages).
const DeleteMax = 100

type deleteRow struct {
	Id           int32 `db:"id"`
	SenderUserId int64 `db:"sender_user_id"`
}

// Deleted is what Delete wrote: the ids deleted and the pts of the row of
// the channel's journal (0 when none was).
type Deleted struct {
	ChannelId int64
	Ids       []int32
	Pts       int32
	PtsCount  int32
}

// Delete deletes messages ids of the channel for everyone, by b's owner
// (task 043): its own messages, and any of a supergroup or a channel when
// it is the creator or an admin who may delete messages
// (chatAdminRights.delete_messages). Ids that do not exist or are deleted
// are left out; another's message
// without the right is MESSAGE_DELETE_FORBIDDEN, as Telegram answers
// (core.telegram.org/method/channels.deleteMessages). The deleted ids are
// one row of the journal, pts_count their number; the channel's
// top_message moves to the last message left.
func (d *Dao) Delete(b *userbox.Box, channelId int64, ids []int32) (*Deleted, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	c, me, err := channelForMember(b, channelId)
	if err != nil {
		return nil, err
	}
	p := me.participant()
	if !p.Member() {
		return nil, mtproto.ErrChannelPrivate
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	admin := a.can(channelperm.DeleteOthers, b.Date()) == nil

	out := &Deleted{ChannelId: channelId}
	ids = slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(ids))), func(id int32) bool { return id <= 0 })
	if len(ids) == 0 {
		return out, nil
	}
	args := []any{channelId}
	for _, id := range ids {
		args = append(args, id)
	}
	var rows []deleteRow
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT id, sender_user_id FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+") FOR UPDATE",
		args...); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.SenderUserId != b.UserId() && !admin {
			return nil, mtproto.ErrMessageDeleteForbidden
		}
		out.Ids = append(out.Ids, r.Id)
	}
	if len(out.Ids) == 0 {
		return out, nil
	}
	args = []any{channelId}
	for _, id := range out.Ids {
		args = append(args, id)
	}
	in := "(?" + strings.Repeat(", ?", len(out.Ids)-1) + ")"
	if _, err = b.Tx().Exec("UPDATE channel_messages SET deleted = 1 WHERE channel_id = ? AND id IN "+in, args...); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("DELETE FROM channel_message_mentions WHERE channel_id = ? AND message_id IN "+in, args...); err != nil {
		return nil, err
	}
	// A deleted poll counts no more among its creator's unread votes.
	if _, err = b.Tx().Exec("UPDATE polls SET has_unread = 0 WHERE peer_type = ? AND peer_id = ? AND has_unread = 1 AND message_id IN "+in,
		append([]any{mtproto.PEER_CHANNEL}, args...)...); err != nil {
		return nil, err
	}
	if slices.Contains(out.Ids, c.TopMessage) {
		var top []int32
		if err = b.Tx().QueryRowsPartial(&top, "SELECT COALESCE(MAX(id), 0) FROM channel_messages WHERE channel_id = ? AND deleted = 0", channelId); err != nil {
			return nil, err
		}
		if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", first(top), channelId); err != nil {
			return nil, err
		}
	}
	out.PtsCount = int32(len(out.Ids))
	ch.AddPtsUpdate(mtproto.MakeTLUpdateDeleteChannelMessages(&mtproto.Update{ChannelId: channelId, Messages: out.Ids}).To_Update(), out.PtsCount)
	out.Pts = ch.Pts()
	return out, nil
}

// EmitDeletePush adds the push of the deletion to the channel's members.
func (d *Dao) EmitDeletePush(b *userbox.Box, permAuthKeyId int64, del *Deleted) error {
	if len(del.Ids) == 0 {
		return nil
	}
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId:     del.ChannelId,
		FromId:        b.UserId(),
		FromAuthKeyId: permAuthKeyId,
		Updates: mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateDeleteChannelMessages(&mtproto.Update{
			ChannelId: del.ChannelId,
			Messages:  del.Ids,
			Pts_INT32: del.Pts,
			PtsCount:  del.PtsCount,
		}).To_Update()),
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: del.ChannelId, Method: string(proto.MessageName(in)), Data: data})
	return nil
}

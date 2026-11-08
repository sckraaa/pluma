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
	// Copies are the deletions of the copies of deleted posts, one per
	// discussion group; Posts the posts whose comments went with a deleted
	// copy (task 048).
	Copies []*Deleted
	Posts  []PostCommentsOff
}

// PostCommentsOff is a post that no longer has comments, as stored, and
// the pts of that change.
type PostCommentsOff struct {
	ChannelId int64
	Message   *mtproto.Message
	Pts       int32
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
//
// A deleted post takes its copy in the discussion group with it; the
// comments stay in the group. A deleted copy turns the comments of its
// post off (core.telegram.org/api/discussion: "the comment section of a
// particular post can be disabled by removing the auto-forwarded channel
// post message"). Either way the other channel is locked with this one, in
// ascending order, and written in the same transaction.
func (d *Dao) Delete(b *userbox.Box, channelId int64, ids []int32) (*Deleted, error) {
	ids = slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(ids))), func(id int32) bool { return id <= 0 })
	links, err := discussionLinks(b, channelId, ids)
	if err != nil {
		return nil, err
	}
	ch, err := openWithLinked(b, channelId, links)
	if err != nil {
		return nil, err
	}
	_, me, err := channelForMember(b, channelId)
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
	if err = removeMessages(b, ch, out); err != nil {
		return nil, err
	}
	// Read again with the channels locked: a link changes only with both
	// of its channels locked.
	if links, err = discussionLinks(b, channelId, out.Ids); err != nil {
		return nil, err
	}
	if err = removeLinked(b, channelId, links, out); err != nil {
		return nil, err
	}
	return out, nil
}

// removeMessages deletes out.Ids of the locked channel: the mentions of
// them go, top_message moves to the last message left, and one row of the
// journal holds them, pts_count their number.
func removeMessages(b *userbox.Box, ch *channelbox.Channel, out *Deleted) error {
	args := []any{out.ChannelId}
	for _, id := range out.Ids {
		args = append(args, id)
	}
	in := "(?" + strings.Repeat(", ?", len(out.Ids)-1) + ")"
	if _, err := b.Tx().Exec("UPDATE channel_messages SET deleted = 1 WHERE channel_id = ? AND id IN "+in, args...); err != nil {
		return err
	}
	if _, err := b.Tx().Exec("DELETE FROM channel_message_mentions WHERE channel_id = ? AND message_id IN "+in, args...); err != nil {
		return err
	}
	var top []int32
	if err := b.Tx().QueryRowsPartial(&top, "SELECT top_message FROM channels WHERE id = ?", out.ChannelId); err != nil {
		return err
	}
	if slices.Contains(out.Ids, first(top)) {
		var last []int32
		if err := b.Tx().QueryRowsPartial(&last, "SELECT COALESCE(MAX(id), 0) FROM channel_messages WHERE channel_id = ? AND deleted = 0", out.ChannelId); err != nil {
			return err
		}
		if _, err := b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", first(last), out.ChannelId); err != nil {
			return err
		}
	}
	out.PtsCount = int32(len(out.Ids))
	ch.AddPtsUpdate(mtproto.MakeTLUpdateDeleteChannelMessages(&mtproto.Update{ChannelId: out.ChannelId, Messages: out.Ids}).To_Update(), out.PtsCount)
	out.Pts = ch.Pts()
	return nil
}

// EmitDeletePush adds the push of the deletion to the channel's members,
// and of what went with it to the members of the other channels. The
// answer to the deletion holds the channel's own pts alone: every device
// of the owner is told of the rest.
func (d *Dao) EmitDeletePush(b *userbox.Box, permAuthKeyId int64, del *Deleted) error {
	if len(del.Ids) == 0 {
		return nil
	}
	if err := d.emitDeletion(b, permAuthKeyId, del); err != nil {
		return err
	}
	for _, c := range del.Copies {
		if err := d.emitDeletion(b, 0, c); err != nil {
			return err
		}
	}
	for _, p := range del.Posts {
		u := mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: p.Message, Pts_INT32: p.Pts, PtsCount: 1}).To_Update()
		if err := d.emitChannelUpdate(b, p.ChannelId, 0, u); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dao) emitDeletion(b *userbox.Box, permAuthKeyId int64, del *Deleted) error {
	return d.emitChannelUpdate(b, del.ChannelId, permAuthKeyId, mtproto.MakeTLUpdateDeleteChannelMessages(&mtproto.Update{
		ChannelId: del.ChannelId,
		Messages:  del.Ids,
		Pts_INT32: del.Pts,
		PtsCount:  del.PtsCount,
	}).To_Update())
}

func (d *Dao) emitChannelUpdate(b *userbox.Box, channelId, permAuthKeyId int64, u *mtproto.Update) error {
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId:     channelId,
		FromId:        b.UserId(),
		FromAuthKeyId: permAuthKeyId,
		Updates:       mtproto.MakeUpdatesByUpdates(u),
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: channelId, Method: string(proto.MessageName(in)), Data: data})
	return nil
}

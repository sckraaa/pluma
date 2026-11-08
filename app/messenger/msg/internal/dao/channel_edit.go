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
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// The edit of a message of a channel (task 043, ADR 0034): the one copy
// changes in the editor's box transaction with the channel locked, one row
// of the channel's journal holds the whole message after the edit (as for a
// user's box, ADR 0019), and one event pushes it to the members.

type channelMessageRow struct {
	SenderUserId int64  `db:"sender_user_id"`
	MessageData  string `db:"message_data"`
}

// ChannelEdited is what EditChannel wrote: the message as stored and its
// pts, and for a post with comments the same edit of its copy in the
// discussion group.
type ChannelEdited struct {
	Message *mtproto.Message
	Pts     int32
	GroupId int64
	Copy    *ChannelSent
}

// EditChannel applies edit to message id of the channel by b's owner. Its
// author edits a message of a supergroup; in a channel (broadcast) the
// creator and the admins who may edit messages edit any post, its author
// one it posted while it may post (Telegram's rights,
// core.telegram.org/constructor/chatAdminRights). MESSAGE_ID_INVALID for a
// message that does not exist or is deleted, MESSAGE_AUTHOR_REQUIRED for
// one the owner may not edit. The copy of a post in its discussion group
// shows the post (task 048), so it changes with it: in the same
// transaction, with a pts of its own in the group's box.
func (d *Dao) EditChannel(b *userbox.Box, channelId int64, id int32, edit *mtproto.Message) (*ChannelEdited, error) {
	// The group is locked with the channel, in ascending order: its id is
	// read first; a copy is removed only with both locked, so it is read
	// again below.
	groupId, err := postCopyGroup(b, channelId, id)
	if err != nil {
		return nil, err
	}
	if groupId != 0 && groupId < channelId {
		if _, err = channelbox.Open(b, groupId); err != nil {
			return nil, err
		}
	}
	ch, s, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	if groupId > channelId {
		if _, err = channelbox.Open(b, groupId); err != nil {
			return nil, err
		}
	}
	var rows []channelMessageRow
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT sender_user_id, message_data FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0 FOR UPDATE", channelId, id); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrMessageIdInvalid
	}
	// One who may not post (a subscriber, a member restricted from
	// sending) edits nothing; others' messages only in a broadcast channel,
	// with edit_messages.
	own := rows[0].SenderUserId == b.UserId()
	switch {
	case s.can(channelperm.SendPlain, b.Date()) != nil:
		return nil, mtproto.ErrMessageAuthorRequired
	case !own && (!s.ch.Broadcast || s.can(channelperm.EditOthers, b.Date()) != nil):
		return nil, mtproto.ErrMessageAuthorRequired
	}
	var stored *mtproto.Message
	if err = jsonx.UnmarshalFromString(rows[0].MessageData, &stored); err != nil {
		return nil, err
	}
	if stored.GetPredicateName() != mtproto.Predicate_message {
		return nil, mtproto.ErrMessageIdInvalid
	}
	// New media is sent anew: its kind must be one s may send now.
	if !proto.Equal(stored.GetMedia(), edit.GetMedia()) {
		edit = proto.Clone(edit).(*mtproto.Message)
		if err = checkSend(s, edit, b.Date()); err != nil {
			return nil, err
		}
	}
	m := applyEdit(stored, edit)
	var copyId int32
	if groupId != 0 {
		if copyId, err = postCopyId(b, channelId, id); err != nil {
			return nil, err
		}
	}
	if copyId != 0 {
		// What is stored is the count of when the post was written.
		if m.Replies, err = postReplies(b, groupId, copyId, ch.Pts()+1); err != nil {
			return nil, err
		}
	}
	ch.AddPtsUpdate(mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE channel_messages SET message_data = ?, message = ?, message_filter_type = ? WHERE channel_id = ? AND id = ?",
		data, m.GetMessage(), mtproto.GetMediaType(m), channelId, id); err != nil {
		return nil, err
	}
	out := &ChannelEdited{Message: m, Pts: ch.Pts()}
	if copyId != 0 {
		if out.Copy, err = editPostCopy(b, groupId, copyId, m); err != nil {
			return nil, err
		}
		out.GroupId = groupId
	}
	return out, nil
}

// postCopyGroup is the discussion group holding the copy of the post, 0
// for none.
func postCopyGroup(b *userbox.Box, channelId int64, postId int32) (int64, error) {
	var groups []int64
	if err := b.Tx().QueryRowsPartial(&groups,
		"SELECT group_id FROM channel_discussion_posts WHERE channel_id = ? AND post_id = ?", channelId, postId); err != nil {
		return 0, err
	}
	if len(groups) == 0 {
		return 0, nil
	}
	return groups[0], nil
}

// postCopyId is the copy of the post in its discussion group, read with
// both locked: 0 when the group removed it in between.
func postCopyId(b *userbox.Box, channelId int64, postId int32) (int32, error) {
	var ids []int32
	if err := b.Tx().QueryRowsPartial(&ids,
		"SELECT group_message_id FROM channel_discussion_posts WHERE channel_id = ? AND post_id = ?", channelId, postId); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}

// repliersMax is the most authors of recent_repliers
// (core.telegram.org/constructor/messageReplies: the clients show three).
const repliersMax = 3

// postReplies is the messageReplies of the comments of a post whose copy is
// copyId, with the pts the post is written at. An edit carries them as they
// are now: Android keeps the message of updateEditChannelMessage as it
// comes (MessagesController.java:19356), and tdesktop clears the button of
// a message without replies (history_item.cpp:2307-2314).
func postReplies(b *userbox.Box, groupId int64, copyId, pts int32) (*mtproto.MessageReplies, error) {
	var counts []struct {
		Count int32 `db:"n"`
		MaxId int32 `db:"max_id"`
	}
	if err := b.Tx().QueryRowsPartial(&counts,
		"SELECT COUNT(*) AS n, COALESCE(MAX(id), 0) AS max_id FROM channel_messages WHERE channel_id = ? AND reply_top_id = ? AND deleted = 0",
		groupId, copyId); err != nil {
		return nil, err
	}
	var repliers []int64
	if err := b.Tx().QueryRowsPartial(&repliers,
		"SELECT sender_user_id FROM channel_messages WHERE channel_id = ? AND reply_top_id = ? AND deleted = 0"+
			" GROUP BY sender_user_id ORDER BY MAX(id) DESC LIMIT ?", groupId, copyId, repliersMax); err != nil {
		return nil, err
	}
	r := &mtproto.MessageReplies{
		Comments:       true,
		Replies:        counts[0].Count,
		RepliesPts:     pts,
		ChannelId:      mtproto.MakeFlagsInt64(groupId),
		RecentRepliers: []*mtproto.Peer{},
	}
	for _, id := range repliers {
		r.RecentRepliers = append(r.RecentRepliers, mtproto.MakePeerUser(id))
	}
	if counts[0].MaxId != 0 {
		r.MaxId = mtproto.MakeFlagsInt32(counts[0].MaxId)
	}
	return mtproto.MakeTLMessageReplies(r).To_MessageReplies(), nil
}

// editPostCopy brings the copy copyId in the locked discussion group to the
// post as edited: nil when the copy is gone.
func editPostCopy(b *userbox.Box, groupId int64, copyId int32, post *mtproto.Message) (*ChannelSent, error) {
	var rows []channelMessageRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT sender_user_id, message_data FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0 FOR UPDATE", groupId, copyId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	var stored *mtproto.Message
	if err := jsonx.UnmarshalFromString(rows[0].MessageData, &stored); err != nil {
		return nil, err
	}
	group, err := channelbox.Open(b, groupId)
	if err != nil {
		return nil, err
	}
	m := applyEdit(stored, post)
	group.AddPtsUpdate(mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE channel_messages SET message_data = ?, message = ?, message_filter_type = ? WHERE channel_id = ? AND id = ?",
		data, m.GetMessage(), mtproto.GetMediaType(m), groupId, copyId); err != nil {
		return nil, err
	}
	return &ChannelSent{Message: m, Pts: group.Pts()}, nil
}

// EmitChannelEditPush adds the push of an edited message of the channel to
// its members (sync.pushChannelUpdates, keyed by the channel), and of its
// copy to the members of the discussion group. The answer to the edit
// holds the post alone: every device of the editor is told of the copy.
func (d *Dao) EmitChannelEditPush(b *userbox.Box, channelId, permAuthKeyId int64, e *ChannelEdited) error {
	if err := d.emitEdit(b, channelId, permAuthKeyId, e.Message, e.Pts); err != nil {
		return err
	}
	if e.Copy == nil {
		return nil
	}
	return d.emitEdit(b, e.GroupId, 0, e.Copy.Message, e.Copy.Pts)
}

func (d *Dao) emitEdit(b *userbox.Box, channelId, permAuthKeyId int64, m *mtproto.Message, pts int32) error {
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId:     channelId,
		FromId:        b.UserId(),
		FromAuthKeyId: permAuthKeyId,
		Updates: mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{
			Message_MESSAGE: m,
			Pts_INT32:       pts,
			PtsCount:        1,
		}).To_Update()),
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: channelId, Method: string(proto.MessageName(in)), Data: data})
	return nil
}

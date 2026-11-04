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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// The edit of a message of a channel (task 043, ADR 0034): the one copy
// changes in the editor's box transaction with the channel locked, one row
// of the channel's journal holds the whole message after the edit (as for a
// user's box, ADR 0019), and one event pushes it to the members.

const adminEditMessages = mtproto.ADMIN_EDIT_MESSAGES

type channelMessageRow struct {
	SenderUserId int64  `db:"sender_user_id"`
	MessageData  string `db:"message_data"`
}

// EditChannel applies edit to message id of the channel by b's owner. Its
// author edits a message of a supergroup; in a channel (broadcast) the
// creator and the admins who may edit messages edit any post, its author
// one it posted while it may post (Telegram's rights,
// core.telegram.org/constructor/chatAdminRights). MESSAGE_ID_INVALID for a
// message that does not exist or is deleted, MESSAGE_AUTHOR_REQUIRED for
// one the owner may not edit. It returns the message as stored and its pts.
func (d *Dao) EditChannel(b *userbox.Box, channelId int64, id int32, edit *mtproto.Message) (*mtproto.Message, int32, error) {
	s, err := channelSender(b, channelId)
	if err != nil && !errors.Is(err, mtproto.ErrChatWriteForbidden) {
		return nil, 0, err
	}
	ch, err := channelbox.Open(b, channelId)
	if err != nil {
		if errors.Is(err, channelbox.ErrNoChannel) {
			return nil, 0, mtproto.ErrChannelInvalid
		}
		return nil, 0, err
	}
	var rows []channelMessageRow
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT sender_user_id, message_data FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0 FOR UPDATE", channelId, id); err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, 0, mtproto.ErrMessageIdInvalid
	}
	own := rows[0].SenderUserId == b.UserId()
	switch {
	case s == nil:
		// A member who may not post (a subscriber, one restricted) edits
		// nothing.
		return nil, 0, mtproto.ErrMessageAuthorRequired
	case !s.Broadcast && !own,
		s.Broadcast && !own && s.Role != 2 && (s.Role != 1 || s.AdminRights&adminEditMessages == 0):
		return nil, 0, mtproto.ErrMessageAuthorRequired
	}
	var stored *mtproto.Message
	if err = jsonx.UnmarshalFromString(rows[0].MessageData, &stored); err != nil {
		return nil, 0, err
	}
	if stored.GetPredicateName() != mtproto.Predicate_message {
		return nil, 0, mtproto.ErrMessageIdInvalid
	}
	m := applyEdit(stored, edit)
	ch.AddPtsUpdate(mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		return nil, 0, err
	}
	if _, err = b.Tx().Exec("UPDATE channel_messages SET message_data = ?, message = ?, message_filter_type = ? WHERE channel_id = ? AND id = ?",
		data, m.GetMessage(), mtproto.GetMediaType(m), channelId, id); err != nil {
		return nil, 0, err
	}
	return m, ch.Pts(), nil
}

// EmitChannelEditPush adds the push of an edited message of the channel to
// its members (sync.pushChannelUpdates, keyed by the channel).
func (d *Dao) EmitChannelEditPush(b *userbox.Box, channelId, permAuthKeyId int64, m *mtproto.Message, pts int32) error {
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

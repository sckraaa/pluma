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
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Pinned messages of a channel (task 043, ADR 0034): the pinned flag of the
// one copy changes in the pinner's box transaction with the channel
// locked; one row of the channel's journal, updatePinnedChannelMessages,
// tells the members; a pin, not an unpin, also writes the service message
// messageActionPinMessage (core.telegram.org/method/messages.updatePinnedMessage).

// ChannelPinned is what a pin or an unpin of a channel wrote: the update of
// the journal with its pts and, for a pin, the service message.
type ChannelPinned struct {
	Update  *mtproto.Update
	Service *ChannelSent
}

type pinRow struct {
	MessageData string `db:"message_data"`
	Pinned      bool   `db:"pinned"`
	TopicId     int32  `db:"topic_id"`
}

// PinChannel pins message id of the channel, or unpins it, by b's owner:
// the creator, an admin with pin_messages (in a broadcast channel also
// edit_messages), a member of a supergroup whom no restriction bans
// pin_messages (channelperm.Can, PinMessages). MESSAGE_ID_INVALID for a
// message that does not exist, is deleted or is a service message. A pin
// of what is pinned already, or an unpin of what is not, writes nothing
// (nil). silent pins without a notification.
func (d *Dao) PinChannel(b *userbox.Box, channelId int64, id int32, unpin, silent bool, randomId int64) (*ChannelPinned, error) {
	ch, s, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	if s.guest {
		return nil, mtproto.ErrChatAdminRequired
	}
	if err = s.can(channelperm.PinMessages, b.Date()); err != nil {
		return nil, err
	}
	var rows []pinRow
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT message_data, pinned, topic_id FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0 FOR UPDATE", channelId, id); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrMessageIdInvalid
	}
	var m *mtproto.Message
	if err = jsonx.UnmarshalFromString(rows[0].MessageData, &m); err != nil {
		return nil, err
	}
	if m.GetPredicateName() != mtproto.Predicate_message {
		return nil, mtproto.ErrMessageIdInvalid
	}
	if rows[0].Pinned == !unpin {
		return nil, nil
	}
	m.Pinned = !unpin
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE channel_messages SET pinned = ?, message_data = ? WHERE channel_id = ? AND id = ?",
		!unpin, data, channelId, id); err != nil {
		return nil, err
	}
	out := &ChannelPinned{Update: mtproto.MakeTLUpdatePinnedChannelMessages(&mtproto.Update{
		Pinned:    !unpin,
		ChannelId: channelId,
		Messages:  []int32{id},
	}).To_Update()}
	ch.AddPtsUpdate(out.Update, 1)
	if unpin {
		return out, nil
	}

	service := mtproto.MakePinnedMessageService(silent, b.UserId(), mtproto.MakePeerUtil(mtproto.PEER_CHANNEL, channelId), id)
	service.Id = ch.NextMessageId()
	service.Out = false
	service.Date = int32(b.Date())
	if s.ch.Broadcast {
		service.Post = true
		service.FromId = nil
	}
	topicId := int32(channelbox.GeneralTopicId)
	if s.forum && rows[0].TopicId != 0 {
		// The service message is in the topic of the message pinned
		// (Android MessageObject.getTopicId).
		topicId = rows[0].TopicId
		service.ReplyTo.ForumTopic = true
		service.ReplyTo.ReplyToTopId = wrapperspb.Int32(topicId)
	}
	if err = ch.ApplyTTL(service); err != nil {
		return nil, err
	}
	ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: service}).To_Update(), 1)
	if data, err = jsonx.MarshalToString(service); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, random_id, pts, message_filter_type, message_data, topic_id, date2) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		channelId, service.Id, b.UserId(), randomId, ch.Pts(), mtproto.GetMediaType(service), data, channelbox.TopicColumn(topicId), b.Date()); err != nil {
		return nil, err
	}
	if s.forum {
		err = channelbox.ForumMoved(b, channelId, topicId, service.Id)
		b.InvalidateCache(dialog.GetDialogCacheKeyByPeer(b.UserId(), mtproto.PEER_CHANNEL, channelId))
	} else {
		err = movedTo(b, channelId, service.Id)
	}
	if err != nil {
		return nil, err
	}
	out.Service = &ChannelSent{RandomId: randomId, Message: service, Pts: ch.Pts()}
	return out, nil
}

// UnpinAllChannel unpins every pinned message of the channel by b's owner,
// as PinChannel checks it: one updatePinnedChannelMessages with them all,
// nil when none is pinned.
func (d *Dao) UnpinAllChannel(b *userbox.Box, channelId int64) (*mtproto.Update, error) {
	ch, s, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	if s.guest {
		return nil, mtproto.ErrChatAdminRequired
	}
	if err = s.can(channelperm.PinMessages, b.Date()); err != nil {
		return nil, err
	}
	var rows []struct {
		Id          int32  `db:"id"`
		MessageData string `db:"message_data"`
	}
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT id, message_data FROM channel_messages WHERE channel_id = ? AND pinned = 1 AND deleted = 0 ORDER BY id FOR UPDATE", channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]int32, 0, len(rows))
	for _, r := range rows {
		var m *mtproto.Message
		if err = jsonx.UnmarshalFromString(r.MessageData, &m); err != nil {
			return nil, err
		}
		m.Pinned = false
		data, err := jsonx.MarshalToString(m)
		if err != nil {
			return nil, err
		}
		if _, err = b.Tx().Exec("UPDATE channel_messages SET pinned = 0, message_data = ? WHERE channel_id = ? AND id = ?", data, channelId, r.Id); err != nil {
			return nil, err
		}
		ids = append(ids, r.Id)
	}
	u := mtproto.MakeTLUpdatePinnedChannelMessages(&mtproto.Update{ChannelId: channelId, Messages: ids}).To_Update()
	ch.AddPtsUpdate(u, 1)
	return u, nil
}

// EmitChannelUpdatesPush adds the push of updates of the channel's journal
// to its members (sync.pushChannelUpdates, keyed by the channel).
func (d *Dao) EmitChannelUpdatesPush(b *userbox.Box, channelId, permAuthKeyId int64, updates ...*mtproto.Update) error {
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId:     channelId,
		FromId:        b.UserId(),
		FromAuthKeyId: permAuthKeyId,
		Updates:       mtproto.MakeUpdatesByUpdates(updates...),
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: channelId, Method: string(proto.MessageName(in)), Data: data})
	return nil
}

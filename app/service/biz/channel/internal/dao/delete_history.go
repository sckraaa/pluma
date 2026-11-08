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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

const (
	// HistoryPortion is the most messages one transaction of
	// channels.deleteHistory deletes for everyone: the channel stays locked
	// for that long, and a row of the journal holds that many ids.
	HistoryPortion = 1000
	// historyParticipantsMax is the largest channel whose history may be
	// deleted for everyone (core.telegram.org/method/channels.deleteHistory:
	// CHANNEL_TOO_BIG).
	historyParticipantsMax = 1000

	// participantMessages selects what a user wrote to a supergroup.
	participantMessages = " FROM channel_messages m WHERE m.channel_id = ? AND m.sender_user_id = ? AND m.deleted = 0" +
		" AND NOT EXISTS (SELECT 1 FROM channel_discussion_posts p WHERE p.group_id = m.channel_id AND p.group_message_id = m.id)"
)

// HistoryDeleted is one portion of DeleteHistory.
type HistoryDeleted struct {
	*Deleted
	// MaxId is the last message of the range to delete, fixed by the first
	// portion so that what is posted meanwhile stays.
	MaxId int32
	More  bool // the range has more messages
}

// checkHistoryMember checks that b's owner is in the channel: outside of it
// CHANNEL_PRIVATE, or CHANNEL_PARICIPANT_MISSING for a public channel it can
// read but has no history of its own in (core.telegram.org/method/
// channels.deleteHistory).
func checkHistoryMember(c *memberChannelRow, me *memberRow) error {
	switch {
	case me.participant().Member():
		return nil
	case c.Public:
		return mtproto.ErrChannelParicipantMissing
	}
	return mtproto.ErrChannelPrivate
}

// historyMaxId is the last message of a deletion up to maxId: the channel's
// last one for 0 or anything above it, as the clients send (Android
// Integer.MAX_VALUE, MessagesController.java:10252; Web K "the server
// clamps max_id", appMessagesManager.ts flushHistory).
func historyMaxId(maxId, top int32) int32 {
	if maxId <= 0 || maxId > top {
		return top
	}
	return maxId
}

// DeleteHistory deletes for everyone the next portion of the messages of the
// channel up to maxId, by its creator: the clients offer it to the creator
// only (tdesktop PeerData::canRevokeFullHistory, data_peer.cpp:1813; Android
// and iOS by channelFull.can_delete_channel, ChatActivity.java:3821), and
// the method answers CHAT_ADMIN_REQUIRED and CHANNEL_TOO_BIG otherwise. Each
// portion is its own transaction and its own row of the journal, so that
// the channel is not locked for the whole of a long history, and a request
// cut short is finished by its retry. The first message of the channel, its
// creation, stays (Web K flushHistory: "a channel keeps its creation
// service message through a clear").
func (d *Dao) DeleteHistory(b *userbox.Box, channelId int64, maxId int32) (*HistoryDeleted, error) {
	// The channels of the portion's discussion links lock first, in
	// ascending order, so the portion is known before the locks.
	var known []int32
	if err := b.Tx().QueryRowsPartial(&known,
		"SELECT id FROM channel_messages WHERE channel_id = ? AND id > 1 AND (? <= 0 OR id <= ?) AND deleted = 0 ORDER BY id LIMIT ?",
		channelId, maxId, maxId, HistoryPortion); err != nil {
		return nil, err
	}
	ch, err := openForRows(b, channelId, known)
	if err != nil {
		return nil, err
	}
	c, me, err := channelForMember(b, channelId)
	if err != nil {
		return nil, err
	}
	if err = checkHistoryMember(c, me); err != nil {
		return nil, err
	}
	if me.Role != channel.RoleCreator {
		return nil, mtproto.ErrChatAdminRequired
	}
	if c.ParticipantsCount > historyParticipantsMax {
		return nil, mtproto.ErrChannelTooBig
	}
	maxId = historyMaxId(maxId, c.TopMessage)
	var ids []int32
	if err = b.Tx().QueryRowsPartial(&ids,
		"SELECT id FROM channel_messages WHERE channel_id = ? AND id > 1 AND id <= ? AND deleted = 0 ORDER BY id LIMIT ? FOR UPDATE",
		channelId, maxId, HistoryPortion); err != nil {
		return nil, err
	}
	del, err := deleteRows(b, ch, channelId, onlyKnown(ids, known))
	if err != nil {
		return nil, err
	}
	return &HistoryDeleted{Deleted: del, MaxId: maxId, More: len(ids) == HistoryPortion}, nil
}

// openForRows locks channelId and the other channel of each discussion link
// of ids, in ascending order, and returns channelId's box.
func openForRows(b *userbox.Box, channelId int64, ids []int32) (*channelbox.Channel, error) {
	links, err := discussionLinks(b, channelId, ids)
	if err != nil {
		return nil, err
	}
	return openWithLinked(b, channelId, links)
}

// onlyKnown keeps of ids those known before the locks were taken: for
// them the channels of their links are locked. A message written in between
// goes with the next portion.
func onlyKnown(ids, known []int32) []int32 {
	return slices.DeleteFunc(ids, func(id int32) bool { return !slices.Contains(known, id) })
}

// HideHistory hides the messages of the channel up to maxId from b's owner
// (channels.deleteHistory without for_everyone): the first message it sees
// becomes the next one. Its read mark follows, its devices get the new mark
// and updateChannelAvailableMessages with seq. It returns the
// available_min_id of the update, the last message the owner does not see,
// or 0 when it sees all of the channel; a request that hides nothing more
// answers what is hidden already and writes nothing. The channel is not
// locked: nothing of it changes.
func (d *Dao) HideHistory(b *userbox.Box, channelId int64, maxId int32) (int32, error) {
	c, me, err := channelForMember(b, channelId)
	if err != nil {
		return 0, err
	}
	if err = checkHistoryMember(c, me); err != nil {
		return 0, err
	}
	r, err := readerRow(b, channelId)
	if err != nil {
		return 0, err
	}
	hidden := max(r.AvailableMinId-1, 0)
	maxId = historyMaxId(maxId, c.TopMessage)
	if maxId <= hidden {
		return hidden, nil
	}
	if _, err = b.Tx().Exec("UPDATE channel_participants SET available_min_id = ? WHERE channel_id = ? AND user_id = ?",
		maxId+1, channelId, b.UserId()); err != nil {
		return 0, err
	}
	r.AvailableMinId = maxId + 1
	InvalidateDialog(b, channelId)
	if maxId > r.ReadInboxMaxId {
		if err = d.moveRead(b, channelId, maxId, r); err != nil {
			return 0, err
		}
	}
	u := mtproto.MakeTLUpdateChannelAvailableMessages(&mtproto.Update{ChannelId: channelId, AvailableMinId: maxId}).To_Update()
	if err = d.EmitSeqUpdate(b, u, nil, nil); err != nil {
		return 0, err
	}
	return maxId, nil
}

// ParticipantHistoryDeleted is one portion of DeleteParticipantHistory.
type ParticipantHistoryDeleted struct {
	*Deleted
	Left int32 // the messages of the participant still in the channel
}

// DeleteParticipantHistory deletes for everyone the newest portion of the
// messages of userId in the supergroup, by a member with the right to delete
// the messages of others (channelperm.DeleteOthers, as channels.deleteMessages
// for another's message; CHAT_ADMIN_REQUIRED, core.telegram.org/method/
// channels.deleteParticipantHistory). Left tells how many remain. The copy
// of a channel's post in its discussion group is left out: its author is the
// channel (postCopy in msg, core.telegram.org/api/discussion), though its row
// names the admin who posted.
func (d *Dao) DeleteParticipantHistory(b *userbox.Box, channelId, userId int64) (*ParticipantHistoryDeleted, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	c, me, err := channelForMember(b, channelId)
	if err != nil {
		return nil, err
	}
	if !me.participant().Member() {
		return nil, mtproto.ErrChannelPrivate
	}
	if c.Broadcast {
		// "of a given supergroup" (the method's page); the clients offer it
		// nowhere else.
		return nil, mtproto.ErrChannelInvalid
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if err = a.can(channelperm.DeleteOthers, b.Date()); err != nil {
		return nil, err
	}
	var ids []int32
	if err = b.Tx().QueryRowsPartial(&ids,
		"SELECT m.id"+participantMessages+" ORDER BY m.id DESC LIMIT ? FOR UPDATE",
		channelId, userId, DeleteMax); err != nil {
		return nil, err
	}
	// What a member wrote to a supergroup is no post, and the copies of
	// posts are left out: no message here has a discussion link.
	del, err := deleteRows(b, ch, channelId, ids)
	if err != nil {
		return nil, err
	}
	var left []int32
	if err = b.Tx().QueryRowsPartial(&left,
		"SELECT COUNT(*)"+participantMessages,
		channelId, userId); err != nil {
		return nil, err
	}
	return &ParticipantHistoryDeleted{Deleted: del, Left: first(left)}, nil
}

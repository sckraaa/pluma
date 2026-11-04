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
	inbox "github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// DeleteLimit is the most members a channel may have to be deleted:
// CHANNEL_TOO_LARGE past it (core.telegram.org/method/channels.deleteChannel).
const DeleteLimit = 1000

// participantRow is a member of a channel being deleted.
type participantRow struct {
	UserId       int64 `db:"user_id"`
	State        int32 `db:"state"`
	BannedRights int32 `db:"banned_rights"`
	UntilDate    int32 `db:"until_date"`
}

// DeleteChannel deletes the channel for everyone, by b's owner, who must be
// its creator (task 055; core.telegram.org/method/channels.deleteChannel:
// CHANNEL_INVALID for none or one deleted, CHANNEL_PRIVATE to one not in a
// private channel, CHAT_ADMIN_REQUIRED to another member,
// CHANNEL_TOO_LARGE past DeleteLimit members). In b's transaction with the
// channel locked:
//   - the channel is marked deleted, which every read honours (ADR 0034:
//     a deleted channel is channelForbidden, its links and history are
//     gone); its messages stay as rows, unread;
//   - every member leaves, so that it counts against no limit and gets no
//     more pushes; the creator's dialog goes;
//   - its username is freed and its invite links revoked;
//   - each other member gets inbox.channelDeletedToInbox, which drops the
//     dialog and journals updateChannel in that member's own box.
//
// It returns the members other than b's owner.
func (d *Dao) DeleteChannel(b *userbox.Box, channelId int64) ([]int64, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	c, me, err := channelForMember(b, channelId)
	if err != nil {
		return nil, err
	}
	switch p := me.participant(); {
	case !p.Member() && !c.Public:
		return nil, mtproto.ErrChannelPrivate
	case !p.Member() || me.Role != channel.RoleCreator:
		return nil, mtproto.ErrChatAdminRequired
	case c.ParticipantsCount > DeleteLimit:
		return nil, mtproto.ErrChannelTooLarge
	}

	var rows []participantRow
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT user_id, state, banned_rights, until_date FROM channel_participants WHERE channel_id = ? FOR UPDATE", channelId); err != nil {
		return nil, err
	}
	var others []int64
	for _, r := range rows {
		p := channel.ImmutableChannelParticipant{State: r.State, BannedRights: r.BannedRights, UntilDate: r.UntilDate}
		if p.Member() && r.UserId != b.UserId() {
			others = append(others, r.UserId)
		}
	}

	if _, err = b.Tx().Exec("UPDATE channels SET deleted = 1, participants_count = 0, admins_count = 0, version = version + 1 WHERE id = ?",
		channelId); err != nil {
		return nil, err
	}
	// Every member leaves, a restricted one too; a ban with view_messages
	// stays a ban.
	if _, err = b.Tx().Exec("UPDATE channel_participants SET state = ?, role = 0, admin_rights = 0, `rank` = '', banned_rights = 0, until_date = 0"+
		" WHERE channel_id = ? AND (state <> ? OR banned_rights & ? = 0)",
		channel.StateLeft, channelId, channel.StateBanned, mtproto.BANNED_VIEW_MESSAGES); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("DELETE FROM username WHERE peer_type = ? AND peer_id = ?", mtproto.PEER_CHANNEL, channelId); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE chat_invites SET revoked = 1 WHERE chat_id = ?", channelId); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE dialogs SET deleted = 1 WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		b.UserId(), mtproto.PEER_CHANNEL, channelId); err != nil {
		return nil, err
	}
	InvalidateDialog(b, channelId)

	for _, id := range others {
		ev := &inbox.TLInboxChannelDeletedToInbox{UserId: id, FromId: b.UserId(), ChannelId: channelId}
		data, err := jsonx.Marshal(ev)
		if err != nil {
			return nil, err
		}
		b.Emit(userbox.Event{Topic: d.Topics.Inbox, Recipient: id, Method: string(proto.MessageName(ev)), Data: data})
	}
	return others, nil
}

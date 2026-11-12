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
//   - its username is freed and its invite links revoked, and the monoforum
//     of its direct messages is deleted with it: it leaves the dialogs of
//     all who have it, b's owner's here, the others' by
//     inbox.channelDeletedToInbox;
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
	// The monoforum of its direct messages goes with it (task 084): nobody
	// may open it without the channel, and it leaves the dialogs. Its id is
	// above the channel's, which keeps the order of the row locks.
	var monos []struct {
		Id         int64  `db:"id"`
		AccessHash int64  `db:"access_hash"`
		Title      string `db:"title"`
	}
	if err = b.Tx().QueryRowsPartial(&monos,
		"SELECT m.id, m.access_hash, c.title FROM channels m JOIN channels c ON c.id = m.linked_monoforum_id"+
			" WHERE m.monoforum = 1 AND m.linked_monoforum_id = ? AND m.id > ? AND m.deleted = 0 FOR UPDATE", channelId, channelId); err != nil {
		return nil, err
	}
	for _, mono := range monos {
		if _, err = b.Tx().Exec("UPDATE channels SET deleted = 1, version = version + 1 WHERE id = ?", mono.Id); err != nil {
			return nil, err
		}
		// Those who have the chat with the channel in their dialogs, the
		// admins and the subscribers alike, whether in the channel or not,
		// drop it as the members drop the channel: nobody is in a
		// monoforum, so its dialogs are its only readers. b's owner drops
		// its own here, its devices get updateChannel with the monoforum
		// as channelForbidden; the others by inbox.channelDeletedToInbox.
		var holders []int64
		if err = b.Tx().QueryRowsPartial(&holders,
			"SELECT user_id FROM dialogs WHERE peer_type = ? AND peer_id = ? AND deleted = 0", mtproto.PEER_CHANNEL, mono.Id); err != nil {
			return nil, err
		}
		for _, id := range holders {
			if id != b.UserId() {
				err = d.emitChannelDeleted(b, id, mono.Id)
			} else {
				err = d.dropOwnDialog(b, &channel.ImmutableChannel{
					Id: mono.Id, AccessHash: mono.AccessHash, Title: mono.Title, Megagroup: true, Monoforum: true, Deleted: true,
				})
			}
			if err != nil {
				return nil, err
			}
		}
	}
	// Every member leaves, a restricted one too; one kicked stays kicked.
	if _, err = b.Tx().Exec("UPDATE channel_participants SET state = ?, role = 0, admin_rights = 0, `rank` = '', banned_rights = 0, until_date = 0"+
		" WHERE channel_id = ? AND state <> ?",
		channel.StateLeft, channelId, channel.StateKicked); err != nil {
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
		if err = d.emitChannelDeleted(b, id, channelId); err != nil {
			return nil, err
		}
	}
	return others, nil
}

// dropOwnDialog drops b's owner's dialog of the deleted channel ch and
// journals updateChannel with seq for its devices, ch as it sees it now.
func (d *Dao) dropOwnDialog(b *userbox.Box, ch *channel.ImmutableChannel) error {
	if _, err := b.Tx().Exec("UPDATE dialogs SET deleted = 1 WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		b.UserId(), mtproto.PEER_CHANNEL, ch.GetId()); err != nil {
		return err
	}
	InvalidateDialog(b, ch.GetId())
	u := mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: ch.GetId()}).To_Update()
	return d.EmitSeqUpdate(b, u, nil, []*mtproto.Chat{ch.ToChat(nil, int32(b.Date()))})
}

// emitChannelDeleted adds inbox.channelDeletedToInbox of channelId for
// userId: its own box drops the dialog and journals updateChannel.
func (d *Dao) emitChannelDeleted(b *userbox.Box, userId, channelId int64) error {
	ev := &inbox.TLInboxChannelDeletedToInbox{UserId: userId, FromId: b.UserId(), ChannelId: channelId}
	data, err := jsonx.Marshal(ev)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Inbox, Recipient: userId, Method: string(proto.MessageName(ev)), Data: data})
	return nil
}

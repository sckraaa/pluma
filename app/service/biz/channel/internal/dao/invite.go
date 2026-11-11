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
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/premium"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// InviteMax is the most users one channels.inviteToChannel adds (Telegram
// adds at most 200 to a channel by invitation, core.telegram.org/api/
// channel; the clients send them in one call).
const InviteMax = 200

// Invited is what AddMembers wrote: the users added and the service message
// of a supergroup (nil in a channel, or when nobody was added).
type Invited struct {
	ChannelChange
	Users []int64
}

// AddMembers adds users ids to the channel, by b's owner (task 041): the
// creator and the admins who may invite users add to any; in a supergroup
// a member whom neither the default rights nor its own restriction ban
// invite_users adds (channelperm.Can). Otherwise CHAT_ADMIN_REQUIRED. Users already in are
// left out; one kicked comes back only by an admin who may ban
// (USER_KICKED otherwise); one at the limit of channels is
// USER_CHANNELS_TOO_MUCH, as Telegram answers
// (core.telegram.org/method/channels.inviteToChannel). Each added user's
// dialog is written in its own box by the event AddMembers emits (ADR 0021).
// The request to join of one added no longer stands.
func (d *Dao) AddMembers(b *userbox.Box, channelId int64, ids []int64) (*Invited, error) {
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
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if err = a.can(channelperm.InviteUsers, b.Date()); err != nil {
		return nil, err
	}
	canBan := a.can(channelperm.BanUsers, b.Date()) == nil

	out := &Invited{ChannelChange: ChannelChange{ChannelId: channelId}}
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	for _, id := range ids {
		if id <= 0 || id == b.UserId() {
			continue
		}
		var rows []memberRow
		if err = b.Tx().QueryRowsPartial(&rows,
			"SELECT role, state, banned_rights, until_date FROM channel_participants WHERE channel_id = ? AND user_id = ? FOR UPDATE", channelId, id); err != nil {
			return nil, err
		}
		var p *memberRow
		if len(rows) > 0 {
			p = &rows[0]
		}
		switch {
		case p.participant().Member():
			continue
		case p != nil && p.participant().Kicked(int32(b.Date())) && !canBan:
			return nil, mtproto.ErrUserKicked
		}
		if err = checkChannelsLimit(b, id); err != nil {
			return nil, err
		}
		out.Users = append(out.Users, id)
	}
	if len(out.Users) == 0 {
		return out, nil
	}
	if err = d.writeMembers(b, ch, c, channelId, out.Users, &out.ChannelChange); err != nil {
		return nil, err
	}
	return out, nil
}

// checkChannelsLimit refuses id at the limit of its channels
// (USER_CHANNELS_TOO_MUCH).
func checkChannelsLimit(b *userbox.Box, id int64) error {
	var joined []int64
	if err := b.Tx().QueryRowsPartial(&joined, "SELECT channel_id FROM channel_participants WHERE user_id = ? AND state = 0 LIMIT ?", id, premium.ChannelsLimit); err != nil {
		return err
	}
	if len(joined) >= premium.ChannelsLimit {
		return mtproto.ErrUserChannelsTooMuch
	}
	return nil
}

// writeMembers makes users, none of them in the channel now, its members by
// b's owner: their rows, the counter, the service message of a supergroup
// (into change) and an inbox event for each. USERS_TOO_MUCH when they do not
// fit.
func (d *Dao) writeMembers(b *userbox.Box, ch *channelbox.Channel, c *memberChannelRow, channelId int64, users []int64, change *ChannelChange) error {
	if c.ParticipantsCount+int32(len(users)) > MembersMax {
		return mtproto.ErrUsersTooMuch
	}
	var err error
	if !c.Broadcast {
		action := mtproto.MakeTLMessageActionChatAddUser(&mtproto.MessageAction{Users: users}).To_MessageAction()
		if change.Message, change.Pts, err = addServiceMessage(b, ch, channelId, false, action); err != nil {
			return err
		}
		if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", change.Message.GetId(), channelId); err != nil {
			return err
		}
	}
	var availableMinId int32
	if c.HiddenPrehistory && !c.Broadcast {
		availableMinId = change.Message.GetId()
	}
	// The owner who left comes back as the owner (task 078, Join).
	owners, err := ownerAddedBack(b, channelId, users)
	if err != nil {
		return err
	}
	for _, id := range users {
		if _, err = b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, state, inviter_user_id, available_min_id, `date`) VALUES (?, ?, 0, 0, ?, ?, ?)"+
			" ON DUPLICATE KEY UPDATE role = IF(role = ?, role, 0), state = 0, admin_rights = 0, `rank` = '', banned_rights = 0, until_date = 0, kicked_by = 0,"+
			" inviter_user_id = VALUES(inviter_user_id), available_min_id = VALUES(available_min_id), `date` = VALUES(`date`)",
			channelId, id, b.UserId(), availableMinId, b.Date(), channelperm.RoleCreator); err != nil {
			return err
		}
		ev := &inbox.TLInboxChannelInvitedToInbox{UserId: id, FromId: b.UserId(), ChannelId: channelId}
		data, err := jsonx.Marshal(ev)
		if err != nil {
			return err
		}
		b.Emit(userbox.Event{Topic: d.Topics.Inbox, Recipient: id, Method: string(proto.MessageName(ev)), Data: data})
	}
	if _, err = b.Tx().Exec("UPDATE channels SET participants_count = participants_count + ?, admins_count = admins_count + ? WHERE id = ?", len(users), owners, channelId); err != nil {
		return err
	}
	return d.dropRequests(b, channelId, users...)
}

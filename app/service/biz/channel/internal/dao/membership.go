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
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// Joining and leaving a channel (task 041, ADR 0034, p. 3): one box
// transaction of the user, with the channel locked after it, writes the
// participant row, the counters, the user's dialog, the service message of
// a supergroup and updateChannel with the user's seq.

const (
	// ChannelsLimit is the most channels and supergroups a user is in
	// (channels_limit_default, core.telegram.org/api/config: 500).
	ChannelsLimit = 500
	// MembersMax is the most members of a supergroup or channel
	// (megagroup_size_max, core.telegram.org/api/config: 200000).
	MembersMax = 200000
)

// ChannelChange is what a change of a channel wrote: its service message
// (nil when it has none) with its pts; Reload when the members are to load
// the channel again (updateChannel, ADR 0034, p. 3).
type ChannelChange struct {
	ChannelId int64
	Message   *mtproto.Message
	Pts       int32
	Reload    bool
	// Self and Count are, for a join or a leave, the owner's participant
	// row and the channel's participants_count as the transaction wrote
	// them.
	Self  *channel.ImmutableChannelParticipant
	Count int32
}

type memberChannelRow struct {
	Broadcast         bool  `db:"broadcast"`
	Deleted           bool  `db:"deleted"`
	HiddenPrehistory  bool  `db:"hidden_prehistory"`
	ParticipantsCount int32 `db:"participants_count"`
	TopMessage        int32 `db:"top_message"`
	Public            bool  `db:"public"`
}

type memberRow struct {
	Role         int32 `db:"role"`
	State        int32 `db:"state"`
	BannedRights int32 `db:"banned_rights"`
	UntilDate    int32 `db:"until_date"`
}

// Invite is a row of chat_invites.
type Invite struct {
	ChatId        int64 `db:"chat_id"`
	AdminId       int64 `db:"admin_id"`
	Revoked       bool  `db:"revoked"`
	RequestNeeded bool  `db:"request_needed"`
	ExpireDate    int64 `db:"expire_date"`
	UsageLimit    int32 `db:"usage_limit"`
	Usage         int32 `db:"usage2"`
}

// Works tells whether the link may be used at now: not revoked, not
// expired, not used up.
func (i *Invite) Works(now int64) bool {
	return !i.Revoked && (i.ExpireDate == 0 || i.ExpireDate > now) && (i.UsageLimit == 0 || i.Usage < i.UsageLimit)
}

const inviteColumns = "i.chat_id, i.admin_id, i.revoked, i.request_needed, i.expire_date, i.usage_limit, i.usage2"

// Invite returns the invite link of a channel that exists;
// INVITE_HASH_INVALID for any other.
func (d *Dao) Invite(ctx context.Context, link string) (*Invite, error) {
	var rows []Invite
	if err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT "+inviteColumns+" FROM chat_invites i JOIN channels c ON c.id = i.chat_id WHERE i.link = ? AND c.deleted = 0", link); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, mtproto.ErrInviteHashInvalid
	}
	return &rows[0], nil
}

// InviteChannel returns the channel of the invite link; INVITE_HASH_INVALID
// when no channel has it.
func (d *Dao) InviteChannel(ctx context.Context, link string) (int64, error) {
	inv, err := d.Invite(ctx, link)
	if err != nil {
		return 0, err
	}
	return inv.ChatId, nil
}

// channelForMember reads the channel locked by ch and b's owner's row of
// it, locked.
func channelForMember(b *userbox.Box, channelId int64) (*memberChannelRow, *memberRow, error) {
	var chans []memberChannelRow
	if err := b.Tx().QueryRowsPartial(&chans,
		"SELECT c.broadcast, c.deleted, c.hidden_prehistory, c.participants_count, c.top_message,"+
			" EXISTS (SELECT 1 FROM username u WHERE u.peer_type = ? AND u.peer_id = c.id AND u.editable = 1) AS public"+
			" FROM channels c WHERE c.id = ?",
		mtproto.PEER_CHANNEL, channelId); err != nil {
		return nil, nil, err
	}
	if len(chans) == 0 || chans[0].Deleted {
		return nil, nil, mtproto.ErrChannelInvalid
	}
	var members []memberRow
	if err := b.Tx().QueryRowsPartial(&members,
		"SELECT role, state, banned_rights, until_date FROM channel_participants WHERE channel_id = ? AND user_id = ? FOR UPDATE",
		channelId, b.UserId()); err != nil {
		return nil, nil, err
	}
	if len(members) == 0 {
		return &chans[0], nil, nil
	}
	return &chans[0], &members[0], nil
}

func (m *memberRow) participant() *channel.ImmutableChannelParticipant {
	if m == nil {
		return nil
	}
	return &channel.ImmutableChannelParticipant{Role: m.Role, State: m.State, BannedRights: m.BannedRights, UntilDate: m.UntilDate}
}

// openChannel locks the channel in b's transaction.
func openChannel(b *userbox.Box, channelId int64) (*channelbox.Channel, error) {
	ch, err := channelbox.Open(b, channelId)
	if errors.Is(err, channelbox.ErrNoChannel) {
		return nil, mtproto.ErrChannelInvalid
	}
	return ch, err
}

// Join makes b's owner a member of the channel: by itself in a public one
// (link empty), by the invite link link in any. Errors as Telegram's
// (core.telegram.org/method/channels.joinChannel): CHANNEL_PRIVATE for a
// private channel without a link or for one kicked from it,
// USER_ALREADY_PARTICIPANT, CHANNELS_TOO_MUCH, USERS_TOO_MUCH,
// INVITE_HASH_EXPIRED for a revoked, expired or used up link.
func (d *Dao) Join(b *userbox.Box, channelId int64, link string) (*ChannelChange, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	c, me, err := channelForMember(b, channelId)
	if err != nil {
		return nil, err
	}
	p := me.participant()
	switch {
	case p.Member():
		return nil, mtproto.ErrUserAlreadyParticipant
	case p != nil && p.Kicked(int32(b.Date())):
		return nil, mtproto.ErrChannelPrivate
	}

	var inviter int64
	if link != "" {
		var invites []Invite
		if err = b.Tx().QueryRowsPartial(&invites,
			"SELECT "+inviteColumns+" FROM chat_invites i WHERE i.link = ? FOR UPDATE", link); err != nil {
			return nil, err
		}
		if len(invites) == 0 || invites[0].ChatId != channelId {
			return nil, mtproto.ErrInviteHashInvalid
		}
		inv := invites[0]
		switch {
		case !inv.Works(b.Date()):
			return nil, mtproto.ErrInviteHashExpired
		case inv.RequestNeeded:
			// Links that need an admin's approval are made by
			// messages.exportChatInvite, which channels do not have yet:
			// such a link cannot be a channel's (task 041, join requests).
			return nil, mtproto.ErrInviteHashInvalid
		}
		inviter = inv.AdminId
		if _, err = b.Tx().Exec("UPDATE chat_invites SET usage2 = usage2 + 1 WHERE link = ?", link); err != nil {
			return nil, err
		}
		// The user may have joined by this link before and left: the row
		// of the link and the user is there (UNIQUE link, user_id), and the
		// join is new (an upsert, as basic groups write it, chat_box.go).
		if _, err = b.Tx().Exec("INSERT INTO chat_invite_participants (chat_id, link, user_id, requested, approved_by, date2) VALUES (?, ?, ?, 0, 0, ?) AS new"+
			" ON DUPLICATE KEY UPDATE chat_id = new.chat_id, requested = 0, approved_by = 0, date2 = new.date2",
			channelId, link, b.UserId(), b.Date()); err != nil {
			return nil, err
		}
	} else if !c.Public {
		return nil, mtproto.ErrChannelPrivate
	}

	var joined []int64
	if err = b.Tx().QueryRowsPartial(&joined, "SELECT channel_id FROM channel_participants WHERE user_id = ? AND state = 0 LIMIT ?",
		b.UserId(), ChannelsLimit); err != nil {
		return nil, err
	}
	switch {
	case len(joined) >= ChannelsLimit:
		return nil, mtproto.ErrChannelsTooMuch
	case c.ParticipantsCount >= MembersMax:
		return nil, mtproto.ErrUsersTooMuch
	}

	change := &ChannelChange{ChannelId: channelId}
	if !c.Broadcast {
		// A supergroup tells its members who joined (the clients draw the
		// text of the action); a channel does not.
		action := mtproto.MakeTLMessageActionChatAddUser(&mtproto.MessageAction{Users: []int64{b.UserId()}}).To_MessageAction()
		if link != "" {
			action = mtproto.MakeTLMessageActionChatJoinedByLink(&mtproto.MessageAction{InviterId: inviter}).To_MessageAction()
		}
		if change.Message, change.Pts, err = addServiceMessage(b, ch, channelId, false, action); err != nil {
			return nil, err
		}
	}
	top := max(c.TopMessage, change.Message.GetId())
	// Who joins a supergroup with its history hidden sees it from the
	// message of the join on (channels.togglePreHistoryHidden).
	var availableMinId int32
	if c.HiddenPrehistory && !c.Broadcast {
		availableMinId = change.Message.GetId()
	}
	role := channel.RoleMember
	if me != nil && me.Role == channel.RoleCreator {
		role = channel.RoleCreator
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, state, inviter_user_id, available_min_id, `date`) VALUES (?, ?, ?, 0, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE role = VALUES(role), state = 0, admin_rights = 0, `rank` = '', banned_rights = 0, until_date = 0, kicked_by = 0,"+
		" inviter_user_id = VALUES(inviter_user_id), available_min_id = VALUES(available_min_id), `date` = VALUES(`date`)",
		channelId, b.UserId(), role, inviter, availableMinId, b.Date()); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE channels SET participants_count = participants_count + 1, top_message = ? WHERE id = ?", top, channelId); err != nil {
		return nil, err
	}
	change.Self = channel.MakeTLImmutableChannelParticipant(&channel.ImmutableChannelParticipant{
		ChannelId:      channelId,
		UserId:         b.UserId(),
		Role:           role,
		State:          channel.StateIn,
		InviterUserId:  inviter,
		AvailableMinId: availableMinId,
		Date:           b.Date(),
	}).To_ImmutableChannelParticipant()
	change.Count = c.ParticipantsCount + 1
	// The dialog starts read up to the last message.
	if err = upsertDialog(b, channelId, top); err != nil {
		return nil, err
	}
	return change, nil
}

// Leave takes b's owner out of the channel: USER_NOT_PARTICIPANT for one
// not in it, USER_CREATOR for its creator (core.telegram.org/method/
// channels.leaveChannel). Its dialog goes, as the clients drop it.
func (d *Dao) Leave(b *userbox.Box, channelId int64) (*ChannelChange, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	c, me, err := channelForMember(b, channelId)
	if err != nil {
		return nil, err
	}
	switch {
	case !me.participant().Member():
		return nil, mtproto.ErrUserNotParticipant
	case me.Role == channel.RoleCreator:
		return nil, mtproto.ErrUserCreator
	}
	change := &ChannelChange{ChannelId: channelId}
	if !c.Broadcast {
		action := mtproto.MakeTLMessageActionChatDeleteUser(&mtproto.MessageAction{UserId: b.UserId()}).To_MessageAction()
		if change.Message, change.Pts, err = addServiceMessage(b, ch, channelId, false, action); err != nil {
			return nil, err
		}
		if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", change.Message.GetId(), channelId); err != nil {
			return nil, err
		}
	}
	// A restricted member leaves its restriction behind: a left row has
	// none (channels.editBanned, 042, restricts members only).
	if _, err = b.Tx().Exec("UPDATE channel_participants SET state = ?, role = 0, admin_rights = 0, `rank` = '', banned_rights = 0, until_date = 0 WHERE channel_id = ? AND user_id = ?",
		channel.StateLeft, channelId, b.UserId()); err != nil {
		return nil, err
	}
	admins := 0
	if me.Role == channel.RoleAdmin {
		admins = 1
	}
	if _, err = b.Tx().Exec("UPDATE channels SET participants_count = GREATEST(participants_count - 1, 0), admins_count = GREATEST(admins_count - ?, 0) WHERE id = ?",
		admins, channelId); err != nil {
		return nil, err
	}
	change.Self = channel.MakeTLImmutableChannelParticipant(&channel.ImmutableChannelParticipant{
		ChannelId: channelId,
		UserId:    b.UserId(),
		State:     channel.StateLeft,
		Date:      b.Date(),
	}).To_ImmutableChannelParticipant()
	change.Count = max(c.ParticipantsCount-1, 0)
	if _, err = b.Tx().Exec("UPDATE dialogs SET deleted = 1 WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		b.UserId(), mtproto.PEER_CHANNEL, channelId); err != nil {
		return nil, err
	}
	InvalidateDialog(b, channelId)
	return change, nil
}

// addServiceMessage writes a service message of b's owner to the channel:
// the next id and pts of the channel, one copy. In a supergroup it is from
// the owner; in a channel (broadcast) it is a post, without an author, as
// the first message of a channel is (channel.createChannel).
func addServiceMessage(b *userbox.Box, ch *channelbox.Channel, channelId int64, broadcast bool, action *mtproto.MessageAction) (*mtproto.Message, int32, error) {
	m := mtproto.MakeTLMessageService(&mtproto.Message{
		Id:     ch.NextMessageId(),
		Post:   broadcast,
		PeerId: mtproto.MakePeerChannel(channelId),
		Date:   int32(b.Date()),
		Action: action,
	}).To_Message()
	if !broadcast {
		m.FromId = mtproto.MakePeerUser(b.UserId())
	}
	ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		return nil, 0, err
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_data, date2) VALUES (?, ?, ?, ?, ?, ?)",
		channelId, m.Id, b.UserId(), ch.Pts(), data, b.Date()); err != nil {
		return nil, 0, err
	}
	return m, ch.Pts(), nil
}

// upsertDialog shows the channel in b's owner's dialogs, read up to top.
func upsertDialog(b *userbox.Box, channelId int64, top int32) error {
	InvalidateDialog(b, channelId)
	res, err := b.Tx().Exec("UPDATE dialogs SET top_message = ?, read_inbox_max_id = ?, date2 = ?, deleted = 0 WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		top, top, b.Date(), b.UserId(), mtproto.PEER_CHANNEL, channelId)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err = b.Tx().Exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, top_message, read_inbox_max_id, draft_message_data, date2) VALUES (?, ?, ?, ?, ?, ?, 'null', ?)",
		b.UserId(), mtproto.PEER_CHANNEL, channelId, mtproto.MakePeerDialogId(mtproto.PEER_CHANNEL, channelId), top, top, b.Date())
	return err
}

// EmitChannelPush adds the push of the change to the channel's members
// (sync.pushChannelUpdates, keyed by the channel), as msg does for the
// messages it sends: the new service message, and updateChannel without
// seq when they are to load the channel again.
func (d *Dao) EmitChannelPush(b *userbox.Box, permAuthKeyId int64, change *ChannelChange) error {
	var updates []*mtproto.Update
	if change.Message != nil {
		updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{
			Message_MESSAGE: change.Message,
			Pts_INT32:       change.Pts,
			PtsCount:        1,
		}).To_Update())
	}
	if change.Reload {
		updates = append(updates, mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: change.ChannelId}).To_Update())
	}
	if len(updates) == 0 {
		return nil
	}
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId:     change.ChannelId,
		FromId:        b.UserId(),
		FromAuthKeyId: permAuthKeyId,
		Updates:       mtproto.MakeUpdatesByUpdates(updates...),
	}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: change.ChannelId, Method: string(proto.MessageName(in)), Data: data})
	return nil
}

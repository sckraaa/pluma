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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/adminlog"
	"math"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/premium"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// The rights in a channel (task 042): admins promoted and demoted
// (channels.editAdmin), members restricted, kicked and freed
// (channels.editBanned), the default restrictions of a supergroup
// (messages.editChatDefaultBannedRights). Each is written in the actor's
// box transaction with the channel locked; the members load the channel
// again on updateChannel (ADR 0034, p. 3), a kicked user loses it by an
// inbox event.

// RankMax is the longest admin rank, in characters
// (core.telegram.org/constructor/channelParticipantAdmin: "up to 16").
const RankMax = 16

type actorRow struct {
	Broadcast           bool  `db:"broadcast"`
	Gigagroup           bool  `db:"gigagroup"`
	Blocked             bool  `db:"blocked"`
	DefaultBannedRights int32 `db:"default_banned_rights"`
	Role                int32 `db:"role"`
	State               int32 `db:"state"`
	AdminRights         int32 `db:"admin_rights"`
	BannedRights        int32 `db:"banned_rights"`
	UntilDate           int32 `db:"until_date"`
}

// actor is b's owner as a participant of a channel the transaction locked.
type actor struct {
	ch channelperm.Channel
	p  channelperm.Participant
}

func (a *actor) can(act channelperm.Action, now int64) error {
	return channelperm.Can(a.ch, a.p, act, int32(now))
}

// rightsActor reads the channel, locked, and b's owner's row of it.
// CHANNEL_PRIVATE for one out of it.
func rightsActor(b *userbox.Box, channelId int64) (*actor, error) {
	var rows []actorRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT c.broadcast, c.gigagroup, c.blocked, c.default_banned_rights, p.role, p.state, p.admin_rights, p.banned_rights, p.until_date"+
			" FROM channels c JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = ? WHERE c.id = ? AND c.deleted = 0",
		b.UserId(), channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].State != channelperm.StateIn {
		return nil, mtproto.ErrChannelPrivate
	}
	r := rows[0]
	return &actor{
		ch: channelperm.Channel{Broadcast: r.Broadcast, Gigagroup: r.Gigagroup, Default: r.DefaultBannedRights, Blocked: r.Blocked},
		p: channelperm.Participant{Role: r.Role, State: r.State, AdminRights: r.AdminRights,
			BannedRights: r.BannedRights, UntilDate: r.UntilDate},
	}, nil
}

type targetRow struct {
	Role         int32  `db:"role"`
	State        int32  `db:"state"`
	AdminRights  int32  `db:"admin_rights"`
	Rank         string `db:"rank"`
	BannedRights int32  `db:"banned_rights"`
	UntilDate    int32  `db:"until_date"`
	PromotedBy   int64  `db:"promoted_by"`
}

// rightsTarget reads the row of userId in the channel, locked; nil when it
// has none.
func rightsTarget(b *userbox.Box, channelId, userId int64) (*targetRow, error) {
	var rows []targetRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT role, state, admin_rights, `rank`, banned_rights, until_date, promoted_by FROM channel_participants WHERE channel_id = ? AND user_id = ? FOR UPDATE",
		channelId, userId); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// in tells whether the target is a member of the channel.
func (t *targetRow) in() bool { return t != nil && t.State == channelperm.StateIn }

// kicked tells whether the target is kept out of the channel at now: a
// kick runs to its until_date (0 or Int32.max: for good).
func (t *targetRow) kicked(now int64) bool {
	return t != nil && t.State == channelperm.StateKicked &&
		(t.UntilDate == 0 || t.UntilDate == math.MaxInt32 || now < int64(t.UntilDate))
}

// editableAdmin tells whether a may change the rights of the admin t: the
// creator any, an admin with add_admins the admins it promoted
// (core.telegram.org/api/rights; the clients show the same as canEdit).
func (a *actor) editableAdmin(t *targetRow, selfId int64, now int64) bool {
	if a.can(channelperm.AddAdmins, now) != nil {
		return false
	}
	return a.p.Role == channelperm.RoleCreator || t.PromotedBy == selfId
}

func checkRank(rank string) error { return channelperm.CheckRank(rank) }

// EditParticipantRank sets the member tag of userId in a supergroup, as the
// clients offer it (Android ProfileActivity.java:7250-7257, ChatObject.
// canManageMyTag; tdesktop PeerData::canManageRanks): the creator anyone's; a
// member its own unless it is forbidden, by the group or by its own
// restriction; an admin with manage_ranks a member's and an admin's it
// promoted; every admin its own. A broadcast channel has no tags.
func (d *Dao) EditParticipantRank(b *userbox.Box, channelId, userId int64, rank string) (*ChannelChange, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if a.ch.Broadcast {
		return nil, mtproto.ErrChatAdminRequired
	}
	if err = checkRank(rank); err != nil {
		return nil, err
	}
	t, err := rightsTarget(b, channelId, userId)
	if err != nil {
		return nil, err
	}
	if !t.in() {
		return nil, mtproto.ErrUserNotParticipant
	}
	if !a.canEditRank(t, userId == b.UserId(), b.UserId(), b.Date()) {
		return nil, mtproto.ErrChatAdminRequired
	}
	if t.Rank == rank {
		return nil, mtproto.ErrChatNotModified
	}
	if _, err = b.Tx().Exec("UPDATE channel_participants SET `rank` = ? WHERE channel_id = ? AND user_id = ?", rank, channelId, userId); err != nil {
		return nil, err
	}
	if err = adminlog.Write(b, channelId, mtproto.MakeTLChannelAdminLogEventActionParticipantEditRank(&mtproto.ChannelAdminLogEventAction{
		UserId: userId, PrevRank: t.Rank, NewRank: rank,
	}).To_ChannelAdminLogEventAction(), rank); err != nil {
		return nil, err
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}

// canEditRank tells whether a may set the tag of t, which is its own when self.
func (a *actor) canEditRank(t *targetRow, self bool, selfId, now int64) bool {
	if a.p.Role == channelperm.RoleCreator {
		return true
	}
	if self {
		return a.p.Role == channelperm.RoleAdmin || !a.rankForbidden(now)
	}
	manages := a.p.Role == channelperm.RoleAdmin && a.p.AdminRights&mtproto.ADMIN_MANAGE_RANKS != 0
	if !manages || t.Role == channelperm.RoleCreator {
		return false
	}
	return t.Role != channelperm.RoleAdmin || t.PromotedBy == selfId
}

// rankForbidden tells whether a member may not change its own tag at now.
func (a *actor) rankForbidden(now int64) bool {
	if a.ch.Default&mtproto.BANNED_EDIT_RANK != 0 {
		return true
	}
	return a.p.BannedRights&mtproto.BANNED_EDIT_RANK != 0 && (a.p.UntilDate == 0 || a.p.UntilDate == math.MaxInt32 || now < int64(a.p.UntilDate))
}

// EditAdmin gives userId the admin rights rights in the channel, or takes
// them away with rights 0; rank is its title, kept as it is when keepRank.
// The rules (core.telegram.org/api/rights, method/channels.editAdmin):
//   - the creator changes its own rank and anonymity only;
//   - one with add_admins promotes members and changes the admins it
//     promoted (CHAT_ADMIN_REQUIRED for others), giving at most its own
//     rights (RIGHT_FORBIDDEN); the creator gives every right;
//   - the creator is not edited by others (USER_CREATOR);
//   - the user must be in the channel: USER_NOT_PARTICIPANT, which iOS
//     answers by inviting the user and trying again (PeerAdmins.swift:205-222),
//     USER_KICKED for one kicked;
//   - a bot is the exception: it joins by being made an admin. The clients
//     send only this method for a new bot (Android setUserAdminRole,
//     `!user.bot && addingNew`; tdesktop SaveChannelAdmin), and a broadcast
//     channel has no other way in (tdesktop api_chat_participants.cpp:593:
//     USER_BOT); the method answers BOT_GROUPS_BLOCKED, which only a bot
//     joining by it can get (core.telegram.org/method/channels.editAdmin).
//
// A promotion lifts the member's restriction. The members load the channel
// again.
func (d *Dao) EditAdmin(b *userbox.Box, channelId, userId int64, rights int32, rank string, keepRank bool) (*ChannelChange, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	if rights&^channelperm.AdminMask != 0 {
		return nil, mtproto.ErrRightForbidden
	}
	if !keepRank {
		if err = checkRank(rank); err != nil {
			return nil, err
		}
	}
	t, err := rightsTarget(b, channelId, userId)
	if err != nil {
		return nil, err
	}
	now := b.Date()
	change := &ChannelChange{ChannelId: channelId, Reload: true}
	if userId == b.UserId() && a.p.Role == channelperm.RoleCreator {
		// The creator holds every right; what it chooses is to stay
		// anonymous, and its title.
		if !keepRank {
			t.Rank = rank
		}
		if _, err = b.Tx().Exec("UPDATE channel_participants SET admin_rights = ?, `rank` = ? WHERE channel_id = ? AND user_id = ?",
			rights&mtproto.ADMIN_ANONYMOUS, t.Rank, channelId, userId); err != nil {
			return nil, err
		}
		return change, nil
	}
	if err = a.can(channelperm.AddAdmins, now); err != nil {
		return nil, err
	}
	joins := !t.in()
	switch {
	case t != nil && t.Role == channelperm.RoleCreator:
		return nil, mtproto.ErrUserCreator
	case t.kicked(now):
		return nil, mtproto.ErrUserKicked
	case joins:
		// Checked below, once the rights are known to be allowed.
	case t.Role == channelperm.RoleAdmin && !a.editableAdmin(t, b.UserId(), now):
		return nil, mtproto.ErrChatAdminRequired
	}
	if joins && rights == 0 {
		return nil, mtproto.ErrUserNotParticipant
	}
	if rights&^channelperm.Grantable(a.ch, a.p, int32(now)) != 0 {
		return nil, mtproto.ErrRightForbidden
	}
	if joins {
		if t, err = d.joinBot(b, ch, channelId, userId, change); err != nil {
			return nil, err
		}
	}
	wasAdmin := t.Role == channelperm.RoleAdmin
	if keepRank {
		rank = t.Rank
	}
	if rights == 0 {
		if !wasAdmin {
			return change, nil
		}
		if _, err = b.Tx().Exec("UPDATE channel_participants SET role = ?, admin_rights = 0, `rank` = '', promoted_by = 0 WHERE channel_id = ? AND user_id = ?",
			channelperm.RoleMember, channelId, userId); err != nil {
			return nil, err
		}
		if _, err = b.Tx().Exec("UPDATE channels SET admins_count = GREATEST(admins_count - 1, 0) WHERE id = ?", channelId); err != nil {
			return nil, err
		}
		next := *t
		next.Role, next.AdminRights, next.Rank, next.PromotedBy = channelperm.RoleMember, 0, "", 0
		if err = logParticipant(b, channelId, userId, t, &next, true); err != nil {
			return nil, err
		}
		return change, nil
	}
	promotedBy := t.PromotedBy
	if !wasAdmin {
		promotedBy = b.UserId()
	}
	if _, err = b.Tx().Exec("UPDATE channel_participants SET role = ?, admin_rights = ?, `rank` = ?, promoted_by = ?, banned_rights = 0, until_date = 0, kicked_by = 0 WHERE channel_id = ? AND user_id = ?",
		channelperm.RoleAdmin, rights, rank, promotedBy, channelId, userId); err != nil {
		return nil, err
	}
	if !wasAdmin {
		if _, err = b.Tx().Exec("UPDATE channels SET admins_count = admins_count + 1 WHERE id = ?", channelId); err != nil {
			return nil, err
		}
	}
	next := *t
	next.Role, next.AdminRights, next.Rank, next.PromotedBy = channelperm.RoleAdmin, rights, rank, promotedBy
	next.BannedRights, next.UntilDate = 0, 0
	if err = logParticipant(b, channelId, userId, t, &next, true); err != nil {
		return nil, err
	}
	return change, nil
}

// joinBot makes the bot userId a member of the channel, for EditAdmin to
// promote it: USER_NOT_PARTICIPANT for anyone else. The new row is returned.
func (d *Dao) joinBot(b *userbox.Box, ch *channelbox.Channel, channelId, userId int64, change *ChannelChange) (*targetRow, error) {
	var nochats []bool
	if err := b.Tx().QueryRowsPartial(&nochats, "SELECT bot_nochats FROM bots WHERE bot_id = ?", userId); err != nil {
		return nil, err
	}
	if len(nochats) == 0 {
		return nil, mtproto.ErrUserNotParticipant
	}
	c, err := memberChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	if !c.Broadcast && nochats[0] {
		return nil, mtproto.ErrBotGroupsBlocked
	}
	if err = checkChannelsLimit(b, userId); err != nil {
		return nil, err
	}
	if err = d.writeMembers(b, ch, c, channelId, []int64{userId}, change); err != nil {
		return nil, err
	}
	return &targetRow{Role: channelperm.RoleMember, State: channelperm.StateIn}, nil
}

// EditBanned applies the restrictions rights until until (as the client
// sent it, channelperm.UntilDate) to userId in the channel, by one with
// ban_users (CHAT_ADMIN_REQUIRED). With view_messages the user is kicked:
// out of the channel and kept out until the date, its dialog gone by an
// inbox event, a service message in a supergroup; the user need not be in
// the channel, as the clients block users from the list of the removed.
// With rights 0 a kicked user may join again and a restricted member is
// free. Other flags restrict a member (USER_NOT_PARTICIPANT for one out).
// The creator and admins one may not edit are USER_ADMIN_INVALID; an admin
// one may edit loses its rights. BANNED_RIGHTS_INVALID for unknown flags,
// PARTICIPANT_ID_INVALID for oneself.
func (d *Dao) EditBanned(b *userbox.Box, channelId, userId int64, rights, until int32) (*ChannelChange, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	now := b.Date()
	switch {
	case rights&^channelperm.BannedMask != 0:
		return nil, mtproto.ErrBannedRightsInvalid
	case userId == b.UserId():
		return nil, mtproto.ErrParticipantIdInvalid
	}
	if err = a.can(channelperm.BanUsers, now); err != nil {
		return nil, err
	}
	t, err := rightsTarget(b, channelId, userId)
	if err != nil {
		return nil, err
	}
	if t != nil && (t.Role == channelperm.RoleCreator || t.Role == channelperm.RoleAdmin && !a.editableAdmin(t, b.UserId(), now)) {
		return nil, mtproto.ErrUserAdminInvalid
	}
	change := &ChannelChange{ChannelId: channelId, Reload: true}
	wasAdmin := t != nil && t.Role == channelperm.RoleAdmin
	switch {
	case rights&mtproto.BANNED_VIEW_MESSAGES != 0:
		if err = d.kick(b, ch, a, change, channelId, userId, t, rights, channelperm.UntilDate(until, int32(now))); err != nil {
			return nil, err
		}
	case rights == 0:
		switch {
		case t == nil || t.State == channelperm.StateLeft:
			return change, nil
		case t.State == channelperm.StateKicked:
			_, err = b.Tx().Exec("UPDATE channel_participants SET state = ?, banned_rights = 0, until_date = 0, kicked_by = 0 WHERE channel_id = ? AND user_id = ?",
				channelperm.StateLeft, channelId, userId)
		default:
			_, err = b.Tx().Exec("UPDATE channel_participants SET banned_rights = 0, until_date = 0, kicked_by = 0 WHERE channel_id = ? AND user_id = ?",
				channelId, userId)
		}
		if err != nil {
			return nil, err
		}
	default:
		if !t.in() {
			return nil, mtproto.ErrUserNotParticipant
		}
		if _, err = b.Tx().Exec("UPDATE channel_participants SET role = ?, admin_rights = 0, `rank` = '', promoted_by = 0, banned_rights = ?, until_date = ?, kicked_by = ? WHERE channel_id = ? AND user_id = ?",
			channelperm.RoleMember, rights, channelperm.UntilDate(until, int32(now)), b.UserId(), channelId, userId); err != nil {
			return nil, err
		}
	}
	if wasAdmin && rights != 0 {
		if _, err = b.Tx().Exec("UPDATE channels SET admins_count = GREATEST(admins_count - 1, 0) WHERE id = ?", channelId); err != nil {
			return nil, err
		}
	}
	var next targetRow
	if t != nil {
		next = *t
	}
	switch {
	case rights&mtproto.BANNED_VIEW_MESSAGES != 0:
		next.State, next.BannedRights, next.UntilDate = channelperm.StateKicked, rights, channelperm.UntilDate(until, int32(now))
		next.Role, next.AdminRights, next.Rank = channelperm.RoleMember, 0, ""
	case rights == 0:
		if next.State == channelperm.StateKicked {
			next.State = channelperm.StateLeft
		}
		next.BannedRights, next.UntilDate = 0, 0
	default:
		next.BannedRights, next.UntilDate = rights, channelperm.UntilDate(until, int32(now))
		next.Role, next.AdminRights, next.Rank = channelperm.RoleMember, 0, ""
	}
	if err = logParticipant(b, channelId, userId, t, &next, false); err != nil {
		return nil, err
	}
	return change, nil
}

// logParticipant records a change of rights of userId in the recent actions:
// ParticipantToggleAdmin for admin rights, ParticipantToggleBan for a
// restriction, its row before (nil: none) and after, as the creator sees
// them.
func logParticipant(b *userbox.Box, channelId, userId int64, prev, next *targetRow, admin bool) error {
	now := int32(b.Date())
	viewer := &channel.ImmutableChannelParticipant{Role: channel.RoleCreator, State: channel.StateIn}
	view := func(t *targetRow) *mtproto.ChannelParticipant {
		p := &channel.ImmutableChannelParticipant{ChannelId: channelId, UserId: userId, State: channel.StateLeft}
		if t != nil {
			p.Role, p.State, p.AdminRights, p.Rank = t.Role, t.State, t.AdminRights, t.Rank
			p.BannedRights, p.UntilDate, p.PromotedBy = t.BannedRights, t.UntilDate, t.PromotedBy
		}
		return p.ToChannelParticipant(viewer, now)
	}
	a := &mtproto.ChannelAdminLogEventAction{PrevParticipant: view(prev), NewParticipant: view(next)}
	action := mtproto.MakeTLChannelAdminLogEventActionParticipantToggleBan(a).To_ChannelAdminLogEventAction()
	if admin {
		action = mtproto.MakeTLChannelAdminLogEventActionParticipantToggleAdmin(a).To_ChannelAdminLogEventAction()
	}
	return adminlog.Write(b, channelId, action, "")
}

// kick takes userId out of the channel and keeps it out until until: its
// row is kicked, a member leaves the count, a supergroup gets the service
// message of its removal, and the user's box drops the dialog by
// inbox.channelKickedToInbox.
func (d *Dao) kick(b *userbox.Box, ch *channelbox.Channel, a *actor, change *ChannelChange, channelId, userId int64, t *targetRow, rights, until int32) error {
	if t.in() {
		if !a.ch.Broadcast {
			action := mtproto.MakeTLMessageActionChatDeleteUser(&mtproto.MessageAction{UserId: userId}).To_MessageAction()
			var err error
			if change.Message, change.Pts, err = addServiceMessage(b, ch, channelId, false, action); err != nil {
				return err
			}
			if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", change.Message.GetId(), channelId); err != nil {
				return err
			}
		}
		if _, err := b.Tx().Exec("UPDATE channels SET participants_count = GREATEST(participants_count - 1, 0) WHERE id = ?", channelId); err != nil {
			return err
		}
	}
	if _, err := b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, state, banned_rights, until_date, kicked_by, date) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE role = VALUES(role), state = VALUES(state), admin_rights = 0, `rank` = '', promoted_by = 0,"+
		" banned_rights = VALUES(banned_rights), until_date = VALUES(until_date), kicked_by = VALUES(kicked_by)",
		channelId, userId, channelperm.RoleMember, channelperm.StateKicked, rights, until, b.UserId(), b.Date()); err != nil {
		return err
	}
	if err := d.dropRequests(b, channelId, userId); err != nil {
		return err
	}
	ev := &inbox.TLInboxChannelKickedToInbox{UserId: userId, FromId: b.UserId(), ChannelId: channelId}
	data, err := jsonx.Marshal(ev)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Inbox, Recipient: userId, Method: string(proto.MessageName(ev)), Data: data})
	return nil
}

// EditDefaultBannedRights sets the restrictions of every member of a
// supergroup, by one with ban_users (CHAT_ADMIN_REQUIRED). A broadcast
// channel has none (CHANNEL_INVALID), nor has a broadcast group, whose
// members only read and whose permissions the clients do not offer
// (tdesktop ChannelData::canEditPermissions, data_channel.cpp:801-805;
// Telegram's error for it is not verified); view_messages is not a default
// (BANNED_RIGHTS_INVALID); the rights it has are CHAT_NOT_MODIFIED
// (core.telegram.org/method/messages.editChatDefaultBannedRights). The
// channel's version grows, as the clients compare it.
func (d *Dao) EditDefaultBannedRights(b *userbox.Box, channelId int64, rights int32) (*ChannelChange, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	switch {
	case a.ch.Broadcast, a.ch.Gigagroup:
		return nil, mtproto.ErrChannelInvalid
	case rights&^channelperm.BannedMask != 0, rights&mtproto.BANNED_VIEW_MESSAGES != 0:
		return nil, mtproto.ErrBannedRightsInvalid
	}
	if err = a.can(channelperm.BanUsers, b.Date()); err != nil {
		return nil, err
	}
	if rights == a.ch.Default {
		return nil, mtproto.ErrChatNotModified
	}
	if _, err = b.Tx().Exec("UPDATE channels SET default_banned_rights = ?, version = version + 1 WHERE id = ?", rights, channelId); err != nil {
		return nil, err
	}
	if err = adminlog.Write(b, channelId, mtproto.MakeTLChannelAdminLogEventActionDefaultBannedRights(&mtproto.ChannelAdminLogEventAction{
		PrevBannedRights: mtproto.BannedRights(a.ch.Default).ToChatBannedRights(),
		NewBannedRights:  mtproto.BannedRights(rights).ToChatBannedRights(),
	}).To_ChannelAdminLogEventAction(), ""); err != nil {
		return nil, err
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}

// EditCreator hands the channel to userId, by its creator: the user, a
// member, becomes the creator, the old creator an admin with every right
// promoted by the new one, and the members load the channel again
// (core.telegram.org/method/channels.editCreator, messages.editChatCreator).
// The caller has checked the 2FA password. CHAT_ADMIN_REQUIRED for one
// not the creator, CHAT_NOT_MODIFIED for itself, USER_ID_INVALID for a
// user not in the channel; CHANNELS_ADMIN_PUBLIC_TOO_MUCH when the channel
// is public and the user owns PublicChannelsLimit public channels.
func (d *Dao) EditCreator(b *userbox.Box, channelId, userId int64) (*ChannelChange, error) {
	if _, err := openChannel(b, channelId); err != nil {
		return nil, err
	}
	a, err := rightsActor(b, channelId)
	if err != nil {
		return nil, err
	}
	switch {
	case a.p.Role != channelperm.RoleCreator:
		return nil, mtproto.ErrChatAdminRequired
	case userId == b.UserId():
		return nil, mtproto.ErrChatNotModified
	}
	t, err := rightsTarget(b, channelId, userId)
	if err != nil {
		return nil, err
	}
	if !t.in() {
		return nil, mtproto.ErrUserIdInvalid
	}
	name, err := channelUsername(b.Tx(), channelId)
	if err != nil {
		return nil, err
	}
	if name != "" {
		n, err := ownedPublic(b.Tx(), userId, channelId)
		if err != nil {
			return nil, err
		}
		if n >= premium.PublicChannelsLimit {
			return nil, mtproto.ErrChannelsAdminPublicTooMuch
		}
	}
	if _, err = b.Tx().Exec("UPDATE channel_participants SET role = ?, admin_rights = 0, promoted_by = 0, banned_rights = 0, until_date = 0, kicked_by = 0 WHERE channel_id = ? AND user_id = ?",
		channelperm.RoleCreator, channelId, userId); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE channel_participants SET role = ?, admin_rights = ?, promoted_by = ? WHERE channel_id = ? AND user_id = ?",
		channelperm.RoleAdmin, channelperm.CreatorRights, userId, channelId, b.UserId()); err != nil {
		return nil, err
	}
	// The creator counts among the admins (channel.createChannel): a member
	// made the creator adds one, an admin does not.
	admins := 0
	if t.Role != channelperm.RoleAdmin {
		admins = 1
	}
	if _, err = b.Tx().Exec("UPDATE channels SET creator_user_id = ?, admins_count = admins_count + ?, version = version + 1 WHERE id = ?",
		userId, admins, channelId); err != nil {
		return nil, err
	}
	return &ChannelChange{ChannelId: channelId, Reload: true}, nil
}

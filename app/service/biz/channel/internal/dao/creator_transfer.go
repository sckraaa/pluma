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
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The owner of a supergroup or channel who leaves it (task 078,
// core.telegram.org/api/channel, "Leaving groups/channels"): the clients
// ask who will own it next (channels.getFutureCreatorAfterLeave, or
// messages.getFutureChatCreatorAfterLeave with the channel), show that
// user, and leave. The owner's row stays the creator's, left; that user
// takes the channel CreatorTransferDelay later, unless the owner comes back
// first (Join), which keeps the channel the owner's. A supergroup is told
// by messageActionNewCreatorPending when the owner leaves and by
// messageActionChangeCreator when the transfer is done, both on behalf of
// the old owner, as the clients draw them ("{user} will become the new main
// admin in 7 days if {from} does not return", tdesktop lang.strings
// lng_action_new_creator_pending; Android strings.xml ActionChangeCreator
// "... ownership of the group ..."). A channel (broadcast) tells its
// subscribers nothing, as it says nothing of who leaves it.

const (
	// CreatorTransferDelay is how long after its owner left a channel goes
	// to its next owner: 7 days (core.telegram.org/api/channel; tdesktop
	// lng_leave_next_owner_box_about "in **1 week**").
	CreatorTransferDelay = 7 * 24 * time.Hour
	// transferPortion is the most transfers one pass of the worker does.
	transferPortion = 100
)

// futureCreatorQuery picks who owns a channel after its owner: a member in
// it now, not the owner (the first ?), neither restricted nor a bot nor a
// deleted account; the admins before the other members, and among them
// the one in the channel the longest (date is when the row was last
// written in: the join), then the lowest id. The admins come first as
// those the owner trusted with the channel: tdesktop lists them first
// where the owner picks another one (select_future_owner_box.cpp,
// ParticipantType::Admins, "promoted"/"joined" dates). A hidden member (a
// ghost, ADR 0048) is never one: the owner is shown to all.
const futureCreatorQuery = "SELECT p.user_id FROM channel_participants p JOIN users u ON u.id = p.user_id" +
	" WHERE p.channel_id = ? AND p.state = 0 AND p.hidden = 0 AND p.role <> 2 AND p.user_id <> ? AND u.deleted = 0 AND u.is_bot = 0" +
	" AND NOT (p.banned_rights <> 0 AND (p.until_date = 0 OR p.until_date = 2147483647 OR p.until_date > ?))" +
	" ORDER BY p.role = 1 DESC, p.`date`, p.user_id LIMIT 1"

// futureCreator is the user futureCreatorQuery picks in b's transaction
// for the channel without exclude, 0 for nobody.
func futureCreator(b *userbox.Box, channelId, exclude int64) (int64, error) {
	var ids []int64
	if err := b.Tx().QueryRowsPartial(&ids, futureCreatorQuery, channelId, exclude, b.Date()); err != nil || len(ids) == 0 {
		return 0, err
	}
	return ids[0], nil
}

// FutureCreator answers who owns the channel if selfId, its owner, leaves
// it now (channels.getFutureCreatorAfterLeave): the user Leave would name,
// 0 for nobody. CHANNEL_INVALID for a channel that does not exist,
// CHAT_ADMIN_REQUIRED for anyone but its owner in it (the method is the
// owners', core.telegram.org/method/channels.getFutureCreatorAfterLeave).
func (d *Dao) FutureCreator(ctx context.Context, selfId, channelId int64) (int64, error) {
	var rows []struct {
		Role  int32 `db:"role"`
		State int32 `db:"state"`
	}
	var exists []int64
	if err := d.Mysql.QueryRowsPartial(ctx, &exists, "SELECT id FROM channels WHERE id = ? AND deleted = 0", channelId); err != nil {
		return 0, err
	}
	if len(exists) == 0 {
		return 0, mtproto.ErrChannelInvalid
	}
	if err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT role, state FROM channel_participants WHERE channel_id = ? AND user_id = ?", channelId, selfId); err != nil {
		return 0, err
	}
	if len(rows) == 0 || rows[0].State != channel.StateIn || rows[0].Role != channel.RoleCreator {
		return 0, mtproto.ErrChatAdminRequired
	}
	var ids []int64
	if err := d.Mysql.QueryRowsPartial(ctx, &ids, futureCreatorQuery, channelId, selfId, time.Now().Unix()); err != nil || len(ids) == 0 {
		return 0, err
	}
	return ids[0], nil
}

// leaveAsCreator is the part of Leave of the owner of the channel, in b's
// transaction: the user futureCreator picks takes it CreatorTransferDelay
// later (a row of channel_creator_transfers), and a supergroup is told so.
// With nobody to take it the channel stays the owner's, who may come back.
func leaveAsCreator(b *userbox.Box, ch *channelbox.Channel, change *ChannelChange, broadcast bool) error {
	channelId := change.ChannelId
	next, err := futureCreator(b, channelId, b.UserId())
	if err != nil || next == 0 {
		return err
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_creator_transfers (channel_id, from_user_id, to_user_id, due_at, date2) VALUES (?, ?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE from_user_id = VALUES(from_user_id), to_user_id = VALUES(to_user_id), due_at = VALUES(due_at), date2 = VALUES(date2)",
		channelId, b.UserId(), next, b.Date()+int64(CreatorTransferDelay/time.Second), b.Date()); err != nil {
		return err
	}
	if broadcast {
		return nil
	}
	action := mtproto.MakeTLMessageActionNewCreatorPending(&mtproto.MessageAction{NewCreatorId: next}).To_MessageAction()
	m, pts, err := addServiceMessage(b, ch, channelId, false, action)
	if err != nil {
		return err
	}
	if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", m.GetId(), channelId); err != nil {
		return err
	}
	change.Updates = append(change.Updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{
		Message_MESSAGE: m,
		Pts_INT32:       pts,
		PtsCount:        1,
	}).To_Update())
	return nil
}

// cancelCreatorTransfer drops the transfer of the channel b's owner left,
// who comes back as its owner (Join).
func cancelCreatorTransfer(b *userbox.Box, channelId int64) error {
	_, err := b.Tx().Exec("DELETE FROM channel_creator_transfers WHERE channel_id = ? AND from_user_id = ?", channelId, b.UserId())
	return err
}

// ownerAddedBack is the part of writeMembers for the owner who left and is
// added back among users (channels.inviteToChannel): its row, which kept
// the creator's role, makes it the owner again, as its own Join does, so
// its transfer goes. It tells how many of users are such an owner (0 or
// 1), for the count of admins.
func ownerAddedBack(b *userbox.Box, channelId int64, users []int64) (int, error) {
	if len(users) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(users)+2)
	args = append(args, channelId, channel.RoleCreator)
	for _, id := range users {
		args = append(args, id)
	}
	var owners []int64
	if err := b.Tx().QueryRowsPartial(&owners,
		"SELECT user_id FROM channel_participants WHERE channel_id = ? AND role = ? AND user_id IN (?"+strings.Repeat(", ?", len(users)-1)+")",
		args...); err != nil || len(owners) == 0 {
		return 0, err
	}
	if _, err := b.Tx().Exec("DELETE FROM channel_creator_transfers WHERE channel_id = ? AND from_user_id = ?", channelId, owners[0]); err != nil {
		return 0, err
	}
	return len(owners), nil
}

type creatorTransfer struct {
	ChannelId  int64 `db:"channel_id"`
	FromUserId int64 `db:"from_user_id"`
	ToUserId   int64 `db:"to_user_id"`
}

// TransferDueCreators does the transfers due at now (transferCreator) and
// tells how many channels changed owner. One that fails does not stop the
// others: its row stays for the next pass, and the error is the first met.
func (d *Dao) TransferDueCreators(ctx context.Context, now int64) (int, error) {
	var due []creatorTransfer
	if err := d.Mysql.QueryRowsPartial(ctx, &due,
		"SELECT channel_id, from_user_id, to_user_id FROM channel_creator_transfers WHERE due_at <= ? ORDER BY due_at LIMIT ?",
		now, transferPortion); err != nil {
		return 0, err
	}
	var firstErr error
	n := 0
	for _, t := range due {
		done, err := d.transferCreator(ctx, t, now)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if done {
			n++
		}
	}
	return n, firstErr
}

// transferCreator hands the channel of t to its next owner, in that user's
// box transaction with the channel locked: the user t names if still fit
// (futureCreatorQuery), otherwise the one it picks now. The new owner gets
// the creator's row and the old owner's left row an ordinary one; a
// supergroup gets messageActionChangeCreator on behalf of the old owner,
// and the members load the channel again. A transfer that no longer
// applies goes: the channel deleted, the owner back or no longer its
// owner, nobody to take it. It tells whether the channel changed owner.
func (d *Dao) transferCreator(ctx context.Context, t creatorTransfer, now int64) (bool, error) {
	done := false
	var change *ChannelChange
	_, err := d.RunBox(ctx, t.ToUserId, func(b *userbox.Box) error {
		done = false
		ch, err := openChannel(b, t.ChannelId)
		if err != nil {
			return err
		}
		var rows []creatorTransfer
		if err = b.Tx().QueryRowsPartial(&rows,
			"SELECT channel_id, from_user_id, to_user_id FROM channel_creator_transfers WHERE channel_id = ? AND due_at <= ? FOR UPDATE",
			t.ChannelId, now); err != nil || len(rows) == 0 {
			return err
		}
		t = rows[0]
		if _, err = b.Tx().Exec("DELETE FROM channel_creator_transfers WHERE channel_id = ?", t.ChannelId); err != nil {
			return err
		}
		var channels []struct {
			Broadcast     bool  `db:"broadcast"`
			Deleted       bool  `db:"deleted"`
			CreatorUserId int64 `db:"creator_user_id"`
		}
		if err = b.Tx().QueryRowsPartial(&channels, "SELECT broadcast, deleted, creator_user_id FROM channels WHERE id = ?", t.ChannelId); err != nil {
			return err
		}
		if len(channels) == 0 || channels[0].Deleted || channels[0].CreatorUserId != t.FromUserId {
			return nil
		}
		var owner []memberRow
		if err = b.Tx().QueryRowsPartial(&owner,
			"SELECT role, state, banned_rights, until_date FROM channel_participants WHERE channel_id = ? AND user_id = ? FOR UPDATE",
			t.ChannelId, t.FromUserId); err != nil {
			return err
		}
		if len(owner) == 0 || owner[0].Role != channel.RoleCreator || owner[0].State == channel.StateIn {
			return nil
		}
		next := t.ToUserId
		var fit []int64
		if err = b.Tx().QueryRowsPartial(&fit,
			"SELECT p.user_id FROM channel_participants p JOIN users u ON u.id = p.user_id"+
				" WHERE p.channel_id = ? AND p.user_id = ? AND p.state = 0 AND p.hidden = 0 AND u.deleted = 0 AND u.is_bot = 0"+
				" AND NOT (p.banned_rights <> 0 AND (p.until_date = 0 OR p.until_date = 2147483647 OR p.until_date > ?))",
			t.ChannelId, t.ToUserId, b.Date()); err != nil {
			return err
		}
		if len(fit) == 0 {
			if next, err = futureCreator(b, t.ChannelId, t.FromUserId); err != nil || next == 0 {
				return err
			}
		}
		var target []memberRow
		if err = b.Tx().QueryRowsPartial(&target,
			"SELECT role, state, banned_rights, until_date FROM channel_participants WHERE channel_id = ? AND user_id = ? FOR UPDATE",
			t.ChannelId, next); err != nil || len(target) == 0 {
			return err
		}
		if _, err = b.Tx().Exec("UPDATE channel_participants SET role = ?, admin_rights = 0, promoted_by = 0, banned_rights = 0, until_date = 0, kicked_by = 0 WHERE channel_id = ? AND user_id = ?",
			channel.RoleCreator, t.ChannelId, next); err != nil {
			return err
		}
		if _, err = b.Tx().Exec("UPDATE channel_participants SET role = ?, admin_rights = 0, `rank` = '' WHERE channel_id = ? AND user_id = ?",
			channel.RoleMember, t.ChannelId, t.FromUserId); err != nil {
			return err
		}
		// The creator counts among the admins: a member made the creator
		// adds one; the old owner left the count when it left (Leave).
		admins := 0
		if target[0].Role != channel.RoleAdmin {
			admins = 1
		}
		if _, err = b.Tx().Exec("UPDATE channels SET creator_user_id = ?, admins_count = admins_count + ?, version = version + 1 WHERE id = ?",
			next, admins, t.ChannelId); err != nil {
			return err
		}
		change = &ChannelChange{ChannelId: t.ChannelId, Reload: true}
		if !channels[0].Broadcast {
			action := mtproto.MakeTLMessageActionChangeCreator(&mtproto.MessageAction{NewCreatorId: next}).To_MessageAction()
			if change.Message, change.Pts, err = addServiceMessageFrom(b, ch, t.ChannelId, t.FromUserId, false, action); err != nil {
				return err
			}
			if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", change.Message.GetId(), t.ChannelId); err != nil {
				return err
			}
		}
		done = true
		return d.EmitChannelPush(b, 0, change)
	})
	return done && err == nil, err
}

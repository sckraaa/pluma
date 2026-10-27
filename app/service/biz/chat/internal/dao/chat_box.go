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
	"database/sql"
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// A change of a basic group runs in the box transaction of the user who
// makes it (ADR 0021): it locks the chat's row first (LockChat), so the
// changes of one chat follow one another, and reads the participants under
// that lock. The lock order is the operator's user_state (userbox.Run),
// then the chat: no transaction takes a user_state lock after a chat's.

const (
	chatColumns        = "id, creator_user_id, access_hash, participant_count, title, about, photo_id, default_banned_rights, migrated_to_id, migrated_to_access_hash, noforwards, available_reactions_type, available_reactions, deactivated, ttl_period, version, `date`"
	participantColumns = "id, chat_id, user_id, participant_type, link, usage2, inviter_user_id, invited_at, kicked_at, left_at, is_bot, state, date2, rank2"

	lockChatQuery      = "SELECT " + chatColumns + " FROM chats WHERE id = ? FOR UPDATE"
	participantsQuery  = "SELECT " + participantColumns + " FROM chat_participants WHERE chat_id = ? ORDER BY id"
	lastChatOfQuery    = "SELECT " + chatColumns + " FROM chats WHERE creator_user_id = ? ORDER BY `date` DESC LIMIT 1"
	inviteByLinkQuery  = "SELECT id, chat_id, admin_id, link, permanent, revoked, request_needed, start_date, expire_date, usage_limit, usage2, requested, title, date2 FROM chat_invites WHERE link = ?"
	linkUsageQuery     = "SELECT COUNT(*) FROM chat_invite_participants WHERE link = ? AND requested = 0"
	pendingQuery       = "SELECT COUNT(*) FROM chat_invite_participants WHERE chat_id = ? AND user_id = ? AND requested = 1"
	requestersQuery    = "SELECT user_id FROM chat_invite_participants WHERE chat_id = ? AND requested = 1 GROUP BY user_id ORDER BY MAX(date2) DESC, MAX(id) DESC"
	upsertInviteeQuery = "INSERT INTO chat_invite_participants (chat_id, link, user_id, requested, approved_by, date2) VALUES (?, ?, ?, ?, 0, ?) AS new" +
		" ON DUPLICATE KEY UPDATE chat_id = new.chat_id, requested = new.requested, approved_by = 0, date2 = new.date2"
	deleteChatQuery = "UPDATE chats SET participant_count = 0, deactivated = 1, version = version + 1 WHERE id = ?"
)

// RecentRequestersMax is how many of the latest requesters to join a chat
// updatePendingJoinRequests and chatFull name: as many as the clients show
// (Telegram-Android, ChatActivityMemberRequestsDelegate).
const RecentRequestersMax = 3

// LockedChat is a basic group as a box transaction reads it: its row
// locked, its participants of every state. The chat has no photo: the
// photo is a service call away, which a box transaction does not make.
type LockedChat struct {
	*mtproto.MutableChat
	// PhotoId is the chat's photo_id.
	PhotoId int64
}

// LockChat locks chatId's row in b and reads the chat with its participants
// (box transactions read committed: every participant written under an
// earlier lock of the chat is seen). A chat that does not exist is
// mtproto.ErrChatIdInvalid.
func (d *Dao) LockChat(b *userbox.Box, chatId int64) (*LockedChat, error) {
	var do dataobject.ChatsDO
	if err := b.Tx().QueryRowPartial(&do, lockChatQuery, chatId); errors.Is(err, sql.ErrNoRows) {
		return nil, mtproto.ErrChatIdInvalid
	} else if err != nil {
		return nil, err
	}
	var rows []dataobject.ChatParticipantsDO
	if err := b.Tx().QueryRowsPartial(&rows, participantsQuery, chatId); err != nil {
		return nil, err
	}
	c := &LockedChat{
		MutableChat: mtproto.MakeTLMutableChat(&mtproto.MutableChat{
			Chat:             d.MakeImmutableChatByDO(&do),
			ChatParticipants: make([]*mtproto.ImmutableChatParticipant, 0, len(rows)),
		}).To_MutableChat(),
		PhotoId: do.PhotoId,
	}
	for i := range rows {
		c.ChatParticipants = append(c.ChatParticipants, d.MakeImmutableChatParticipant(&rows[i]))
	}
	return c, nil
}

// LastChatOfTx returns the chat creatorId created last, nil if none.
func (d *Dao) LastChatOfTx(b *userbox.Box, creatorId int64) (*dataobject.ChatsDO, error) {
	var do dataobject.ChatsDO
	if err := b.Tx().QueryRowPartial(&do, lastChatOfQuery, creatorId); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &do, nil
}

// InviteByLinkTx returns the invite of link, nil if there is none.
func (d *Dao) InviteByLinkTx(b *userbox.Box, link string) (*dataobject.ChatInvitesDO, error) {
	var do dataobject.ChatInvitesDO
	if err := b.Tx().QueryRowPartial(&do, inviteByLinkQuery, link); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &do, nil
}

// LinkUsageTx counts the users who joined by link.
func (d *Dao) LinkUsageTx(b *userbox.Box, link string) (int32, error) {
	var n int32
	err := b.Tx().QueryRow(&n, linkUsageQuery, link)
	return n, err
}

// PendingRequestTx tells whether userId asked to join chatId and is not
// answered yet.
func (d *Dao) PendingRequestTx(b *userbox.Box, chatId, userId int64) (bool, error) {
	var n int
	err := b.Tx().QueryRow(&n, pendingQuery, chatId, userId)
	return n > 0, err
}

// RecentRequestersTx returns the requests to join chatId not answered yet:
// their number and the latest requesters, newest first.
func (d *Dao) RecentRequestersTx(b *userbox.Box, chatId int64) (*chat.RecentChatInviteRequesters, error) {
	var ids []int64
	if err := b.Tx().QueryRowsPartial(&ids, requestersQuery, chatId); err != nil {
		return nil, err
	}
	return recentRequesters(ids), nil
}

// RecentRequesters is RecentRequestersTx outside a transaction.
func (d *Dao) RecentRequesters(ctx context.Context, chatId int64) (*chat.RecentChatInviteRequesters, error) {
	var ids []int64
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &ids, requestersQuery, chatId); err != nil {
		return nil, err
	}
	return recentRequesters(ids), nil
}

// LatestRequesters returns up to n of the requesters to join chatId not
// answered yet, newest first.
func (d *Dao) LatestRequesters(ctx context.Context, chatId int64, n int) ([]int64, error) {
	var ids []int64
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &ids, requestersQuery+" LIMIT ?", chatId, n); err != nil {
		return nil, err
	}
	return ids, nil
}

func recentRequesters(ids []int64) *chat.RecentChatInviteRequesters {
	recent := ids[:min(len(ids), RecentRequestersMax)]
	if recent == nil {
		recent = []int64{}
	}
	return chat.MakeTLRecentChatInviteRequesters(&chat.RecentChatInviteRequesters{
		RequestsPending:  int32(len(ids)),
		RecentRequesters: recent,
	}).To_RecentChatInviteRequesters()
}

// PutInviteeTx records that userId joined chatId by link, or asked to
// (requested): the user's row of the link is written again if it is there.
func (d *Dao) PutInviteeTx(b *userbox.Box, chatId int64, link string, userId int64, requested bool, date int64) error {
	_, err := b.Tx().Exec(upsertInviteeQuery, chatId, link, userId, requested, date)
	return err
}

// DeleteChatTx deactivates chatId with no participants, its version bumped
// once.
func (d *Dao) DeleteChatTx(b *userbox.Box, chatId int64) error {
	_, err := b.Tx().Exec(deleteChatQuery, chatId)
	return err
}

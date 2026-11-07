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
	"math/rand/v2"

	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The move of a basic group to a supergroup (task 047, ADR 0034, p. 10):
// one box transaction of the group's creator locks the group, makes the
// supergroup with the members and its message 1 channelMigrateFrom,
// deactivates the group with migrated_to and writes chatMigrateTo into the
// creator's box. The other members get their chatMigrateTo and the
// supergroup's dialog by inbox events of the same transaction.

// ChannelsLimit is the most channels and supergroups one user is in
// (channels_limit_default, as biz channel counts it).
const ChannelsLimit = 500

// MigrateAdminRights are the admin rights of an admin of a basic group in
// the supergroup it becomes: what a basic group's admin may do, as tdesktop
// (data_chat.cpp:52-63) and Android (ChatRightsEditActivity.java:1089-1091)
// give them; Telegram's own set is not verified.
const MigrateAdminRights = mtproto.ADMIN_OTHER | mtproto.ADMIN_CHANGE_INFO | mtproto.ADMIN_DELETE_MESSAGES |
	mtproto.ADMIN_BAN_USERS | mtproto.ADMIN_INVITE_USERS | mtproto.ADMIN_PIN_MESSAGES | mtproto.ADMIN_MANAGE_CALL |
	mtproto.ADMIN_MANAGE_RANKS

// Migrated is what a move wrote, or, for a group moved already, the
// supergroup it moved to (Again).
type Migrated struct {
	ChannelId  int64
	AccessHash int64
	Again      bool
	// For a move done now: message 1 of the supergroup with its pts, and
	// the creator's chatMigrateTo.
	First    *mtproto.Message
	FirstPts int32
	Service  []*mtproto.MessageBox
}

type migrateChatRow struct {
	Creator     int64 `db:"creator_user_id"`
	MigratedTo  int64 `db:"migrated_to_id"`
	MigratedAh  int64 `db:"migrated_to_access_hash"`
	Deactivated bool  `db:"deactivated"`
}

// MigrateChat moves the basic group chat, read before the transaction, to
// a new supergroup by b's owner (dialogMessageId: the id every member's
// copy of chatMigrateTo shares), its creator (CHAT_ADMIN_REQUIRED for
// another). A group moved already gives its supergroup again, so that a
// repeat after a failure makes no second one; a deleted group is
// CHAT_ID_INVALID; CHANNELS_TOO_MUCH when the creator is in ChannelsLimit
// channels.
//
// The supergroup takes the group's title, description, photo and default
// restrictions; the creator is its creator, the admins its admins with
// MigrateAdminRights, the others members. migrated_from_max_id is the
// newest message of the group in any member's box: the ids of a basic
// group are each member's own, and every member's history of the group
// ends below it. The group's invite links become the supergroup's.
func (d *Dao) MigrateChat(b *userbox.Box, chat *mtproto.MutableChat, dialogMessageId int64) (*Migrated, error) {
	chatId := chat.Id()
	var rows []migrateChatRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT creator_user_id, migrated_to_id, migrated_to_access_hash, deactivated FROM chats WHERE id = ? FOR UPDATE", chatId); err != nil {
		return nil, err
	}
	switch {
	case len(rows) == 0:
		return nil, mtproto.ErrChatIdInvalid
	case rows[0].Creator != b.UserId():
		return nil, mtproto.ErrChatAdminRequired
	case rows[0].MigratedTo != 0:
		return &Migrated{ChannelId: rows[0].MigratedTo, AccessHash: rows[0].MigratedAh, Again: true}, nil
	case rows[0].Deactivated:
		return nil, mtproto.ErrChatIdInvalid
	}
	var joined []int64
	if err := b.Tx().QueryRowsPartial(&joined, "SELECT channel_id FROM channel_participants WHERE user_id = ? AND state = 0 LIMIT ?",
		b.UserId(), ChannelsLimit); err != nil {
		return nil, err
	}
	if len(joined) >= ChannelsLimit {
		return nil, mtproto.ErrChannelsTooMuch
	}
	var maxIds []int32
	if err := b.Tx().QueryRowsPartial(&maxIds,
		"SELECT COALESCE(MAX(top_message), 0) FROM dialogs WHERE peer_type = ? AND peer_id = ?", mtproto.PEER_CHAT, chatId); err != nil {
		return nil, err
	}

	ch, err := channelbox.Create(b, channelbox.NewChannel{
		AccessHash:    rand.Int64(), //nolint:gosec // an access hash checks a client's reference, it is no secret of crypto
		CreatorUserId: b.UserId(),
		Title:         chat.Title(),
		About:         chat.About(),
		Megagroup:     true,
		Date:          b.Date(),
	})
	if err != nil {
		return nil, err
	}
	out := &Migrated{ChannelId: ch.Id()}
	if err = b.Tx().QueryRow(&out.AccessHash, "SELECT access_hash FROM channels WHERE id = ?", out.ChannelId); err != nil {
		return nil, err
	}

	// The members, as the group has them now.
	var members []*mtproto.ImmutableChatParticipant
	chat.Walk(func(_ int64, p *mtproto.ImmutableChatParticipant) error {
		if p.IsChatMemberStateNormal() {
			members = append(members, p)
		}
		return nil
	})
	var count, admins int32
	for _, p := range members {
		role, rights := channelperm.RoleMember, int32(0)
		switch {
		case p.UserId == b.UserId():
			role = channelperm.RoleCreator
		case p.IsChatMemberAdmin() || p.IsChatMemberCreator():
			role, rights = channelperm.RoleAdmin, MigrateAdminRights
		}
		if role != channelperm.RoleMember {
			admins++
		}
		count++
		if _, err = b.Tx().Exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `rank`, inviter_user_id, promoted_by, `date`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			out.ChannelId, p.UserId, role, rights, p.GetRank(), p.GetInviterUserId(), promotedBy(role, b.UserId()), b.Date()); err != nil {
			return nil, err
		}
	}

	// Message 1: the supergroup comes from the group.
	out.First = mtproto.MakeTLMessageService(&mtproto.Message{
		Id:     ch.NextMessageId(),
		FromId: mtproto.MakePeerUser(b.UserId()),
		PeerId: mtproto.MakePeerChannel(out.ChannelId),
		Date:   int32(b.Date()),
		Action: mtproto.MakeTLMessageActionChannelMigrateFrom(&mtproto.MessageAction{Title_STRING: chat.Title(), ChatId: chatId}).To_MessageAction(),
	}).To_Message()
	ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: out.First}).To_Update(), 1)
	out.FirstPts = ch.Pts()
	data, err := jsonx.MarshalToString(out.First)
	if err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_filter_type, message_data, date2) VALUES (?, ?, ?, ?, ?, ?, ?)",
		out.ChannelId, out.First.Id, b.UserId(), out.FirstPts, mtproto.GetMediaType(out.First), data, b.Date()); err != nil {
		return nil, err
	}
	var banned int32
	if r := chat.DefaultBannedRights(); r != nil {
		banned = int32(uint32(r.ToBannedRights())) &^ mtproto.BANNED_VIEW_MESSAGES
	}
	if _, err = b.Tx().Exec("UPDATE channels SET photo_id = ?, default_banned_rights = ?, migrated_from_chat_id = ?, migrated_from_max_id = ?,"+
		" participants_count = ?, admins_count = ?, top_message = ? WHERE id = ?",
		chat.Photo().GetId(), banned, chatId, maxIds[0], count, admins, out.First.Id, out.ChannelId); err != nil {
		return nil, err
	}
	if err = movedTo(b, out.ChannelId, out.First.Id); err != nil {
		return nil, err
	}

	// The creator's chatMigrateTo, while the group is still active.
	action := mtproto.MakeTLMessageActionChatMigrateTo(&mtproto.MessageAction{ChannelId: out.ChannelId}).To_MessageAction()
	if out.Service, err = d.SendOutgoing(b, mtproto.PEER_CHAT, chatId, []Outgoing{{
		RandomId:        rand.Int64(), //nolint:gosec // random_id of a service message, not a secret
		DialogMessageId: dialogMessageId,
		Message:         chat.MakeMessageService(b.UserId(), action),
	}}); err != nil {
		return nil, err
	}

	// The group: moved, deactivated; its members moved with it.
	if _, err = b.Tx().Exec("UPDATE chats SET migrated_to_id = ?, migrated_to_access_hash = ?, deactivated = 1, version = version + 1 WHERE id = ?",
		out.ChannelId, out.AccessHash, chatId); err != nil {
		return nil, err
	}
	if _, err = b.Tx().Exec("UPDATE chat_participants SET state = ? WHERE chat_id = ? AND state = ?",
		mtproto.ChatMemberStateMigrated, chatId, mtproto.ChatMemberStateNormal); err != nil {
		return nil, err
	}
	// Its links lead to the supergroup; one permanent link it has anyway.
	if _, err = b.Tx().Exec("UPDATE chat_invites SET chat_id = ?, migrated_to_id = ? WHERE chat_id = ?", out.ChannelId, out.ChannelId, chatId); err != nil {
		return nil, err
	}
	var permanent []int64
	if err = b.Tx().QueryRowsPartial(&permanent, "SELECT id FROM chat_invites WHERE chat_id = ? AND admin_id = ? AND permanent = 1 AND revoked = 0 LIMIT 1",
		out.ChannelId, b.UserId()); err != nil {
		return nil, err
	}
	if len(permanent) == 0 {
		if _, err = b.Tx().Exec("INSERT INTO chat_invites (chat_id, admin_id, link, permanent, date2) VALUES (?, ?, ?, 1, ?)",
			out.ChannelId, b.UserId(), chatpb.GenChannelInviteHash(), b.Date()); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func promotedBy(role int32, creator int64) int64 {
	if role == channelperm.RoleAdmin {
		return creator
	}
	return 0
}

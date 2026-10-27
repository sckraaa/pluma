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

package core

import (
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// chatTx is a change of a basic group in the box transaction of the user
// who makes it, the operator (ADR 0021): the chat as the transaction locked
// and read it, which the change keeps up to date with what it writes. The
// writes go through b.Tx(), the version of the chat grows with each change
// of its participants or of what a client keeps of it, and the cache keys
// of what changed go after the commit.
type chatTx struct {
	c *ChatCore
	*dao.LockedChat
	now int64
}

// changeChat runs fn in a box transaction of operatorId over chatId, locked
// and read first, and returns the chat as the transaction left it. A
// business error of fn rolls the transaction back and is returned; fn that
// changes nothing writes nothing. fn runs again if the transaction is
// retried.
func (c *ChatCore) changeChat(operatorId, chatId int64, fn func(b *userbox.Box, t *chatTx) error) (*mtproto.MutableChat, error) {
	// The chat as the cache has it: whether it exists, and its photo,
	// which the transaction cannot ask the media service for.
	before, err := c.svcCtx.Dao.GetExcludeParticipantsMutableChat(c.ctx, chatId)
	if err != nil {
		return nil, err
	}
	var t *chatTx
	_, err = c.svcCtx.Dao.RunBox(c.ctx, operatorId, []string{c.svcCtx.Dao.GetChatCacheKey(chatId)}, func(b *userbox.Box) error {
		t = nil
		locked, err := c.svcCtx.Dao.LockChat(b, chatId)
		if err != nil {
			return err
		}
		t = &chatTx{c: c, LockedChat: locked, now: b.Date()}
		t.Chat.Photo = before.Chat.GetPhoto()
		return fn(b, t)
	})
	if err != nil {
		return nil, err
	}
	return t.result(), nil
}

// result is the chat the transaction committed, with its photo: the one
// read before the transaction (or given by the change) while it is still
// the chat's, otherwise the media service's.
func (t *chatTx) result() *mtproto.MutableChat {
	d := t.c.svcCtx.Dao
	switch {
	case t.PhotoId == 0:
		t.Chat.Photo = mtproto.MakeTLPhotoEmpty(nil).To_Photo()
	case t.PhotoId != t.Chat.Photo.GetId():
		p, err := d.MediaClient.MediaGetPhoto(t.c.ctx, &media.TLMediaGetPhoto{PhotoId: t.PhotoId})
		if err != nil {
			t.c.Logger.Errorf("chat %d: photo %d: %v", t.Chat.Id, t.PhotoId, err)
			p = mtproto.MakeTLPhotoEmpty(nil).To_Photo()
		}
		t.Chat.Photo = p
	}
	if d.Plugin != nil {
		t.Chat.CallActive, t.Chat.CallNotEmpty = d.Plugin.GetChatCallActiveAndNotEmpty(t.c.ctx, 0, t.Chat.Id)
		t.Chat.Call = d.Plugin.GetChatGroupCall(t.c.ctx, 0, t.Chat.Id)
	}
	return t.MutableChat
}

// participant returns the participant userId of any state, nil if the
// user was never in the chat.
func (t *chatTx) participant(userId int64) *mtproto.ImmutableChatParticipant {
	p, _ := t.GetImmutableChatParticipant(userId)
	return p
}

// member returns the participant userId if the user is in the chat now.
func (t *chatTx) member(userId int64) *mtproto.ImmutableChatParticipant {
	if p := t.participant(userId); p != nil && p.IsChatMemberStateNormal() {
		return p
	}
	return nil
}

// members returns the users in the chat now, by ascending id.
func (t *chatTx) members() []int64 {
	ids := t.ParticipantIdList()
	slices.Sort(ids)
	return ids
}

// changed marks the participants of userIds as changed: their cache keys
// go after the commit.
func (t *chatTx) changed(b *userbox.Box, userIds ...int64) {
	for _, id := range userIds {
		b.InvalidateCache(t.c.svcCtx.Dao.GetChatParticipantCacheKey(t.Chat.Id, id))
	}
}

// writeCount writes the number of members after a change of the
// participants, which bumps the version.
func (t *chatTx) writeCount(b *userbox.Box) error {
	n := int32(len(t.members()))
	if _, err := t.c.svcCtx.Dao.ChatsDAO.UpdateParticipantCountTx(b.Tx(), n, t.Chat.Id); err != nil {
		return err
	}
	t.Chat.ParticipantsCount = n
	t.Chat.Version++
	return nil
}

// bumpVersion bumps the version of the chat after a change of the
// participants that keeps their number.
func (t *chatTx) bumpVersion(b *userbox.Box) error {
	if _, err := t.c.svcCtx.Dao.ChatsDAO.UpdateVersionTx(b.Tx(), t.Chat.Id); err != nil {
		return err
	}
	t.Chat.Version++
	return nil
}

// maxChatMembers is the size limit of a basic group (chat_size_max).
const maxChatMembers = 200

// open tells whether the chat takes members: not migrated to a supergroup,
// not deleted.
func (t *chatTx) open() error {
	switch {
	case t.Chat.GetMigratedTo() != nil:
		return mtproto.ErrMigratedToChannel
	case t.Chat.Deactivated:
		return mtproto.ErrChatIdInvalid
	}
	return nil
}

// addMember adds userId to the chat, invited by inviterId, or brings back a
// user who left or was removed. With checkInviter the inviter must be in
// the chat and may invite; without it the caller let the user in (an
// invite link, a request an admin approved).
func (t *chatTx) addMember(b *userbox.Box, inviterId, userId int64, isBot, checkInviter bool) error {
	if err := t.open(); err != nil {
		return err
	}
	if checkInviter {
		me := t.participant(inviterId)
		if me == nil || !me.IsChatMemberStateNormal() && !me.IsChatMemberCreator() {
			return mtproto.ErrInputUserDeactivated
		}
		// 400	CHAT_ADMIN_REQUIRED	You must be an admin in this chat to do this
		if !me.CanInviteUsers() && !t.Chat.DefaultBannedRights.CanInviteUsers(int32(t.now)) {
			return mtproto.ErrChatAdminRequired
		}
	}
	switch {
	case t.member(userId) != nil:
		return mtproto.ErrUserAlreadyParticipant
	case len(t.members()) >= maxChatMembers:
		return mtproto.ErrUsersTooMuch
	}

	participantType := int32(mtproto.ChatMemberNormal)
	if t.Chat.Creator == userId {
		participantType = mtproto.ChatMemberCreator
	}
	d := t.c.svcCtx.Dao
	if p := t.participant(userId); p == nil {
		do := &dataobject.ChatParticipantsDO{
			ChatId:          t.Chat.Id,
			UserId:          userId,
			ParticipantType: participantType,
			InviterUserId:   inviterId,
			InvitedAt:       t.now,
			Date2:           t.now,
			IsBot:           isBot,
		}
		id, _, err := d.ChatParticipantsDAO.InsertTx(b.Tx(), do)
		if err != nil {
			return err
		}
		do.Id = id
		t.ChatParticipants = append(t.ChatParticipants, d.MakeImmutableChatParticipant(do))
	} else {
		if _, err := d.ChatParticipantsDAO.UpdateTx(b.Tx(), participantType, inviterId, t.now, isBot, p.Id); err != nil {
			return err
		}
		p.ParticipantType = participantType
		p.AdminRights = nil
		p.InviterUserId = inviterId
		p.InvitedAt = t.now
		p.State = mtproto.ChatMemberStateNormal
		p.KickedAt = 0
		p.LeftAt = 0
		p.IsBot = isBot
	}
	t.changed(b, userId)
	return t.writeCount(b)
}

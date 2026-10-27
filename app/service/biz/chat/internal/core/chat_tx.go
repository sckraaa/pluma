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
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/updates/seqpeers"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// chatTx is a change of a basic group in the box transaction of the user
// who makes it, the operator (ADR 0021): the chat as the transaction locked
// and read it, which the change keeps up to date with what it writes. The
// writes go through b.Tx(), the version of the chat grows with each change
// of its participants or of what a client keeps of it, and the cache keys
// of what changed go after the commit. The updates with seq the change gives
// the members are events of the same transaction (tell).
type chatTx struct {
	c *ChatCore
	*dao.LockedChat
	now int64
	// users are the users of the operator's own updates, as the operator
	// sees them, looked up before the transaction (changeMembers).
	users *userpb.Vector_ImmutableUser
}

// changeChat runs fn in a box transaction of operatorId over chatId, locked
// and read first, and returns the chat as the transaction left it. A
// business error of fn rolls the transaction back and is returned; fn that
// changes nothing writes nothing. fn runs again if the transaction is
// retried.
func (c *ChatCore) changeChat(operatorId, chatId int64, fn func(b *userbox.Box, t *chatTx) error) (*mtproto.MutableChat, error) {
	return c.change(operatorId, chatId, false, nil, fn)
}

// changeMembers is changeChat for a change whose updates to the operator
// name users: the participants of the chat, who invited them and extra are
// looked up as the operator sees them before the transaction, which calls
// no service. A user who joins between the look-up and the lock goes
// without a user in the operator's push; the push of the service message
// and getDifference carry it.
func (c *ChatCore) changeMembers(operatorId, chatId int64, extra []int64, fn func(b *userbox.Box, t *chatTx) error) (*mtproto.MutableChat, error) {
	return c.change(operatorId, chatId, true, extra, fn)
}

func (c *ChatCore) change(operatorId, chatId int64, withUsers bool, extra []int64, fn func(b *userbox.Box, t *chatTx) error) (*mtproto.MutableChat, error) {
	d := c.svcCtx.Dao
	// The chat as the cache has it: whether it exists, its photo, which
	// the transaction cannot ask the media service for, and with users
	// its participants.
	var (
		before *mtproto.MutableChat
		users  *userpb.Vector_ImmutableUser
		err    error
	)
	if !withUsers {
		before, err = d.GetExcludeParticipantsMutableChat(c.ctx, chatId)
	} else if before, err = d.GetMutableChat(c.ctx, chatId); err == nil {
		ids := []int64{operatorId}
		add := func(id int64) {
			if id != 0 && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		for _, p := range before.GetChatParticipants() {
			add(p.UserId)
			add(p.InviterUserId)
		}
		for _, id := range extra {
			add(id)
		}
		users, err = d.Users.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids})
	}
	if err != nil {
		return nil, err
	}

	var t *chatTx
	_, err = d.RunBox(c.ctx, operatorId, []string{d.GetChatCacheKey(chatId)}, func(b *userbox.Box) error {
		t = nil
		locked, err := d.LockChat(b, chatId)
		if err != nil {
			return err
		}
		t = &chatTx{c: c, LockedChat: locked, now: b.Date(), users: users}
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

// admins returns the members who answer the requests to join: the
// creator and the admins, by ascending id.
func (t *chatTx) admins() []int64 {
	var ids []int64
	t.Walk(func(userId int64, p *mtproto.ImmutableChatParticipant) error {
		if p.IsChatMemberStateNormal() && p.CanInviteUsers() {
			ids = append(ids, userId)
		}
		return nil
	})
	slices.Sort(ids)
	return ids
}

// tell gives updates with seq to the users of to: the operator's own
// account journals each one in the transaction and pushes it to its devices
// with the users and the chat it names, as the operator sees them
// (EmitSeqUpdate); every other user gets one inbox event with all of them,
// whose users and chats msg builds as that user sees them when it applies
// it (EmitSeqInbox).
func (t *chatTx) tell(b *userbox.Box, to []int64, updates ...*mtproto.Update) error {
	d := t.c.svcCtx.Dao
	for _, id := range to {
		if id != b.UserId() {
			if err := d.EmitSeqInbox(b, id, updates...); err != nil {
				return err
			}
			continue
		}
		for _, u := range updates {
			h := mtproto.NewIDListHelper()
			seqpeers.Pick(h, id, u)
			var chats []*mtproto.Chat
			if slices.Contains(h.ChatIdList, t.Chat.Id) {
				chats = []*mtproto.Chat{t.ToUnsafeChat(id)}
			}
			if err := d.EmitSeqUpdate(b, u, t.users.GetUserListByIdList(id, h.UserIdList...), chats); err != nil {
				return err
			}
		}
	}
	return nil
}

// tellMembers gives updates with seq to every member of the chat now.
func (t *chatTx) tellMembers(b *userbox.Box, updates ...*mtproto.Update) error {
	return t.tell(b, t.members(), updates...)
}

// participantsUpdate is the whole list of the members with the version of
// the chat. Each change of the participants sends the list, not a delta:
// every client replaces its list from it and checks only that the version
// grows, while the deltas (updateChatParticipantAdd and the others) need
// the version to follow on by one, which a title or a photo breaks.
func (t *chatTx) participantsUpdate() *mtproto.Update {
	return mtproto.MakeTLUpdateChatParticipants(&mtproto.Update{
		Participants_CHATPARTICIPANTS: t.ToChatParticipants(0),
	}).To_Update()
}

// forbiddenUpdate tells a user removed from the chat, or who left it, that
// the members are no longer theirs to see.
func (t *chatTx) forbiddenUpdate() *mtproto.Update {
	return mtproto.MakeTLUpdateChatParticipants(&mtproto.Update{
		Participants_CHATPARTICIPANTS: mtproto.MakeTLChatParticipantsForbidden(&mtproto.ChatParticipants{
			ChatId: t.Chat.Id,
		}).To_ChatParticipants(),
	}).To_Update()
}

// chatUpdate tells the members to read the chat again.
func (t *chatTx) chatUpdate() *mtproto.Update {
	return mtproto.MakeTLUpdateChat(&mtproto.Update{ChatId_INT64: t.Chat.Id}).To_Update()
}

// tellRequests gives the admins the requests to join pending now.
func (t *chatTx) tellRequests(b *userbox.Box, r *chat.RecentChatInviteRequesters) error {
	return t.tell(b, t.admins(), mtproto.MakeTLUpdatePendingJoinRequests(&mtproto.Update{
		Peer_PEER:        mtproto.MakePeerChat(t.Chat.Id),
		RequestsPending:  r.GetRequestsPending(),
		RecentRequesters: r.GetRecentRequesters(),
	}).To_Update())
}

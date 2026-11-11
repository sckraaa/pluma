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
	"github.com/teamgram/teamgram-server/pkg/ghost"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The support ghost in basic groups (task 080, ADR 0048): a member whose
// row is hidden (chat_participants.hidden). It gets the messages, the
// whole history and the members with itself among them; the others' list
// of members, its version and the number of members do not change when it
// comes or goes, and no service message tells of it.

// joinHiddenAs runs the hidden join of the ghost userId into chatId in the
// box transaction of the chat's creator, in whose name it comes (by an
// invite link, or by pluma-admin): the inbox event that copies the
// group's history into the ghost's box must come from another box than
// the ghost's own (userbox.CheckEvent). before runs first under the lock
// (a link is checked again there). A ghost who created the group is
// USER_CREATOR: there it is seen, as the creator.
func (c *ChatCore) joinHiddenAs(chatId, userId int64, before func(b *userbox.Box, t *chatTx) error) (*chatTxResult, error) {
	creator, err := c.creatorOf(chatId)
	if err != nil {
		return nil, err
	}
	if creator == userId {
		return nil, mtproto.ErrUserCreator
	}
	res := &chatTxResult{}
	res.chat, err = c.changeChat(creator, chatId, func(b *userbox.Box, t *chatTx) error {
		res.joined = false
		if before != nil {
			if err := before(b, t); err != nil {
				return err
			}
		}
		joined, err := t.joinHidden(b, userId)
		res.joined = joined
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// chatTxResult is the chat a hidden join committed and whether the ghost
// joined, false when it was in.
type chatTxResult struct {
	chat   *mtproto.MutableChat
	joined bool
}

// joinHidden makes the ghost userId a hidden member of the chat in b, the
// box transaction of another user (joinHiddenAs): its row (ghost.JoinChat;
// one an admin removed comes back too, as by a link or the operator's
// command), the members with itself told to it alone, and
// inbox.chatHistoryToInbox, which brings the whole history into its box.
// It tells whether the ghost joined, false when it was in.
func (t *chatTx) joinHidden(b *userbox.Box, userId int64) (bool, error) {
	if err := t.open(); err != nil {
		return false, err
	}
	out, err := ghost.JoinChat(b.Tx(), t.Chat.Id, userId, t.now, false)
	switch {
	case err != nil:
		return false, err
	case out == ghost.Member:
		return false, nil
	case out != ghost.Joined:
		return false, mtproto.ErrChatIdInvalid
	}
	d := t.c.svcCtx.Dao
	p, err := d.ParticipantTx(b, t.Chat.Id, userId)
	if err != nil {
		return false, err
	}
	if i := slices.IndexFunc(t.ChatParticipants, func(q *mtproto.ImmutableChatParticipant) bool { return q.UserId == userId }); i >= 0 {
		t.ChatParticipants[i] = p
	} else {
		t.ChatParticipants = append(t.ChatParticipants, p)
	}
	t.changed(b, userId)
	if err = d.EmitChatHistory(b, userId, t.Chat.Id, false); err != nil {
		return false, err
	}
	return true, t.tell(b, []int64{userId}, t.participantsUpdate())
}

// ghostTarget tells whether userId, whose row in the chat is target (nil
// if none), is the ghost (ADR 0048): a hidden member, or, without a live
// row, an account with users.ghost. A change aimed at the ghost answers the
// same whether it is in the chat or not, so that the answer does not tell.
func (t *chatTx) ghostTarget(b *userbox.Box, target *mtproto.ImmutableChatParticipant, userId int64) (bool, error) {
	if target != nil && target.IsChatMemberStateNormal() {
		return target.Hidden, nil
	}
	return ghost.IsGhost(b.Tx(), userId)
}

// kickAbsentGhost answers an admin's removal of the ghost userId (ADR
// 0048) that is not in the chat as the removal of the ghost hidden in it
// answers: no USER_NOT_PARTICIPANT, which would tell the admin whether the
// ghost was there. It writes the ghost's hidden kicked row (ghost.KickChat:
// a mention no longer brings it in), which the reply of bff
// messages.deleteChatUser reads as the removal of a hidden member, and
// tells whether it did. Anyone else, an operator who may not remove, or a
// ghost in the chat goes the usual way.
func (t *chatTx) kickAbsentGhost(b *userbox.Box, me, removed *mtproto.ImmutableChatParticipant, userId int64) (bool, error) {
	if removed != nil && removed.IsChatMemberStateNormal() || !me.CanAdminBanUsers() {
		return false, nil
	}
	hidden, err := ghost.IsGhost(b.Tx(), userId)
	if err != nil || !hidden {
		return false, err
	}
	if err = t.open(); err != nil {
		return false, err
	}
	if err = ghost.KickChat(b.Tx(), t.Chat.Id, userId, t.now); err != nil {
		return false, err
	}
	p, err := t.c.svcCtx.Dao.ParticipantTx(b, t.Chat.Id, userId)
	if err != nil {
		return false, err
	}
	if i := slices.IndexFunc(t.ChatParticipants, func(q *mtproto.ImmutableChatParticipant) bool { return q.UserId == userId }); i >= 0 {
		t.ChatParticipants[i] = p
	} else {
		t.ChatParticipants = append(t.ChatParticipants, p)
	}
	t.changed(b, userId)
	return true, nil
}

// leaveHidden takes the ghost userId out of the chat where it is hidden, in
// b, its own box transaction: the row is left, the ghost's devices get the
// forbidden list. It tells whether the ghost was in.
func (t *chatTx) leaveHidden(b *userbox.Box, userId int64) (bool, error) {
	left, err := ghost.LeaveChat(b.Tx(), t.Chat.Id, userId, t.now)
	if err != nil || !left {
		return false, err
	}
	if p := t.participant(userId); p != nil {
		p.State, p.LeftAt, p.KickedAt = mtproto.ChatMemberStateLeft, t.now, 0
	}
	t.changed(b, userId)
	return true, t.tell(b, []int64{userId}, t.forbiddenUpdate())
}

// ChatJoinHidden
// chat.joinHidden user_id:long chat_id:long = Bool;
//
// The operator's command (pluma-admin ghost join): the ghost joins the
// basic group hidden, in the name of the group's creator (joinHiddenAs).
// An account that is no ghost is USER_ID_INVALID.
func (c *ChatCore) ChatJoinHidden(in *chat.TLChatJoinHidden) (*mtproto.Bool, error) {
	if hidden, err := ghost.IsGhost(ghost.OnDB(c.ctx, c.svcCtx.Dao.Mysql.DB), in.UserId); err != nil || !hidden {
		if err == nil {
			err = mtproto.ErrUserIdInvalid
		}
		c.Logger.Errorf("chat.joinHidden - user %d, chat %d: %v", in.UserId, in.ChatId, err)
		return nil, err
	}
	res, err := c.joinHiddenAs(in.ChatId, in.UserId, nil)
	if err != nil {
		c.Logger.Errorf("chat.joinHidden - user %d, chat %d: %v", in.UserId, in.ChatId, err)
		return nil, err
	}
	return mtproto.ToBool(res.joined), nil
}

// ChatLeaveHidden
// chat.leaveHidden user_id:long chat_id:long = Bool;
//
// The operator's command (pluma-admin ghost leave): the ghost leaves the
// basic group where it is hidden, unseen.
func (c *ChatCore) ChatLeaveHidden(in *chat.TLChatLeaveHidden) (*mtproto.Bool, error) {
	var left bool
	_, err := c.changeChat(in.UserId, in.ChatId, func(b *userbox.Box, t *chatTx) error {
		var err error
		left, err = t.leaveHidden(b, in.UserId)
		return err
	})
	if err != nil {
		c.Logger.Errorf("chat.leaveHidden - user %d, chat %d: %v", in.UserId, in.ChatId, err)
		return nil, err
	}
	return mtproto.ToBool(left), nil
}

// ChatSetUserHidden
// chat.setUserHidden user_id:long hidden:Bool = Int32;
//
// The account became a ghost, or stopped being one (pluma-admin ghost
// set): its rows where it is a plain member of a basic group become hidden
// or shown, each group in a box transaction of the account with the chat
// locked. The number of members follows, and the members get the list of
// the members with seq, as after any change of it; no service message. A
// row of an admin or of the creator stays shown: an admin is seen by all.
// It returns the number of groups changed.
func (c *ChatCore) ChatSetUserHidden(in *chat.TLChatSetUserHidden) (*mtproto.Int32, error) {
	hidden := mtproto.FromBool(in.GetHidden())
	var ids []int64
	if err := c.svcCtx.Dao.Mysql.DB.QueryRowsPartial(c.ctx, &ids,
		"SELECT chat_id FROM chat_participants WHERE user_id = ? AND state = ? AND participant_type = ? AND hidden <> ? ORDER BY chat_id",
		in.UserId, mtproto.ChatMemberStateNormal, mtproto.ChatMemberNormal, hidden); err != nil {
		c.Logger.Errorf("chat.setUserHidden - user %d: %v", in.UserId, err)
		return nil, err
	}
	var n int32
	for _, chatId := range ids {
		changed := false
		_, err := c.changeChat(in.UserId, chatId, func(b *userbox.Box, t *chatTx) error {
			changed = false
			p := t.member(in.UserId)
			if p == nil || !p.IsChatMemberNormal() || p.Hidden == hidden {
				return nil
			}
			if err := c.svcCtx.Dao.SetHiddenTx(b, p.Id, hidden); err != nil {
				return err
			}
			p.Hidden, changed = hidden, true
			t.changed(b, in.UserId)
			if err := t.writeCount(b); err != nil {
				return err
			}
			return t.tellMembers(b, t.participantsUpdate())
		})
		if err != nil {
			c.Logger.Errorf("chat.setUserHidden - user %d, chat %d: %v", in.UserId, chatId, err)
			return nil, err
		}
		if changed {
			n++
		}
	}
	return mtproto.MakeTLInt32(&mtproto.Int32{V: n}).To_Int32(), nil
}

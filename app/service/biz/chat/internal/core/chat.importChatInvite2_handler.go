// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChatImportChatInvite2
// chat.importChatInvite2 self_id:long hash:string = ChatInviteImported;
//
// The user joins by the link, or asks to when the link needs an admin's
// approval, in its box transaction with the chat locked (ADR 0021): the
// link is checked again under the lock, and the membership or the request
// is written with the use of the link; the link of an admin who may no
// longer invite is dead (usable). Every member, the new one
// included, gets the members with seq; a request gives the creator and the
// admins the requests pending. A request already pending changes nothing.
// The requesters of the result tell that the user asked.
func (c *ChatCore) ChatImportChatInvite2(in *chat.TLChatImportChatInvite2) (*chat.ChatInviteImported, error) {
	invite, err := c.svcCtx.Dao.ChatInvitesDAO.SelectByLink(c.ctx, in.Hash)
	if err != nil {
		c.Logger.Errorf("chat.importChatInvite - error: %v", err)
		return nil, err
	} else if invite == nil {
		err = mtproto.ErrInviteHashInvalid
		c.Logger.Errorf("chat.importChatInvite - error: %v", err)
		return nil, err
	}

	// The joiner gets the members, the users of which are looked up; a
	// requester is no admin and gets nothing.
	change := c.changeChat
	if !invite.RequestNeeded {
		change = func(operatorId, chatId int64, fn func(b *userbox.Box, t *chatTx) error) (*mtproto.MutableChat, error) {
			return c.changeMembers(operatorId, chatId, []int64{in.SelfId, invite.AdminId}, fn)
		}
	}
	var requesters *chat.RecentChatInviteRequesters
	chat2, err := change(in.SelfId, invite.ChatId, func(b *userbox.Box, t *chatTx) error {
		requesters = nil
		d := t.c.svcCtx.Dao
		invite, err := d.InviteByLinkTx(b, in.Hash)
		if err != nil {
			return err
		}
		if err = t.usable(b, invite); err != nil {
			return err
		}
		if !invite.RequestNeeded {
			if err = t.addMember(b, invite.AdminId, in.SelfId, false, false); err != nil {
				return err
			}
			if err = d.PutInviteeTx(b, t.Chat.Id, in.Hash, in.SelfId, false, t.now); err != nil {
				return err
			}
			return t.tellMembers(b, t.participantsUpdate())
		}

		if err = t.open(); err != nil {
			return err
		}
		switch {
		case t.member(in.SelfId) != nil:
			return mtproto.ErrUserAlreadyParticipant
		case len(t.members()) >= maxChatMembers:
			return mtproto.ErrUsersTooMuch
		}
		pending, err := d.PendingRequestTx(b, t.Chat.Id, in.SelfId)
		if err != nil {
			return err
		}
		if pending {
			requesters, err = d.RecentRequestersTx(b, t.Chat.Id)
			return err
		}
		if err = d.PutInviteeTx(b, t.Chat.Id, in.Hash, in.SelfId, true, t.now); err != nil {
			return err
		}
		if requesters, err = d.RecentRequestersTx(b, t.Chat.Id); err != nil {
			return err
		}
		return t.tellRequests(b, requesters)
	})
	if err != nil {
		c.Logger.Errorf("chat.importChatInvite - error: %v", err)
		return nil, err
	}

	return chat.MakeTLChatInviteImported(&chat.ChatInviteImported{
		Chat:       chat2,
		Requesters: requesters,
	}).To_ChatInviteImported(), nil
}

// usable checks an invite link of the chat as the transaction reads it:
// not revoked, not expired, its admin still one who may invite (mayInvite:
// not removed, not gone, not demoted where the members may not invite),
// not used up. A dead link is INVITE_HASH_EXPIRED, as Telegram answers.
func (t *chatTx) usable(b *userbox.Box, invite *dataobject.ChatInvitesDO) error {
	switch {
	case invite == nil, invite.ChatId != t.Chat.Id:
		return mtproto.ErrInviteHashInvalid
	case invite.Revoked:
		return mtproto.ErrInviteHashExpired
	case invite.ExpireDate != 0 && t.now > invite.ExpireDate:
		return mtproto.ErrInviteHashExpired
	case mayInvite(t.MutableChat, invite.AdminId, t.now) != nil:
		return mtproto.ErrInviteHashExpired
	case invite.UsageLimit <= 0:
		return nil
	}
	used, err := t.c.svcCtx.Dao.LinkUsageTx(b, invite.Link)
	if err != nil {
		return err
	}
	if used >= invite.UsageLimit {
		return mtproto.ErrInviteHashExpired
	}
	return nil
}

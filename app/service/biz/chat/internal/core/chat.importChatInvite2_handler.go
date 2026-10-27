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
// is written with the use of the link. A request already pending changes
// nothing. The requesters of the result tell that the user asked.
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

	var requesters *chat.RecentChatInviteRequesters
	chat2, err := c.changeChat(in.SelfId, invite.ChatId, func(b *userbox.Box, t *chatTx) error {
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
			return d.PutInviteeTx(b, t.Chat.Id, in.Hash, in.SelfId, false, t.now)
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
		if !pending {
			if err = d.PutInviteeTx(b, t.Chat.Id, in.Hash, in.SelfId, true, t.now); err != nil {
				return err
			}
		}
		requesters, err = d.RecentRequestersTx(b, t.Chat.Id)
		return err
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
// not revoked, not expired, not used up.
func (t *chatTx) usable(b *userbox.Box, invite *dataobject.ChatInvitesDO) error {
	switch {
	case invite == nil, invite.ChatId != t.Chat.Id:
		return mtproto.ErrInviteHashInvalid
	case invite.Revoked:
		return mtproto.ErrInviteHashExpired
	case invite.ExpireDate != 0 && t.now > invite.ExpireDate:
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

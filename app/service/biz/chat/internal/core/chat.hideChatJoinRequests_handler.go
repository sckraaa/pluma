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
	"github.com/teamgram/teamgram-server/app/service/biz/chat/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChatHideChatJoinRequests
// chat.hideChatJoinRequests flags:# self_id:long chat_id:long approved:flags.0?true link:flags.1?string user_id:flags.2?long = RecentChatInviteRequesters;
//
// An admin approves or declines the request of user_id to join, in its box
// transaction with the chat locked (ADR 0021): an approved user becomes a
// member, invited by the admin, and every member gets the members with
// seq. The creator and the admins get the requests left, which it
// returns. The requests of a whole link (no user_id) are not served.
func (c *ChatCore) ChatHideChatJoinRequests(in *chat.TLChatHideChatJoinRequests) (*chat.RecentChatInviteRequesters, error) {
	if in.GetUserId() == nil {
		c.Logger.Errorf("chat.hideChatJoinRequests - error: the requests of a link are not served")
		return nil, mtproto.ErrMethodNotImpl
	}
	joinId := in.GetUserId().GetValue()

	// The requesters the admin's own update names: the latest ones now and
	// the one that moves up among them when this request goes. Requesters
	// are no participants, whom changeMembers looks up anyway.
	before, err := c.svcCtx.Dao.LatestRequesters(c.ctx, in.ChatId, dao.RecentRequestersMax+1)
	if err != nil {
		c.Logger.Errorf("chat.hideChatJoinRequests - error: %v", err)
		return nil, err
	}
	var requesters *chat.RecentChatInviteRequesters
	_, err = c.changeMembers(in.SelfId, in.ChatId, append(before, joinId), func(b *userbox.Box, t *chatTx) error {
		requesters = nil
		me := t.member(in.SelfId)
		if me == nil || !me.CanInviteUsers() {
			return mtproto.ErrChatAdminRequired
		}
		d := t.c.svcCtx.Dao
		pending, err := d.PendingRequestTx(b, in.ChatId, joinId)
		if err != nil {
			return err
		}
		if !pending {
			return mtproto.ErrHideRequesterMissing
		}
		if in.GetApproved() {
			if err = t.addMember(b, in.SelfId, joinId, false, false); err != nil {
				return err
			}
			if _, err = d.ChatInviteParticipantsDAO.UpdateApprovedByTx(b.Tx(), in.SelfId, in.ChatId, joinId); err != nil {
				return err
			}
			if err = t.tellMembers(b, t.participantsUpdate()); err != nil {
				return err
			}
		} else if _, err = d.ChatInviteParticipantsDAO.DeleteTx(b.Tx(), in.ChatId, joinId); err != nil {
			return err
		}
		if requesters, err = d.RecentRequestersTx(b, in.ChatId); err != nil {
			return err
		}
		return t.tellRequests(b, requesters)
	})
	if err != nil {
		c.Logger.Errorf("chat.hideChatJoinRequests - error: %v", err)
		return nil, err
	}
	return requesters, nil
}

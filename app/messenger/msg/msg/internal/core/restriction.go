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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/moderation"
)

// checkRestriction refuses a send of a restricted account (task 025, ADR
// 0039), as the clients expect of Telegram's spam limits: PEER_FLOOD for a
// private chat with someone who does not have the sender in contacts
// (Android AlertsCreator.java:510, tdesktop apiwrap.cpp:525, iOS
// PendingMessageManager.swift:90), USER_BANNED_IN_CHANNEL in a group
// (core.telegram.org/method/messages.sendMessage). A chat with oneself and
// with the service notifications account stay open, so that a restricted
// user is not cut off from the appeal (Android opens @spambot from the
// PEER_FLOOD alert). A banned account, whose sessions are being ended, has
// no exception for its contacts.
//
// A service message is not a message the user wrote: it records a chat
// action (leaving a group, creating one) that its own service has already
// carried out, and refusing it would leave that action half done.
func (c *MsgCore) checkRestriction(in *msg.TLMsgSendMessageV2) error {
	if onlyServiceMessages(in.GetMessage()) {
		return nil
	}
	r, err := c.svcCtx.Dao.Moderation.Restriction(c.ctx, in.GetUserId())
	if err != nil {
		return err
	}
	if r == nil {
		return nil
	}
	switch in.GetPeerType() {
	case mtproto.PEER_USER:
		if in.GetPeerId() == in.GetUserId() || in.GetPeerId() == moderation.ServiceUserId {
			return nil
		}
		if r.Kind == moderation.RestrictBanned {
			return moderation.ErrPeerFlood
		}
		users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
			Id: []int64{in.GetUserId(), in.GetPeerId()},
			To: []int64{in.GetUserId(), in.GetPeerId()},
		})
		if err != nil {
			return err
		}
		if peer, _ := users.GetImmutableUser(in.GetPeerId()); peer != nil {
			if contact, _ := peer.CheckContact(in.GetUserId()); contact {
				return nil
			}
		}
		return moderation.ErrPeerFlood
	case mtproto.PEER_CHAT, mtproto.PEER_CHANNEL:
		return mtproto.ErrUserBannedInChannel
	}
	return nil
}

// onlyServiceMessages tells whether a send holds nothing but service
// messages.
func onlyServiceMessages(outBox []*msg.OutboxMessage) bool {
	for _, m := range outBox {
		if m.GetMessage().GetPredicateName() != mtproto.Predicate_messageService {
			return false
		}
	}
	return true
}

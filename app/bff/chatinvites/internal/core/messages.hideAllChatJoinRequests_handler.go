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
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// MessagesHideAllChatJoinRequests
// messages.hideAllChatJoinRequests#e085f4ea flags:# approved:flags.0?true peer:InputPeer link:flags.1?string = Updates;
//
// Every request to join a channel, or those of one link (task 041). A basic
// group answers them one by one, as messages.hideChatJoinRequest does, so
// each approved member gets its service message.
func (c *ChatInvitesCore) MessagesHideAllChatJoinRequests(in *mtproto.TLMessagesHideAllChatJoinRequests) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	if peer.IsChat() {
		return c.hideAllChatRequests(in)
	}
	if !peer.IsChannel() {
		return nil, mtproto.ErrPeerIdInvalid
	}
	return c.hideChannelRequests(peer, in.GetApproved(), true, in.GetLink().GetValue(), 0)
}

// hideAllChatRequests answers the pending requests of a basic group, a page
// at a time: an answered request leaves the list.
func (c *ChatInvitesCore) hideAllChatRequests(in *mtproto.TLMessagesHideAllChatJoinRequests) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	last := mtproto.MakeEmptyUpdates()
	for {
		page, err := c.svcCtx.Dao.ChatClient.ChatGetChatInviteImporters(c.ctx, &chatpb.TLChatGetChatInviteImporters{
			SelfId:    c.MD.UserId,
			ChatId:    peer.PeerId,
			Requested: true,
			Link:      in.GetLink(),
			Limit:     100,
		})
		if err != nil {
			c.Logger.Errorf("messages.hideAllChatJoinRequests - error: %v", err)
			return nil, err
		}
		importers := page.GetDatas()
		if len(importers) == 0 {
			return last, nil
		}
		for _, importer := range importers {
			if last, err = c.MessagesHideChatJoinRequest(&mtproto.TLMessagesHideChatJoinRequest{
				Approved: in.GetApproved(),
				Peer:     in.GetPeer(),
				UserId:   mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: importer.GetUserId()}).To_InputUser(),
			}); err != nil {
				return nil, err
			}
		}
	}
}

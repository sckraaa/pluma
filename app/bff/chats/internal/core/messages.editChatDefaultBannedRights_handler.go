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
	"time"

	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// MessagesEditChatDefaultBannedRights
// messages.editChatDefaultBannedRights#a5866b41 peer:InputPeer banned_rights:ChatBannedRights = Updates;
//
// biz chat gives every member, this account's devices included,
// updateChatDefaultBannedRights with seq in the transaction of the change
// (ADR 0021); the reply carries the chat.
func (c *ChatsCore) MessagesEditChatDefaultBannedRights(in *mtproto.TLMessagesEditChatDefaultBannedRights) (*mtproto.Updates, error) {
	var (
		peer     = mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
		rUpdates *mtproto.Updates
		date     = time.Now().Unix()
	)

	switch peer.PeerType {
	case mtproto.PEER_CHAT:
		chat, err := c.svcCtx.Dao.ChatClient.Client().ChatEditChatDefaultBannedRights(c.ctx, &chatpb.TLChatEditChatDefaultBannedRights{
			ChatId:       peer.PeerId,
			OperatorId:   c.MD.UserId,
			BannedRights: in.BannedRights,
		})
		if err != nil {
			c.Logger.Errorf("messages.editChatDefaultBannedRights - error: %v", err)
			return nil, err
		}

		rUpdates = mtproto.MakeTLUpdates(&mtproto.Updates{
			Updates: []*mtproto.Update{},
			Users:   []*mtproto.User{},
			Chats:   []*mtproto.Chat{chat.ToUnsafeChat(c.MD.UserId)},
			Date:    int32(date),
			Seq:     0,
		}).To_Updates()
	case mtproto.PEER_CHANNEL:
		c.Logger.Errorf("messages.editChatDefaultBannedRights blocked, License key from https://teamgram.net required to unlock enterprise features.")

		return nil, mtproto.ErrEnterpriseIsBlocked
	default:
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("messages.editChatDefaultBannedRights - invalid peer type: %v", peer)
		return nil, err
	}

	return rUpdates, nil
}

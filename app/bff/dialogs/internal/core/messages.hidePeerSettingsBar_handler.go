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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// MessagesHidePeerSettingsBar
// messages.hidePeerSettingsBar#4facb138 peer:InputPeer = Bool;
//
// biz hides the action bar of the chat and pushes updatePeerSettings
// without a bar, with seq, to every device of the account, this one
// included, in the same transaction (ADR 0020); the peer goes with it. A
// chat without a bar changes nothing.
func (c *DialogsCore) MessagesHidePeerSettingsBar(in *mtproto.TLMessagesHidePeerSettingsBar) (*mtproto.Bool, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())

	var (
		users []*mtproto.User
		chats []*mtproto.Chat
	)
	switch peer.PeerType {
	case mtproto.PEER_SELF, mtproto.PEER_USER:
		mUsers, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
			Id: []int64{c.MD.UserId, peer.PeerId},
			To: []int64{c.MD.UserId},
		})
		if err != nil {
			c.Logger.Errorf("messages.hidePeerSettingsBar - error: %v", err)
			return nil, err
		}
		users = mUsers.GetUserListByIdList(c.MD.UserId, peer.PeerId)
		if len(users) == 0 {
			err = mtproto.ErrPeerIdInvalid
			c.Logger.Errorf("messages.hidePeerSettingsBar - error: %v", err)
			return nil, err
		}
	case mtproto.PEER_CHAT:
		mChats, err := c.svcCtx.Dao.ChatClient.ChatGetChatListByIdList(c.ctx, &chatpb.TLChatGetChatListByIdList{
			SelfId: c.MD.UserId,
			IdList: []int64{peer.PeerId},
		})
		if err != nil {
			c.Logger.Errorf("messages.hidePeerSettingsBar - error: %v", err)
			return nil, err
		}
		chats = mChats.GetChatListByIdList(c.MD.UserId, peer.PeerId)
		if len(chats) == 0 {
			err = mtproto.ErrPeerIdInvalid
			c.Logger.Errorf("messages.hidePeerSettingsBar - error: %v", err)
			return nil, err
		}
	case mtproto.PEER_CHANNEL:
		if c.svcCtx.Plugin != nil {
			chats = c.svcCtx.Plugin.GetChannelListByIdList(c.ctx, c.MD.UserId, peer.PeerId)
		}
	default:
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("messages.hidePeerSettingsBar - error: %v", err)
		return nil, err
	}

	if _, err := c.svcCtx.Dao.UserClient.UserDeletePeerSettings(c.ctx, &userpb.TLUserDeletePeerSettings{
		UserId:   c.MD.UserId,
		PeerType: peer.PeerType,
		PeerId:   peer.PeerId,
		Users:    users,
		Chats:    chats,
	}); err != nil {
		c.Logger.Errorf("messages.hidePeerSettingsBar - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}

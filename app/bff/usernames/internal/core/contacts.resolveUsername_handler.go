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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// ContactsResolveUsername
// contacts.resolveUsername#f93ccba3 username:string = contacts.ResolvedPeer;
//
// The peer of a public username, with the user or chat as the user sees
// it: USERNAME_INVALID for what cannot be a username, USERNAME_NOT_OCCUPIED
// when nobody has it. Pluma serves no channels: a channel username, left
// from data of another installation, resolves to nothing. referer (the
// referral program) has no meaning here.
func (c *UsernamesCore) ContactsResolveUsername(in *mtproto.TLContactsResolveUsername) (*mtproto.Contacts_ResolvedPeer, error) {
	var (
		peer *mtproto.PeerUtil
	)

	id := userpb.GetBotIdByName(in.GetUsername())
	if id > 0 {
		peer = mtproto.MakeUserPeerUtil(id)
	} else {
		rName, err := c.svcCtx.Dao.UserClient.UserResolveUsername(c.ctx, &userpb.TLUserResolveUsername{
			Username: in.GetUsername(),
		})
		if err != nil {
			c.Logger.Errorf("contacts.resolveUsername - reply: {%v}", err)
			return nil, err
		}

		peer = mtproto.FromPeer(rName)
	}

	resolvedPeer := mtproto.MakeTLContactsResolvedPeer(&mtproto.Contacts_ResolvedPeer{
		Peer:  peer.ToPeer(),
		Chats: []*mtproto.Chat{},
		Users: []*mtproto.User{},
	}).To_Contacts_ResolvedPeer()

	switch peer.PeerType {
	case mtproto.PEER_USER:
		mUsers, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
			Id: []int64{c.MD.UserId, peer.PeerId},
			To: []int64{c.MD.UserId},
		})
		if err != nil {
			c.Logger.Errorf("contacts.resolveUsername - error: %v", err)
			return nil, err
		}
		if it, ok := mUsers.GetImmutableUser(peer.PeerId); !ok || it.Deleted() {
			err = mtproto.ErrUsernameNotOccupied
			c.Logger.Errorf("contacts.resolveUsername - error: %v", err)
			return nil, err
		}
		resolvedPeer.Users = mUsers.GetUserListByIdList(c.MD.UserId, peer.PeerId)
	case mtproto.PEER_CHAT:
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetChatBySelfId(c.ctx, &chat.TLChatGetChatBySelfId{
			SelfId: c.MD.UserId,
			ChatId: peer.PeerId,
		})
		if err != nil {
			c.Logger.Errorf("contacts.resolveUsername - error: %v", err)
			return nil, err
		}
		resolvedPeer.Chats = []*mtproto.Chat{chat.ToUnsafeChat(c.MD.UserId)}
	case mtproto.PEER_CHANNEL:
		if c.svcCtx.Plugin == nil {
			err := mtproto.ErrUsernameNotOccupied
			c.Logger.Errorf("contacts.resolveUsername - a channel username, channels are not served: %v", err)
			return nil, err
		}
		resolvedPeer.Chats = c.svcCtx.Plugin.GetChannelListByIdList(c.ctx, c.MD.UserId, peer.PeerId)
	default:
		err := mtproto.ErrUsernameNotOccupied
		c.Logger.Errorf("contacts.resolveUsername - error: %v", err)
		return nil, err
	}

	return resolvedPeer, nil
}

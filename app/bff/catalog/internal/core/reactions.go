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
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// The reactions to messages of private chats and basic groups (task 053):
// msg writes them, biz message gives the messages with them as their
// reader sees them. Supergroups and channels are not served yet (044).

// reactionPeer is the dialog of a reactions method as the account sees it:
// a private chat (Saved Messages included) or a basic group.
func (c *CatalogCore) reactionPeer(in *mtproto.InputPeer) (*mtproto.PeerUtil, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in)
	switch peer.PeerType {
	case mtproto.PEER_SELF:
		return mtproto.MakePeerUtil(mtproto.PEER_USER, c.MD.UserId), nil
	case mtproto.PEER_USER, mtproto.PEER_CHAT:
		return peer, nil
	case mtproto.PEER_CHANNEL:
		return nil, mtproto.ErrEnterpriseIsBlocked
	}
	return nil, mtproto.ErrPeerIdInvalid
}

// peersOf returns the users with ids and the chat of peer, as the account
// sees them.
func (c *CatalogCore) peersOf(peer *mtproto.PeerUtil, ids []int64) ([]*mtproto.User, []*mtproto.Chat, error) {
	ids = append([]int64{c.MD.UserId}, ids...)
	if peer.PeerType == mtproto.PEER_USER {
		ids = append(ids, peer.PeerId)
	}
	users, err := c.svcCtx.User.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids, To: []int64{c.MD.UserId}})
	if err != nil {
		return nil, nil, err
	}
	chats := []*mtproto.Chat{}
	if peer.PeerType == mtproto.PEER_CHAT {
		chat, err := c.svcCtx.Chat.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: peer.PeerId})
		if err != nil {
			return nil, nil, err
		}
		chats = append(chats, chat.ToUnsafeChat(c.MD.UserId))
	}
	return users.GetUserListByIdList(c.MD.UserId, ids...), chats, nil
}

// reactorsOf are the users of the recent reactions of updates.
func reactorsOf(updates ...*mtproto.Update) []int64 {
	var ids []int64
	for _, u := range updates {
		for _, r := range u.GetReactions_MESSAGEREACTIONS().GetRecentReactions() {
			if id := r.GetPeerId().GetUserId(); id != 0 {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

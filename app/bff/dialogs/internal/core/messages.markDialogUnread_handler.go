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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// MessagesMarkDialogUnread
// messages.markDialogUnread#8c5006f8 flags:# unread:flags.0?true parent_peer:flags.1?InputPeer peer:InputDialogPeer = Bool;
//
// Sets the unread mark of a dialog. Every device of the account, this one
// included, gets updateDialogUnreadMark with seq: the dialog service pushes
// it in the transaction of the change (ADR 0020), with the peer resolved
// here. With parent_peer, a monoforum (a channel's direct messages, task
// 084), peer is one of its sublists: the mark is the admins' of the direct
// messages, who get updateDialogUnreadMark with saved_peer_id
// (core.telegram.org/api/monoforum); false when it was as asked.
func (c *DialogsCore) MessagesMarkDialogUnread(in *mtproto.TLMessagesMarkDialogUnread) (*mtproto.Bool, error) {
	var (
		peer *mtproto.PeerUtil
	)

	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.markDialogUnread - error: %v", err)
		return nil, err
	}
	if in.GetParentPeer() != nil {
		parent := mtproto.FromInputPeer2(c.MD.UserId, in.GetParentPeer())
		sublist := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer().GetPeer())
		if parent.PeerType != mtproto.PEER_CHANNEL || in.GetPeer().GetPredicateName() != mtproto.Predicate_inputDialogPeer || sublist.PeerType != mtproto.PEER_USER {
			err := mtproto.ErrPeerIdInvalid
			c.Logger.Errorf("messages.markDialogUnread - parent peer %s: %v", in.GetParentPeer().GetPredicateName(), err)
			return nil, err
		}
		changed, err := c.svcCtx.Dao.Channel.ChannelMarkSublistUnread(c.ctx, &channelpb.TLChannelMarkSublistUnread{
			SelfId:        c.MD.UserId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			ChannelId:     parent.PeerId,
			PeerId:        sublist.PeerId,
			Unread:        mtproto.ToBool(in.GetUnread()),
		})
		if err != nil {
			c.Logger.Errorf("messages.markDialogUnread - sublist %d of %d: %v", sublist.PeerId, parent.PeerId, err)
			return nil, err
		}
		return changed, nil
	}

	switch in.Peer.GetPredicateName() {
	case mtproto.Predicate_inputDialogPeer:
		peer = mtproto.FromInputPeer2(c.MD.UserId, in.Peer.Peer)
	case mtproto.Predicate_inputDialogPeerFolder:
		// error
		c.Logger.Errorf("client not send inputDialogPeerFolder: %v", in.Peer)
		return mtproto.BoolFalse, nil
	default:
		err := mtproto.ErrInputConstructorInvalid
		c.Logger.Errorf("messages.markDialogUnread - error: %v", err)
		return nil, err
	}

	users, chats, err := c.unreadMarkPeer(peer)
	if err != nil {
		c.Logger.Errorf("messages.markDialogUnread - peer (%d, %d): %v", peer.PeerType, peer.PeerId, err)
		return nil, err
	}
	rValue, err := c.svcCtx.Dao.DialogClient.DialogMarkDialogUnread(c.ctx, &dialog.TLDialogMarkDialogUnread{
		UserId:     c.MD.UserId,
		PeerType:   peer.PeerType,
		PeerId:     peer.PeerId,
		UnreadMark: mtproto.ToBool(in.Unread),
		Users:      users,
		Chats:      chats,
	})
	if err != nil {
		c.Logger.Errorf("messages.markDialogUnread - error: %v", err)
		return nil, err
	}

	return rValue, nil
}

// unreadMarkPeer returns the peer of a dialog as the user sees it, for the
// update of its unread mark: the user (itself for Saved Messages) or the
// basic group. Pluma has no channels: another peer has no dialog.
func (c *DialogsCore) unreadMarkPeer(peer *mtproto.PeerUtil) ([]*mtproto.User, []*mtproto.Chat, error) {
	switch peer.PeerType {
	case mtproto.PEER_SELF, mtproto.PEER_USER:
		users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
			Id: []int64{c.MD.UserId, peer.PeerId},
		})
		if err != nil {
			return nil, nil, err
		}
		return users.GetUserListByIdList(c.MD.UserId, peer.PeerId), nil, nil
	case mtproto.PEER_CHAT:
		chats, err := c.svcCtx.Dao.ChatClient.ChatGetChatListByIdList(c.ctx, &chatpb.TLChatGetChatListByIdList{
			SelfId: c.MD.UserId,
			IdList: []int64{peer.PeerId},
		})
		if err != nil {
			return nil, nil, err
		}
		return nil, chats.GetChatListByIdList(c.MD.UserId, peer.PeerId), nil
	}
	return nil, nil, mtproto.ErrPeerIdInvalid
}

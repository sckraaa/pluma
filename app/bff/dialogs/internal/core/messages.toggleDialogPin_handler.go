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
	"slices"

	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// MessagesToggleDialogPin
// messages.toggleDialogPin#a731e257 flags:# pinned:flags.0?true peer:InputDialogPeer = Bool;
func (c *DialogsCore) MessagesToggleDialogPin(in *mtproto.TLMessagesToggleDialogPin) (*mtproto.Bool, error) {
	var (
		peer *mtproto.PeerUtil
	)

	switch in.GetPeer().GetPredicateName() {
	case mtproto.Predicate_inputDialogPeer:
		peer = mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer().GetPeer())
	case mtproto.Predicate_inputDialogPeerFolder:
		// error
		c.Logger.Errorf("messages.toggleDialogPin - error: client not send inputDialogPeerFolder: %v", in.GetPeer())
		return mtproto.BoolFalse, nil
	default:
		err := mtproto.ErrInputConstructorInvalid
		c.Logger.Errorf("messages.toggleDialogPin - error: %v", err)
		return nil, err
	}
	if peer.PeerType == mtproto.PEER_SELF {
		peer.PeerType = mtproto.PEER_USER
	}

	// biz pushes updateDialogPinned with seq to every device of the account,
	// this one included, in the transaction of the pin (ADR 0020); the peer
	// goes with it.
	h := mtproto.NewIDListHelper()
	h.PickByPeerUtil(peer.PeerType, peer.PeerId)
	users, chats, err := c.pinPeers(h)
	if err != nil {
		c.Logger.Errorf("messages.toggleDialogPin - error: %v", err)
		return nil, err
	}

	_, err = c.svcCtx.Dao.DialogClient.DialogToggleDialogPin(c.ctx, &dialog.TLDialogToggleDialogPin{
		UserId:   c.MD.UserId,
		PeerType: peer.PeerType,
		PeerId:   peer.PeerId,
		Pinned:   mtproto.ToBool(in.Pinned),
		Users:    users,
		Chats:    chats,
	})
	if err != nil {
		c.Logger.Errorf("messages.toggleDialogPin - error: %v", err)
		return mtproto.BoolFalse, nil
	}

	return mtproto.BoolTrue, nil
}

// pinPeers returns the users and chats of h as the account sees them: the
// peers an update of pins names, for its push with seq (ADR 0020).
// Channels are not served.
func (c *DialogsCore) pinPeers(h *mtproto.IDListHelper) (users []*mtproto.User, chats []*mtproto.Chat, err error) {
	h.Visit(
		func(userIdList []int64) {
			// The account is in the request: the others are seen as it sees them.
			id := userIdList
			if !slices.Contains(id, c.MD.UserId) {
				id = append([]int64{c.MD.UserId}, id...)
			}
			var r *userpb.Vector_ImmutableUser
			if r, err = c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: id}); err == nil {
				users = r.GetUserListByIdList(c.MD.UserId, userIdList...)
			}
		},
		func(chatIdList []int64) {
			if err != nil {
				return
			}
			var r *chatpb.Vector_MutableChat
			if r, err = c.svcCtx.Dao.ChatClient.ChatGetChatListByIdList(c.ctx, &chatpb.TLChatGetChatListByIdList{IdList: chatIdList}); err == nil {
				chats = r.GetChatListByIdList(c.MD.UserId, chatIdList...)
			}
		},
		nil)
	return users, chats, err
}

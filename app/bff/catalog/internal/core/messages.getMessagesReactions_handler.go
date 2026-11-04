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
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// MessagesGetMessagesReactions
// messages.getMessagesReactions#8bba90e6 peer:InputPeer id:Vector<int> = Updates;
//
// The reactions of the account's messages id of the dialog, an
// updateMessageReactions for each one that exists, an empty object for one
// without reactions: Desktop clears the reactions of a polled message
// nothing came for (data_message_reactions.cpp:1731-1736, task 053).
func (c *CatalogCore) MessagesGetMessagesReactions(in *mtproto.TLMessagesGetMessagesReactions) (*mtproto.Updates, error) {
	peer, err := c.reactionPeer(in.Peer)
	if err != nil {
		return nil, err
	}
	if len(in.Id) > 100 {
		return nil, mtproto.ErrMsgIdInvalid
	}
	updates := []*mtproto.Update{}
	if len(in.Id) > 0 {
		boxes, err := c.svcCtx.Message.MessageGetUserMessageList(c.ctx, &messagepb.TLMessageGetUserMessageList{UserId: c.MD.UserId, IdList: in.Id})
		if err != nil {
			c.Logger.Errorf("messages.getMessagesReactions - error: %v", err)
			return nil, err
		}
		for _, box := range boxes.GetDatas() {
			if box.GetPeerType() != peer.PeerType || box.GetPeerId() != peer.PeerId || box.GetMessage() == nil {
				continue
			}
			view := box.GetMessage().GetReactions()
			if view == nil {
				view = reactions.ViewOrEmpty(nil, c.MD.UserId, box.GetSenderUserId(), reactions.BoxKind(box))
			}
			updates = append(updates, mtproto.MakeTLUpdateMessageReactions(&mtproto.Update{
				Peer_PEER:                  peer.ToPeer(),
				MsgId_INT32:                box.GetMessageId(),
				Reactions_MESSAGEREACTIONS: view,
			}).To_Update())
		}
	}
	users, chats, err := c.peersOf(peer, reactorsOf(updates...))
	if err != nil {
		return nil, err
	}
	return mtproto.MakeUpdatesByUpdatesUsersChats(users, chats, updates...), nil
}

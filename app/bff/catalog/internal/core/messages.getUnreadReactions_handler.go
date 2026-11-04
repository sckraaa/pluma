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
)

// MessagesGetUnreadReactions
// messages.getUnreadReactions#bd7f90ac flags:# peer:InputPeer top_msg_id:flags.0?int saved_peer_id:flags.1?InputPeer offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
//
// The account's messages of the dialog with reactions it has not seen,
// newest first, paged as the history (task 053). Topics and saved dialogs of
// monoforums do not exist here.
func (c *CatalogCore) MessagesGetUnreadReactions(in *mtproto.TLMessagesGetUnreadReactions) (*mtproto.Messages_Messages, error) {
	peer, err := c.reactionPeer(in.Peer)
	if err != nil {
		return nil, err
	}
	boxes, err := c.svcCtx.Message.MessageGetUnreadReactions(c.ctx, &messagepb.TLMessageGetUnreadReactions{
		UserId:    c.MD.UserId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
		OffsetId:  in.OffsetId,
		AddOffset: in.AddOffset,
		Limit:     in.Limit,
		MinId:     in.MinId,
		MaxId:     in.MaxId,
	})
	if err != nil {
		c.Logger.Errorf("messages.getUnreadReactions - error: %v", err)
		return nil, err
	}
	messages := make([]*mtproto.Message, 0, len(boxes.GetDatas()))
	for _, box := range boxes.GetDatas() {
		messages = append(messages, box.ToMessage(c.MD.UserId))
	}
	h := mtproto.NewIDListHelper()
	h.PickByMessages(messages...)
	for _, m := range messages {
		for _, r := range m.GetReactions().GetRecentReactions() {
			h.PickByPeer(r.GetPeerId())
		}
	}
	users, chats, err := c.peersOf(peer, h.UserIdList)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesMessages(&mtproto.Messages_Messages{
		Messages: messages,
		Chats:    chats,
		Users:    users,
	}).To_Messages_Messages(), nil
}

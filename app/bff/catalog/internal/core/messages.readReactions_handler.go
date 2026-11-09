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
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// MessagesReadReactions
// messages.readReactions#9ec44f93 flags:# peer:InputPeer top_msg_id:flags.0?int saved_peer_id:flags.1?InputPeer = messages.AffectedHistory;
//
// Every reaction to the account's messages of the dialog is seen (task 053).
func (c *CatalogCore) MessagesReadReactions(in *mtproto.TLMessagesReadReactions) (*mtproto.Messages_AffectedHistory, error) {
	if p := mtproto.FromInputPeer2(c.MD.UserId, in.Peer); p.PeerType == mtproto.PEER_CHANNEL {
		r, err := c.svcCtx.Channel.ChannelReadReactions(c.ctx, &channelpb.TLChannelReadReactions{
			SelfId:        c.MD.UserId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			ChannelId:     p.PeerId,
			TopMsgId:      in.GetTopMsgId().GetValue(),
		})
		if err != nil {
			c.Logger.Errorf("messages.readReactions - error: %v", err)
			return nil, err
		}
		return r, nil
	}
	peer, err := c.reactionPeer(in.Peer)
	if err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Msg.MsgReadReactions(c.ctx, &msgpb.TLMsgReadReactions{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
	})
	if err != nil {
		c.Logger.Errorf("messages.readReactions - error: %v", err)
		return nil, err
	}
	return r, nil
}

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
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// MessagesReadMentions
// messages.readMentions#36e5bf4d flags:# peer:InputPeer top_msg_id:flags.0?int = messages.AffectedHistory;
//
// msg reads a portion of the dialog's unread mentions; offset above 0 asks
// the client to repeat the call (tdesktop menu_send.cpp
// SetupUnreadMentionsMenu, tweb appMessagesManager.readMentions). top_msg_id
// names a forum topic, and forums are supergroups.
func (c *MessagesCore) MessagesReadMentions(in *mtproto.TLMessagesReadMentions) (*mtproto.Messages_AffectedHistory, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	switch peer.PeerType {
	case mtproto.PEER_SELF, mtproto.PEER_USER, mtproto.PEER_CHAT:
	case mtproto.PEER_CHANNEL:
		c.Logger.Errorf("messages.readMentions blocked, License key from https://teamgram.net required to unlock enterprise features.")
		return nil, mtproto.ErrEnterpriseIsBlocked
	default:
		c.Logger.Errorf("messages.readMentions - error: invalid peer %v", in.Peer)
		return nil, mtproto.ErrPeerIdInvalid
	}

	affected, err := c.svcCtx.Dao.MsgClient.MsgReadMentions(c.ctx, &msgpb.TLMsgReadMentions{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
	})
	if err != nil {
		c.Logger.Errorf("messages.readMentions - error: %v", err)
		return nil, err
	}
	return affected, nil
}

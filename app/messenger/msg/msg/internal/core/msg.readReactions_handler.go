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
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgReadReactions
// msg.readReactions user_id:long auth_key_id:long peer_type:int peer_id:long = messages.AffectedHistory;
//
// The reactions to the user's messages of the dialog are seen (task 053):
// the dialog's counter goes to 0 in one transaction, with nothing left for
// another call (offset 0). Nothing is journaled (Dao.ReadReactions).
func (c *MsgCore) MsgReadReactions(in *msg.TLMsgReadReactions) (*mtproto.Messages_AffectedHistory, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	if peerType != mtproto.PEER_USER && peerType != mtproto.PEER_CHAT {
		c.Logger.Errorf("msg.readReactions - error: invalid peer %d/%d", in.PeerType, in.PeerId)
		return nil, mtproto.ErrPeerIdInvalid
	}
	res, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId,
		[]string{dialog.GetDialogCacheKeyByPeer(in.UserId, peerType, peerId)},
		func(b *userbox.Box) error {
			return c.svcCtx.Dao.ReadReactions(b, peerType, peerId)
		})
	if err != nil {
		c.Logger.Errorf("msg.readReactions - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{
		Pts:      res.State.Pts,
		PtsCount: 0,
		Offset:   0,
	}).To_Messages_AffectedHistory(), nil
}

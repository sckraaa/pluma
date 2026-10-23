// Copyright 2024 Teamgram Authors
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
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgReadHistoryV2
// msg.readHistoryV2 user_id:long auth_key_id:long peer_type:int peer_id:long max_id:int = messages.AffectedMessages;
//
// The dialog is read in one box transaction: read_inbox_max_id only grows
// and unread_count is counted from the messages under the box lock. The
// senders of the messages read learn it with inbox events.
func (c *MsgCore) MsgReadHistoryV2(in *msg.TLMsgReadHistoryV2) (*mtproto.Messages_AffectedMessages, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	if peerType != mtproto.PEER_USER && peerType != mtproto.PEER_CHAT {
		c.Logger.Errorf("msg.readHistoryV2 - error: invalid peer %d/%d", in.PeerType, in.PeerId)
		return nil, mtproto.ErrPeerIdInvalid
	}
	// The chat with oneself has no incoming messages to read.
	self := peerType == mtproto.PEER_USER && peerId == in.UserId

	res, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId,
		[]string{dialog.GetDialogCacheKeyByPeer(in.UserId, peerType, peerId)},
		func(b *userbox.Box) error {
			if self {
				return nil
			}
			read, err := c.svcCtx.Dao.ReadHistory(b, peerType, peerId, in.MaxId)
			if err != nil || read == nil {
				return err
			}
			if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, in.UserId, in.AuthKeyId, mtproto.MakeUpdatesByUpdates(read.Update)); err != nil {
				return err
			}
			for _, s := range read.Senders {
				if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxReadOutboxHistory{
					UserId:             s.UserId,
					FromId:             in.UserId,
					PeerType:           peerType,
					PeerId:             peerId,
					MaxDialogMessageId: s.DialogMessageId,
				}); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		c.Logger.Errorf("msg.readHistoryV2 - error: %v", err)
		return nil, err
	}
	return affectedMessages(res), nil
}

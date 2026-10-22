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
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgDeleteHistory
// msg.deleteHistory user_id:long auth_key_id:long peer_type:int peer_id:long just_clear:Bool revoke:Bool = messages.AffectedHistory;
//
// It deletes a portion of the newest messages of the dialog, as Telegram
// does: offset above 0 asks the client to repeat the call. Without
// just_clear the last portion deletes the dialog row too. With revoke (and
// not just_clear) each portion also deletes the dialog's history of every
// other member.
func (c *MsgCore) MsgDeleteHistory(in *msg.TLMsgDeleteHistory) (*mtproto.Messages_AffectedHistory, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	if peerType != mtproto.PEER_USER && peerType != mtproto.PEER_CHAT {
		c.Logger.Errorf("msg.deleteHistory - error: invalid peer %d/%d", in.PeerType, in.PeerId)
		return nil, mtproto.ErrPeerIdInvalid
	}

	revoke := in.Revoke && !in.JustClear
	var recipients []int64
	if revoke {
		var err error
		if recipients, err = c.recipients(in.UserId, peerType, peerId); err != nil {
			c.Logger.Errorf("msg.deleteHistory - error: %v", err)
			return nil, err
		}
	}

	var left int32
	res, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId,
		[]string{dialog.GetDialogCacheKeyByPeer(in.UserId, peerType, peerId)},
		func(b *userbox.Box) error {
			left = 0
			cleared, err := c.svcCtx.Dao.ClearHistory(b, peerType, peerId, in.JustClear)
			if err != nil || cleared == nil {
				return err
			}
			left = cleared.Left
			if len(cleared.Updates) > 0 {
				if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, in.UserId, in.AuthKeyId, mtproto.MakeUpdatesByUpdates(cleared.Updates...)); err != nil {
					return err
				}
			}
			if cleared.Deleted == nil {
				return nil
			}
			for _, to := range recipients {
				if peerType == mtproto.PEER_USER {
					err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxDeleteUserHistoryToInbox{UserId: to, FromId: in.UserId})
				} else {
					err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxDeleteChatHistoryToInbox{UserId: to, FromId: in.UserId, PeerChatId: peerId})
				}
				if err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		c.Logger.Errorf("msg.deleteHistory - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{
		Pts:      res.State.Pts,
		PtsCount: ptsCount(res),
		Offset:   left,
	}).To_Messages_AffectedHistory(), nil
}

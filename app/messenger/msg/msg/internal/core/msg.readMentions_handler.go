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

// MsgReadMentions
// msg.readMentions user_id:long auth_key_id:long peer_type:int peer_id:long = messages.AffectedHistory;
//
// It reads a portion of the dialog's unread mentions as readMessageContents
// does, and the user's other devices get updateReadMessagesContents; offset
// above 0 asks the client to repeat the call. Mentions are not opened media:
// their senders learn nothing.
func (c *MsgCore) MsgReadMentions(in *msg.TLMsgReadMentions) (*mtproto.Messages_AffectedHistory, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	if peerType != mtproto.PEER_USER && peerType != mtproto.PEER_CHAT {
		c.Logger.Errorf("msg.readMentions - error: invalid peer %d/%d", in.PeerType, in.PeerId)
		return nil, mtproto.ErrPeerIdInvalid
	}

	var left int32
	res, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId,
		[]string{dialog.GetDialogCacheKeyByPeer(in.UserId, peerType, peerId)},
		func(b *userbox.Box) error {
			left = 0
			read, err := c.svcCtx.Dao.ReadMentionsPortion(b, peerType, peerId)
			if err != nil {
				return err
			}
			left = read.Left
			if read.Contents == nil {
				return nil
			}
			return c.svcCtx.Dao.EmitUpdatesNotMe(b, in.UserId, in.AuthKeyId, mtproto.MakeUpdatesByUpdates(read.Contents.Update))
		})
	if err != nil {
		c.Logger.Errorf("msg.readMentions - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{
		Pts:      res.State.Pts,
		PtsCount: ptsCount(res),
		Offset:   left,
	}).To_Messages_AffectedHistory(), nil
}

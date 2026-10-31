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
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// MessagesDeleteSavedHistory
// messages.deleteSavedHistory#4dc5085f flags:# parent_peer:flags.0?InputPeer peer:InputPeer max_id:int min_date:flags.2?int max_date:flags.3?int = messages.AffectedHistory;
//
// It deletes the sub-chat of Saved Messages with peer: up to max_id when
// above 0, within min_date and max_date when given, a portion at a time
// (offset above 0 asks the client to repeat the call). The sub-chat leaves
// messages.getSavedDialogs with its last message. parent_peer names the
// monoforum of a channel, and channels are not served yet.
func (c *SavedMessageDialogsCore) MessagesDeleteSavedHistory(in *mtproto.TLMessagesDeleteSavedHistory) (*mtproto.Messages_AffectedHistory, error) {
	if in.GetParentPeer() != nil {
		c.Logger.Errorf("messages.deleteSavedHistory blocked, License key from https://teamgram.net required to unlock enterprise features.")
		return nil, mtproto.ErrEnterpriseIsBlocked
	}
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	switch peer.PeerType {
	case mtproto.PEER_SELF, mtproto.PEER_USER, mtproto.PEER_CHAT:
	case mtproto.PEER_CHANNEL:
		c.Logger.Errorf("messages.deleteSavedHistory blocked, License key from https://teamgram.net required to unlock enterprise features.")
		return nil, mtproto.ErrEnterpriseIsBlocked
	default:
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("messages.deleteSavedHistory - error: %v", err)
		return nil, err
	}
	savedType, savedId := peer.PeerType, peer.PeerId
	if savedType == mtproto.PEER_SELF {
		// "My notes": the sub-chat of the user's own messages.
		savedType, savedId = mtproto.PEER_USER, c.MD.UserId
	}
	affectedHistory, err := c.svcCtx.Dao.MsgClient.MsgDeleteHistoryRange(c.ctx, &msgpb.TLMsgDeleteHistoryRange{
		UserId:        c.MD.UserId,
		AuthKeyId:     c.MD.PermAuthKeyId,
		PeerType:      mtproto.PEER_USER,
		PeerId:        c.MD.UserId,
		SavedPeerType: savedType,
		SavedPeerId:   savedId,
		MaxId:         in.MaxId,
		MinDate:       in.GetMinDate().GetValue(),
		MaxDate:       in.GetMaxDate().GetValue(),
		Revoke:        mtproto.BoolFalse,
	})
	if err != nil {
		c.Logger.Errorf("messages.deleteSavedHistory - error: %v", err)
		return nil, err
	}
	return affectedHistory, nil
}

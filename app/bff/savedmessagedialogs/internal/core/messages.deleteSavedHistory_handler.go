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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// MessagesDeleteSavedHistory
// messages.deleteSavedHistory#4dc5085f flags:# parent_peer:flags.0?InputPeer peer:InputPeer max_id:int min_date:flags.2?int max_date:flags.3?int = messages.AffectedHistory;
//
// It deletes the sub-chat of Saved Messages with peer: up to max_id when
// above 0, within min_date and max_date when given, a portion at a time
// (offset above 0 asks the client to repeat the call). The peer is a user,
// the account itself ("My notes"), a chat, or a channel the posts or the
// supergroup messages were saved from. The sub-chat leaves
// messages.getSavedDialogs with its last message.
func (c *SavedMessageDialogsCore) MessagesDeleteSavedHistory(in *mtproto.TLMessagesDeleteSavedHistory) (*mtproto.Messages_AffectedHistory, error) {
	monoforumId, err := c.parentMonoforum(in.GetParentPeer())
	if err != nil {
		c.Logger.Errorf("messages.deleteSavedHistory - error: %v", err)
		return nil, err
	}
	if monoforumId != 0 {
		// A sublist of a monoforum goes for everyone, by an admin of the
		// channel's direct messages (core.telegram.org/api/monoforum).
		peerId, err := c.sublistPeer(in.GetPeer())
		if err != nil {
			c.Logger.Errorf("messages.deleteSavedHistory - error: %v", err)
			return nil, err
		}
		r, err := c.svcCtx.Dao.Channel.ChannelDeleteSublistHistory(c.ctx, &channelpb.TLChannelDeleteSublistHistory{
			SelfId:        c.MD.UserId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			ChannelId:     monoforumId,
			PeerId:        peerId,
			MaxId:         in.MaxId,
			MinDate:       in.GetMinDate().GetValue(),
			MaxDate:       in.GetMaxDate().GetValue(),
		})
		if err != nil {
			c.Logger.Errorf("messages.deleteSavedHistory - sublist %d of %d: %v", peerId, monoforumId, err)
			return nil, err
		}
		return r, nil
	}
	saved, err := c.savedPeer(in.GetPeer())
	if err != nil {
		c.Logger.Errorf("messages.deleteSavedHistory - error: %v", err)
		return nil, err
	}
	affectedHistory, err := c.svcCtx.Dao.MsgClient.MsgDeleteHistoryRange(c.ctx, &msgpb.TLMsgDeleteHistoryRange{
		UserId:        c.MD.UserId,
		AuthKeyId:     c.MD.PermAuthKeyId,
		PeerType:      mtproto.PEER_USER,
		PeerId:        c.MD.UserId,
		SavedPeerType: saved.PeerType,
		SavedPeerId:   saved.PeerId,
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

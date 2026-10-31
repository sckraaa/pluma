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
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgDeleteHistoryRange
// msg.deleteHistoryRange user_id:long auth_key_id:long peer_type:int peer_id:long saved_peer_type:int saved_peer_id:long max_id:int min_date:int max_date:int revoke:Bool = messages.AffectedHistory;
//
// It deletes a portion of a part of the dialog (dao.Range): the messages of
// some days (messages.deleteHistory with min_date and max_date) or of a
// sub-chat of Saved Messages (messages.deleteSavedHistory). The user's
// other devices get updateDeleteMessages; with revoke, in a private chat
// the other user loses their copies too. offset above 0 asks the client to
// repeat the call, as Android does (MessagesController.deleteMessagesRange).
func (c *MsgCore) MsgDeleteHistoryRange(in *msg.TLMsgDeleteHistoryRange) (*mtproto.Messages_AffectedHistory, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	if peerType != mtproto.PEER_USER && peerType != mtproto.PEER_CHAT {
		c.Logger.Errorf("msg.deleteHistoryRange - error: invalid peer %d/%d", in.PeerType, in.PeerId)
		return nil, mtproto.ErrPeerIdInvalid
	}
	saved := in.SavedPeerType != mtproto.PEER_EMPTY
	if saved && (peerType != mtproto.PEER_USER || peerId != in.UserId) {
		c.Logger.Errorf("msg.deleteHistoryRange - error: a sub-chat of %d/%d", peerType, peerId)
		return nil, mtproto.ErrPeerIdInvalid
	}
	r := dao.Range{
		PeerType:      peerType,
		PeerId:        peerId,
		SavedPeerType: in.SavedPeerType,
		SavedPeerId:   in.SavedPeerId,
		MaxId:         in.MaxId,
		MinDate:       in.MinDate,
		MaxDate:       in.MaxDate,
	}

	// The messages of a private chat go from the other user's box too; in
	// a basic group, messages.deleteHistory takes them from the user's box
	// only.
	revoke := mtproto.FromBool(in.Revoke) && peerType == mtproto.PEER_USER && peerId != in.UserId
	var recipients map[int64][]int64
	if revoke {
		var err error
		if recipients, err = c.dialogRecipients(in.UserId, []mtproto.DialogID{mtproto.MakeDialogId(in.UserId, peerType, peerId)}); err != nil {
			c.Logger.Errorf("msg.deleteHistoryRange - error: %v", err)
			return nil, err
		}
	}

	var left int32
	res, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId,
		[]string{dialog.GetDialogCacheKeyByPeer(in.UserId, peerType, peerId)},
		func(b *userbox.Box) error {
			left = 0
			deleted, err := c.svcCtx.Dao.DeleteRangePortion(b, r)
			if err != nil || deleted == nil {
				return err
			}
			left = deleted.Left
			if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, in.UserId, in.AuthKeyId, mtproto.MakeUpdatesByUpdates(deleted.Update)); err != nil {
				return err
			}
			if !revoke {
				return nil
			}
			return c.emitDeleted(b, in.UserId, deleted, recipients)
		})
	if err != nil {
		c.Logger.Errorf("msg.deleteHistoryRange - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{
		Pts:      res.State.Pts,
		PtsCount: ptsCount(res),
		Offset:   left,
	}).To_Messages_AffectedHistory(), nil
}

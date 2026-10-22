/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package core

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgDeletePhoneCallHistory
// msg.deletePhoneCallHistory user_id:long auth_key_id:long revoke:Bool = messages.AffectedFoundMessages;
//
// It deletes a portion of the call history; offset above 0 asks the client
// to repeat the call.
func (c *MsgCore) MsgDeletePhoneCallHistory(in *msg.TLMsgDeletePhoneCallHistory) (*mtproto.Messages_AffectedFoundMessages, error) {
	var (
		ids  []int32
		left int32
	)
	res, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		ids, left = nil, 0
		deleted, err := c.svcCtx.Dao.DeletePhoneCallPortion(b)
		if err != nil || deleted == nil {
			return err
		}
		ids, left = deleted.Ids(), deleted.Left
		if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, in.UserId, in.AuthKeyId, mtproto.MakeUpdatesByUpdates(deleted.Update)); err != nil {
			return err
		}
		if !in.Revoke {
			return nil
		}
		// Calls are in private chats: the recipient of each dialog is its peer.
		recipients := map[int64][]int64{}
		for _, dd := range deleted.ByDialog(in.UserId) {
			if dd.PeerType == mtproto.PEER_USER && dd.PeerId != in.UserId {
				recipients[mtproto.MakePeerDialogId(dd.PeerType, dd.PeerId)] = []int64{dd.PeerId}
			}
		}
		return c.emitDeleted(b, in.UserId, deleted, recipients)
	})
	if err != nil {
		c.Logger.Errorf("msg.deletePhoneCallHistory - error: %v", err)
		return nil, err
	}
	if ids == nil {
		ids = []int32{}
	}
	return mtproto.MakeTLMessagesAffectedFoundMessages(&mtproto.Messages_AffectedFoundMessages{
		Pts:      res.State.Pts,
		PtsCount: ptsCount(res),
		Offset:   left,
		Messages: ids,
	}).To_Messages_AffectedFoundMessages(), nil
}

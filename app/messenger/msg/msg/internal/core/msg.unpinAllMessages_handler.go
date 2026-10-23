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
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgUnpinAllMessages
// msg.unpinAllMessages user_id:long auth_key_id:long peer_type:int peer_id:long = messages.AffectedHistory;
//
// It unpins a portion of the dialog's pinned messages; offset above 0 asks
// the client to repeat the call. The other members unpin theirs.
func (c *MsgCore) MsgUnpinAllMessages(in *msg.TLMsgUnpinAllMessages) (*mtproto.Messages_AffectedHistory, error) {
	peerType, peerId := selfPeer(in.UserId, in.PeerType, in.PeerId)
	if peerType != mtproto.PEER_USER && peerType != mtproto.PEER_CHAT {
		c.Logger.Errorf("msg.unpinAllMessages - error: invalid peer %d/%d", in.PeerType, in.PeerId)
		return nil, mtproto.ErrPeerIdInvalid
	}
	recipients, err := c.recipients(in.UserId, peerType, peerId)
	if err != nil {
		c.Logger.Errorf("msg.unpinAllMessages - error: %v", err)
		return nil, err
	}

	var left int32
	res, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId,
		[]string{dialog.GetDialogCacheKeyByPeer(in.UserId, peerType, peerId)},
		func(b *userbox.Box) error {
			left = 0
			unpinned, err := c.svcCtx.Dao.UnpinPortion(b, peerType, peerId)
			if err != nil || unpinned == nil {
				return err
			}
			left = unpinned.Left
			if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, in.UserId, in.AuthKeyId, mtproto.MakeUpdatesByUpdates(unpinned.Update)); err != nil {
				return err
			}
			for _, to := range recipients {
				if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxUnpinAllMessages{
					UserId:   to,
					FromId:   in.UserId,
					PeerType: peerType,
					PeerId:   peerId,
				}); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		c.Logger.Errorf("msg.unpinAllMessages - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{
		Pts:      res.State.Pts,
		PtsCount: ptsCount(res),
		Offset:   left,
	}).To_Messages_AffectedHistory(), nil
}

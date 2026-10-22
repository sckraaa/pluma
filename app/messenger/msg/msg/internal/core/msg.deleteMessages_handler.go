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
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgDeleteMessages
// msg.deleteMessages user_id:long auth_key_id:long peer_type:int peer_id:long revoke:Bool id:Vector<int> = messages.AffectedMessages;
func (c *MsgCore) MsgDeleteMessages(in *msg.TLMsgDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	if in.UserId == 0 {
		c.Logger.Errorf("msg.deleteMessages - error: no user")
		return nil, mtproto.ErrInputRequestInvalid
	}
	if in.PeerType != mtproto.PEER_EMPTY {
		c.Logger.Errorf("msg.deleteMessages - error: invalid peer %d/%d", in.PeerType, in.PeerId)
		return nil, mtproto.ErrPeerIdInvalid
	}
	if len(in.Id) == 0 {
		st, err := c.svcCtx.Dao.ReadState(c.ctx, in.UserId)
		if err != nil {
			return nil, err
		}
		return mtproto.MakeTLMessagesAffectedMessages(&mtproto.Messages_AffectedMessages{
			Pts: st.Pts,
		}).To_Messages_AffectedMessages(), nil
	}

	r, err := c.deleteMessages(in)
	if err != nil {
		c.Logger.Errorf("msg.deleteMessages - error: %v", err)
		return nil, err
	}
	return r, nil
}

func (c *MsgCore) deleteMessages(in *msg.TLMsgDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	// The dialogs of the messages, to know who else gets the deletion. A
	// message never moves to another dialog, so this read outside the
	// transaction stays true for the messages deleted in it.
	rows, err := c.svcCtx.Dao.MessagesDAO.SelectByMessageIdList(c.ctx, in.UserId, in.Id)
	if err != nil {
		return nil, err
	}
	var (
		dialogs []mtproto.DialogID
		keys    []string
	)
	for _, r := range rows {
		did := mtproto.DialogID{A: r.DialogId1, B: r.DialogId2}
		if !slices.Contains(dialogs, did) {
			dialogs = append(dialogs, did)
			peer := mtproto.FromPeer(mtproto.GetPeerByDialogId(in.UserId, did))
			keys = append(keys, dialog.GetDialogCacheKeyByPeer(in.UserId, peer.PeerType, peer.PeerId))
		}
	}
	var recipients map[int64][]int64
	if in.Revoke {
		if recipients, err = c.dialogRecipients(in.UserId, dialogs); err != nil {
			return nil, err
		}
	}

	res, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, keys, func(b *userbox.Box) error {
		deleted, err := c.svcCtx.Dao.DeleteMessages(b, in.Id)
		if err != nil || deleted == nil {
			return err
		}
		if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, in.UserId, in.AuthKeyId, mtproto.MakeUpdatesByUpdates(deleted.Update)); err != nil {
			return err
		}
		if !in.Revoke {
			return nil
		}
		return c.emitDeleted(b, in.UserId, deleted, recipients)
	})
	if err != nil {
		return nil, err
	}
	return affectedMessages(res), nil
}

// emitDeleted adds, for each dialog of the deleted messages, an inbox event
// to each of its recipients with the dialog_message_id of the messages.
func (c *MsgCore) emitDeleted(b *userbox.Box, userId int64, deleted *dao.Deleted, recipients map[int64][]int64) error {
	for _, dd := range deleted.ByDialog(userId) {
		for _, to := range recipients[mtproto.MakePeerDialogId(dd.PeerType, dd.PeerId)] {
			if err := c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxDeleteMessagesToInbox{
				UserId:   to,
				FromId:   userId,
				PeerType: dd.PeerType,
				PeerId:   dd.PeerId,
				Id:       dd.DialogMessageIds,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

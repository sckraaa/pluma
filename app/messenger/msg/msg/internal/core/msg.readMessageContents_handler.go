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
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgReadMessageContents
// msg.readMessageContents user_id:long auth_key_id:long peer_type:int peer_id:long id:Vector<ContentMessage> = messages.AffectedMessages;
//
// The flags of the messages are read in the box transaction and cleared
// only where set: one updateReadMessagesContents with the ids changed, none
// when nothing changed. The senders of opened media learn it with inbox
// events.
func (c *MsgCore) MsgReadMessageContents(in *msg.TLMsgReadMessageContents) (*mtproto.Messages_AffectedMessages, error) {
	ids := make([]int32, 0, len(in.Id))
	for _, m := range in.Id {
		ids = append(ids, m.Id)
	}

	res, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		read, err := c.svcCtx.Dao.ReadContents(b, ids)
		if err != nil || read == nil {
			return err
		}
		if err = c.svcCtx.Dao.EmitUpdatesNotMe(b, in.UserId, in.AuthKeyId, mtproto.MakeUpdatesByUpdates(read.Update)); err != nil {
			return err
		}
		for _, m := range read.Media {
			if err = c.svcCtx.Dao.EmitInbox(b, &inbox.TLInboxReadMediaUnreadToInboxV2{
				UserId:   m.SenderId,
				FromId:   in.UserId,
				PeerType: m.PeerType,
				PeerId:   m.PeerId,
				Id:       m.DialogMessageIds,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		c.Logger.Errorf("msg.readMessageContents - error: %v", err)
		return nil, err
	}
	return affectedMessages(res), nil
}

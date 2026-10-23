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
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/plugin"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// MsgPushUserMessage
// msg.pushUserMessage user_id:long auth_key_id:long (UserMessage) = Bool;
//
// push_type 0 is a regular send. Any other type delivers the message to the
// peer only: the sender (a service account) keeps no copy.
func (c *MsgCore) MsgPushUserMessage(in *msg.TLMsgPushUserMessage) (*mtproto.Bool, error) {
	peer := mtproto.MakePeerUtil(in.PeerType, in.PeerId)
	if !peer.IsUser() {
		c.Logger.Errorf("msg.pushUserMessage - error: peer %d/%d is not a user", in.PeerType, in.PeerId)
		return mtproto.BoolFalse, nil
	}

	if in.PushType == 0 {
		if _, err := c.sendUserMessages(in.UserId, 0, peer.PeerId, []*msg.OutboxMessage{in.Message}); err != nil {
			c.Logger.Errorf("msg.pushUserMessage - error: %v", err)
			return nil, err
		}
		return mtproto.BoolTrue, nil
	}

	idHelper := mtproto.NewIDListHelper(in.UserId, peer.PeerId)
	idHelper.PickByMessage(in.GetMessage().GetMessage())
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
		Id: idHelper.UserIdList,
		To: []int64{peer.PeerId},
	})
	if err != nil {
		c.Logger.Errorf("msg.pushUserMessage - error: %v", err)
		return nil, err
	}
	if sender, _ := users.GetImmutableUser(in.UserId); sender == nil || sender.Deleted() {
		return nil, mtproto.ErrInputUserDeactivated
	}
	if peerUser, _ := users.GetImmutableUser(peer.PeerId); peerUser == nil || peerUser.Deleted() {
		return nil, mtproto.ErrInputUserDeactivated
	}

	message := plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, in.Message.Message, in.UserId, false, func() bool {
		hasBot := false
		users.Visit(func(it *mtproto.ImmutableUser) {
			if it.IsBot() {
				hasBot = true
			}
		})
		return hasBot
	})
	dialogMessageId, err := c.svcCtx.Dao.NextId(c.ctx)
	if err != nil {
		return nil, err
	}

	// No box of the sender changes: the event is the whole operation, and a
	// failure to write it is the operation's failure.
	ev, err := c.svcCtx.Dao.InboxEvent(&inbox.TLInboxSendUserMessageToInboxV2{
		UserId:   peer.PeerId,
		FromId:   in.UserId,
		PeerType: mtproto.PEER_USER,
		PeerId:   peer.PeerId,
		BoxList: []*mtproto.MessageBox{{
			UserId:            in.UserId,
			SenderUserId:      in.UserId,
			PeerType:          mtproto.PEER_USER,
			PeerId:            peer.PeerId,
			RandomId:          in.Message.RandomId,
			DialogMessageId:   dialogMessageId,
			MessageFilterType: mtproto.GetMediaType(message),
			Message:           message,
		}},
		Users: users.GetUserListByIdList(peer.PeerId, idHelper.UserIdList...),
	})
	if err != nil {
		return nil, err
	}
	if err = c.svcCtx.Dao.InsertEvent(c.ctx, ev); err != nil {
		c.Logger.Errorf("msg.pushUserMessage - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

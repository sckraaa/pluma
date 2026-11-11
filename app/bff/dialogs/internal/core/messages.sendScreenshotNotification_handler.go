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
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// MessagesSendScreenshotNotification
// messages.sendScreenshotNotification#a1405817 peer:InputPeer reply_to:InputReplyTo random_id:long = Updates;
// messages.sendScreenshotNotification#c97df020 peer:InputPeer reply_to_msg_id:int random_id:long = Updates;
//
// The service message "took a screenshot" in a private chat, replying to the
// message on the screen (Android SendMessagesHelper.java:2026).
func (c *DialogsCore) MessagesSendScreenshotNotification(in *mtproto.TLMessagesSendScreenshotNotification) (*mtproto.Updates, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	if !peer.IsUser() {
		return nil, mtproto.ErrPeerIdInvalid
	}
	replyTo := in.GetReplyToMsgId()
	if in.GetReplyTo() != nil {
		replyTo = in.GetReplyTo().GetReplyToMsgId()
	}
	message := &mtproto.Message{
		Out:    true,
		FromId: mtproto.MakePeerUser(c.MD.UserId),
		PeerId: peer.ToPeer(),
		Date:   int32(time.Now().Unix()),
		Action: mtproto.MakeTLMessageActionScreenshotTaken(nil).To_MessageAction(),
	}
	if replyTo != 0 {
		message.ReplyTo = mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
			ReplyToMsgId_FLAGINT32: wrapperspb.Int32(replyTo),
		}).To_MessageReplyHeader()
	}
	r, err := c.svcCtx.Dao.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
		Message: []*msgpb.OutboxMessage{msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: true,
			RandomId:  in.GetRandomId(),
			Message:   mtproto.MakeTLMessageService(message).To_Message(),
		}).To_OutboxMessage()},
	})
	if err != nil {
		c.Logger.Errorf("messages.sendScreenshotNotification - error: %v", err)
		return nil, err
	}
	return r, nil
}

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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/readmarks"

	"google.golang.org/grpc/status"
)

// errMessageTooOld: the message was sent more than pm_read_date_expire_period
// ago, and its read date is gone, or it was read before the dates were
// kept: "the requested information is not available"
// (core.telegram.org/method/messages.getOutboxReadDate; tdesktop shows it
// as such, api_who_reacted.cpp).
var errMessageTooOld = status.Error(mtproto.ErrBadRequest, "MESSAGE_TOO_OLD")

// MessagesGetOutboxReadDate
// messages.getOutboxReadDate#8c4bfe5d peer:InputPeer msg_id:int = OutboxReadDate;
func (c *MessagesCore) MessagesGetOutboxReadDate(in *mtproto.TLMessagesGetOutboxReadDate) (*mtproto.OutboxReadDate, error) {
	// Possible errors
	// Code	Type	Description
	// 400	MESSAGE_ID_INVALID	The provided message id is invalid.
	// 400	MESSAGE_NOT_READ_YET	The specified message wasn't read yet.
	// 400	MESSAGE_TOO_OLD	The message is too old, the requested information is not available.
	// 400	PEER_ID_INVALID	The provided peer id is invalid.
	// 403	USER_PRIVACY_RESTRICTED	The user's privacy settings do not allow you to do this.
	// 403	YOUR_PRIVACY_RESTRICTED	You cannot fetch the read date of this message because you have disallowed other users to do so for your messages; to fix, allow other users to see your exact last online date OR purchase a Telegram Premium subscription.

	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	if !peer.IsUser() {
		c.Logger.Errorf("messages.getOutboxReadDate - only user peer is supported")
		return nil, mtproto.ErrPeerIdInvalid
	}

	// TODO: 1. Check USER_PRIVACY_RESTRICTED
	//		 2. Check YOUR_PRIVACY_RESTRICTED

	msgBox, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessage(c.ctx, &message.TLMessageGetUserMessage{
		UserId: c.MD.UserId,
		Id:     in.MsgId,
	})
	if err != nil {
		c.Logger.Errorf("messages.getOutboxReadDate - error: %v", err)
		return nil, mtproto.ErrMessageIdInvalid
	}
	// Only the user's own messages are read by the peer; the dates kept
	// are of those.
	if msgBox.SenderUserId != c.MD.UserId {
		return nil, mtproto.ErrMessageIdInvalid
	}
	if msgBox.PeerType != mtproto.PEER_USER || msgBox.GetPeerId() != peer.PeerId {
		c.Logger.Errorf("messages.getOutboxReadDate - only user peer is supported")
		return nil, mtproto.ErrPeerIdInvalid
	}
	if readmarks.TooOld(int64(msgBox.GetMessage().GetDate()), time.Now().Unix(), readmarks.PrivateExpirePeriod) {
		return nil, errMessageTooOld
	}

	rList, err := c.svcCtx.Dao.MessageClient.MessageGetOutboxReadDate(c.ctx, &message.TLMessageGetOutboxReadDate{
		UserId:   c.MD.UserId,
		PeerType: peer.PeerType,
		PeerId:   peer.PeerId,
		MsgId:    in.MsgId,
	})
	if err != nil {
		c.Logger.Errorf("messages.getOutboxReadDate - error: %v", err)
		return nil, err
	}
	if len(rList.GetDatas()) == 0 {
		return nil, mtproto.ErrMessageNotReadYet
	}
	date := rList.GetDatas()[0].GetDate()
	if date == 0 {
		return nil, errMessageTooOld
	}

	return mtproto.MakeTLOutboxReadDate(&mtproto.OutboxReadDate{
		Date: date,
	}).To_OutboxReadDate(), nil
}

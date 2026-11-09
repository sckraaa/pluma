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
	"math/rand"
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/pkg/historyttl"
)

// MessagesSetHistoryTTL
// messages.setHistoryTTL#b80e5fe4 peer:InputPeer period:int = Updates;
//
// The auto-delete period of the messages sent to the chat from now on (task
// 063; core.telegram.org/method/messages.setHistoryTTL). In a private chat
// either side sets it; in a group or a channel, whoever may change its info
// (tdesktop TTLValidator::can). A service message tells the chat, and
// updatePeerHistoryTTL gives the devices the new period.
func (c *DialogsCore) MessagesSetHistoryTTL(in *mtproto.TLMessagesSetHistoryTTL) (*mtproto.Updates, error) {
	period := in.GetPeriod()
	if !historyttl.Valid(period) {
		return nil, mtproto.ErrTtlPeriodInvalid
	}
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	switch {
	case peer.PeerType == mtproto.PEER_CHANNEL:
		r, err := c.svcCtx.Dao.Channel.ChannelSetHistoryTTL(c.ctx, &channelpb.TLChannelSetHistoryTTL{
			SelfId:        c.MD.UserId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			ChannelId:     peer.PeerId,
			TtlPeriod:     period,
		})
		if err != nil {
			c.Logger.Errorf("messages.setHistoryTTL - error: %v", err)
			return nil, err
		}
		return r, nil
	case peer.PeerType == mtproto.PEER_CHAT:
		// The period goes first: the message about it can be sent again,
		// a period lost behind a sent message cannot be noticed.
		if _, err := c.svcCtx.Dao.ChatClient.ChatSetHistoryTTL(c.ctx, &chatpb.TLChatSetHistoryTTL{
			SelfId:    c.MD.UserId,
			ChatId:    peer.PeerId,
			TtlPeriod: period,
		}); err != nil {
			c.Logger.Errorf("messages.setHistoryTTL - error: %v", err)
			return nil, err
		}
	case peer.PeerType == mtproto.PEER_USER && peer.PeerId != c.MD.UserId:
	default:
		// Saved Messages have no period (tdesktop TTLValidator::can, !isSelf).
		return nil, mtproto.ErrPeerIdInvalid
	}

	r, err := c.svcCtx.Dao.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
		Message: []*msgpb.OutboxMessage{msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: true,
			RandomId:  rand.Int63(), //nolint:gosec // G404: a random_id is unique, not secret
			Message: mtproto.MakeTLMessageService(&mtproto.Message{
				Out:    true,
				FromId: mtproto.MakePeerUser(c.MD.UserId),
				PeerId: peer.ToPeer(),
				Date:   int32(time.Now().Unix()),
				Action: historyttl.Action(period, 0),
			}).To_Message(),
		}).To_OutboxMessage()},
	})
	if err != nil {
		c.Logger.Errorf("messages.setHistoryTTL - error: %v", err)
		return nil, err
	}
	return r, nil
}

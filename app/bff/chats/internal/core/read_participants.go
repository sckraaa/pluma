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
	"errors"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/readmarks"
)

// readParticipants are the members of a basic group who read the user's
// message msgId in it, with the dates they read it, the latest first
// (messages.getMessageReadParticipants). The dates are kept for the user's
// own messages of the last readmarks.ChatExpirePeriod in groups of up to
// readmarks.ChatSizeThreshold members (core.telegram.org/api/config), the
// limits by which the clients offer the list (Android ChatActivity,
// showMessageSeen); a member who read it before the dates were kept has
// the date 0, as every reader had before. Members who left are not listed.
func (c *ChatsCore) readParticipants(inPeer *mtproto.InputPeer, msgId int32) ([]*mtproto.ReadParticipantDate, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, inPeer)
	switch peer.PeerType {
	case mtproto.PEER_CHAT:
	case mtproto.PEER_CHANNEL:
		c.Logger.Errorf("messages.getMessageReadParticipants blocked, License key from https://teamgram.net required to unlock enterprise features.")
		return nil, mtproto.ErrEnterpriseIsBlocked
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}

	msgBox, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessage(c.ctx, &message.TLMessageGetUserMessage{
		UserId: c.MD.UserId,
		Id:     msgId,
	})
	if errors.Is(err, mtproto.ErrMessageIdInvalid) {
		return nil, mtproto.ErrMsgIdInvalid
	} else if err != nil {
		c.Logger.Errorf("messages.getMessageReadParticipants - error: %v", err)
		return nil, err
	}
	if msgBox.PeerType != mtproto.PEER_CHAT || msgBox.PeerId != peer.PeerId || msgBox.SenderUserId != c.MD.UserId {
		return nil, mtproto.ErrMsgIdInvalid
	}
	if readmarks.TooOld(int64(msgBox.GetMessage().GetDate()), time.Now().Unix(), readmarks.ChatExpirePeriod) {
		return nil, mtproto.ErrMsgTooOld
	}

	members, err := c.svcCtx.Dao.ChatClient.Client().ChatGetChatParticipantIdList(c.ctx, &chatpb.TLChatGetChatParticipantIdList{
		ChatId: peer.PeerId,
	})
	if err != nil {
		c.Logger.Errorf("messages.getMessageReadParticipants - error: %v", err)
		return nil, err
	}
	if !slices.Contains(members.GetDatas(), c.MD.UserId) {
		return nil, mtproto.ErrPeerIdInvalid
	}
	if len(members.GetDatas()) > readmarks.ChatSizeThreshold {
		return nil, mtproto.ErrChatTooBig
	}

	dates, err := c.svcCtx.Dao.MessageClient.MessageGetOutboxReadDate(c.ctx, &message.TLMessageGetOutboxReadDate{
		UserId:   c.MD.UserId,
		PeerType: mtproto.PEER_CHAT,
		PeerId:   peer.PeerId,
		MsgId:    msgId,
	})
	if err != nil {
		c.Logger.Errorf("messages.getMessageReadParticipants - error: %v", err)
		return nil, err
	}
	readers := make([]*mtproto.ReadParticipantDate, 0, len(dates.GetDatas()))
	for _, d := range dates.GetDatas() {
		if d.GetUserId() != c.MD.UserId && slices.Contains(members.GetDatas(), d.GetUserId()) {
			readers = append(readers, d)
		}
	}
	return readers, nil
}

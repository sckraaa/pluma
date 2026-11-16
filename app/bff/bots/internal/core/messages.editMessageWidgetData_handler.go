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

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
)

// MessagesEditMessageWidgetData
// messages.editMessageWidgetData#f624bc1e flags:# peer:InputPeer id:int public_data:flags.0?string chat_data:flags.1?string = Updates;
//
// The bot writes the state of the widget of a message it sent, by the
// message as it knows it, "only the bot that sent it and only the
// original" (Android jni/twidget/widget-sandbox/docs/tgw-format.md, "The
// bot's edits of the state"): a forward or a message of another is
// MESSAGE_ID_INVALID. The edit (widgetEdit) goes through msg as the bot's
// own edit of the message, silent: every holder of the message gets
// updateMessageWidgetData in a private chat or a basic group, and
// updateEditChannelMessage in a supergroup, "the usual edit of channel
// posts"; the answer has the bot's.
func (c *BotsCore) MessagesEditMessageWidgetData(in *mtproto.TLMessagesEditMessageWidgetData) (*mtproto.Updates, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	peer := c.chatPeer(in.Peer)
	box, err := c.ownWidgetMessage(peer, in.Id)
	if err != nil {
		c.Logger.Errorf("messages.editMessageWidgetData - error: %v", err)
		return nil, err
	}
	edited, err := c.widgetEdit(box.GetMessage(), in.GetPublicData(), in.GetChatData())
	if err != nil {
		return nil, err
	}
	updates, err := c.svcCtx.MsgEditMessageV2(c.ctx, &msgpb.TLMsgEditMessageV2{
		UserId:     c.MD.UserId,
		AuthKeyId:  c.MD.PermAuthKeyId,
		PeerType:   peer.PeerType,
		PeerId:     peer.PeerId,
		EditType:   msgpb.EditTypeNormal,
		NewMessage: msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{NoWebpage: true, Message: edited}).To_OutboxMessage(),
		DstMessage: box,
	})
	if err != nil {
		c.Logger.Errorf("messages.editMessageWidgetData - error: %v", err)
		return nil, err
	}
	return updates, nil
}

// ownWidgetMessage is message id of the chat as the calling bot names it,
// when the bot sent it itself: of its box in a private chat or a basic
// group, of the channel in a supergroup. MESSAGE_ID_INVALID for none, a
// message of another, sent via a bot, or forwarded.
func (c *BotsCore) ownWidgetMessage(peer *mtproto.PeerUtil, id int32) (*mtproto.MessageBox, error) {
	var box *mtproto.MessageBox
	switch {
	case peer.IsUser(), peer.IsChat():
		b, err := c.svcCtx.MessageGetUserMessage(c.ctx, &message.TLMessageGetUserMessage{UserId: c.MD.UserId, Id: id})
		switch {
		case errors.Is(err, mtproto.ErrMessageIdInvalid), errors.Is(err, mtproto.ErrMsgIdInvalid):
			return nil, mtproto.ErrMessageIdInvalid
		case err != nil:
			return nil, err
		}
		if (mtproto.DialogID{A: b.GetDialogId1(), B: b.GetDialogId2()}) != mtproto.MakeDialogId(c.MD.UserId, peer.PeerType, peer.PeerId) ||
			b.GetSenderUserId() != c.MD.UserId {
			return nil, mtproto.ErrMessageIdInvalid
		}
		box = b
	case peer.IsChannel():
		author, err := c.svcCtx.ChannelPostAuthor(c.ctx, peer.PeerId, id)
		if err != nil {
			return nil, err
		}
		if author != c.MD.UserId {
			return nil, mtproto.ErrMessageIdInvalid
		}
		messages, err := c.svcCtx.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: peer.PeerId, Id: []int32{id}})
		switch {
		case errors.Is(err, mtproto.ErrChannelPrivate), errors.Is(err, mtproto.ErrChannelInvalid):
			return nil, mtproto.ErrMessageIdInvalid
		case err != nil:
			return nil, err
		}
		if len(messages.GetDatas()) != 1 {
			return nil, mtproto.ErrMessageIdInvalid
		}
		box = &mtproto.MessageBox{
			UserId: c.MD.UserId, MessageId: id, SenderUserId: c.MD.UserId,
			PeerType: mtproto.PEER_CHANNEL, PeerId: peer.PeerId, Message: messages.GetDatas()[0],
		}
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
	if m := box.GetMessage(); m.GetFwdFrom() != nil || m.GetViaBotId().GetValue() != 0 {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return box, nil
}

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
	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"

	"google.golang.org/protobuf/proto"
)

// MessagesEditInlineBotMessageWidgetData
// messages.editInlineBotMessageWidgetData#6396ce2b flags:# id:InputBotInlineMessageID public_data:flags.0?string chat_data:flags.1?string = Bool;
//
// The bot writes the state of the widget of a message a user sent via it,
// by its inline message id, which the bot got with the widget's request
// (updateInlineBotWidgetQuery); "chat_data is allowed there, a via-bot
// message is an original" (Android jni/twidget/widget-sandbox/docs/tgw-format.md,
// "The bot's edits of the state"). The message is found and edited as by
// messages.editInlineBotMessage: in the sender's name (msg.EditTypeViaBot),
// silent, every holder getting the edit of the state as for
// messages.editMessageWidgetData.
func (c *BotsCore) MessagesEditInlineBotMessageWidgetData(in *mtproto.TLMessagesEditInlineBotMessageWidgetData) (*mtproto.Bool, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	found, err := c.findInlineMessage(in.GetId())
	if err == nil {
		found, err = c.senderCopy(found)
	}
	if err != nil {
		c.Logger.Errorf("messages.editInlineBotMessageWidgetData - error: %v", err)
		return nil, err
	}
	stored := found.message()
	if stored.GetViaBotId().GetValue() != c.MD.UserId || stored.GetFwdFrom() != nil {
		return nil, mtproto.ErrMessageIdInvalid
	}
	edited, err := c.widgetEdit(stored, in.GetPublicData(), in.GetChatData())
	if err != nil {
		return nil, err
	}
	if _, err = c.svcCtx.MsgEditMessageV2(c.ctx, &msgpb.TLMsgEditMessageV2{
		UserId:     found.owner,
		AuthKeyId:  0,
		PeerType:   found.peer.PeerType,
		PeerId:     found.peer.PeerId,
		EditType:   msgpb.EditTypeViaBot,
		NewMessage: msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{NoWebpage: true, Message: edited}).To_OutboxMessage(),
		DstMessage: proto.Clone(found.box).(*mtproto.MessageBox),
	}); err != nil {
		c.Logger.Errorf("messages.editInlineBotMessageWidgetData - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

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

// MessagesEditInlineBotMessage
// messages.editInlineBotMessage#a423bb51 flags:# no_webpage:flags.1?true invert_media:flags.16?true id:InputBotInlineMessageID message:flags.11?string media:flags.14?InputMedia reply_markup:flags.2?ReplyMarkup entities:flags.3?Vector<MessageEntity> rich_message:flags.23?InputRichMessage = Bool;
//
// The bot edits a message a user sent via it (core.telegram.org/method/
// messages.editInlineBotMessage, "Edit an inline bot message"; task 106,
// ADR 0068 p. 7): the message the id names (inline_message.go) must have
// the bot in via_bot_id and be the user's own, not a forward of it;
// MESSAGE_ID_INVALID otherwise. The edit is the sender's: msg writes it in
// the sender's box and the other copies (msg.EditTypeViaBot), every
// session of the sender gets it (none of them asked), and the others get
// updateEditMessage or updateEditChannelMessage as for any edit.
func (c *BotsCore) MessagesEditInlineBotMessage(in *mtproto.TLMessagesEditInlineBotMessage) (*mtproto.Bool, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	found, err := c.findInlineMessage(in.GetId())
	if err == nil {
		found, err = c.senderCopy(found)
	}
	if err != nil {
		c.Logger.Errorf("messages.editInlineBotMessage - error: %v", err)
		return nil, err
	}
	stored := found.message()
	if stored.GetPredicateName() != mtproto.Predicate_message || stored.GetViaBotId().GetValue() != c.MD.UserId || stored.GetFwdFrom() != nil {
		return nil, mtproto.ErrMessageIdInvalid
	}
	edited, err := c.inlineEdit(stored, in)
	if err != nil {
		return nil, err
	}

	if _, err = c.svcCtx.MsgEditMessageV2(c.ctx, &msgpb.TLMsgEditMessageV2{
		UserId:    found.owner,
		AuthKeyId: 0,
		PeerType:  found.peer.PeerType,
		PeerId:    found.peer.PeerId,
		EditType:  msgpb.EditTypeViaBot,
		NewMessage: msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: in.GetNoWebpage(),
			Message:   edited,
		}).To_OutboxMessage(),
		DstMessage: proto.Clone(found.box).(*mtproto.MessageBox),
	}); err != nil {
		c.Logger.Errorf("messages.editInlineBotMessage - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

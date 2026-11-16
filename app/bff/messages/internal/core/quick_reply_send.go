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

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// A send with quick_reply_shortcut adds its messages to a quick reply
// shortcut of the account instead of sending them (task 105,
// core.telegram.org/api/business#quick-reply-shortcuts): "send messages to
// inputPeerSelf, specifying the shortcut". The messages are made as any
// other — the text, the entities, the media uploaded and checked — and
// service msg keeps them (msg.addQuickReplyMessages).

// checkQuickReplySend checks a send to a shortcut before anything is made
// of it: a bot has no shortcuts (QUICK_REPLIES_BOT_NOT_ALLOWED, "Quick
// replies cannot be used by bots", core.telegram.org/method/
// messages.sendMessage and messages.sendMedia); the peer must be the account itself
// (PEER_ID_INVALID otherwise); a shortcut has no time to go out at
// (SCHEDULE_DATE_INVALID; Telegram names no error for both together, not
// verified). A send without a shortcut passes.
func (c *MessagesCore) checkQuickReplySend(peer *mtproto.PeerUtil, shortcut *mtproto.InputQuickReplyShortcut, scheduleDate *wrapperspb.Int32Value) error {
	switch {
	case shortcut == nil:
		return nil
	case c.MD.IsBot:
		return mtproto.ErrQuickRepliesBotNotAllowed
	case !peer.IsSelfUser(c.MD.UserId):
		return mtproto.ErrPeerIdInvalid
	case scheduleDate != nil:
		return mtproto.ErrScheduleDateInvalid
	}
	return nil
}

// addQuickReplyMessages adds the messages made for a send to the shortcut.
// The answer is the shortcut's updates (updateMessageID,
// updateQuickReplyMessage, updateNewQuickReply for a shortcut created).
func (c *MessagesCore) addQuickReplyMessages(shortcut *mtproto.InputQuickReplyShortcut, list []*msgpb.OutboxMessage) (*mtproto.Updates, error) {
	return c.svcCtx.Dao.MsgClient.MsgAddQuickReplyMessages(c.ctx, &msgpb.TLMsgAddQuickReplyMessages{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		Shortcut:  shortcut,
		Message:   list,
	})
}

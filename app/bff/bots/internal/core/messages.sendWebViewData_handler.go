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
	"time"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

const (
	// maxWebViewData bounds the data of messages.sendWebViewData
	// (core.telegram.org/bots/webapps, Telegram.WebApp.sendData: "up to
	// 4096 bytes").
	maxWebViewData = 4096
	// webViewDataButtons is how many of the bot's latest messages in the
	// chat are searched for the keyboard button of the mini app (task 106).
	webViewDataButtons = 50
)

// MessagesSendWebViewData
// messages.sendWebViewData#dc0242c8 bot:InputUser random_id:long button_text:string data:string = Updates;
//
// sendData of a mini app opened from a keyboard button
// (keyboardButtonSimpleWebView, core.telegram.org/bots/webapps): the clients
// send it once, only from such an app in the private chat with its bot, and
// close the app (tdesktop inline_bots/bot_attach_web_view.cpp:1849-1866,
// Android ui/bots/BotWebViewAttachedSheet.java:476-490, iOS
// TelegramEngine/Messages/BotWebView.swift:343-361). It is one service
// message from the user to the bot whose two copies differ: the user's is
// messageActionWebViewDataSent(text), the bot's
// messageActionWebViewDataSentMe(text, data) (OutboxMessage.incoming_action
// of msg), so the data reaches only the bot. button_text must be the text
// of such a button in one of the bot's latest messages of the chat
// (BUTTON_TEXT_INVALID otherwise, this server's choice: the method's errors
// are not documented). random_id makes a repeat the same message, as for
// messages.sendMessage.
func (c *BotsCore) MessagesSendWebViewData(in *mtproto.TLMessagesSendWebViewData) (*mtproto.Updates, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	bot, err := c.resolveBot(in.GetBot())
	if err != nil {
		return nil, err
	}
	data := in.GetData()
	if len(data) > maxWebViewData {
		return nil, mtproto.ErrDataTooLong
	}
	// The page cannot send empty data or more than 4096 bytes
	// (telegram.org/js/telegram-web-app.js, sendData: "Data is required",
	// "Data is too long", read 2026-11-16); data that is not UTF-8 cannot
	// be a string of the message.
	if data == "" || !utf8.ValidString(data) {
		return nil, mtproto.ErrDataInvalid
	}
	ok, err := c.webViewButtonShown(bot.Id(), in.GetButtonText())
	if err != nil {
		c.Logger.Errorf("messages.sendWebViewData - buttons of bot %d: %v", bot.Id(), err)
		return nil, mtproto.ErrInternalServerError
	}
	if !ok {
		return nil, mtproto.ErrButtonTextInvalid
	}

	updates, err := c.svcCtx.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  mtproto.PEER_USER,
		PeerId:    bot.Id(),
		Message: []*msgpb.OutboxMessage{msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: true,
			RandomId:  in.GetRandomId(),
			Message: mtproto.MakeTLMessageService(&mtproto.Message{
				Out:    true,
				FromId: mtproto.MakePeerUser(c.MD.UserId),
				PeerId: mtproto.MakePeerUser(bot.Id()),
				Date:   int32(time.Now().Unix()),
				Action: mtproto.MakeTLMessageActionWebViewDataSent(&mtproto.MessageAction{
					Text: in.GetButtonText(),
				}).To_MessageAction(),
			}).To_Message(),
			IncomingAction: mtproto.MakeTLMessageActionWebViewDataSentMe(&mtproto.MessageAction{
				Text: in.GetButtonText(),
				Data: data,
			}).To_MessageAction(),
		}).To_OutboxMessage()},
	})
	if err != nil {
		c.Logger.Errorf("messages.sendWebViewData - bot %d: %v", bot.Id(), err)
		return nil, err
	}
	return updates, nil
}

// webViewButtonShown tells whether one of the bot's latest messages in its
// chat with c's user has a reply keyboard with a mini app button of the
// text: the keyboard button mini apps of core.telegram.org/bots/webapps are
// those of reply keyboards.
func (c *BotsCore) webViewButtonShown(botId int64, text string) (bool, error) {
	if text == "" {
		return false, nil
	}
	markups, err := c.svcCtx.RecentBotMarkups(c.ctx, c.MD.UserId, botId, webViewDataButtons)
	if err != nil {
		return false, err
	}
	return hasWebViewButton(markups, text), nil
}

// hasWebViewButton tells whether a reply keyboard of markups has a
// keyboardButtonSimpleWebView of the text.
func hasWebViewButton(markups []*mtproto.ReplyMarkup, text string) bool {
	for _, m := range markups {
		if m.GetPredicateName() != mtproto.Predicate_replyKeyboardMarkup {
			continue
		}
		for _, row := range m.GetRows() {
			for _, b := range row.GetButtons() {
				if b.GetPredicateName() == mtproto.Predicate_keyboardButtonSimpleWebView && b.GetText() == text {
					return true
				}
			}
		}
	}
	return false
}

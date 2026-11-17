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
	"github.com/teamgram/teamgram-server/pkg/botstore"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// BotsGetRequestedWebViewButton
// bots.getRequestedWebViewButton#bf25b7f3 bot:InputUser webapp_req_id:string = KeyboardButton;
//
// The mini app asks the user for chats (event web_app_request_chat): the
// clients fetch the button its bot prepared, let the user choose by its
// peer_type and send the choice with messages.sendBotRequestedPeer and
// webapp_req_id (tdesktop inline_bots/bot_attach_web_view.cpp:2155-2190,
// Android ui/web/BotWebViewContainer.java:3015-3045). The answer is the
// stored keyboardButtonRequestPeer, which a client of layer 229 or later
// gets as keyboardButton with buttonTypeRequestPeer (pkg/layercompat), the
// form both clients read. A user that is not a bot is BOT_ID_INVALID, a
// webapp_req_id that is not a live button of the bot for this user
// WEBAPP_REQ_ID_INVALID (core.telegram.org/method/bots.getRequestedWebViewButton).
func (c *BotsCore) BotsGetRequestedWebViewButton(in *mtproto.TLBotsGetRequestedWebViewButton) (*mtproto.KeyboardButton, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	bot, err := c.resolveBot(in.GetBot())
	if errors.Is(err, mtproto.ErrBotInvalid) {
		return nil, mtproto.ErrBotIdInvalid
	}
	if err != nil {
		return nil, err
	}
	button, err := c.preparedButton("bots.getRequestedWebViewButton", bot.Id(), in.GetWebappReqId())
	if err != nil {
		return nil, err
	}
	// The parts the bot asked for are the server's to keep
	// (botmarkup.PreparedPeerButton): the client gets the button alone.
	return mtproto.MakeTLKeyboardButtonRequestPeer(&mtproto.KeyboardButton{
		Style:       button.GetStyle(),
		Text:        button.GetText(),
		ButtonId:    button.GetButtonId(),
		PeerType:    button.GetPeerType(),
		MaxQuantity: button.GetMaxQuantity(),
	}).To_KeyboardButton(), nil
}

// preparedButton returns the live button the bot prepared for the calling
// user under webapp_req_id, WEBAPP_REQ_ID_INVALID for none.
func (c *BotsCore) preparedButton(method string, botId int64, id string) (*mtproto.KeyboardButton, error) {
	if !wellFormedPreparedId(id) {
		return nil, mtproto.ErrWebappReqIdInvalid
	}
	p, err := c.preparedOf(method, botstore.PreparedKeyboardButton, botId, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, mtproto.ErrWebappReqIdInvalid
	}
	button := &mtproto.KeyboardButton{}
	if err = jsonx.Unmarshal(p.Payload, button); err != nil || button.GetPredicateName() != mtproto.Predicate_keyboardButtonRequestPeer {
		c.Logger.Errorf("%s - prepared button of bot %d: %v", method, botId, err)
		return nil, mtproto.ErrInternalServerError
	}
	return button, nil
}

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
	"github.com/teamgram/teamgram-server/pkg/botmarkup"
	"github.com/teamgram/teamgram-server/pkg/botstore"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// BotsRequestWebViewButton
// bots.requestWebViewButton#31a2a35e user_id:InputUser button:KeyboardButton = bots.RequestedButton;
//
// A bot prepares a button that asks the user of its mini app for chats (Bot
// API savePreparedKeyboardButton; the app passes webapp_req_id to
// web_app_request_chat, core.telegram.org/api/bots/buttons): "an
// inputKeyboardButtonRequestPeer of any RequestPeerType"
// (core.telegram.org/method/bots.requestWebViewButton), BUTTON_INVALID for
// anything else (botmarkup.PreparedPeerButton). A button that asks to create
// a bot is PEER_TYPES_INVALID: managed bots are not served here (ADR 0043),
// so messages.sendBotRequestedPeer could never answer it. The button is kept
// for the user for botstore.PreparedTTL.
func (c *BotsCore) BotsRequestWebViewButton(in *mtproto.TLBotsRequestWebViewButton) (*mtproto.Bots_RequestedButton, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	userId, err := c.preparedFor(in.GetUserId())
	if err != nil {
		return nil, err
	}
	button, err := botmarkup.PreparedPeerButton(in.GetButton())
	if err != nil {
		return nil, err
	}
	if button.GetPeerType().GetPredicateName() == mtproto.Predicate_requestPeerTypeCreateBot {
		return nil, mtproto.ErrPeerTypesInvalid
	}
	payload, err := jsonx.Marshal(button)
	if err != nil {
		c.Logger.Errorf("bots.requestWebViewButton - bot %d: %v", c.MD.UserId, err)
		return nil, mtproto.ErrInternalServerError
	}
	p, err := c.putPrepared("bots.requestWebViewButton", botstore.PreparedKeyboardButton, userId, payload)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLBotsRequestedButton(&mtproto.Bots_RequestedButton{WebappReqId: p.Id}).To_Bots_RequestedButton(), nil
}

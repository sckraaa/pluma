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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/botstore"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// MessagesGetPreparedInlineMessage
// messages.getPreparedInlineMessage#857ebdb8 bot:InputUser id:string = messages.PreparedInlineMessage;
//
// The mini app shares a message its bot prepared for the user (event
// web_app_send_prepared_message): the clients show the result, let the user
// choose chats of peer_types and send it there with
// messages.sendInlineBotResult(query_id, result.id) (tdesktop
// inline_bots/bot_attach_web_view.cpp:1999-2120, Android
// ui/bots/BotShareSheet.java:78, iOS PreparedInlineMessages.swift:14-22).
// query_id is a new answer of an inline query of the bot holding the result
// and its kinds of chats (dao.InlineAnswer.PeerTypes), so that send takes
// it as it is; it lives as long as the prepared message does. Only the
// user the bot prepared it for gets it: an id that is not one is
// ID_INVALID, one that is gone (expired, of another bot or user) is
// ID_EXPIRED (core.telegram.org/method/messages.getPreparedInlineMessage);
// the clients show either as an expired message.
func (c *BotsCore) MessagesGetPreparedInlineMessage(in *mtproto.TLMessagesGetPreparedInlineMessage) (*mtproto.Messages_PreparedInlineMessage, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	bot, err := c.resolveBot(in.GetBot())
	if err != nil {
		return nil, err
	}
	if !wellFormedPreparedId(in.GetId()) {
		return nil, mtproto.ErrIdInvalid
	}
	p, err := c.preparedOf("messages.getPreparedInlineMessage", botstore.PreparedInlineMessage, bot.Id(), in.GetId())
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, mtproto.ErrIdExpired
	}
	var m preparedMessage
	if err = jsonx.Unmarshal(p.Payload, &m); err != nil || m.Result == nil {
		c.Logger.Errorf("messages.getPreparedInlineMessage - prepared message of bot %d: %v", bot.Id(), err)
		return nil, mtproto.ErrInternalServerError
	}

	// cache_time is what is left of the prepared message: the answer stays
	// that long, and dao keeps an answer an hour past its cache time, so a
	// message shared at its last moment still goes out.
	left := max(p.ExpireAt-int32(time.Now().Unix()), 1)
	a := &dao.InlineAnswer{
		Results:   []*mtproto.BotInlineResult{m.Result},
		CacheTime: left,
		Bot:       bot.Id(),
		PeerTypes: m.PeerTypes,
	}
	if m.Game != nil {
		a.Games = map[string]dao.GameRef{m.Result.GetId(): *m.Game}
	}
	queryId, err := dao.NewQueryId()
	if err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	if err = c.svcCtx.PutInlineAnswer(c.ctx, queryId, a); err != nil {
		c.Logger.Errorf("messages.getPreparedInlineMessage - bot %d: %v", bot.Id(), err)
		return nil, mtproto.ErrInternalServerError
	}
	users, err := c.usersFor(c.MD.UserId, bot.Id())
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesPreparedInlineMessage(&mtproto.Messages_PreparedInlineMessage{
		QueryId:   queryId,
		Result:    m.Result,
		PeerTypes: peerTypesTL(m.PeerTypes),
		CacheTime: left,
		Users:     users,
	}).To_Messages_PreparedInlineMessage(), nil
}

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
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/botwidgets"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// MessagesGetBotWidgetQueryAnswer
// messages.getBotWidgetQueryAnswer#5c550579 peer:InputPeer msg_id:int query_type:WidgetQueryType data:bytes = messages.BotWidgetQueryAnswer;
//
// A widget asks its bot (task 106): the clients call it with the message
// in the user's own coordinates and the widget's data (tdesktop
// tgw/tgw_bridge.cpp BotQueries::send, Android utils/twidget/WidgetMedia.java
// sendQuery). The server's rules (Android
// jni/twidget/widget-sandbox/docs/tgw-format.md, "A request"): data of 1 to
// 64 bytes (WIDGET_QUERY_DATA_INVALID); only a message whose widget may ask
// (can_query, which the server set when it was sent), never a forward
// (WIDGET_QUERY_NOT_ALLOWED); at most 5 requests per 5 s of a user to a bot
// (FLOOD_WAIT); the bot gets updateBotWidgetQuery, or
// updateInlineBotWidgetQuery for a message sent via it, and the request
// waits for its answer up to 15 s (WIDGET_RESPONSE_TIMEOUT, which the
// clients read: tgw_bridge.cpp:63, WidgetMedia.java:919). Only a data
// query without the wallet is served (WIDGET_QUERY_TYPE_UNSUPPORTED): the
// wallet is TON, outside the project (ADR 0068).
func (c *BotsCore) MessagesGetBotWidgetQueryAnswer(in *mtproto.TLMessagesGetBotWidgetQueryAnswer) (*mtproto.Messages_BotWidgetQueryAnswer, error) {
	if qt := in.GetQueryType(); qt.GetPredicateName() != mtproto.Predicate_widgetQueryTypeData || qt.GetWithWallet() {
		return nil, mtproto.ErrWidgetQueryTypeUnsupported
	}
	if n := len(in.GetData()); n == 0 || n > botwidgets.MaxQueryData {
		return nil, mtproto.ErrWidgetQueryDataInvalid
	}
	peer := c.chatPeer(in.Peer)
	p, err := c.pressedMessage(peer, in.MsgId)
	if err != nil {
		c.Logger.Errorf("messages.getBotWidgetQueryAnswer - error: %v", err)
		return nil, err
	}
	media := p.message.GetMedia()
	switch {
	case !botwidgets.Is(media):
		return nil, mtproto.ErrMessageIdInvalid
	case !media.GetCanQuery() || media.GetIsCopy() || p.message.GetFwdFrom() != nil:
		return nil, mtproto.ErrWidgetQueryNotAllowed
	}
	if _, err = c.botUser(p.botId); err != nil {
		return nil, mtproto.ErrWidgetQueryNotAllowed
	}

	if wait, err := c.svcCtx.AllowWidgetQuery(c.ctx, c.MD.UserId, p.botId); err != nil {
		c.Logger.Errorf("messages.getBotWidgetQueryAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	} else if wait > 0 {
		return nil, mtproto.NewErrFloodWaitX(int32(wait))
	}
	if !c.svcCtx.EnterWait(c.MD.UserId) {
		return nil, mtproto.ErrWidgetResponseTimeout
	}
	defer c.svcCtx.LeaveWait(c.MD.UserId)
	queryId, err := dao.NewQueryId()
	if err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	if err = c.svcCtx.BeginWidgetQuery(c.ctx, queryId, p.botId); err != nil {
		c.Logger.Errorf("messages.getBotWidgetQueryAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	u, chats, err := c.widgetUpdate(queryId, p, in.MsgId, in.GetQueryType(), in.GetData())
	if err != nil {
		return nil, c.clientError("messages.getBotWidgetQueryAnswer", err)
	}
	users, err := c.usersFor(p.botId, c.MD.UserId)
	if err != nil {
		return nil, c.clientError("messages.getBotWidgetQueryAnswer", err)
	}
	if err = c.svcCtx.PushToBot(c.ctx, p.botId, u, users, chats); err != nil {
		c.Logger.Errorf("messages.getBotWidgetQueryAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}

	answer, err := c.svcCtx.WaitWidgetAnswer(c.ctx, queryId)
	if err != nil {
		c.Logger.Errorf("messages.getBotWidgetQueryAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if answer == nil {
		return nil, mtproto.ErrWidgetResponseTimeout
	}
	r := &mtproto.Messages_BotWidgetQueryAnswer{}
	if answer.Result != nil {
		r.Result = wrapperspb.String(*answer.Result)
	}
	return mtproto.MakeTLMessagesBotWidgetQueryAnswer(r).To_Messages_BotWidgetQueryAnswer(), nil
}

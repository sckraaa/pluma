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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/core"
)

// RPCBotWidgets (task 106, ADR 0068 p. 8): the widgets of bot messages.

// MessagesGetBotWidgetQueryAnswer
// messages.getBotWidgetQueryAnswer#5c550579 peer:InputPeer msg_id:int query_type:WidgetQueryType data:bytes = messages.BotWidgetQueryAnswer;
func (s *Service) MessagesGetBotWidgetQueryAnswer(ctx context.Context, request *mtproto.TLMessagesGetBotWidgetQueryAnswer) (*mtproto.Messages_BotWidgetQueryAnswer, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getBotWidgetQueryAnswer - metadata: {%s}", c.MD)
	return c.MessagesGetBotWidgetQueryAnswer(request)
}

// MessagesSetBotWidgetQueryAnswer
// messages.setBotWidgetQueryAnswer#f8cdec6a flags:# query_id:long result:flags.0?string = Bool;
func (s *Service) MessagesSetBotWidgetQueryAnswer(ctx context.Context, request *mtproto.TLMessagesSetBotWidgetQueryAnswer) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.setBotWidgetQueryAnswer - metadata: {%s}", c.MD)
	return c.MessagesSetBotWidgetQueryAnswer(request)
}

// MessagesEditMessageWidgetData
// messages.editMessageWidgetData#f624bc1e flags:# peer:InputPeer id:int public_data:flags.0?string chat_data:flags.1?string = Updates;
func (s *Service) MessagesEditMessageWidgetData(ctx context.Context, request *mtproto.TLMessagesEditMessageWidgetData) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.editMessageWidgetData - metadata: {%s}", c.MD)
	return c.MessagesEditMessageWidgetData(request)
}

// MessagesEditInlineBotMessageWidgetData
// messages.editInlineBotMessageWidgetData#6396ce2b flags:# id:InputBotInlineMessageID public_data:flags.0?string chat_data:flags.1?string = Bool;
func (s *Service) MessagesEditInlineBotMessageWidgetData(ctx context.Context, request *mtproto.TLMessagesEditInlineBotMessageWidgetData) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.editInlineBotMessageWidgetData - metadata: {%s}", c.MD)
	return c.MessagesEditInlineBotMessageWidgetData(request)
}

// MessagesEditWidgetData
// messages.editWidgetData#eac343c0 flags:# widget_id:long public_data:string cache_time:flags.0?int = Bool;
func (s *Service) MessagesEditWidgetData(ctx context.Context, request *mtproto.TLMessagesEditWidgetData) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.editWidgetData - metadata: {%s}", c.MD)
	return c.MessagesEditWidgetData(request)
}

// MessagesGetWidgetPublicData
// messages.getWidgetPublicData#6fc53181 peer:InputPeer msg_id:int hash:long = messages.WidgetPublicData;
func (s *Service) MessagesGetWidgetPublicData(ctx context.Context, request *mtproto.TLMessagesGetWidgetPublicData) (*mtproto.Messages_WidgetPublicData, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getWidgetPublicData - metadata: {%s}", c.MD)
	return c.MessagesGetWidgetPublicData(request)
}

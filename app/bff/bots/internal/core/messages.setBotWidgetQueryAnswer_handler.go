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
)

// MessagesSetBotWidgetQueryAnswer
// messages.setBotWidgetQueryAnswer#f8cdec6a flags:# query_id:long result:flags.0?string = Bool;
//
// The bot answers a widget's request by its id (task 106): result is text
// of at most 1024 bytes (WIDGET_RESULT_TOO_LARGE, WIDGET_DATA_INVALID),
// "only for the client that asked; none is an empty answer"; a request
// that is not the bot's, or has expired, is QUERY_ID_INVALID (Android
// jni/twidget/widget-sandbox/docs/tgw-format.md, "A request").
func (c *BotsCore) MessagesSetBotWidgetQueryAnswer(in *mtproto.TLMessagesSetBotWidgetQueryAnswer) (*mtproto.Bool, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	a := &dao.WidgetAnswer{}
	if in.Result != nil {
		if err := botwidgets.CheckData(in.Result, mtproto.ErrWidgetResultTooLarge, mtproto.ErrWidgetDataInvalid); err != nil {
			return nil, err
		}
		v := in.Result.GetValue()
		a.Result = &v
	}
	ok, err := c.svcCtx.SetWidgetAnswer(c.ctx, in.QueryId, c.MD.UserId, a)
	if err != nil {
		c.Logger.Errorf("messages.setBotWidgetQueryAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if !ok {
		return nil, mtproto.ErrQueryIdInvalid
	}
	return mtproto.BoolTrue, nil
}

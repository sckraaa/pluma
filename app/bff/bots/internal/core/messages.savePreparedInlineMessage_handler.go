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
	"github.com/teamgram/teamgram-server/pkg/botstore"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// MessagesSavePreparedInlineMessage
// messages.savePreparedInlineMessage#f21f7f2f flags:# result:InputBotInlineResult user_id:InputUser peer_types:flags.0?Vector<InlineQueryPeerType> = messages.BotPreparedInlineMessage;
//
// A bot prepares a message its mini app lets the user share (Bot API
// savePreparedInlineMessage; the app hands the id to
// web_app_send_prepared_message, core.telegram.org/api/bots/webapps). The
// result is checked as an answer of an inline query is (inlineResult, and
// inlineGameResult for a game of the bot), and kept with its kinds of chats
// for the user for botstore.PreparedTTL; expire_date says until when.
func (c *BotsCore) MessagesSavePreparedInlineMessage(in *mtproto.TLMessagesSavePreparedInlineMessage) (*mtproto.Messages_BotPreparedInlineMessage, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	userId, err := c.preparedFor(in.GetUserId())
	if err != nil {
		return nil, err
	}
	kinds, err := preparedPeerKinds(in.GetPeerTypes())
	if err != nil {
		return nil, err
	}
	m := preparedMessage{PeerTypes: kinds}
	if in.GetResult().GetPredicateName() == mtproto.Predicate_inputBotInlineResultGame {
		var ref dao.GameRef
		if m.Result, ref, err = c.inlineGameResult(in.GetResult()); err == nil {
			m.Game = &ref
		}
	} else {
		m.Result, err = c.inlineResult(in.GetResult())
	}
	if err != nil {
		return nil, err
	}
	payload, err := jsonx.Marshal(&m)
	if err != nil {
		c.Logger.Errorf("messages.savePreparedInlineMessage - bot %d: %v", c.MD.UserId, err)
		return nil, mtproto.ErrInternalServerError
	}
	p, err := c.putPrepared("messages.savePreparedInlineMessage", botstore.PreparedInlineMessage, userId, payload)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesBotPreparedInlineMessage(&mtproto.Messages_BotPreparedInlineMessage{
		Id:         p.Id,
		ExpireDate: p.ExpireAt,
	}).To_Messages_BotPreparedInlineMessage(), nil
}

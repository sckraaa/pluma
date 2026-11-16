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
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// MessagesGetInlineGameHighScores
// messages.getInlineGameHighScores#f635e1b id:InputBotInlineMessageID user_id:InputUser = messages.HighScores;
//
// getGameHighScores for a game a user sent via the bot: the table of the
// chat of the message the inline message id names, around the player, the
// player, two players on each side and the top three
// (core.telegram.org/bots/api#getgamehighscores, botstore.HighScores). The
// bot of the game asks it (USER_BOT_REQUIRED for a user, which names its
// messages by the chat: getGameHighScores).
func (c *BotsCore) MessagesGetInlineGameHighScores(in *mtproto.TLMessagesGetInlineGameHighScores) (*mtproto.Messages_HighScores, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	m, _, err := c.findInlineGameMessage(in.Id)
	if err != nil {
		c.Logger.Errorf("messages.getInlineGameHighScores - error: %v", err)
		return nil, err
	}
	target := mtproto.FromInputUser(c.MD.UserId, in.UserId)
	if !target.IsUser() {
		return nil, mtproto.ErrUserIdInvalid
	}
	chat, err := botstore.ScoreChatOf(m.owner, m.peer.PeerType, m.peer.PeerId)
	if err != nil {
		return nil, mtproto.ErrPeerIdInvalid
	}
	scores, err := c.svcCtx.Bots.HighScores(c.ctx, m.game.Id, chat, target.PeerId)
	if err != nil {
		c.Logger.Errorf("messages.getInlineGameHighScores - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	return c.highScores(scores)
}

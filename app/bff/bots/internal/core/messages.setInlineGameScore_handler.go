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
)

// MessagesSetInlineGameScore
// messages.setInlineGameScore#15ad9f64 flags:# edit_message:flags.0?true force:flags.1?true id:InputBotInlineMessageID user_id:InputUser score:int = Bool;
//
// setGameScore for a game a user sent via the bot (an inline game,
// core.telegram.org/api/bots/games): the message is the one the inline
// message id names (findInlineGameMessage), and the table is of the chat
// it is in ("for an inline message — the chat it lies in", ADR 0068 p. 7):
// a private chat of two users, either of whom plays, a group or a
// supergroup of its members. The rules are setGameScore's (setScore):
// SCORE_INVALID, BOT_SCORE_NOT_MODIFIED without force, and with
// edit_message the player's messageActionGameScore in answer to the game.
func (c *BotsCore) MessagesSetInlineGameScore(in *mtproto.TLMessagesSetInlineGameScore) (*mtproto.Bool, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	if in.Score < 0 {
		return nil, mtproto.ErrScoreInvalid
	}
	m, msgId, err := c.findInlineGameMessage(in.Id)
	if err != nil {
		c.Logger.Errorf("messages.setInlineGameScore - error: %v", err)
		return nil, err
	}
	playerId, err := c.player(in.UserId)
	if err != nil {
		return nil, err
	}
	if err = c.setScore("messages.setInlineGameScore", m, msgId, playerId, in.Score, in.Force, in.EditMessage); err != nil {
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

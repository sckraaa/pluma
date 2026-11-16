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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/botstore"
)

// MessagesSetGameScore
// messages.setGameScore#8ef8ecc0 flags:# edit_message:flags.0?true force:flags.1?true peer:InputPeer id:int user_id:InputUser score:int = Updates;
//
// The bot of the game sets the score of a player in the chat of its game
// message (core.telegram.org/api/bots/games; the rules of
// core.telegram.org/bots/api#setgamescore): a score is not negative
// (SCORE_INVALID), and without force only a greater one replaces the
// player's ("Returns an error, if the new score is not greater than the
// user's current score in the chat and force is False":
// BOT_SCORE_NOT_MODIFIED). The table is of the chat: a private chat, a
// group or a supergroup (botstore.ScoreChatOf); the player of a private
// chat with the bot is the user of that chat, of a group or a supergroup
// its member (playerInChat).
//
// With edit_message the chat gets messageActionGameScore from the player,
// in answer to the game message ("a messageService with a
// messageActionGameScore is generated", api/bots/games). The score is
// stored first: it matters more, and a repeat of the call posts the same
// message once (scoreRandomId). The message reaches the bot as any message
// of the chat does; the answer is empty updates, since the message's
// updates are the player's.
func (c *BotsCore) MessagesSetGameScore(in *mtproto.TLMessagesSetGameScore) (*mtproto.Updates, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	if in.Score < 0 {
		return nil, mtproto.ErrScoreInvalid
	}
	m, err := c.findGameMessage(in.Peer, in.Id)
	if err != nil {
		c.Logger.Errorf("messages.setGameScore - error: %v", err)
		return nil, err
	}
	if m.game.BotId != c.MD.UserId {
		return nil, mtproto.ErrMessageIdInvalid
	}
	playerId, err := c.player(in.UserId)
	if err != nil {
		return nil, err
	}
	if m.peer.IsUser() && m.peer.PeerId != playerId {
		return nil, mtproto.ErrUserIdInvalid
	}
	if err = c.playerInChat(m, playerId); err != nil {
		if !errors.Is(err, mtproto.ErrUserIdInvalid) {
			c.Logger.Errorf("messages.setGameScore - error: %v", err)
		}
		return nil, err
	}
	chat, err := botstore.ScoreChatOf(c.MD.UserId, m.peer.PeerType, m.peer.PeerId)
	if err != nil {
		return nil, mtproto.ErrPeerIdInvalid
	}
	var reply *scoreReply
	if in.EditMessage {
		if reply, err = c.gameScoreReply(m, in.Id, playerId); err != nil {
			c.Logger.Errorf("messages.setGameScore - error: %v", err)
			return nil, err
		}
	}

	if err = c.svcCtx.Bots.SetScore(c.ctx, m.game.Id, chat, playerId, in.Score, in.Force, int32(time.Now().Unix())); err != nil {
		if !errors.Is(err, mtproto.ErrBotScoreNotModified) && !errors.Is(err, mtproto.ErrScoreInvalid) {
			c.Logger.Errorf("messages.setGameScore - error: %v", err)
			return nil, mtproto.ErrInternalServerError
		}
		return nil, err
	}
	if reply != nil {
		if err = c.postScore(reply, playerId, m.game.Id, in.Score, scoreRandomId(chat, in.Id, m.game.Id, playerId, in.Score)); err != nil {
			c.Logger.Errorf("messages.setGameScore - the score's message: %v", err)
			return nil, err
		}
	}
	return mtproto.MakeEmptyUpdates(), nil
}

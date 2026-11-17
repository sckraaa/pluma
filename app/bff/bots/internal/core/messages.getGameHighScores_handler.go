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

// MessagesGetGameHighScores
// messages.getGameHighScores#e822649d peer:InputPeer id:int user_id:InputUser = messages.HighScores;
//
// The table of the game of a message in its chat around a player: the
// player, two players on each side and the top three
// (core.telegram.org/bots/api#getgamehighscores, botstore.HighScores). The
// bot of the game asks it, for its own games only (MESSAGE_ID_INVALID
// otherwise); a user may ask it of a game message the user has, as Web K
// can (appGamesManager.ts:72-89). The users are as the caller sees them.
func (c *BotsCore) MessagesGetGameHighScores(in *mtproto.TLMessagesGetGameHighScores) (*mtproto.Messages_HighScores, error) {
	m, err := c.findGameMessage(in.Peer, in.Id)
	if err != nil {
		c.Logger.Errorf("messages.getGameHighScores - error: %v", err)
		return nil, err
	}
	if c.MD.IsBot && m.game.BotId != c.MD.UserId {
		return nil, mtproto.ErrMessageIdInvalid
	}
	target := mtproto.FromInputUser(c.MD.UserId, in.UserId)
	if !target.IsUser() {
		return nil, mtproto.ErrUserIdInvalid
	}
	chat, err := botstore.ScoreChatOf(c.MD.UserId, m.peer.PeerType, m.peer.PeerId)
	if err != nil {
		return nil, mtproto.ErrPeerIdInvalid
	}
	scores, err := c.svcCtx.Bots.HighScores(c.ctx, m.game.Id, chat, target.PeerId)
	if err != nil {
		c.Logger.Errorf("messages.getGameHighScores - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	return c.highScores(scores)
}

// highScores is the table as the caller sees it (messages.highScores).
func (c *BotsCore) highScores(scores []botstore.HighScore) (*mtproto.Messages_HighScores, error) {
	out := &mtproto.Messages_HighScores{
		Scores: make([]*mtproto.HighScore, 0, len(scores)),
		Users:  []*mtproto.User{},
	}
	ids := make([]int64, 0, len(scores))
	for _, s := range scores {
		out.Scores = append(out.Scores, mtproto.MakeTLHighScore(&mtproto.HighScore{Pos: s.Pos, UserId: s.UserId, Score: s.Score}).To_HighScore())
		ids = append(ids, s.UserId)
	}
	if len(ids) > 0 {
		users, err := c.usersFor(c.MD.UserId, ids...)
		if err != nil {
			c.Logger.Errorf("messages.getGameHighScores - users: %v", err)
			return nil, err
		}
		out.Users = users
	}
	return mtproto.MakeTLMessagesHighScores(out).To_Messages_HighScores(), nil
}

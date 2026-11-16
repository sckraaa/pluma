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

package botstore

import (
	"context"
	"fmt"
	"sort"

	"github.com/teamgram/proto/mtproto"
)

// The scores of games (game_scores, ADR 0068 p. 7) by game, chat and user,
// by the rules of the Bot API's setGameScore and getGameHighScores
// (core.telegram.org/bots/api#setgamescore, #getgamehighscores).

// ScoreChat is the chat a score belongs to: a basic group or a channel, or
// the pair of users of a private chat (Id < Id2, or both the user for the
// chat with oneself).
type ScoreChat struct {
	Type int32
	Id   int64
	Id2  int64
}

// ScoreChatOf is the chat of a game message of selfId's peer: the same for
// both users of a private chat.
func ScoreChatOf(selfId int64, peerType int32, peerId int64) (ScoreChat, error) {
	switch peerType {
	case mtproto.PEER_SELF:
		return ScoreChat{Type: mtproto.PEER_USER, Id: selfId, Id2: selfId}, nil
	case mtproto.PEER_USER:
		did := mtproto.MakeDialogId(selfId, peerType, peerId)
		return ScoreChat{Type: mtproto.PEER_USER, Id: did.A, Id2: did.B}, nil
	case mtproto.PEER_CHAT, mtproto.PEER_CHANNEL:
		return ScoreChat{Type: peerType, Id: peerId}, nil
	}
	return ScoreChat{}, fmt.Errorf("botstore: no score chat for peer type %d", peerType)
}

// SetScore sets the user's score of the game in the chat. Without force
// only a greater score replaces the stored one: BOT_SCORE_NOT_MODIFIED
// otherwise, as the Bot API's setGameScore answers "if the new score is not
// greater than the user's current score in the chat and force is False". A
// negative score is SCORE_INVALID.
//
// One statement reads and writes the row, so two calls at once for the same
// user and chat end with the greater score, and the lesser of them gets
// BOT_SCORE_NOT_MODIFIED unless it came first. The rows affected tell the
// outcome (no clientFoundRows in the DSN): 1 a new row, 2 a changed one, 0
// one left as it was. updated_at is assigned before score, so its IF still
// sees the old score.
func (s *Store) SetScore(ctx context.Context, gameId int64, chat ScoreChat, userId int64, score int32, force bool, now int32) error {
	if score < 0 {
		return mtproto.ErrScoreInvalid
	}
	var n int64
	for attempt := 1; ; attempt++ {
		r, err := s.db.Exec(ctx, `INSERT INTO game_scores (game_id, chat_type, chat_id, chat_id2, user_id, score, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				updated_at = IF(? OR VALUES(score) > score, VALUES(updated_at), updated_at),
				score = IF(? OR VALUES(score) > score, VALUES(score), score)`,
			gameId, chat.Type, chat.Id, chat.Id2, userId, score, now, force, force)
		if err != nil {
			// InnoDB may break a lock wait of two first scores of a player
			// as a deadlock; the statement is whole, so it runs again.
			if retryable(err) && attempt < randomIdAttempts {
				continue
			}
			return err
		}
		if n, err = r.RowsAffected(); err != nil {
			return err
		}
		break
	}
	if n == 0 && !force {
		return mtproto.ErrBotScoreNotModified
	}
	return nil
}

// HighScore is a line of a game's table (TL highScore).
type HighScore struct {
	Pos    int32
	UserId int64
	Score  int32
}

// highScoreNeighbours is how many players around the user the table shows
// on each side, and highScoreTop the leaders it always shows: "the score of
// the specified user and several of their neighbors in a game ... the
// target user, plus two of their closest neighbors on each side. Will also
// return the top three users if the user and their neighbors are not among
// them" (core.telegram.org/bots/api#getgamehighscores).
const (
	highScoreNeighbours = 2
	highScoreTop        = 3
)

type scoreRow struct {
	UserId    int64 `db:"user_id"`
	Score     int32 `db:"score"`
	UpdatedAt int32 `db:"updated_at"`
}

// HighScores returns the table of the game in the chat as userId sees it,
// by position: the top three, the user and two players on each side of the
// user. Players with the same score are ranked by who reached it first,
// then by id, so every position is taken once. A user with no score sees
// the top three.
func (s *Store) HighScores(ctx context.Context, gameId int64, chat ScoreChat, userId int64) ([]HighScore, error) {
	const where = "game_id = ? AND chat_type = ? AND chat_id = ? AND chat_id2 = ?"
	args := []any{gameId, chat.Type, chat.Id, chat.Id2}

	var top []scoreRow
	if err := s.db.QueryRowsPartial(ctx, &top,
		"SELECT user_id, score, updated_at FROM game_scores WHERE "+where+" ORDER BY score DESC, updated_at, user_id LIMIT ?",
		append(args, highScoreTop)...); err != nil {
		return nil, err
	}
	out := make([]HighScore, 0, highScoreTop+2*highScoreNeighbours+1)
	seen := make(map[int64]bool, cap(out))
	add := func(pos int32, r scoreRow) {
		if !seen[r.UserId] {
			seen[r.UserId] = true
			out = append(out, HighScore{Pos: pos, UserId: r.UserId, Score: r.Score})
		}
	}
	for i, r := range top {
		add(int32(i+1), r)
	}

	var mine []scoreRow
	if err := s.db.QueryRowsPartial(ctx, &mine,
		"SELECT user_id, score, updated_at FROM game_scores WHERE "+where+" AND user_id = ?", append(args, userId)...); err != nil {
		return nil, err
	}
	if len(mine) == 0 {
		return out, nil
	}
	me := mine[0]
	// before is the order of the table: a row ranks above me.
	const before = "(score > ? OR (score = ? AND (updated_at < ? OR (updated_at = ? AND user_id < ?))))"
	beforeArgs := []any{me.Score, me.Score, me.UpdatedAt, me.UpdatedAt, me.UserId}
	var above []int64
	if err := s.db.QueryRowsPartial(ctx, &above,
		"SELECT COUNT(*) FROM game_scores WHERE "+where+" AND "+before, append(append([]any{}, args...), beforeArgs...)...); err != nil {
		return nil, err
	}
	if len(above) != 1 {
		return nil, fmt.Errorf("botstore: count of scores: %d rows", len(above))
	}
	pos := int32(above[0]) + 1

	var up, down []scoreRow
	if err := s.db.QueryRowsPartial(ctx, &up,
		"SELECT user_id, score, updated_at FROM game_scores WHERE "+where+" AND "+before+" ORDER BY score, updated_at DESC, user_id DESC LIMIT ?",
		append(append(append([]any{}, args...), beforeArgs...), highScoreNeighbours)...); err != nil {
		return nil, err
	}
	const after = "(score < ? OR (score = ? AND (updated_at > ? OR (updated_at = ? AND user_id > ?))))"
	if err := s.db.QueryRowsPartial(ctx, &down,
		"SELECT user_id, score, updated_at FROM game_scores WHERE "+where+" AND "+after+" ORDER BY score DESC, updated_at, user_id LIMIT ?",
		append(append(append([]any{}, args...), beforeArgs...), highScoreNeighbours)...); err != nil {
		return nil, err
	}
	for i := len(up) - 1; i >= 0; i-- {
		add(pos-int32(i+1), up[i])
	}
	add(pos, me)
	for i, r := range down {
		add(pos+int32(i+1), r)
	}
	// The queries see the table at slightly different moments; the lines
	// still go by position.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Pos < out[j].Pos })
	return out, nil
}

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
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// The games of bots (bot_games, ADR 0068 p. 7): the admin tool makes them,
// a bot sends one with inputMediaGame(inputGameShortName), users forward it
// with inputGameID. A game is not part of the bot's info, so its changes
// leave the bot's versions alone.

// Limits of the fields of a game: the columns of bot_games.
const (
	MaxGameTitle       = 64
	MaxGameDescription = 512
)

// Errors of the admin tool's changes of games.
var (
	ErrNoGame      = errors.New("botstore: no such game")
	ErrGameInvalid = errors.New("botstore: invalid game")
)

// Game is a game of a bot (TL game).
type Game struct {
	Id          int64  `db:"id"`
	AccessHash  int64  `db:"access_hash"`
	BotId       int64  `db:"bot_id"`
	ShortName   string `db:"short_name"`
	Title       string `db:"title"`
	Description string `db:"description"`
	PhotoId     int64  `db:"photo_id"`
	DocumentId  int64  `db:"document_id"`
}

const gameColumns = "id, access_hash, bot_id, short_name, title, description, photo_id, document_id"

func (g *Game) validate() error {
	if !ValidShortName(g.ShortName) {
		return ErrShortNameInvalid
	}
	if n := utf8.RuneCountInString(g.Title); n == 0 || n > MaxGameTitle || strings.TrimSpace(g.Title) == "" {
		return fmt.Errorf("%w: title of 1 to %d characters", ErrGameInvalid, MaxGameTitle)
	}
	if utf8.RuneCountInString(g.Description) > MaxGameDescription {
		return fmt.Errorf("%w: description over %d characters", ErrGameInvalid, MaxGameDescription)
	}
	if g.PhotoId == 0 {
		// game.photo is a Photo, not optional (TL game).
		return fmt.Errorf("%w: a game needs a photo", ErrGameInvalid)
	}
	return nil
}

// AddGame makes a game of the bot, or brings back a removed game of the
// same short name with the new fields; it keeps its id and access hash, so
// the games already in chats and their scores stay its own.
func (s *Store) AddGame(ctx context.Context, g Game) (Game, error) {
	if err := g.validate(); err != nil {
		return Game{}, err
	}
	for range randomIdAttempts {
		id, err := randomId63()
		if err != nil {
			return Game{}, err
		}
		accessHash, err := randomId63()
		if err != nil {
			return Game{}, err
		}
		var out Game
		err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
			var bots []int64
			if err := tx.QueryRowsPartial(&bots, "SELECT bot_id FROM bots WHERE bot_id = ?", g.BotId); err != nil {
				return err
			}
			if len(bots) == 0 {
				return ErrNoBot
			}
			var rows []struct {
				Id      int64 `db:"id"`
				Deleted bool  `db:"deleted"`
			}
			if err := tx.QueryRowsPartial(&rows, "SELECT id, deleted FROM bot_games WHERE bot_id = ? AND short_name = ? FOR UPDATE", g.BotId, g.ShortName); err != nil {
				return err
			}
			if len(rows) > 0 && !rows[0].Deleted {
				return ErrShortNameTaken
			}
			if len(rows) > 0 {
				id = rows[0].Id
				if _, err := tx.Exec("UPDATE bot_games SET title = ?, description = ?, photo_id = ?, document_id = ?, deleted = 0 WHERE id = ?",
					g.Title, g.Description, g.PhotoId, g.DocumentId, id); err != nil {
					return err
				}
			} else if _, err := tx.Exec(`INSERT INTO bot_games (id, access_hash, bot_id, short_name, title, description, photo_id, document_id, deleted)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
				id, accessHash, g.BotId, g.ShortName, g.Title, g.Description, g.PhotoId, g.DocumentId); err != nil {
				return err
			}
			return tx.QueryRowPartial(&out, "SELECT "+gameColumns+" FROM bot_games WHERE id = ?", id)
		})
		if retryable(err) {
			continue
		}
		return out, err
	}
	return Game{}, fmt.Errorf("botstore: no free game id after %d attempts", randomIdAttempts)
}

// EditGame replaces the fields of the bot's game of g.ShortName.
func (s *Store) EditGame(ctx context.Context, g Game) (Game, error) {
	if err := g.validate(); err != nil {
		return Game{}, err
	}
	var out Game
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		var rows []int64
		if err := tx.QueryRowsPartial(&rows, "SELECT id FROM bot_games WHERE bot_id = ? AND short_name = ? AND deleted = 0 FOR UPDATE", g.BotId, g.ShortName); err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrNoGame
		}
		if _, err := tx.Exec("UPDATE bot_games SET title = ?, description = ?, photo_id = ?, document_id = ? WHERE id = ?",
			g.Title, g.Description, g.PhotoId, g.DocumentId, rows[0]); err != nil {
			return err
		}
		return tx.QueryRowPartial(&out, "SELECT "+gameColumns+" FROM bot_games WHERE id = ?", rows[0])
	})
	return out, err
}

// DeleteGame removes the bot's game: it can no longer be sent, and its
// scores stay for a game brought back by AddGame.
func (s *Store) DeleteGame(ctx context.Context, botId int64, shortName string) error {
	r, err := s.db.Exec(ctx, "UPDATE bot_games SET deleted = 1 WHERE bot_id = ? AND short_name = ? AND deleted = 0", botId, shortName)
	if err != nil {
		return err
	}
	if n, err := r.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrNoGame
	}
	return nil
}

// Games returns the bot's games, by short name.
func (s *Store) Games(ctx context.Context, botId int64) ([]Game, error) {
	var rows []Game
	err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+gameColumns+" FROM bot_games WHERE bot_id = ? AND deleted = 0 ORDER BY short_name", botId)
	return rows, err
}

// GameByShortName returns the bot's game of inputGameShortName, nil for
// none.
func (s *Store) GameByShortName(ctx context.Context, botId int64, shortName string) (*Game, error) {
	var rows []Game
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT "+gameColumns+" FROM bot_games WHERE bot_id = ? AND short_name = ? AND deleted = 0", botId, shortName); err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// GameById returns the game of inputGameID, nil for none or for an access
// hash that is not the game's.
func (s *Store) GameById(ctx context.Context, id, accessHash int64) (*Game, error) {
	var rows []Game
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT "+gameColumns+" FROM bot_games WHERE id = ? AND access_hash = ? AND deleted = 0", id, accessHash); err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// GameOf returns the game of the id as a stored message holds it
// (messageMediaGame.game.id), removed or not: the message keeps showing
// the game it was sent with, and its scores still count. nil for none.
func (s *Store) GameOf(ctx context.Context, id int64) (*Game, error) {
	var rows []Game
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+gameColumns+" FROM bot_games WHERE id = ?", id); err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

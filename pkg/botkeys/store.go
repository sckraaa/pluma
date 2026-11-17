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

package botkeys

import (
	"context"
	"errors"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// upsertKeys writes the row of a bot only while tokenHash is the bot's
// token (bots.token): keys of a token replaced meanwhile are never stored.
const upsertKeys = `INSERT INTO bot_secret_keys (bot_id, token_hash, webapp_key, login_key, updated_at)
	SELECT b.bot_id, b.token, ?, ?, ? FROM bots b WHERE b.bot_id = ? AND b.token = ?
	ON DUPLICATE KEY UPDATE token_hash = VALUES(token_hash), webapp_key = VALUES(webapp_key),
		login_key = VALUES(login_key), updated_at = VALUES(updated_at)`

// sealed returns the sealed keys of token for botId.
func (s *Sealer) sealed(botId int64, token string) (webApp, login []byte, err error) {
	keys := Derive(token)
	if webApp, err = s.Seal(botId, PurposeWebApp, keys.WebApp); err != nil {
		return nil, nil, err
	}
	if login, err = s.Seal(botId, PurposeLogin, keys.Login); err != nil {
		return nil, nil, err
	}
	return webApp, login, nil
}

// WriteTx stores the keys of token, the whole token of botId whose stored
// hash is tokenHash, in tx: the transaction that makes tokenHash the bot's
// token, so that the keys and the token change together (ADR 0068, p. 3).
// The caller writes bots.token first.
func (s *Sealer) WriteTx(tx *sqlx.Tx, botId int64, token, tokenHash string) error {
	webApp, login, err := s.sealed(botId, token)
	if err != nil {
		return err
	}
	_, err = tx.Exec(upsertKeys, webApp, login, time.Now().Unix(), botId, tokenHash)
	return err
}

// Store reads and writes bot_secret_keys. Without a sealer (no
// BOT_SECRET_ENCRYPTION_KEY) it stores nothing and finds no keys.
type Store struct {
	db     *sqlx.DB
	sealer *Sealer
}

// NewStore returns a store on db; sealer may be nil.
func NewStore(db *sqlx.DB, sealer *Sealer) *Store {
	return &Store{db: db, sealer: sealer}
}

// Enabled tells whether keys are stored and read at all.
func (s *Store) Enabled() bool {
	return s.sealer != nil
}

type keysRow struct {
	TokenHash string `db:"token_hash"`
	WebApp    []byte `db:"webapp_key"`
	Login     []byte `db:"login_key"`
}

func (s *Store) open(botId int64, r keysRow) (Keys, error) {
	webApp, err := s.sealer.Open(botId, PurposeWebApp, r.WebApp)
	if err != nil {
		return Keys{}, err
	}
	login, err := s.sealer.Open(botId, PurposeLogin, r.Login)
	if err != nil {
		return Keys{}, err
	}
	return Keys{WebApp: webApp, Login: login}, nil
}

// Ensure stores the keys of token, with which botId has just logged in
// (auth.importBotAuthorization), unless the row already holds keys of this
// token that open: a login writes only for a bot without keys, with keys of
// another token, or with keys sealed under a lost encryption key. It tells
// whether it wrote; nothing is written for a token that is no longer the
// bot's.
func (s *Store) Ensure(ctx context.Context, botId int64, token, tokenHash string) (bool, error) {
	if s.sealer == nil {
		return false, nil
	}
	var rows []keysRow
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT token_hash, webapp_key, login_key FROM bot_secret_keys WHERE bot_id = ?", botId); err != nil {
		return false, err
	}
	if len(rows) == 1 && rows[0].TokenHash == tokenHash {
		if _, err := s.open(botId, rows[0]); err == nil {
			return false, nil
		} else if !errors.Is(err, ErrSealedInvalid) {
			return false, err
		}
	}
	webApp, login, err := s.sealer.sealed(botId, token)
	if err != nil {
		return false, err
	}
	r, err := s.db.Exec(ctx, upsertKeys, webApp, login, time.Now().Unix(), botId, tokenHash)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Get returns the keys of the bot's current token; ok is false when there
// are none: no encryption key, a bot that has not logged in or been given a
// token since the keys were introduced, or keys of a token replaced. Keys
// sealed under another encryption key are ErrSealedInvalid, which callers
// treat as no keys after logging it: the bot's next login stores them again.
func (s *Store) Get(ctx context.Context, botId int64) (keys Keys, ok bool, err error) {
	if s.sealer == nil {
		return Keys{}, false, nil
	}
	var rows []keysRow
	if err = s.db.QueryRowsPartial(ctx, &rows,
		`SELECT k.token_hash, k.webapp_key, k.login_key FROM bot_secret_keys k
		JOIN bots b ON b.bot_id = k.bot_id AND b.token = k.token_hash WHERE k.bot_id = ?`, botId); err != nil || len(rows) == 0 {
		return Keys{}, false, err
	}
	if keys, err = s.open(botId, rows[0]); err != nil {
		return Keys{}, false, err
	}
	return keys, true, nil
}

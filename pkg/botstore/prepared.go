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
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// What a bot prepares for a user's mini app (bot_prepared): inline
// messages of messages.savePreparedInlineMessage, which the app shares
// through messages.getPreparedInlineMessage, and keyboard buttons of
// bots.requestWebViewButton, which it presses through
// bots.getRequestedWebViewButton. The payload is the caller's encoding.

// The kinds of bot_prepared.
const (
	PreparedInlineMessage  = 1
	PreparedKeyboardButton = 2
)

// Limits of prepared items: their lifetime and how many live ones a bot
// keeps for one user (server's choice, task 106), the size of a payload
// (an inline result with its text, entities and markup is a few KiB), the
// random bytes of an id, and the rows one round of PurgeExpiredPrepared
// deletes.
const (
	PreparedTTL         = 24 * 60 * 60
	MaxPreparedPerPair  = 100
	MaxPreparedPayload  = 256 << 10
	preparedIdBytes     = 18
	preparedPurgeBatch  = 1000
	maxPreparedIdLength = 32
)

// ErrPreparedInvalid is PutPrepared for an unknown kind, an empty payload
// or one over MaxPreparedPayload.
var ErrPreparedInvalid = errors.New("botstore: invalid prepared item")

// Prepared is an item a bot prepared for a user.
type Prepared struct {
	Id       string `db:"id"`
	Kind     int8   `db:"kind"`
	BotId    int64  `db:"bot_id"`
	UserId   int64  `db:"user_id"`
	Payload  []byte `db:"payload"`
	ExpireAt int32  `db:"expire_at"`
}

// PutPrepared stores an item of the bot for the user until now +
// PreparedTTL and returns it with its id, base64url of 18 random bytes. A
// bot keeps MaxPreparedPerPair live items for one user: the oldest beyond
// that go, as a bot that prepares an item for every opening of its app
// would otherwise grow the table without end. The pair's rows are locked
// for the count, so two puts at once do not both pass it.
func (s *Store) PutPrepared(ctx context.Context, kind int8, botId, userId int64, payload []byte, now int32) (Prepared, error) {
	if (kind != PreparedInlineMessage && kind != PreparedKeyboardButton) || len(payload) == 0 || len(payload) > MaxPreparedPayload {
		return Prepared{}, ErrPreparedInvalid
	}
	p := Prepared{Kind: kind, BotId: botId, UserId: userId, Payload: payload, ExpireAt: now + PreparedTTL}
	for range randomIdAttempts {
		b := make([]byte, preparedIdBytes)
		if _, err := rand.Read(b); err != nil {
			return Prepared{}, fmt.Errorf("botstore: read random: %w", err)
		}
		p.Id = base64.RawURLEncoding.EncodeToString(b)
		err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
			var live []string
			if err := tx.QueryRowsPartial(&live,
				"SELECT id FROM bot_prepared WHERE bot_id = ? AND user_id = ? AND expire_at > ? ORDER BY expire_at DESC, id FOR UPDATE",
				botId, userId, now); err != nil {
				return err
			}
			if len(live) >= MaxPreparedPerPair {
				old := live[MaxPreparedPerPair-1:]
				query, args, err := sqlx.In("DELETE FROM bot_prepared WHERE id IN (?)", old)
				if err != nil {
					return err
				}
				if _, err = tx.Exec(query, args...); err != nil {
					return err
				}
			}
			_, err := tx.Exec("INSERT INTO bot_prepared (id, kind, bot_id, user_id, payload, expire_at) VALUES (?, ?, ?, ?, ?, ?)",
				p.Id, p.Kind, p.BotId, p.UserId, p.Payload, p.ExpireAt)
			return err
		})
		if retryable(err) {
			continue
		}
		if err != nil {
			return Prepared{}, err
		}
		return p, nil
	}
	return Prepared{}, fmt.Errorf("botstore: no free prepared id after %d attempts", randomIdAttempts)
}

// PreparedOf returns the live item of the id and kind, nil for none, for
// one expired at now, or for an id of another kind.
func (s *Store) PreparedOf(ctx context.Context, id string, kind int8, now int32) (*Prepared, error) {
	if id == "" || len(id) > maxPreparedIdLength {
		return nil, nil
	}
	var rows []Prepared
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT id, kind, bot_id, user_id, payload, expire_at FROM bot_prepared WHERE id = ? AND kind = ? AND expire_at > ?", id, kind, now); err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// PurgeExpiredPrepared deletes the items expired at now, a batch at a
// time so no statement holds the table long, and returns how many went.
func (s *Store) PurgeExpiredPrepared(ctx context.Context, now int32) (int64, error) {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		r, err := s.db.Exec(ctx, "DELETE FROM bot_prepared WHERE expire_at <= ? LIMIT ?", now, preparedPurgeBatch)
		if err != nil {
			return total, err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < preparedPurgeBatch {
			return total, nil
		}
	}
}

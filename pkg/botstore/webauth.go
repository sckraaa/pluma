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
	"math/rand/v2"
	"time"
	"unicode/utf8"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// The sites users logged in on through bots (web_authorizations,
// account.getWebAuthorizations, ADR 0068 p. 6) and the users who let a bot
// write to them (bot_write_allowed, ADR 0068 p. 4).

// Limits of the columns of web_authorizations; longer values are cut at a
// character boundary, since they describe a session and are not keys.
const (
	maxWebBrowser  = 128
	maxWebPlatform = 64
	maxWebIP       = 45
	maxWebRegion   = 128
)

// ErrWebAuthorizationInvalid is PutWebAuthorization for a record without a
// user, a bot or a valid domain.
var ErrWebAuthorizationInvalid = errors.New("botstore: invalid web authorization")

// WebAuthorization is a site a user logged in on (TL webAuthorization):
// one per user, bot and domain.
type WebAuthorization struct {
	Hash         int64  `db:"hash"`
	UserId       int64  `db:"user_id"`
	BotId        int64  `db:"bot_id"`
	Domain       string `db:"domain"`
	Browser      string `db:"browser"`
	Platform     string `db:"platform"`
	IP           string `db:"ip"`
	Region       string `db:"region"`
	WriteAllowed bool   `db:"write_allowed"`
	DateCreated  int32  `db:"date_created"`
	DateActive   int32  `db:"date_active"`
}

const webAuthColumns = "hash, user_id, bot_id, domain, browser, platform, ip, region, write_allowed, date_created, date_active"

// webAuthAttempts bounds the rounds of PutWebAuthorization (see there).
const webAuthAttempts = 10

// cut returns s cut to at most n characters.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// PutWebAuthorization records a login of the user on the bot's domain at
// now: a new record with a new random hash, or the existing one with the
// session's browser, platform, address and region and date_active now. A
// write permission once given stays. It returns the record as stored.
func (s *Store) PutWebAuthorization(ctx context.Context, w WebAuthorization, now int32) (WebAuthorization, error) {
	d, ok := NormalizeLoginDomain(w.Domain)
	if !ok || w.UserId == 0 || w.BotId == 0 {
		return WebAuthorization{}, ErrWebAuthorizationInvalid
	}
	w.Domain = d
	w.Browser = cut(w.Browser, maxWebBrowser)
	w.Platform = cut(w.Platform, maxWebPlatform)
	w.IP = cut(w.IP, maxWebIP)
	w.Region = cut(w.Region, maxWebRegion)
	// Logins of one user, bot and domain at the same moment lock the same
	// empty range and deadlock each other; they get more rounds than a hash
	// collision needs, each after a short random pause, so they settle on
	// one record instead of failing.
	for attempt := range webAuthAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return WebAuthorization{}, ctx.Err()
			case <-time.After(time.Duration(5+rand.IntN(20*attempt)) * time.Millisecond): //nolint:gosec // G404: a retry pause, not a secret
			}
		}
		hash, err := randomId63()
		if err != nil {
			return WebAuthorization{}, err
		}
		var out WebAuthorization
		err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
			var rows []int64
			if err := tx.QueryRowsPartial(&rows, "SELECT hash FROM web_authorizations WHERE user_id = ? AND bot_id = ? AND domain = ? FOR UPDATE",
				w.UserId, w.BotId, w.Domain); err != nil {
				return err
			}
			if len(rows) > 0 {
				hash = rows[0]
				if _, err := tx.Exec(`UPDATE web_authorizations SET browser = ?, platform = ?, ip = ?, region = ?, write_allowed = write_allowed OR ?, date_active = ?
					WHERE hash = ?`, w.Browser, w.Platform, w.IP, w.Region, w.WriteAllowed, now, hash); err != nil {
					return err
				}
			} else if _, err := tx.Exec(`INSERT INTO web_authorizations (hash, user_id, bot_id, domain, browser, platform, ip, region, write_allowed, date_created, date_active)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				hash, w.UserId, w.BotId, w.Domain, w.Browser, w.Platform, w.IP, w.Region, w.WriteAllowed, now, now); err != nil {
				return err
			}
			return tx.QueryRowPartial(&out, "SELECT "+webAuthColumns+" FROM web_authorizations WHERE hash = ?", hash)
		})
		if retryable(err) {
			// A hash taken (2^-63), or the same login recorded at the same
			// moment: the next round updates that record.
			continue
		}
		return out, err
	}
	return WebAuthorization{}, fmt.Errorf("botstore: web authorization not stored after %d attempts", webAuthAttempts)
}

// WebAuthorizationOf returns the user's record of the bot and domain, nil
// for none: a user who has one logs in again without being asked.
func (s *Store) WebAuthorizationOf(ctx context.Context, userId, botId int64, domain string) (*WebAuthorization, error) {
	d, ok := NormalizeLoginDomain(domain)
	if !ok {
		return nil, nil
	}
	var rows []WebAuthorization
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+webAuthColumns+" FROM web_authorizations WHERE user_id = ? AND bot_id = ? AND domain = ?",
		userId, botId, d); err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// TouchWebAuthorization sets date_active of the user's record to now.
func (s *Store) TouchWebAuthorization(ctx context.Context, userId, hash int64, now int32) error {
	_, err := s.db.Exec(ctx, "UPDATE web_authorizations SET date_active = GREATEST(date_active, ?) WHERE user_id = ? AND hash = ?", now, userId, hash)
	return err
}

// WebAuthorizations returns the user's records, the last active first.
func (s *Store) WebAuthorizations(ctx context.Context, userId int64) ([]WebAuthorization, error) {
	rows := []WebAuthorization{}
	err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+webAuthColumns+" FROM web_authorizations WHERE user_id = ? ORDER BY date_active DESC, hash", userId)
	return rows, err
}

// ResetWebAuthorization removes the user's record of the hash; removed is
// false for a hash that is not the user's.
func (s *Store) ResetWebAuthorization(ctx context.Context, userId, hash int64) (removed bool, err error) {
	r, err := s.db.Exec(ctx, "DELETE FROM web_authorizations WHERE user_id = ? AND hash = ?", userId, hash)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// ResetWebAuthorizations removes all the user's records and returns how
// many there were.
func (s *Store) ResetWebAuthorizations(ctx context.Context, userId int64) (int64, error) {
	r, err := s.db.Exec(ctx, "DELETE FROM web_authorizations WHERE user_id = ?", userId)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

// How a user let a bot write to them, bot_write_allowed.source; the reason
// of messageActionBotAllowed (domain, app, attach_menu, from_request).
const (
	WriteAllowedDomain      = 1
	WriteAllowedApp         = 2
	WriteAllowedAttachMenu  = 3
	WriteAllowedFromRequest = 4
)

// AllowWrite records that the user lets the bot write to them; added is
// true the first time only, when the caller posts messageActionBotAllowed,
// once per bot. The first source stays.
func (s *Store) AllowWrite(ctx context.Context, userId, botId int64, source int8, now int32) (added bool, err error) {
	if source < WriteAllowedDomain || source > WriteAllowedFromRequest {
		return false, fmt.Errorf("botstore: unknown write permission source %d", source)
	}
	r, err := s.db.Exec(ctx, "INSERT IGNORE INTO bot_write_allowed (user_id, bot_id, source, allowed_at) VALUES (?, ?, ?, ?)", userId, botId, source, now)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// WriteAllowed tells whether the user let the bot write to them.
func (s *Store) WriteAllowed(ctx context.Context, userId, botId int64) (bool, error) {
	var rows []int64
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT bot_id FROM bot_write_allowed WHERE user_id = ? AND bot_id = ?", userId, botId); err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

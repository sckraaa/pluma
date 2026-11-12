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

// Package contacttokens keeps the temporary links to a profile (task 084):
// contacts.exportContactToken makes one for the account,
// contacts.importContactToken gives the user of one that has not expired
// ("Temporary profile links ... can be generated even for profiles that
// don't have a username, and they have an expiration date",
// core.telegram.org/api/links).
package contacttokens

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"regexp"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// Lifetime is how long a token opens the profile, in seconds. Telegram's
// number is not documented; iOS counts down from 300 seconds while it
// waits for the token (ChatQrCodeScreen.swift:1899-1906), and tdesktop
// shows what is left as minutes and seconds
// (info_profile_qr_link.cpp:79-83), so five minutes is what the clients
// are made for.
const Lifetime = 300

// tokenBytes of randomness make a token nobody guesses: 18 bytes, 24
// characters of the URL alphabet of base64, which the link host accepts in
// a path (app/interface/links: [A-Za-z0-9_-]{1,128}).
const tokenBytes = 18

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]{24}$`)

// Valid tells whether token has the form of a token of this server; one
// that has not is never looked up.
func Valid(token string) bool {
	return tokenRe.MatchString(token)
}

// Store reads and writes the tokens.
type Store struct {
	db *sqlx.DB
}

// NewStore returns a Store on db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// liveMax is how many tokens of one account are valid at once: enough for
// its devices to show their QR codes together, and a bound on what an
// account can make the table hold.
const liveMax = 10

// Export makes a token of userID at now, unix time, and returns it with
// the time it expires. The tokens that have expired, of any account, are
// removed on the way, so the table holds only what can still be used. The
// valid ones stay valid, a QR code already shown keeps working until its
// time; only the oldest of an account beyond liveMax go.
func (s *Store) Export(ctx context.Context, userID, now int64) (token string, expires int64, err error) {
	var b [tokenBytes]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", 0, err
	}
	token = base64.RawURLEncoding.EncodeToString(b[:])
	expires = now + Lifetime
	if _, err = s.db.Exec(ctx, "DELETE FROM contact_tokens WHERE expires <= ?", now); err != nil {
		return "", 0, err
	}
	if _, err = s.db.Exec(ctx, "INSERT INTO contact_tokens (token, user_id, expires) VALUES (?, ?, ?)", token, userID, expires); err != nil {
		return "", 0, err
	}
	var oldest []int64
	if err = s.db.QueryRowsPartial(ctx, &oldest, "SELECT expires FROM contact_tokens WHERE user_id = ? ORDER BY expires DESC LIMIT 1 OFFSET ?", userID, liveMax-1); err != nil {
		return "", 0, err
	}
	if len(oldest) > 0 {
		if _, err = s.db.Exec(ctx, "DELETE FROM contact_tokens WHERE user_id = ? AND expires < ?", userID, oldest[0]); err != nil {
			return "", 0, err
		}
	}
	return token, expires, nil
}

// Import returns the user of token at now, unix time, 0 for a token that
// is unknown or has expired.
func (s *Store) Import(ctx context.Context, token string, now int64) (int64, error) {
	if !Valid(token) {
		return 0, nil
	}
	var ids []int64
	if err := s.db.QueryRowsPartial(ctx, &ids, "SELECT user_id FROM contact_tokens WHERE token = ? AND expires > ?", token, now); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}

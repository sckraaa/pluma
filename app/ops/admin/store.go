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

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// Roles of the panel, weakest first.
const (
	roleObserver  int8 = 0
	roleModerator int8 = 1
	roleOwner     int8 = 2
)

var errNoChallenge = errors.New("challenge unknown or expired")

const (
	challengeTTL = 5 * time.Minute
	sessionTTL   = 12 * time.Hour
)

// admin is a person of the panel.
type admin struct {
	Login    string `db:"login" json:"login"`
	Role     int8   `db:"role" json:"role"`
	Disabled bool   `db:"disabled" json:"disabled"`
}

// store is the panel's own tables.
type store struct {
	db *sqlx.DB
}

func (s *store) admin(ctx context.Context, login string) (*admin, error) {
	var rows []admin
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT login, role, disabled FROM admin_users WHERE login = ?", login); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (s *store) admins(ctx context.Context) ([]admin, error) {
	var rows []admin
	err := s.db.QueryRowsPartial(ctx, &rows, "SELECT login, role, disabled FROM admin_users ORDER BY login")
	return rows, err
}

// setAdmin adds a person or changes the role of one, and ends the sessions
// of a person whose access was taken away.
func (s *store) setAdmin(ctx context.Context, login string, role int8, disabled bool, by string) error {
	if _, err := s.db.Exec(ctx,
		"INSERT INTO admin_users (login, role, disabled, created_by, created_at) VALUES (?, ?, ?, ?, ?)"+
			" ON DUPLICATE KEY UPDATE role = VALUES(role), disabled = VALUES(disabled)",
		login, role, disabled, by, time.Now().Unix()); err != nil {
		return err
	}
	if !disabled {
		return nil
	}
	_, err := s.db.Exec(ctx, "DELETE FROM admin_sessions WHERE login = ?", login)
	return err
}

func (s *store) passkeysOf(ctx context.Context, login string) ([]*passkey, error) {
	var rows []struct {
		Id        []byte `db:"credential_id"`
		X         []byte `db:"public_x"`
		Y         []byte `db:"public_y"`
		SignCount int64  `db:"sign_count"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT credential_id, public_x, public_y, sign_count FROM admin_passkeys WHERE login = ?", login); err != nil {
		return nil, err
	}
	out := make([]*passkey, len(rows))
	for i, r := range rows {
		out[i] = &passkey{id: r.Id, x: r.X, y: r.Y, signCount: uint32(r.SignCount)}
	}
	return out, nil
}

func (s *store) addPasskey(ctx context.Context, login string, p *passkey) error {
	_, err := s.db.Exec(ctx,
		"INSERT INTO admin_passkeys (credential_id, login, public_x, public_y, sign_count, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		p.id, login, p.x, p.y, p.signCount, time.Now().Unix())
	return err
}

func (s *store) updateSignCount(ctx context.Context, id []byte, count uint32) error {
	_, err := s.db.Exec(ctx, "UPDATE admin_passkeys SET sign_count = ? WHERE credential_id = ?", count, id)
	return err
}

// putChallenge stores a challenge bound to a person, a purpose and, for a
// step-up, the digest of the action it allows.
func (s *store) putChallenge(ctx context.Context, challenge, login, purpose, digest string) error {
	_, err := s.db.Exec(ctx,
		"INSERT INTO admin_challenges (challenge, login, purpose, digest, expires_at) VALUES (?, ?, ?, ?, ?)",
		challenge, login, purpose, digest, time.Now().Add(challengeTTL).Unix())
	return err
}

// takeChallenge consumes a challenge: a challenge is good for one ceremony
// only. It returns errNoChallenge unless the challenge is live and of the
// same person, purpose and digest.
func (s *store) takeChallenge(ctx context.Context, challenge, login, purpose, digest string) error {
	r, err := s.db.Exec(ctx,
		"DELETE FROM admin_challenges WHERE challenge = ? AND login = ? AND purpose = ? AND digest = ? AND expires_at > ?",
		challenge, login, purpose, digest, time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errNoChallenge
	}
	return nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *store) createSession(ctx context.Context, token, login string) error {
	_, err := s.db.Exec(ctx,
		"INSERT INTO admin_sessions (token_hash, login, expires_at) VALUES (?, ?, ?)",
		tokenHash(token), login, time.Now().Add(sessionTTL).Unix())
	return err
}

// sessionLogin returns the person of a live session, "" if none.
func (s *store) sessionLogin(ctx context.Context, token string) (string, error) {
	var rows []struct {
		Login string `db:"login"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT login FROM admin_sessions WHERE token_hash = ? AND expires_at > ?", tokenHash(token), time.Now().Unix()); err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].Login, nil
}

func (s *store) deleteSession(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, "DELETE FROM admin_sessions WHERE token_hash = ?", tokenHash(token))
	return err
}

// prune removes expired challenges and sessions.
func (s *store) prune(ctx context.Context) error {
	now := time.Now().Unix()
	if _, err := s.db.Exec(ctx, "DELETE FROM admin_challenges WHERE expires_at <= ?", now); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, "DELETE FROM admin_sessions WHERE expires_at <= ?", now)
	return err
}


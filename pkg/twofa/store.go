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

package twofa

import (
	"context"
	"errors"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// Password is a user's 2FA password as the server keeps it: the SRP salts
// and verifier v = g^x mod p, never the password or x. A row of
// user_passwords (teamgramd/deploy/sql/migrate-20261024-passwords.sql).
type Password struct {
	UserId   int64  `db:"user_id"`
	Salt1    []byte `db:"salt1"`
	Salt2    []byte `db:"salt2"`
	Verifier []byte `db:"verifier"`
	Hint     string `db:"hint"`
	SetAt    int64  `db:"set_at"` // unix seconds
	// PendingResetDate is when a reset requested with account.resetPassword
	// may remove the password (unix seconds); 0 if none is pending.
	PendingResetDate int64 `db:"pending_reset_date"`
	// PendingDeleteDate is when account.deleteAccount without the password
	// may delete the account (unix seconds); 0 if no deletion is pending.
	PendingDeleteDate int64 `db:"pending_delete_date"`
}

// Store keeps the passwords. Every change is a single conditional statement,
// so concurrent requests of one user cannot interleave inside one.
type Store interface {
	// Get returns the password of userId, or nil if there is none.
	Get(ctx context.Context, userId int64) (*Password, error)
	// Put sets the password of p.UserId, replacing any and dropping the
	// pending reset and deletion.
	Put(ctx context.Context, p *Password) error
	// Delete removes the password of userId; removed is false if there was none.
	Delete(ctx context.Context, userId int64) (removed bool, err error)
	// StartReset sets the reset date of userId's password if no reset is
	// pending; started is false if there is no password or a reset is pending.
	StartReset(ctx context.Context, userId, date int64) (started bool, err error)
	// FinishReset removes userId's password if its reset date is set and not
	// after now.
	FinishReset(ctx context.Context, userId, now int64) (removed bool, err error)
	// CancelReset clears the reset date; cancelled is false if none was set.
	CancelReset(ctx context.Context, userId int64) (cancelled bool, err error)
	// StartDelete sets the deletion date of userId's account if none is set.
	StartDelete(ctx context.Context, userId, date int64) (started bool, err error)
	// CancelDelete clears the deletion date; cancelled is false if none was set.
	CancelDelete(ctx context.Context, userId int64) (cancelled bool, err error)
}

// MySQLStore is the Store on the table user_passwords.
type MySQLStore struct {
	db *sqlx.DB
}

// NewMySQLStore returns the Store on db. Reads go to the primary: a check
// must see a password the moment it is changed.
func NewMySQLStore(db *sqlx.DB) *MySQLStore {
	return &MySQLStore{db: db.Master()}
}

func (s *MySQLStore) Get(ctx context.Context, userId int64) (*Password, error) {
	var p Password
	err := s.db.QueryRowPartial(ctx, &p,
		"select user_id, salt1, salt2, verifier, hint, set_at, pending_reset_date, pending_delete_date from user_passwords where user_id = ?",
		userId)
	switch {
	case errors.Is(err, sqlx.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &p, nil
}

func (s *MySQLStore) Put(ctx context.Context, p *Password) error {
	_, err := s.db.Exec(ctx,
		"insert into user_passwords(user_id, salt1, salt2, verifier, hint, set_at, pending_reset_date, pending_delete_date) values (?, ?, ?, ?, ?, ?, 0, 0)"+
			" on duplicate key update salt1 = values(salt1), salt2 = values(salt2), verifier = values(verifier), hint = values(hint),"+
			" set_at = values(set_at), pending_reset_date = 0, pending_delete_date = 0",
		p.UserId, p.Salt1, p.Salt2, p.Verifier, p.Hint, p.SetAt)
	return err
}

func (s *MySQLStore) Delete(ctx context.Context, userId int64) (bool, error) {
	return s.exec(ctx, "delete from user_passwords where user_id = ?", userId)
}

func (s *MySQLStore) StartReset(ctx context.Context, userId, date int64) (bool, error) {
	return s.exec(ctx, "update user_passwords set pending_reset_date = ? where user_id = ? and pending_reset_date = 0", date, userId)
}

func (s *MySQLStore) FinishReset(ctx context.Context, userId, now int64) (bool, error) {
	return s.exec(ctx, "delete from user_passwords where user_id = ? and pending_reset_date <> 0 and pending_reset_date <= ?", userId, now)
}

func (s *MySQLStore) CancelReset(ctx context.Context, userId int64) (bool, error) {
	return s.exec(ctx, "update user_passwords set pending_reset_date = 0 where user_id = ? and pending_reset_date <> 0", userId)
}

func (s *MySQLStore) StartDelete(ctx context.Context, userId, date int64) (bool, error) {
	return s.exec(ctx, "update user_passwords set pending_delete_date = ? where user_id = ? and pending_delete_date = 0", date, userId)
}

func (s *MySQLStore) CancelDelete(ctx context.Context, userId int64) (bool, error) {
	return s.exec(ctx, "update user_passwords set pending_delete_date = 0 where user_id = ? and pending_delete_date <> 0", userId)
}

func (s *MySQLStore) exec(ctx context.Context, query string, args ...any) (bool, error) {
	r, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

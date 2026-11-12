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
	// Email is the confirmed recovery email (ADR 0058), EmailUnconfirmed
	// the address waiting for its code; empty if none. They live with the
	// password: whatever removes it removes them
	// (migrate-20261116-password-email.sql).
	Email            string `db:"email"`
	EmailUnconfirmed string `db:"email_unconfirmed"`
	// EmailSetAt is when Email was confirmed (unix seconds); 0 without one.
	EmailSetAt int64 `db:"email_set_at"`
	// AwaitsEmail marks a first password set with a recovery email: it is
	// not in force until EmailUnconfirmed is confirmed, and the user has no
	// password until then (ADR 0058, p. 4).
	AwaitsEmail bool `db:"awaits_email"`
}

// Emails are the recovery email of a password: the confirmed address and
// the one waiting for its code.
type Emails struct {
	Email       string
	Unconfirmed string
}

func (p *Password) emails() Emails {
	if p == nil {
		return Emails{}
	}
	return Emails{Email: p.Email, Unconfirmed: p.EmailUnconfirmed}
}

// Store keeps the passwords. Every change is a single statement. Those of
// the Service's requests are conditional on the row the request read
// (Replace, RemoveIf, SwapEmail), so a request never overwrites a change
// made after its read; Put and Delete are not, for the tools and tests.
type Store interface {
	// Get returns the password of userId, or nil if there is none.
	Get(ctx context.Context, userId int64) (*Password, error)
	// Put sets the password of p.UserId in force, replacing any and dropping
	// the pending reset and deletion. The recovery email of a replaced
	// password stays; a new row has none.
	Put(ctx context.Context, p *Password) error
	// Replace sets next, with its recovery email and AwaitsEmail, as the
	// password of next.UserId and drops the pending reset and deletion, if
	// the row is still prev: none for a nil prev, otherwise the same
	// password (set_at, verifier), recovery email and AwaitsEmail.
	// replaced is false otherwise. next differs from prev.
	Replace(ctx context.Context, prev, next *Password) (replaced bool, err error)
	// RemoveIf removes the password of prev.UserId if the row is still prev
	// (as for Replace); removed is false otherwise.
	RemoveIf(ctx context.Context, prev *Password) (removed bool, err error)
	// SwapEmail sets the recovery email of userId's password in force to
	// next (and email_set_at to emailSetAt) if the password is still the
	// one set at setAt and its recovery email is still prev; swapped is
	// false otherwise. prev and next differ.
	SwapEmail(ctx context.Context, userId, setAt int64, prev, next Emails, emailSetAt int64) (swapped bool, err error)
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
		"select user_id, salt1, salt2, verifier, hint, set_at, pending_reset_date, pending_delete_date, email, email_unconfirmed, email_set_at, awaits_email"+
			" from user_passwords where user_id = ?",
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
			" set_at = values(set_at), pending_reset_date = 0, pending_delete_date = 0, awaits_email = 0",
		p.UserId, p.Salt1, p.Salt2, p.Verifier, p.Hint, p.SetAt)
	return err
}

// Replace without prev inserts the row only if there is none: the no-op
// update of an existing one changes no row. With prev it updates the row
// only if it is still prev; next differs from prev, so the affected rows
// (changed rows, the driver's default) tell whether the condition held.
func (s *MySQLStore) Replace(ctx context.Context, prev, next *Password) (bool, error) {
	if prev == nil {
		return s.exec(ctx,
			"insert into user_passwords(user_id, salt1, salt2, verifier, hint, set_at, pending_reset_date, pending_delete_date,"+
				" email, email_unconfirmed, email_set_at, awaits_email)"+
				" values (?, ?, ?, ?, ?, ?, 0, 0, ?, ?, ?, ?)"+
				" on duplicate key update user_id = user_id",
			next.UserId, next.Salt1, next.Salt2, next.Verifier, next.Hint, next.SetAt,
			next.Email, next.EmailUnconfirmed, next.EmailSetAt, next.AwaitsEmail)
	}
	return s.exec(ctx,
		"update user_passwords set salt1 = ?, salt2 = ?, verifier = ?, hint = ?, set_at = ?, pending_reset_date = 0, pending_delete_date = 0,"+
			" email = ?, email_unconfirmed = ?, email_set_at = ?, awaits_email = ?"+
			" where user_id = ? and set_at = ? and verifier = ? and email = ? and email_unconfirmed = ? and awaits_email = ?",
		next.Salt1, next.Salt2, next.Verifier, next.Hint, next.SetAt,
		next.Email, next.EmailUnconfirmed, next.EmailSetAt, next.AwaitsEmail,
		prev.UserId, prev.SetAt, prev.Verifier, prev.Email, prev.EmailUnconfirmed, prev.AwaitsEmail)
}

func (s *MySQLStore) RemoveIf(ctx context.Context, prev *Password) (bool, error) {
	return s.exec(ctx,
		"delete from user_passwords"+
			" where user_id = ? and set_at = ? and verifier = ? and email = ? and email_unconfirmed = ? and awaits_email = ?",
		prev.UserId, prev.SetAt, prev.Verifier, prev.Email, prev.EmailUnconfirmed, prev.AwaitsEmail)
}

// SwapEmail changes the row only if next differs from prev, so the affected
// rows (changed rows, the driver's default) tell whether the condition held.
func (s *MySQLStore) SwapEmail(ctx context.Context, userId, setAt int64, prev, next Emails, emailSetAt int64) (bool, error) {
	return s.exec(ctx,
		"update user_passwords set email = ?, email_unconfirmed = ?, email_set_at = ?"+
			" where user_id = ? and set_at = ? and email = ? and email_unconfirmed = ? and awaits_email = 0",
		next.Email, next.Unconfirmed, emailSetAt, userId, setAt, prev.Email, prev.Unconfirmed)
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

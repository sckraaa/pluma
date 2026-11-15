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

package webbrowser

import (
	"context"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// MySQLStore is the Store on the tables user_web_browser_settings and
// user_web_browser_exceptions
// (teamgramd/deploy/sql/migrate-20261119-web-browser-settings.sql).
type MySQLStore struct {
	db  *sqlx.DB
	now func() time.Time
}

// NewMySQLStore returns the Store on db. Reads go to the primary: a client
// reads the settings right after it changes them.
func NewMySQLStore(db *sqlx.DB) *MySQLStore {
	return &MySQLStore{db: db.Master(), now: time.Now}
}

type settingsRow struct {
	OpenExternal       bool `db:"open_external_browser"`
	DisplayCloseButton bool `db:"display_close_button"`
	FlagsSet           bool `db:"flags_set"`
}

type exceptionRow struct {
	Domain       string `db:"domain"`
	URL          string `db:"url"`
	OpenExternal bool   `db:"open_external"`
	CreatedAt    int64  `db:"created_at"`
}

const exceptionColumns = "domain, url, open_external, created_at"

func (s *MySQLStore) Get(ctx context.Context, userId int64) (Settings, error) {
	settings := Defaults()
	// One transaction, so that the flags and the lists are of one moment.
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		var row settingsRow
		err := tx.QueryRowPartial(&row,
			"select open_external_browser, display_close_button, flags_set from user_web_browser_settings where user_id = ?", userId)
		switch {
		case errors.Is(err, sqlx.ErrNotFound):
		case err != nil:
			return err
		case row.FlagsSet:
			settings.OpenExternal, settings.DisplayCloseButton = row.OpenExternal, row.DisplayCloseButton
		}
		rows, err := exceptionsOf(tx, userId, false)
		if err != nil {
			return err
		}
		settings.External, settings.InApp = split(rows)
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s *MySQLStore) SetFlags(ctx context.Context, userId int64, openExternal, displayCloseButton bool) error {
	_, err := s.db.Exec(ctx,
		"insert into user_web_browser_settings (user_id, open_external_browser, display_close_button, flags_set, updated_at) values (?, ?, ?, 1, ?)"+
			" on duplicate key update open_external_browser = values(open_external_browser), display_close_button = values(display_close_button),"+
			" flags_set = 1, updated_at = values(updated_at)",
		userId, openExternal, displayCloseButton, s.now().UnixMilli())
	return err
}

func (s *MySQLStore) PutException(ctx context.Context, userId int64, e Exception, external bool) error {
	return retryLocks(ctx, func() error {
		return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
			if err := s.lock(tx, userId); err != nil {
				return err
			}
			rows, err := exceptionsOf(tx, userId, true)
			if err != nil {
				return err
			}
			// The new one goes last: after every exception of the account,
			// whatever the clock says.
			createdAt := s.now().UnixMilli()
			n := 1
			for _, r := range rows {
				createdAt = max(createdAt, r.CreatedAt+1)
				if r.OpenExternal == external && r.Domain != e.Domain {
					n++
				}
			}
			if n > MaxExceptions {
				return ErrLimit
			}
			_, err = tx.Exec(
				"insert into user_web_browser_exceptions (user_id, domain, url, open_external, created_at) values (?, ?, ?, ?, ?)"+
					" on duplicate key update url = values(url), open_external = values(open_external), created_at = values(created_at)",
				userId, e.Domain, e.URL, external, createdAt)
			return err
		})
	})
}

func (s *MySQLStore) DeleteException(ctx context.Context, userId int64, domain string, external *bool) (bool, error) {
	query, args := "delete from user_web_browser_exceptions where user_id = ? and domain = ?", []any{userId, domain}
	if external != nil {
		query, args = query+" and open_external = ?", append(args, *external)
	}
	var deleted bool
	err := retryLocks(ctx, func() error {
		return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
			if err := s.lock(tx, userId); err != nil {
				return err
			}
			r, err := tx.Exec(query, args...)
			if err != nil {
				return err
			}
			n, err := r.RowsAffected()
			deleted = n > 0
			return err
		})
	})
	return deleted, err
}

func (s *MySQLStore) DeleteAllExceptions(ctx context.Context, userId int64) (Settings, error) {
	var removed Settings
	err := retryLocks(ctx, func() error {
		return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
			if err := s.lock(tx, userId); err != nil {
				return err
			}
			rows, err := exceptionsOf(tx, userId, true)
			if err != nil {
				return err
			}
			if _, err = tx.Exec("delete from user_web_browser_exceptions where user_id = ?", userId); err != nil {
				return err
			}
			removed.External, removed.InApp = split(rows)
			return nil
		})
	})
	if err != nil {
		return Settings{}, err
	}
	return removed, nil
}

// lock takes the account's row of user_web_browser_settings for the
// transaction, making it if there is none (with flags_set = 0, so that the
// defaults still apply): the writes of the exceptions of one account go
// one after the other, and two of them never pass the limit together.
func (s *MySQLStore) lock(tx *sqlx.Tx, userId int64) error {
	d := Defaults()
	_, err := tx.Exec(
		"insert into user_web_browser_settings (user_id, open_external_browser, display_close_button, flags_set, updated_at) values (?, ?, ?, 0, ?)"+
			" on duplicate key update user_id = user_id",
		userId, d.OpenExternal, d.DisplayCloseButton, s.now().UnixMilli())
	return err
}

// exceptionsOf reads the exceptions of userId in their order; forUpdate
// locks them, for a transaction that holds the account's lock.
func exceptionsOf(tx *sqlx.Tx, userId int64, forUpdate bool) ([]exceptionRow, error) {
	query := "select " + exceptionColumns + " from user_web_browser_exceptions where user_id = ? order by created_at, domain"
	if forUpdate {
		query += " for update"
	}
	var rows []exceptionRow
	if err := tx.QueryRowsPartial(&rows, query, userId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return rows, nil
}

func split(rows []exceptionRow) (external, inApp []Exception) {
	for _, r := range rows {
		e := Exception{Domain: r.Domain, URL: r.URL}
		if r.OpenExternal {
			external = append(external, e)
		} else {
			inApp = append(inApp, e)
		}
	}
	return external, inApp
}

// retryLocks runs f again after a deadlock or a lock wait timeout, up to
// five times: InnoDB rolled the transaction back, nothing of it was kept.
func retryLocks(ctx context.Context, f func() error) error {
	var err error
	for range 5 {
		if err = f(); err == nil || !lockError(err) || ctx.Err() != nil {
			return err
		}
	}
	return err
}

func lockError(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == 1213 || me.Number == 1205)
}

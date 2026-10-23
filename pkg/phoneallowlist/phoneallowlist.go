// Package phoneallowlist stores the phones allowed to log in or sign up
// (closed registration, task 001). The table is phone_allowlist
// (teamgramd/deploy/sql/migrate-20261021.sql); administrators manage it with
// tools/pluma-admin. Phones are normalized digits without '+', as in
// users.phone.
package phoneallowlist

import (
	"context"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// Entry is one allowed phone.
type Entry struct {
	Phone     string `db:"phone"`
	Note      string `db:"note"`
	CreatedAt int64  `db:"created_at"` // unix seconds
}

// DAO reads and changes the allowlist.
type DAO struct {
	db *sqlx.DB
}

// New returns a DAO on db.
func New(db *sqlx.DB) *DAO {
	return &DAO{db: db}
}

// Contains reports whether phone is on the allowlist.
func (d *DAO) Contains(ctx context.Context, phone string) (bool, error) {
	var n int64
	if err := d.db.QueryRow(ctx, &n, "select count(*) from phone_allowlist where phone = ?", phone); err != nil {
		return false, err
	}
	return n > 0, nil
}

// Add puts phone on the allowlist; added is false if it was already there.
func (d *DAO) Add(ctx context.Context, phone, note string) (added bool, err error) {
	r, err := d.db.Exec(ctx, "insert ignore into phone_allowlist(phone, note) values (?, ?)", phone, note)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// Remove takes phone off the allowlist; removed is false if it was not there.
// Existing sessions of the account are not affected.
func (d *DAO) Remove(ctx context.Context, phone string) (removed bool, err error) {
	r, err := d.db.Exec(ctx, "delete from phone_allowlist where phone = ?", phone)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// List returns the allowlist ordered by phone.
func (d *DAO) List(ctx context.Context) ([]Entry, error) {
	var entries []Entry
	err := d.db.QueryRows(ctx, &entries, "select phone, note, unix_timestamp(created_at) as created_at from phone_allowlist order by phone")
	return entries, err
}

// Package phoneallowlist stores who may log in or sign up: the phones of
// the closed registration (task 001, table phone_allowlist,
// teamgramd/deploy/sql/migrate-20261021.sql) and the switch that opens
// registration to every number (table registration_settings, task 072;
// no row: by list). The panel and tools/pluma-admin manage them. Phones are
// normalized digits without '+', as in users.phone.
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

// Contains reports whether phone may log in or sign up: registration is
// open to every number, or phone is on the allowlist. The login flow reads it
// on every request (logincode.Service, pkg/logincode), so that a switch of
// the panel takes effect at the next auth.sendCode and auth.signUp.
func (d *DAO) Contains(ctx context.Context, phone string) (bool, error) {
	var n int64
	if err := d.db.QueryRow(ctx, &n,
		"select (select count(*) from registration_settings where id = 1 and is_open = 1) + (select count(*) from phone_allowlist where phone = ?)", phone); err != nil {
		return false, err
	}
	return n > 0, nil
}

// RegistrationOpen tells whether registration is open to every number.
func (d *DAO) RegistrationOpen(ctx context.Context) (bool, error) {
	var n int64
	if err := d.db.QueryRow(ctx, &n, "select count(*) from registration_settings where id = 1 and is_open = 1"); err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetRegistrationOpen opens registration to every number or back to the list.
func (d *DAO) SetRegistrationOpen(ctx context.Context, open bool, by string) error {
	_, err := d.db.Exec(ctx,
		"insert into registration_settings (id, is_open, updated_by, updated_at) values (1, ?, ?, unix_timestamp())"+
			" on duplicate key update is_open = values(is_open), updated_by = values(updated_by), updated_at = values(updated_at)",
		open, by)
	return err
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

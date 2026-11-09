package logincode

import (
	"context"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// RegistrationSwitch tells whether registration is open: anyone may ask for a
// code, not only the phones of the allowlist.
type RegistrationSwitch interface {
	RegistrationOpen(ctx context.Context) (bool, error)
}

// Registration is the switch in MySQL: the table registration_settings, one
// row (migrate-20261109-login-codes.sql). No row is the default: by list.
type Registration struct {
	db *sqlx.DB
}

// NewRegistration returns the switch on db.
func NewRegistration(db *sqlx.DB) *Registration {
	return &Registration{db: db}
}

// RegistrationOpen reports whether registration is open.
func (r *Registration) RegistrationOpen(ctx context.Context) (bool, error) {
	var n int64
	if err := r.db.QueryRow(ctx, &n, "select count(*) from registration_settings where id = 1 and is_open = 1"); err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetRegistrationOpen opens registration or returns it to the list.
func (r *Registration) SetRegistrationOpen(ctx context.Context, open bool) error {
	v := 0
	if open {
		v = 1
	}
	_, err := r.db.Exec(ctx, "insert into registration_settings(id, is_open) values (1, ?) on duplicate key update is_open = values(is_open)", v)
	return err
}

// RegistrationOpen reports whether registration is open; a Service without
// a switch is by list.
func (s *Service) RegistrationOpen(ctx context.Context) (bool, error) {
	if s.registration == nil {
		return false, nil
	}
	return s.registration.RegistrationOpen(ctx)
}

// SetRegistrationSwitch installs the switch.
func (s *Service) SetRegistrationSwitch(r RegistrationSwitch) { s.registration = r }

// SetSender installs the Telegram delivery of codes.
func (s *Service) SetSender(sender Sender) { s.sender = sender }

// allowed reports whether phone may log in or sign up: on the allowlist, or
// anyone while registration is open.
func (s *Service) allowed(ctx context.Context, phone string) (bool, error) {
	ok, err := s.allowlist.Contains(ctx, phone)
	if err != nil || ok {
		return ok, err
	}
	return s.RegistrationOpen(ctx)
}

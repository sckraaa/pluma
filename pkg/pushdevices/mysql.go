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

package pushdevices

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"

	"github.com/zeromicro/go-zero/core/logx"
)

// MySQLStore is the Store on the table push_devices
// (teamgramd/deploy/sql/migrate-20261025-push-devices.sql).
type MySQLStore struct {
	db     *sqlx.DB
	sealer *Sealer
	now    func() time.Time
}

// NewMySQLStore returns the Store on db. sealer may be nil for a user that
// only removes devices; Register and List need it. Reads go to the primary:
// a device registered a moment ago gets the next push.
func NewMySQLStore(db *sqlx.DB, sealer *Sealer) *MySQLStore {
	return &MySQLStore{db: db.Master(), sealer: sealer, now: time.Now}
}

var errNoSealer = errors.New("pushdevices: the store has no sealer")

type deviceRow struct {
	Id           int64  `db:"id"`
	UserId       int64  `db:"user_id"`
	AuthKeyId    int64  `db:"auth_key_id"`
	TokenType    int32  `db:"token_type"`
	Token        string `db:"token"`
	AppSandbox   bool   `db:"app_sandbox"`
	NoMuted      bool   `db:"no_muted"`
	Secret       []byte `db:"secret"`
	OtherUids    string `db:"other_uids"`
	LangCode     string `db:"lang_code"`
	LockedPeriod int32  `db:"locked_period"`
	Invalidated  bool   `db:"invalidated"`
	CreatedAt    int64  `db:"created_at"`
	UpdatedAt    int64  `db:"updated_at"`
}

const deviceColumns = "id, user_id, auth_key_id, token_type, token, app_sandbox, no_muted, secret, other_uids, lang_code, locked_period, invalidated, created_at, updated_at"

func (s *MySQLStore) Register(ctx context.Context, d *Device) error {
	if s.sealer == nil {
		return errNoSealer
	}
	sealed, err := s.sealer.Seal(d)
	if err != nil {
		return err
	}
	if sealed == nil {
		sealed = []byte{} // the column is not null
	}
	hash := tokenHash(d.Token)
	now := s.now().Unix()
	invalidated := false
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		var old struct {
			Id          int64 `db:"id"`
			Invalidated bool  `db:"invalidated"`
			CreatedAt   int64 `db:"created_at"`
		}
		err := tx.QueryRowPartial(&old,
			"select id, invalidated, created_at from push_devices where user_id = ? and token_type = ? and token_hash = ? for update",
			d.UserId, d.TokenType, hash)
		switch {
		case errors.Is(err, sqlx.ErrNotFound):
			old.CreatedAt = now
		case err != nil:
			return err
		case old.Invalidated:
			invalidated = true
			_, err = tx.Exec("delete from push_devices where id = ?", old.Id)
			return err
		}
		if _, err = tx.Exec(
			"delete from push_devices where (auth_key_id = ? and token_type = ?) or (user_id = ? and token_type = ? and token_hash = ?)",
			d.AuthKeyId, d.TokenType, d.UserId, d.TokenType, hash); err != nil {
			return err
		}
		r, err := tx.Exec(
			"insert into push_devices(user_id, auth_key_id, token_type, token, token_hash, app_sandbox, no_muted, secret, other_uids, lang_code, locked_period, invalidated, created_at, updated_at)"+
				" values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)",
			d.UserId, d.AuthKeyId, d.TokenType, d.Token, hash, d.AppSandbox, d.NoMuted, sealed, formatUids(d.OtherUids), d.LangCode, d.LockedPeriod,
			old.CreatedAt, now)
		if err != nil {
			return err
		}
		d.Id, err = r.LastInsertId()
		d.CreatedAt, d.UpdatedAt = old.CreatedAt, now
		return err
	})
	if err == nil && invalidated {
		return ErrTokenInvalidated
	}
	return err
}

func (s *MySQLStore) Unregister(ctx context.Context, userId int64, tokenType int32, token string) (bool, error) {
	return s.exec(ctx, "delete from push_devices where user_id = ? and token_type = ? and token_hash = ?", userId, tokenType, tokenHash(token))
}

func (s *MySQLStore) SetLockedPeriod(ctx context.Context, userId, authKeyId int64, period int32) error {
	_, err := s.db.Exec(ctx, "update push_devices set locked_period = ? where user_id = ? and auth_key_id = ?", period, userId, authKeyId)
	return err
}

func (s *MySQLStore) List(ctx context.Context, userId int64) ([]*Device, error) {
	if s.sealer == nil {
		return nil, errNoSealer
	}
	var rows []deviceRow
	err := s.db.QueryRowsPartial(ctx, &rows,
		"select "+deviceColumns+" from push_devices where user_id = ? and invalidated = 0 order by id", userId)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	devices := make([]*Device, 0, len(rows))
	for i := range rows {
		d := rows[i].device()
		if d.Secret, err = s.sealer.Open(d, rows[i].Secret); err != nil {
			// Another key, or a damaged row: skipped, not removed, so that a
			// wrong key in the config loses nothing. The client's next
			// registration replaces it.
			logx.WithContext(ctx).Errorf("pushdevices: the secret of device %d (user %d) does not open: skipped", d.Id, d.UserId)
			continue
		}
		devices = append(devices, d)
	}
	return devices, nil
}

func (s *MySQLStore) DeleteAuthorizations(ctx context.Context, userId int64, authKeyIds ...int64) (int64, error) {
	if len(authKeyIds) == 0 {
		return 0, nil
	}
	args := make([]any, 0, 1+len(authKeyIds))
	args = append(args, userId)
	for _, id := range authKeyIds {
		args = append(args, id)
	}
	r, err := s.db.Exec(ctx,
		"delete from push_devices where user_id = ? and auth_key_id in (?"+strings.Repeat(", ?", len(authKeyIds)-1)+")", args...)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

func (s *MySQLStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, "delete from push_devices where id = ?", id)
	return err
}

func (s *MySQLStore) Invalidate(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, "update push_devices set invalidated = 1, updated_at = ? where id = ?", s.now().Unix(), id)
	return err
}

func (s *MySQLStore) exec(ctx context.Context, query string, args ...any) (bool, error) {
	r, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

func (r *deviceRow) device() *Device {
	return &Device{
		Id:           r.Id,
		UserId:       r.UserId,
		AuthKeyId:    r.AuthKeyId,
		TokenType:    r.TokenType,
		Token:        r.Token,
		AppSandbox:   r.AppSandbox,
		NoMuted:      r.NoMuted,
		OtherUids:    parseUids(r.OtherUids),
		LangCode:     r.LangCode,
		LockedPeriod: r.LockedPeriod,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

func formatUids(uids []int64) string {
	s := make([]string, len(uids))
	for i, id := range uids {
		s[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(s, ",")
}

func parseUids(s string) []int64 {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	uids := make([]int64, 0, len(parts))
	for _, p := range parts {
		if id, err := strconv.ParseInt(p, 10, 64); err == nil {
			uids = append(uids, id)
		}
	}
	return uids
}

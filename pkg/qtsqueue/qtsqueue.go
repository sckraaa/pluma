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

// Package qtsqueue keeps the qts of each authorization and its queue of
// updates with qts (migrate-20261119-qts-updates.sql; task 105, ADR 0067).
//
// qts is the sequence of an authorization: a key and the account signed in
// on it ("qts is the sequence number of secret chat messages and other
// updates of the bot", core.telegram.org/api/updates). Its counter is
// encrypted_chat_devices.qts, and two queues take their numbers from it:
// the messages of secret chats (pkg/secretchat, encrypted_chat_queue) and
// the other updates with qts, kept here in qts_updates: what a connected
// bot of Telegram Business is told of the chats it serves. One counter
// keeps the two from ever giving one number twice, and updates.getDifference
// merges them by qts. A client confirms the numbers up to the qts of its
// updates.getDifference, and the confirmed rows go.
//
// The counter is taken in the transaction that writes the row of the
// queue, under the lock of its row: the numbers have no gaps and commit in
// their order, so that a reader that sees the counter at n sees every row
// up to n.
package qtsqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// FirstQts is the qts of an authorization with no events yet, the first
	// event being the next one. It is not 0: at a local qts of 0 Android
	// applies any update without looking for a gap and confirms it
	// (MessagesController.java:18113, :18298), so a lost or late first event
	// would be confirmed undelivered.
	FirstQts = 1
	// Retention is how long an update waits for its confirmation; then the
	// sweep drops it, as the queue of secret chats drops what is older than
	// 7 days (core.telegram.org/api/end-to-end). A bot that stays away
	// longer gets the newer updates only (task 105, decisions to confirm, 8).
	Retention = 7 * 24 * time.Hour
	// DeviceLimit is the most updates an authorization keeps unconfirmed:
	// one that never confirms must not fill the database within the
	// retention. Over it the oldest go first.
	DeviceLimit = 10000
	// PageSize is the most updates of the queue one difference carries;
	// PageBytes the most bytes of their data. They leave the rest of the
	// answer room under the 4 MiB a gRPC client receives (session asks bff
	// with the default, grpc clientconn.go:139), next to a page of secret
	// chats.
	PageSize  = 100
	PageBytes = 1 << 20
	// userIdsMax is the size of the column user_ids.
	userIdsMax = 1024
	sweepBatch = 1000
)

// deviceLimit is DeviceLimit, smaller in tests.
var deviceLimit int32 = DeviceLimit

// Device is an authorization: the account UserId signed in on the permanent
// key AuthKey.
type Device struct {
	UserId  int64
	AuthKey int64
}

// Tx is a transaction the queue writes in: a box transaction
// (userbox.Box.Tx, a marmota *sqlx.Tx) or a *sql.Tx through SQLTx. The
// reads of QueryRow are of one column.
type Tx interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(v any, query string, args ...any) error
}

type sqlTx struct {
	ctx context.Context
	tx  *sql.Tx
}

func (t sqlTx) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(t.ctx, query, args...)
}

func (t sqlTx) QueryRow(v any, query string, args ...any) error {
	return t.tx.QueryRowContext(t.ctx, query, args...).Scan(v)
}

// SQLTx is tx as a Tx, its statements run with ctx.
func SQLTx(ctx context.Context, tx *sql.Tx) Tx { return sqlTx{ctx: ctx, tx: tx} }

// Next gives d the next qts in tx and returns it. The row of the counter
// stays locked to the end of tx.
func Next(tx Tx, d Device) (int32, error) {
	if _, err := tx.Exec("INSERT INTO encrypted_chat_devices (auth_key_id, user_id, qts) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE qts = qts + 1",
		d.AuthKey, d.UserId, FirstQts+1); err != nil {
		return 0, err
	}
	var qts int32
	err := tx.QueryRow(&qts, "SELECT qts FROM encrypted_chat_devices WHERE auth_key_id = ? AND user_id = ?", d.AuthKey, d.UserId)
	return qts, err
}

// Devices returns the authorizations of userId that are signed in, by key,
// read in tx: the ones an update with qts goes to.
func Devices(tx interface {
	QueryRowsPartial(v any, query string, args ...any) error
}, userId int64) ([]int64, error) {
	var keys []int64
	err := tx.QueryRowsPartial(&keys, "SELECT auth_key_id FROM auth_users WHERE user_id = ? AND deleted = 0 ORDER BY auth_key_id", userId)
	return keys, err
}

// Append gives u the next qts of d in tx, sets it in u and queues u with
// the users it names, at now (unix time). It returns the qts. What d keeps
// past DeviceLimit goes, the oldest first.
func Append(tx Tx, d Device, u *mtproto.Update, userIds []int64, now int32) (int32, error) {
	qts, err := Next(tx, d)
	if err != nil {
		return 0, err
	}
	u.Qts = qts
	data, err := jsonx.Marshal(u)
	if err != nil {
		return 0, fmt.Errorf("qtsqueue: encode %s: %w", u.GetPredicateName(), err)
	}
	if _, err = tx.Exec("INSERT INTO qts_updates (auth_key_id, user_id, qts, update_data, user_ids, date) VALUES (?, ?, ?, ?, ?, ?)",
		d.AuthKey, d.UserId, qts, data, joinIds(userIds), now); err != nil {
		return 0, err
	}
	if qts > FirstQts+deviceLimit {
		res, err := tx.Exec("DELETE FROM qts_updates WHERE auth_key_id = ? AND user_id = ? AND qts <= ?", d.AuthKey, d.UserId, qts-deviceLimit)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			logx.Errorf("qtsqueue: user %d dropped %d unconfirmed updates over the limit", d.UserId, n)
		}
	}
	return qts, nil
}

// joinIds is ids as the column user_ids keeps them: the ids that fit in
// it, in order.
func joinIds(ids []int64) string {
	var b strings.Builder
	for _, id := range ids {
		s := strconv.FormatInt(id, 10)
		if b.Len()+len(s)+1 > userIdsMax {
			break
		}
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s)
	}
	return b.String()
}

func splitIds(s string) ([]int64, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Update is an update of the queue.
type Update struct {
	Qts    int32
	Update *mtproto.Update // with its qts
	// UserIds are the users it names, for the users of the difference.
	UserIds []int64
	Date    int32
}

// Store reads and confirms the queues.
type Store struct {
	db *sql.DB
}

// New returns the store on db.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Current returns the qts of d: the last one given, FirstQts when d has
// none yet.
func (s *Store) Current(ctx context.Context, d Device) (int32, error) {
	var qts int32
	err := s.db.QueryRowContext(ctx, "SELECT qts FROM encrypted_chat_devices WHERE auth_key_id = ? AND user_id = ?", d.AuthKey, d.UserId).Scan(&qts)
	if errors.Is(err, sql.ErrNoRows) {
		return FirstQts, nil
	}
	return qts, err
}

// Ack confirms the queue of d up to maxQts: the updates go.
func (s *Store) Ack(ctx context.Context, d Device, maxQts int32) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM qts_updates WHERE auth_key_id = ? AND user_id = ? AND qts <= ?", d.AuthKey, d.UserId, maxQts)
	return err
}

// Page returns the updates of d after the qts from up to upTo, in order, at
// most PageSize of them and PageBytes of their data; more tells that there
// are more up to upTo. upTo is a qts read before: a row past it may not be
// alone in having committed.
func (s *Store) Page(ctx context.Context, d Device, from, upTo int32) (out []*Update, more bool, err error) {
	if upTo <= from {
		return nil, false, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT qts, update_data, user_ids, date FROM qts_updates WHERE auth_key_id = ? AND user_id = ? AND qts > ? AND qts <= ? ORDER BY qts LIMIT ?",
		d.AuthKey, d.UserId, from, upTo, PageSize+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	size := 0
	for rows.Next() {
		if len(out) == PageSize {
			more = true
			break
		}
		var (
			u       = new(Update)
			data    []byte
			userIds string
		)
		if err = rows.Scan(&u.Qts, &data, &userIds, &u.Date); err != nil {
			return nil, false, err
		}
		if len(out) > 0 && size+len(data) > PageBytes {
			more = true
			break
		}
		size += len(data)
		if err = jsonx.Unmarshal(data, &u.Update); err != nil {
			return nil, false, fmt.Errorf("qtsqueue: update %d of user %d: %w", u.Qts, d.UserId, err)
		}
		if u.UserIds, err = splitIds(userIds); err != nil {
			return nil, false, fmt.Errorf("qtsqueue: users of update %d of user %d: %w", u.Qts, d.UserId, err)
		}
		out = append(out, u)
	}
	return out, more, rows.Err()
}

// Difference is what the queue adds to updates.getDifference.
type Difference struct {
	Updates []*Update
	// More tells that the page ends before the counter read: the state's
	// qts is then the last update's.
	More bool
}

// Difference confirms the queue of d up to clientQts (the implicit
// acknowledgement of updates.getDifference) and returns the page after it
// up to upTo, the counter the difference goes by.
func (s *Store) Difference(ctx context.Context, d Device, clientQts, upTo int32) (*Difference, error) {
	if clientQts > 0 {
		if err := s.Ack(ctx, d, clientQts); err != nil {
			return nil, fmt.Errorf("qtsqueue: difference ack: %w", err)
		}
	}
	page, more, err := s.Page(ctx, d, clientQts, upTo)
	if err != nil {
		return nil, fmt.Errorf("qtsqueue: difference: %w", err)
	}
	return &Difference{Updates: page, More: more}, nil
}

// Forget removes the counter and the queue of d, an authorization that is
// gone.
func Forget(ctx context.Context, db *sql.DB, d Device) error {
	if _, err := db.ExecContext(ctx, "DELETE FROM qts_updates WHERE auth_key_id = ? AND user_id = ?", d.AuthKey, d.UserId); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, "DELETE FROM encrypted_chat_devices WHERE auth_key_id = ? AND user_id = ?", d.AuthKey, d.UserId)
	return err
}

// Sweep drops, one batch each, the updates older than the retention as
// of now and those a later login on the same key cannot have been sent
// for: Android keeps the key on logout (MessagesController.java:16079) and
// the next sign-in revives the row of auth_users with a new date_created.
// It is safe to run on several instances at once.
func Sweep(ctx context.Context, db *sql.DB, now time.Time) error {
	cutoff := now.Add(-Retention).Unix()
	if _, err := db.ExecContext(ctx, "DELETE FROM qts_updates WHERE date < ? LIMIT ?", cutoff, sweepBatch); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, "DELETE q FROM qts_updates q JOIN auth_users a ON a.auth_key_id = q.auth_key_id AND a.user_id = q.user_id AND a.deleted = 0 WHERE q.date < a.date_created")
	return err
}

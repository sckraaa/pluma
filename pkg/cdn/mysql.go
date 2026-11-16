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

package cdn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// MySQLStore is the Store on the tables of migrate-20261120-cdn.sql as
// migrate-20261125-https-cdn.sql leaves them.
type MySQLStore struct {
	db *sqlx.DB
}

var _ Store = (*MySQLStore)(nil)

// NewMySQLStore returns the Store on db. Reads go to the primary: a
// preparer and the redirect must see a change the moment it is made.
func NewMySQLStore(db *sqlx.DB) *MySQLStore {
	return &MySQLStore{db: db.Master()}
}

const fileColumns = "id, file_kind, file_id, thumb_size, size, eligible, file_token, aes_key, aes_iv, hashes, created_at"

type fileRow struct {
	Id        int64  `db:"id"`
	FileKind  int8   `db:"file_kind"`
	FileId    int64  `db:"file_id"`
	ThumbSize string `db:"thumb_size"`
	Size      int64  `db:"size"`
	Eligible  bool   `db:"eligible"`
	FileToken []byte `db:"file_token"`
	AesKey    []byte `db:"aes_key"`
	AesIv     []byte `db:"aes_iv"`
	Hashes    []byte `db:"hashes"`
	CreatedAt int64  `db:"created_at"`
}

func (r *fileRow) file() *File {
	return &File{
		RowId:     r.Id,
		Key:       FileKey{Kind: FileKind(r.FileKind), Id: r.FileId, Thumb: r.ThumbSize},
		Size:      r.Size,
		Eligible:  r.Eligible,
		Secrets:   Secrets{Token: r.FileToken, Key: r.AesKey, IV: r.AesIv},
		Hashes:    r.Hashes,
		CreatedAt: r.CreatedAt,
	}
}

func (s *MySQLStore) queryFile(ctx context.Context, where string, args ...any) (*File, error) {
	var r fileRow
	err := s.db.QueryRowPartial(ctx, &r, "select "+fileColumns+" from cdn_files where "+where, args...)
	switch {
	case errors.Is(err, sqlx.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return r.file(), nil
}

func (s *MySQLStore) Get(ctx context.Context, key FileKey) (*File, error) {
	return s.queryFile(ctx, "file_kind = ? and file_id = ? and thumb_size = ?", key.Kind, key.Id, key.Thumb)
}

// Ensure reads first: an insert that meets the row would still take an
// auto-increment value. A plain insert, whose duplicate key is an answer,
// and a conditional update of the size: concurrent INSERT … ON DUPLICATE KEY
// UPDATE on a table with two unique keys can deadlock.
func (s *MySQLStore) Ensure(ctx context.Context, f *File) (*File, error) {
	cur, err := s.Get(ctx, f.Key)
	if err != nil || cur != nil && (cur.Size != 0 || f.Size == 0) {
		return cur, err
	}
	inserted := false
	if cur == nil {
		_, err = s.db.Exec(ctx,
			"insert into cdn_files(file_kind, file_id, thumb_size, size, eligible, created_at) values (?, ?, ?, ?, ?, ?)",
			f.Key.Kind, f.Key.Id, f.Key.Thumb, f.Size, f.Eligible, f.CreatedAt)
		switch {
		case err == nil:
			inserted = true
		case !sqlx.IsDuplicate(err):
			return nil, err
		}
	}
	if !inserted && f.Size != 0 {
		if _, err = s.db.Exec(ctx,
			"update cdn_files set size = ?, eligible = ? where file_kind = ? and file_id = ? and thumb_size = ? and size = 0",
			f.Size, f.Eligible, f.Key.Kind, f.Key.Id, f.Key.Thumb); err != nil {
			return nil, err
		}
	}
	cur, err = s.Get(ctx, f.Key)
	if err == nil && cur == nil {
		err = errors.New("cdn: file row vanished after its insert")
	}
	return cur, err
}

func (s *MySQLStore) ByToken(ctx context.Context, token []byte) (*File, error) {
	if len(token) != TokenSize {
		return nil, nil
	}
	return s.queryFile(ctx, "file_token = ?", token)
}

func (s *MySQLStore) SetKey(ctx context.Context, rowId int64, sec Secrets) (*File, error) {
	if !sec.Valid() {
		return nil, errSecrets
	}
	if _, err := s.db.Exec(ctx,
		"update cdn_files set file_token = ?, aes_key = ?, aes_iv = ? where id = ? and file_token is null",
		sec.Token, sec.Key, sec.IV, rowId); err != nil {
		return nil, err
	}
	return s.queryFile(ctx, "id = ?", rowId)
}

const replicaColumns = "file_row_id, target_id, state, priority, lease_until, attempts, next_try_at, last_error, ready_at, updated_at"

type replicaRow struct {
	FileRowId  int64  `db:"file_row_id"`
	TargetId   int32  `db:"target_id"`
	State      int8   `db:"state"`
	Priority   int8   `db:"priority"`
	LeaseUntil int64  `db:"lease_until"`
	Attempts   int32  `db:"attempts"`
	NextTryAt  int64  `db:"next_try_at"`
	LastError  string `db:"last_error"`
	ReadyAt    int64  `db:"ready_at"`
	UpdatedAt  int64  `db:"updated_at"`
}

func (s *MySQLStore) Replica(ctx context.Context, rowId int64, targetId int32) (*Replica, error) {
	var r replicaRow
	err := s.db.QueryRowPartial(ctx, &r,
		"select "+replicaColumns+" from cdn_replicas where file_row_id = ? and target_id = ?", rowId, targetId)
	switch {
	case errors.Is(err, sqlx.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &Replica{
		FileRowId: r.FileRowId, TargetId: r.TargetId, State: ReplicaState(r.State), Priority: r.Priority,
		LeaseUntil: r.LeaseUntil, Attempts: r.Attempts, NextTryAt: r.NextTryAt, LastError: r.LastError,
		ReadyAt: r.ReadyAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

// Enqueue assigns state last: the columns before it are assigned by what it
// was.
func (s *MySQLStore) Enqueue(ctx context.Context, rowId int64, targetId int32, priority int8, now int64) error {
	_, err := s.db.Exec(ctx,
		"insert into cdn_replicas(file_row_id, target_id, state, priority, updated_at) values (?, ?, 0, ?, ?)"+
			" on duplicate key update"+
			" priority = if(state = 3, values(priority), if(state in (0, 1), greatest(priority, values(priority)), priority)),"+
			" attempts = if(state = 3, 0, attempts),"+
			" next_try_at = if(state = 3, 0, next_try_at),"+
			" last_error = if(state = 3, '', last_error),"+
			" updated_at = if(state in (0, 1, 3), values(updated_at), updated_at),"+
			" state = if(state = 3, 0, state)",
		rowId, targetId, priority, now)
	return err
}

// claimTries is how many candidates Claim tries when other preparers take
// them first.
const claimTries = 4

// Claim reads the next candidate and takes it with an update conditional on
// the row it read, so two preparers never claim one preparation, and the
// one that lost the race tries the next. A locking read (FOR UPDATE SKIP
// LOCKED) would lock every row the ordered scan reads, so a second
// preparer would see none.
func (s *MySQLStore) Claim(ctx context.Context, targetId int32, lease time.Duration, now int64) (*Claim, error) {
	until := now + lease.Milliseconds()
	for range claimTries {
		var r replicaRow
		err := s.db.QueryRowPartial(ctx, &r,
			"select "+replicaColumns+" from cdn_replicas"+
				" where target_id = ? and (state = 0 and next_try_at <= ? or state = 1 and lease_until <= ?)"+
				" order by priority desc, file_row_id limit 1",
			targetId, now, now)
		switch {
		case errors.Is(err, sqlx.ErrNotFound):
			return nil, nil
		case err != nil:
			return nil, err
		}
		took, err := s.exec(ctx,
			"update cdn_replicas set state = 1, attempts = attempts + 1, lease_until = ?, updated_at = ?"+
				" where file_row_id = ? and target_id = ? and state = ? and attempts = ? and lease_until = ? and next_try_at = ?",
			until, now, r.FileRowId, targetId, r.State, r.Attempts, r.LeaseUntil, r.NextTryAt)
		if err != nil {
			return nil, err
		}
		if !took {
			continue
		}
		f, err := s.queryFile(ctx, "id = ?", r.FileRowId)
		if err == nil && f == nil {
			err = fmt.Errorf("cdn: copy of a missing file row %d", r.FileRowId)
		}
		if err != nil {
			return nil, err
		}
		return &Claim{File: *f, TargetId: targetId, Priority: r.Priority, Attempts: r.Attempts + 1, LeaseUntil: until}, nil
	}
	return nil, nil
}

const claimHeld = " where file_row_id = ? and target_id = ? and state = 1 and attempts = ? and lease_until = ?"

// ExtendLease always moves the lease forward, by a millisecond at least, so
// the affected rows (changed rows, the driver's default) tell whether c
// still holds the preparation.
func (s *MySQLStore) ExtendLease(ctx context.Context, c *Claim, lease time.Duration, now int64) (bool, error) {
	until := max(now+lease.Milliseconds(), c.LeaseUntil+1)
	ok, err := s.exec(ctx, "update cdn_replicas set lease_until = ?, updated_at = ?"+claimHeld,
		until, now, c.File.RowId, c.TargetId, c.Attempts, c.LeaseUntil)
	if ok {
		c.LeaseUntil = until
	}
	return ok, err
}

var errClaimLost = errors.New("cdn: claim lost")

func (s *MySQLStore) MarkReady(ctx context.Context, c *Claim, hashes []byte, now int64) (bool, error) {
	res := sqlx.TxWrapper(ctx, s.db, func(tx *sqlx.Tx, result *sqlx.StoreResult) {
		r, err := tx.Exec("update cdn_replicas set state = 2, priority = 0, lease_until = 0, last_error = '', ready_at = ?, updated_at = ?"+claimHeld,
			now, now, c.File.RowId, c.TargetId, c.Attempts, c.LeaseUntil)
		if err != nil {
			result.Err = err
			return
		}
		n, err := r.RowsAffected()
		switch {
		case err != nil:
			result.Err = err
			return
		case n == 0:
			result.Err = errClaimLost
			return
		}
		_, result.Err = tx.Exec("update cdn_files set hashes = ? where id = ?", hashes, c.File.RowId)
	})
	switch {
	case errors.Is(res.Err, errClaimLost):
		return false, nil
	case res.Err != nil:
		return false, res.Err
	}
	return true, nil
}

func (s *MySQLStore) MarkFailed(ctx context.Context, c *Claim, reason string, nextTryAt, now int64) (bool, error) {
	state := StateQueued
	if nextTryAt == 0 {
		state = StateFailed
	}
	return s.exec(ctx, "update cdn_replicas set state = ?, next_try_at = ?, lease_until = 0, last_error = ?, updated_at = ?"+claimHeld,
		state, nextTryAt, clip(reason, maxLastError), now, c.File.RowId, c.TargetId, c.Attempts, c.LeaseUntil)
}

func (s *MySQLStore) MarkAbsent(ctx context.Context, rowId int64, targetId int32, now int64) (bool, error) {
	return s.exec(ctx, "update cdn_replicas set state = 3, updated_at = ? where file_row_id = ? and target_id = ? and state = 2",
		now, rowId, targetId)
}

func (s *MySQLStore) QueueCounts(ctx context.Context, targetId int32) (map[ReplicaState]int64, error) {
	var rows []struct {
		State int8  `db:"state"`
		N     int64 `db:"n"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"select state, count(*) as n from cdn_replicas where target_id = ? group by state", targetId); err != nil {
		return nil, err
	}
	out := make(map[ReplicaState]int64, len(rows))
	for _, r := range rows {
		out[ReplicaState(r.State)] = r.N
	}
	return out, nil
}

type deliveryRow struct {
	DcId            int32  `db:"dc_id"`
	HealthyUntil    int64  `db:"healthy_until"`
	UnhealthyReason string `db:"unhealthy_reason"`
	UpdatedAt       int64  `db:"updated_at"`
}

func (s *MySQLStore) DeliveryHealth(ctx context.Context, dcId int32) (*DeliveryHealth, error) {
	var r deliveryRow
	err := s.db.QueryRowPartial(ctx, &r,
		"select dc_id, healthy_until, unhealthy_reason, updated_at from cdn_deliveries where dc_id = ?", dcId)
	switch {
	case errors.Is(err, sqlx.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	}
	h := DeliveryHealth(r)
	return &h, nil
}

func (s *MySQLStore) PutDeliveryHealth(ctx context.Context, h *DeliveryHealth) error {
	_, err := s.db.Exec(ctx,
		"insert into cdn_deliveries(dc_id, healthy_until, unhealthy_reason, updated_at) values (?, ?, ?, ?)"+
			" on duplicate key update healthy_until = values(healthy_until), unhealthy_reason = values(unhealthy_reason),"+
			" updated_at = values(updated_at)",
		h.DcId, h.HealthyUntil, clip(h.UnhealthyReason, maxUnhealthyReason), h.UpdatedAt)
	return err
}

// AddUsage adds in one statement, so concurrent additions of several bff
// processes all count.
func (s *MySQLStore) AddUsage(ctx context.Context, dcId int32, month string, originBytes, edgeBytesEst int64) error {
	if err := checkUsage(month, originBytes, edgeBytesEst); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx,
		"insert into cdn_delivery_usage(dc_id, month, origin_bytes, edge_bytes_est) values (?, ?, ?, ?)"+
			" on duplicate key update origin_bytes = origin_bytes + values(origin_bytes),"+
			" edge_bytes_est = edge_bytes_est + values(edge_bytes_est)",
		dcId, month, originBytes, edgeBytesEst)
	return err
}

func (s *MySQLStore) Usage(ctx context.Context, dcId int32, month string) (DeliveryUsage, error) {
	u := DeliveryUsage{DcId: dcId, Month: month}
	var r struct {
		OriginBytes  int64 `db:"origin_bytes"`
		EdgeBytesEst int64 `db:"edge_bytes_est"`
	}
	err := s.db.QueryRowPartial(ctx, &r,
		"select origin_bytes, edge_bytes_est from cdn_delivery_usage where dc_id = ? and month = ?", dcId, month)
	switch {
	case errors.Is(err, sqlx.ErrNotFound):
		return u, nil
	case err != nil:
		return u, err
	}
	u.OriginBytes, u.EdgeBytesEst = r.OriginBytes, r.EdgeBytesEst
	return u, nil
}

func (s *MySQLStore) exec(ctx context.Context, query string, args ...any) (bool, error) {
	r, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

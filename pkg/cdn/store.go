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
	"fmt"
	"time"
)

// File is a row of cdn_files (teamgramd/deploy/sql/migrate-20261120-cdn.sql).
// Times in the rows of the Store are unix milliseconds.
type File struct {
	RowId int64
	Key   FileKey
	// Size is the size of the file in bytes; 0 if not yet known.
	Size int64
	// Eligible is false for a file below CDN_MIN_FILE_SIZE when its row was
	// made: it is never prepared.
	Eligible bool
	// Secrets are empty until SetKey.
	Secrets Secrets
	// Hashes is the blob of a Hasher (hashes.go), nil until the first
	// preparation is ready.
	Hashes    []byte
	CreatedAt int64
}

// HasKey reports whether the secrets of f are set.
func (f *File) HasKey() bool { return f.Secrets.Valid() }

// ReplicaState is the state of the preparation of a file for a target.
type ReplicaState int8

const (
	// StateQueued: waiting for a preparation from next_try_at.
	StateQueued ReplicaState = 0
	// StatePushing: claimed by a preparer until lease_until.
	StatePushing ReplicaState = 1
	// StateReady: prepared; the file may be redirected to the deliveries
	// of the target.
	StateReady ReplicaState = 2
	// StateAbsent: was ready, the target no longer has it; queued again by
	// the next request of the file.
	StateAbsent ReplicaState = 3
	// StateFailed: MaxPushAttempts failed; never tried again by itself.
	StateFailed ReplicaState = 4
)

// Replica is a row of cdn_replicas: the preparation of a file for a
// target (TargetHomeOrigin).
type Replica struct {
	FileRowId  int64
	TargetId   int32
	State      ReplicaState
	Priority   int8
	LeaseUntil int64
	Attempts   int32
	NextTryAt  int64
	LastError  string
	ReadyAt    int64
	UpdatedAt  int64
}

// PriorityFirst is the priority of a preparation made before the others
// (cdn_replicas.priority 1); 0 is the rest.
const PriorityFirst int8 = 1

// Claim is a preparation a preparer holds by lease, with its file. Attempts
// and LeaseUntil fence the claim: every change made through it applies
// only while the row is still in the state the claim left it in, so a
// preparer whose lease expired and was claimed again changes nothing.
type Claim struct {
	File       File
	TargetId   int32
	Priority   int8
	Attempts   int32
	LeaseUntil int64
}

// DeliveryHealth is a row of cdn_deliveries: what the last check of a
// delivery found (migrate-20261125-https-cdn.sql).
type DeliveryHealth struct {
	// DcId is the id of the delivery.
	DcId int32
	// HealthyUntil is when the delivery leaves the routes unless checked
	// again; 0 while it is out.
	HealthyUntil int64
	// UnhealthyReason is why it is out (at most 32 bytes), empty while it
	// is healthy.
	UnhealthyReason string
	UpdatedAt       int64
}

// DeliveryUsage is a row of cdn_delivery_usage: the traffic of a delivery
// in a calendar month (UTC).
type DeliveryUsage struct {
	DcId int32
	// Month is UsageMonth of the month.
	Month string
	// OriginBytes is what the home origin served for the delivery, exact.
	OriginBytes int64
	// EdgeBytesEst is the home's estimate of what the CDN sent to clients.
	EdgeBytesEst int64
}

// UsageMonth is the month of cdn_delivery_usage that t falls in: YYYY-MM
// in UTC (the month of Gcore is reset on the 1st, time zone not stated:
// https://docs.gcore.com/cdn/how-the-cdn-service-and-its-additional-options-are-billed.md).
func UsageMonth(t time.Time) string { return t.UTC().Format("2006-01") }

// Store keeps the files, their preparations for the targets, and the
// health and traffic of the deliveries. Every change of a preparation made
// by a preparer goes through its Claim and is conditional on it (see
// Claim).
type Store interface {
	// Get returns the file of key, or nil if it has no row.
	Get(ctx context.Context, key FileKey) (*File, error)
	// Ensure makes the row of f.Key with f.Size, f.Eligible and f.CreatedAt
	// if there is none, and returns the row in force. A row whose size is
	// not yet known (0) takes f.Size and f.Eligible; a known one keeps its
	// own.
	Ensure(ctx context.Context, f *File) (*File, error)
	// ByToken returns the file of a cdn_files.file_token, or nil if none
	// has it.
	ByToken(ctx context.Context, token []byte) (*File, error)
	// SetKey sets the secrets of the file of rowId if it has none, and
	// returns the row in force: the secrets set first win, so a file has
	// one key and token whatever races. It returns nil if there is no row.
	SetKey(ctx context.Context, rowId int64, s Secrets) (*File, error)

	// Replica returns the preparation of the file of rowId for targetId,
	// or nil.
	Replica(ctx context.Context, rowId int64, targetId int32) (*Replica, error)
	// Enqueue queues the preparation of the file of rowId for targetId,
	// idempotently:
	//   - none: a queued one with priority;
	//   - absent: queued anew, its attempts and backoff dropped;
	//   - queued or being prepared: priority raised to priority if lower;
	//   - ready or failed: unchanged.
	Enqueue(ctx context.Context, rowId int64, targetId int32, priority int8, now int64) error
	// Claim takes the next preparation for targetId by lease until
	// now+lease: queued with next_try_at reached, or claimed with an
	// expired lease; the higher priority first, then by file. Each claim
	// is an attempt. It returns nil if there is none, or if concurrent
	// preparers took every candidate it tried (the next call goes on).
	Claim(ctx context.Context, targetId int32, lease time.Duration, now int64) (*Claim, error)
	// ExtendLease moves the lease of c to now+lease; false if c no longer
	// holds the preparation.
	ExtendLease(ctx context.Context, c *Claim, lease time.Duration, now int64) (bool, error)
	// MarkReady sets the hash blob of the file of c and makes the
	// preparation ready, in one transaction; false (and nothing changed)
	// if c no longer holds it.
	MarkReady(ctx context.Context, c *Claim, hashes []byte, now int64) (bool, error)
	// MarkFailed records a failed preparation of c with reason (at most 64
	// bytes): queued again from nextTryAt, or failed for good with
	// nextTryAt 0 (see RetryAt); false if c no longer holds it.
	MarkFailed(ctx context.Context, c *Claim, reason string, nextTryAt, now int64) (bool, error)
	// MarkAbsent makes the ready preparation of the file of rowId for
	// targetId absent; false if it was not ready.
	MarkAbsent(ctx context.Context, rowId int64, targetId int32, now int64) (bool, error)
	// QueueCounts returns the number of preparations for targetId in each
	// state that has any.
	QueueCounts(ctx context.Context, targetId int32) (map[ReplicaState]int64, error)

	// DeliveryHealth returns the health of delivery dcId, or nil if it was
	// never checked.
	DeliveryHealth(ctx context.Context, dcId int32) (*DeliveryHealth, error)
	// PutDeliveryHealth writes what a check of delivery h.DcId found; the
	// reason is cut to 32 bytes.
	PutDeliveryHealth(ctx context.Context, h *DeliveryHealth) error
	// AddUsage adds originBytes and edgeBytesEst (both at least 0) to the
	// traffic of delivery dcId in month (UsageMonth).
	AddUsage(ctx context.Context, dcId int32, month string, originBytes, edgeBytesEst int64) error
	// Usage returns the traffic of delivery dcId in month; zero for a
	// month without any.
	Usage(ctx context.Context, dcId int32, month string) (DeliveryUsage, error)
}

// maxUnhealthyReason is the width of cdn_deliveries.unhealthy_reason.
const maxUnhealthyReason = 32

// validMonth reports whether m is a month of UsageMonth.
func validMonth(m string) bool {
	_, err := time.Parse("2006-01", m)
	return err == nil && len(m) == 7
}

// checkUsage checks the arguments of AddUsage.
func checkUsage(month string, originBytes, edgeBytesEst int64) error {
	if !validMonth(month) || originBytes < 0 || edgeBytesEst < 0 {
		return fmt.Errorf("cdn: usage of month %q, %d and %d bytes", month, originBytes, edgeBytesEst)
	}
	return nil
}

// maxLastError is the width of cdn_replicas.last_error.
const maxLastError = 64

// clip cuts s to n bytes, the width of its column.
func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

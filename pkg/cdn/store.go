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
	"time"
)

// File is a row of cdn_files (teamgramd/deploy/sql/migrate-20261120-cdn.sql).
// Times are unix milliseconds.
type File struct {
	RowId int64
	Key   FileKey
	// Size is the size of the file in bytes; 0 if not yet known.
	Size int64
	// Eligible is false for a file below CDN_MIN_FILE_SIZE when its row was
	// made: it is never pushed.
	Eligible bool
	// Secrets are empty until SetKey.
	Secrets Secrets
	// Hashes is the blob of a Hasher (hashes.go), nil until the first copy
	// is ready.
	Hashes    []byte
	CreatedAt int64
}

// HasKey reports whether the secrets of f are set.
func (f *File) HasKey() bool { return f.Secrets.Valid() }

// ReplicaState is the state of a copy of a file on a node.
type ReplicaState int8

const (
	// StateQueued: waiting for a push from next_try_at.
	StateQueued ReplicaState = 0
	// StatePushing: claimed by a pusher until lease_until.
	StatePushing ReplicaState = 1
	// StateReady: the whole cipher is on the node, checked.
	StateReady ReplicaState = 2
	// StateAbsent: was ready, the node no longer has it; queued again by the
	// next request of the file.
	StateAbsent ReplicaState = 3
	// StateFailed: MaxPushAttempts failed; never tried again by itself.
	StateFailed ReplicaState = 4
)

// Replica is a row of cdn_replicas.
type Replica struct {
	FileRowId  int64
	DcId       int32
	State      ReplicaState
	Priority   int8
	LeaseUntil int64
	Attempts   int32
	NextTryAt  int64
	LastError  string
	ReadyAt    int64
	UpdatedAt  int64
}

// PriorityReupload is the priority of a copy asked for by
// upload.reuploadCdnFile: it is pushed before the others.
const PriorityReupload int8 = 1

// Claim is a copy a pusher holds by lease, with its file. Attempts and
// LeaseUntil fence the claim: every change made through it applies only
// while the row is still in the state the claim left it in, so a pusher
// whose lease expired and was claimed again changes nothing.
type Claim struct {
	File       File
	DcId       int32
	Priority   int8
	Attempts   int32
	LeaseUntil int64
}

// ReadyCopy is a ready copy on a node, for the inventory of the node.
type ReadyCopy struct {
	FileRowId int64
	Token     []byte
	Size      int64
}

// Node is a row of cdn_nodes.
type Node struct {
	DcId int32
	// PublicKey is the PKCS#1 PEM of the node's RSA key; empty until the
	// node first answered.
	PublicKey       string
	Fingerprint     int64
	BootId          []byte
	HealthyUntil    int64
	UnhealthyReason string
	EgressMonth     string // YYYY-MM of EgressBytes
	EgressBytes     int64
	CacheUsed       int64
	CacheCap        int64
	// RtKey is the request_token key in force, RtKeyPrev the one before it
	// (nil if none), RtKeyAt when RtKey was made. PutNode does not change
	// them; RotateRtKey does.
	RtKey     []byte
	RtKeyPrev []byte
	RtKeyAt   int64
	UpdatedAt int64
}

// RtKeys are the request_token keys VerifyRequestToken accepts for n.
func (n *Node) RtKeys() [][]byte { return [][]byte{n.RtKey, n.RtKeyPrev} }

// Store keeps the files, their copies on the nodes and the nodes. Every
// change of a copy made by a pusher goes through its Claim and is
// conditional on it (see Claim).
type Store interface {
	// Get returns the file of key, or nil if it has no row.
	Get(ctx context.Context, key FileKey) (*File, error)
	// Ensure makes the row of f.Key with f.Size, f.Eligible and f.CreatedAt
	// if there is none, and returns the row in force. A row whose size is
	// not yet known (0) takes f.Size and f.Eligible; a known one keeps its
	// own.
	Ensure(ctx context.Context, f *File) (*File, error)
	// ByToken returns the file of a file_token, or nil if none has it.
	ByToken(ctx context.Context, token []byte) (*File, error)
	// SetKey sets the secrets of the file of rowId if it has none, and
	// returns the row in force: the secrets set first win, so a file has
	// one key and token whatever races. It returns nil if there is no row.
	SetKey(ctx context.Context, rowId int64, s Secrets) (*File, error)

	// Replica returns the copy of the file of rowId on node dcId, or nil.
	Replica(ctx context.Context, rowId int64, dcId int32) (*Replica, error)
	// Enqueue queues a push of the file of rowId to node dcId, idempotently:
	//   - no copy: a queued one with priority;
	//   - absent: queued anew, its attempts and backoff dropped;
	//   - queued or pushing: priority raised to priority if lower;
	//   - ready or failed: unchanged.
	Enqueue(ctx context.Context, rowId int64, dcId int32, priority int8, now int64) error
	// Claim takes the next copy to push to node dcId by lease until
	// now+lease: queued with next_try_at reached, or pushing with an
	// expired lease; the higher priority first, then by file. Each claim
	// is an attempt. It returns nil if there is none, or if concurrent
	// pushers took every candidate it tried (the next call goes on).
	Claim(ctx context.Context, dcId int32, lease time.Duration, now int64) (*Claim, error)
	// ExtendLease moves the lease of c to now+lease; false if c no longer
	// holds the copy.
	ExtendLease(ctx context.Context, c *Claim, lease time.Duration, now int64) (bool, error)
	// MarkReady sets the hash blob of the file of c and makes the copy
	// ready, in one transaction; false (and nothing changed) if c no longer
	// holds the copy.
	MarkReady(ctx context.Context, c *Claim, hashes []byte, now int64) (bool, error)
	// MarkFailed records a failed push of c with reason (at most 64 bytes):
	// queued again from nextTryAt, or failed for good with nextTryAt 0 (see
	// RetryAt); false if c no longer holds the copy.
	MarkFailed(ctx context.Context, c *Claim, reason string, nextTryAt, now int64) (bool, error)
	// MarkAbsent makes the ready copy of the file of rowId on node dcId
	// absent; false if it was not ready.
	MarkAbsent(ctx context.Context, rowId int64, dcId int32, now int64) (bool, error)
	// ListReady returns up to limit ready copies on node dcId with files
	// after afterRowId, by file.
	ListReady(ctx context.Context, dcId int32, afterRowId int64, limit int) ([]ReadyCopy, error)
	// QueueCounts returns the number of copies on node dcId in each state
	// that has any.
	QueueCounts(ctx context.Context, dcId int32) (map[ReplicaState]int64, error)

	// Node returns node dcId, or nil if it has no row.
	Node(ctx context.Context, dcId int32) (*Node, error)
	// Nodes returns every node, by DC.
	Nodes(ctx context.Context) ([]*Node, error)
	// PutNode writes what the home saw of node n.DcId: every field but the
	// request_token keys, which stay as they are (none for a new row).
	PutNode(ctx context.Context, n *Node) error
	// RotateRtKey gives node dcId a request_token key: key if it has none,
	// or if its key is at least every old, the key in force becoming the
	// previous one. It returns the node as it is after. key is a fresh one
	// from NewRtKey, unused if nothing changes.
	RotateRtKey(ctx context.Context, dcId int32, key []byte, every time.Duration, now int64) (*Node, error)
}

// maxLastError is the width of cdn_replicas.last_error.
const maxLastError = 64

func clipReason(s string) string {
	if len(s) > maxLastError {
		return s[:maxLastError]
	}
	return s
}

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
	"bytes"
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// MemStore is a Store in memory with the conditions of MySQLStore, for the
// tests of the packages that use it.
type MemStore struct {
	mu       sync.Mutex
	err      error
	nextId   int64
	files    map[int64]*File // by row id
	byKey    map[FileKey]int64
	replicas map[replicaKey]*Replica
	nodes    map[int32]*Node
}

type replicaKey struct {
	rowId int64
	dcId  int32
}

var _ Store = (*MemStore)(nil)

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{
		files:    map[int64]*File{},
		byKey:    map[FileKey]int64{},
		replicas: map[replicaKey]*Replica{},
		nodes:    map[int32]*Node{},
	}
}

// SetErr makes every call fail with err until SetErr(nil): MySQL is down.
func (m *MemStore) SetErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func cloneFile(f *File) *File {
	c := *f
	c.Secrets = Secrets{Token: bytes.Clone(f.Secrets.Token), Key: bytes.Clone(f.Secrets.Key), IV: bytes.Clone(f.Secrets.IV)}
	c.Hashes = bytes.Clone(f.Hashes)
	return &c
}

func cloneNode(n *Node) *Node {
	c := *n
	c.BootId, c.RtKey, c.RtKeyPrev = bytes.Clone(n.BootId), bytes.Clone(n.RtKey), bytes.Clone(n.RtKeyPrev)
	return &c
}

func (m *MemStore) Get(_ context.Context, key FileKey) (*File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if id, ok := m.byKey[key]; ok {
		return cloneFile(m.files[id]), nil
	}
	return nil, nil
}

func (m *MemStore) Ensure(_ context.Context, f *File) (*File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if id, ok := m.byKey[f.Key]; ok {
		cur := m.files[id]
		if cur.Size == 0 && f.Size != 0 {
			cur.Size, cur.Eligible = f.Size, f.Eligible
		}
		return cloneFile(cur), nil
	}
	m.nextId++
	row := &File{RowId: m.nextId, Key: f.Key, Size: f.Size, Eligible: f.Eligible, CreatedAt: f.CreatedAt}
	m.files[row.RowId] = row
	m.byKey[f.Key] = row.RowId
	return cloneFile(row), nil
}

func (m *MemStore) ByToken(_ context.Context, token []byte) (*File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if len(token) != TokenSize {
		return nil, nil
	}
	for _, f := range m.files {
		if bytes.Equal(f.Secrets.Token, token) {
			return cloneFile(f), nil
		}
	}
	return nil, nil
}

func (m *MemStore) SetKey(_ context.Context, rowId int64, sec Secrets) (*File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if !sec.Valid() {
		return nil, errSecrets
	}
	f, ok := m.files[rowId]
	if !ok {
		return nil, nil
	}
	if f.Secrets.Token == nil {
		for _, o := range m.files {
			if bytes.Equal(o.Secrets.Token, sec.Token) {
				return nil, fmt.Errorf("cdn: duplicate file token")
			}
		}
		f.Secrets = Secrets{Token: bytes.Clone(sec.Token), Key: bytes.Clone(sec.Key), IV: bytes.Clone(sec.IV)}
	}
	return cloneFile(f), nil
}

func (m *MemStore) Replica(_ context.Context, rowId int64, dcId int32) (*Replica, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if r, ok := m.replicas[replicaKey{rowId, dcId}]; ok {
		c := *r
		return &c, nil
	}
	return nil, nil
}

func (m *MemStore) Enqueue(_ context.Context, rowId int64, dcId int32, priority int8, now int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	r, ok := m.replicas[replicaKey{rowId, dcId}]
	switch {
	case !ok:
		m.replicas[replicaKey{rowId, dcId}] = &Replica{FileRowId: rowId, DcId: dcId, State: StateQueued, Priority: priority, UpdatedAt: now}
	case r.State == StateAbsent:
		r.State, r.Priority, r.Attempts, r.NextTryAt, r.LastError, r.UpdatedAt = StateQueued, priority, 0, 0, "", now
	case r.State == StateQueued || r.State == StatePushing:
		r.Priority, r.UpdatedAt = max(r.Priority, priority), now
	}
	return nil
}

func (m *MemStore) Claim(_ context.Context, dcId int32, lease time.Duration, now int64) (*Claim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	var next *Replica
	for _, r := range m.replicas {
		due := r.State == StateQueued && r.NextTryAt <= now || r.State == StatePushing && r.LeaseUntil <= now
		if r.DcId != dcId || !due {
			continue
		}
		if next == nil || r.Priority > next.Priority || r.Priority == next.Priority && r.FileRowId < next.FileRowId {
			next = r
		}
	}
	if next == nil {
		return nil, nil
	}
	f, ok := m.files[next.FileRowId]
	if !ok {
		return nil, fmt.Errorf("cdn: copy of a missing file row %d", next.FileRowId)
	}
	next.State, next.Attempts, next.LeaseUntil, next.UpdatedAt = StatePushing, next.Attempts+1, now+lease.Milliseconds(), now
	return &Claim{File: *cloneFile(f), DcId: dcId, Priority: next.Priority, Attempts: next.Attempts, LeaseUntil: next.LeaseUntil}, nil
}

// held returns the copy c holds, or nil; m.mu is held.
func (m *MemStore) held(c *Claim) *Replica {
	r, ok := m.replicas[replicaKey{c.File.RowId, c.DcId}]
	if !ok || r.State != StatePushing || r.Attempts != c.Attempts || r.LeaseUntil != c.LeaseUntil {
		return nil
	}
	return r
}

func (m *MemStore) ExtendLease(_ context.Context, c *Claim, lease time.Duration, now int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	r := m.held(c)
	if r == nil {
		return false, nil
	}
	r.LeaseUntil, r.UpdatedAt = max(now+lease.Milliseconds(), c.LeaseUntil+1), now
	c.LeaseUntil = r.LeaseUntil
	return true, nil
}

func (m *MemStore) MarkReady(_ context.Context, c *Claim, hashes []byte, now int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	r := m.held(c)
	if r == nil {
		return false, nil
	}
	f, ok := m.files[c.File.RowId]
	if !ok {
		return false, fmt.Errorf("cdn: copy of a missing file row %d", c.File.RowId)
	}
	r.State, r.Priority, r.LeaseUntil, r.LastError, r.ReadyAt, r.UpdatedAt = StateReady, 0, 0, "", now, now
	f.Hashes = bytes.Clone(hashes)
	return true, nil
}

func (m *MemStore) MarkFailed(_ context.Context, c *Claim, reason string, nextTryAt, now int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	r := m.held(c)
	if r == nil {
		return false, nil
	}
	r.State = StateQueued
	if nextTryAt == 0 {
		r.State = StateFailed
	}
	r.NextTryAt, r.LeaseUntil, r.LastError, r.UpdatedAt = nextTryAt, 0, clipReason(reason), now
	return true, nil
}

func (m *MemStore) MarkAbsent(_ context.Context, rowId int64, dcId int32, now int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	r, ok := m.replicas[replicaKey{rowId, dcId}]
	if !ok || r.State != StateReady {
		return false, nil
	}
	r.State, r.UpdatedAt = StateAbsent, now
	return true, nil
}

func (m *MemStore) ListReady(_ context.Context, dcId int32, afterRowId int64, limit int) ([]ReadyCopy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	var out []ReadyCopy
	for _, r := range m.replicas {
		if r.DcId == dcId && r.State == StateReady && r.FileRowId > afterRowId {
			f := m.files[r.FileRowId]
			out = append(out, ReadyCopy{FileRowId: r.FileRowId, Token: bytes.Clone(f.Secrets.Token), Size: f.Size})
		}
	}
	slices.SortFunc(out, func(a, b ReadyCopy) int { return cmp.Compare(a.FileRowId, b.FileRowId) })
	if len(out) > max(limit, 0) {
		out = out[:max(limit, 0)]
	}
	return out, nil
}

func (m *MemStore) QueueCounts(_ context.Context, dcId int32) (map[ReplicaState]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	out := map[ReplicaState]int64{}
	for _, r := range m.replicas {
		if r.DcId == dcId {
			out[r.State]++
		}
	}
	return out, nil
}

func (m *MemStore) Node(_ context.Context, dcId int32) (*Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if n, ok := m.nodes[dcId]; ok {
		return cloneNode(n), nil
	}
	return nil, nil
}

func (m *MemStore) Nodes(_ context.Context) ([]*Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	out := make([]*Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		out = append(out, cloneNode(n))
	}
	slices.SortFunc(out, func(a, b *Node) int { return cmp.Compare(a.DcId, b.DcId) })
	return out, nil
}

func (m *MemStore) PutNode(_ context.Context, n *Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	v := cloneNode(n)
	v.RtKey, v.RtKeyPrev, v.RtKeyAt = nil, nil, 0
	if old, ok := m.nodes[n.DcId]; ok {
		v.RtKey, v.RtKeyPrev, v.RtKeyAt = old.RtKey, old.RtKeyPrev, old.RtKeyAt
	}
	m.nodes[n.DcId] = v
	return nil
}

func (m *MemStore) RotateRtKey(_ context.Context, dcId int32, key []byte, every time.Duration, now int64) (*Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if len(key) != RtKeySize || every <= 0 {
		return nil, fmt.Errorf("cdn: request token key of %d bytes, rotation every %v", len(key), every)
	}
	n, ok := m.nodes[dcId]
	switch {
	case !ok:
		n = &Node{DcId: dcId, RtKey: bytes.Clone(key), RtKeyAt: now, UpdatedAt: now}
		m.nodes[dcId] = n
	case len(n.RtKey) == 0:
		n.RtKey, n.RtKeyAt, n.UpdatedAt = bytes.Clone(key), now, now
	case now-n.RtKeyAt >= every.Milliseconds():
		n.RtKeyPrev, n.RtKey, n.RtKeyAt, n.UpdatedAt = n.RtKey, bytes.Clone(key), now, now
	}
	return cloneNode(n), nil
}

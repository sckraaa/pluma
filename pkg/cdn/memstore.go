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
	"context"
	"fmt"
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
	health   map[int32]*DeliveryHealth
	usage    map[usageKey]*DeliveryUsage
}

type replicaKey struct {
	rowId    int64
	targetId int32
}

type usageKey struct {
	dcId  int32
	month string
}

var _ Store = (*MemStore)(nil)

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{
		files:    map[int64]*File{},
		byKey:    map[FileKey]int64{},
		replicas: map[replicaKey]*Replica{},
		health:   map[int32]*DeliveryHealth{},
		usage:    map[usageKey]*DeliveryUsage{},
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

func (m *MemStore) Replica(_ context.Context, rowId int64, targetId int32) (*Replica, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if r, ok := m.replicas[replicaKey{rowId, targetId}]; ok {
		c := *r
		return &c, nil
	}
	return nil, nil
}

func (m *MemStore) Enqueue(_ context.Context, rowId int64, targetId int32, priority int8, now int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	r, ok := m.replicas[replicaKey{rowId, targetId}]
	switch {
	case !ok:
		m.replicas[replicaKey{rowId, targetId}] = &Replica{FileRowId: rowId, TargetId: targetId, State: StateQueued, Priority: priority, UpdatedAt: now}
	case r.State == StateAbsent:
		r.State, r.Priority, r.Attempts, r.NextTryAt, r.LastError, r.UpdatedAt = StateQueued, priority, 0, 0, "", now
	case r.State == StateQueued || r.State == StatePushing:
		r.Priority, r.UpdatedAt = max(r.Priority, priority), now
	}
	return nil
}

func (m *MemStore) Claim(_ context.Context, targetId int32, lease time.Duration, now int64) (*Claim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	var next *Replica
	for _, r := range m.replicas {
		due := r.State == StateQueued && r.NextTryAt <= now || r.State == StatePushing && r.LeaseUntil <= now
		if r.TargetId != targetId || !due {
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
	return &Claim{File: *cloneFile(f), TargetId: targetId, Priority: next.Priority, Attempts: next.Attempts, LeaseUntil: next.LeaseUntil}, nil
}

// held returns the copy c holds, or nil; m.mu is held.
func (m *MemStore) held(c *Claim) *Replica {
	r, ok := m.replicas[replicaKey{c.File.RowId, c.TargetId}]
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
	r.NextTryAt, r.LeaseUntil, r.LastError, r.UpdatedAt = nextTryAt, 0, clip(reason, maxLastError), now
	return true, nil
}

func (m *MemStore) MarkAbsent(_ context.Context, rowId int64, targetId int32, now int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	r, ok := m.replicas[replicaKey{rowId, targetId}]
	if !ok || r.State != StateReady {
		return false, nil
	}
	r.State, r.UpdatedAt = StateAbsent, now
	return true, nil
}

func (m *MemStore) QueueCounts(_ context.Context, targetId int32) (map[ReplicaState]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	out := map[ReplicaState]int64{}
	for _, r := range m.replicas {
		if r.TargetId == targetId {
			out[r.State]++
		}
	}
	return out, nil
}

func (m *MemStore) DeliveryHealth(_ context.Context, dcId int32) (*DeliveryHealth, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if h, ok := m.health[dcId]; ok {
		c := *h
		return &c, nil
	}
	return nil, nil
}

func (m *MemStore) PutDeliveryHealth(_ context.Context, h *DeliveryHealth) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	c := *h
	c.UnhealthyReason = clip(c.UnhealthyReason, maxUnhealthyReason)
	m.health[h.DcId] = &c
	return nil
}

func (m *MemStore) AddUsage(_ context.Context, dcId int32, month string, originBytes, edgeBytesEst int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	if err := checkUsage(month, originBytes, edgeBytesEst); err != nil {
		return err
	}
	u, ok := m.usage[usageKey{dcId, month}]
	if !ok {
		u = &DeliveryUsage{DcId: dcId, Month: month}
		m.usage[usageKey{dcId, month}] = u
	}
	u.OriginBytes += originBytes
	u.EdgeBytesEst += edgeBytesEst
	return nil
}

func (m *MemStore) Usage(_ context.Context, dcId int32, month string) (DeliveryUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return DeliveryUsage{}, m.err
	}
	if u, ok := m.usage[usageKey{dcId, month}]; ok {
		return *u, nil
	}
	return DeliveryUsage{DcId: dcId, Month: month}, nil
}

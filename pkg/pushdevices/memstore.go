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
	"bytes"
	"context"
	"slices"
	"sync"
	"time"
)

// MemStore is a Store in memory with the rules of MySQLStore, for the tests
// of the packages that use devices.
type MemStore struct {
	mu     sync.Mutex
	rows   []memRow
	nextId int64
	err    error
}

type memRow struct {
	Device
	hash        []byte
	invalidated bool
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore { return &MemStore{} }

// SetError makes every later call fail with err (nil restores).
func (m *MemStore) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *MemStore) Register(_ context.Context, d *Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	hash := tokenHash(d.Token)
	now := time.Now().Unix()
	createdAt := now
	for i, r := range m.rows {
		if r.UserId == d.UserId && r.TokenType == d.TokenType && bytes.Equal(r.hash, hash) {
			if r.invalidated {
				m.rows = slices.Delete(m.rows, i, i+1)
				return ErrTokenInvalidated
			}
			createdAt = r.CreatedAt
			break
		}
	}
	m.rows = slices.DeleteFunc(m.rows, func(r memRow) bool {
		return r.AuthKeyId == d.AuthKeyId && r.TokenType == d.TokenType ||
			r.UserId == d.UserId && r.TokenType == d.TokenType && bytes.Equal(r.hash, hash)
	})
	m.nextId++
	d.Id, d.CreatedAt, d.UpdatedAt = m.nextId, createdAt, now
	row := memRow{Device: *d, hash: hash}
	row.Secret = bytes.Clone(d.Secret)
	row.OtherUids = slices.Clone(d.OtherUids)
	m.rows = append(m.rows, row)
	return nil
}

func (m *MemStore) Unregister(_ context.Context, userId int64, tokenType int32, token string) (bool, error) {
	return m.remove(func(r memRow) bool {
		return r.UserId == userId && r.TokenType == tokenType && bytes.Equal(r.hash, tokenHash(token))
	})
}

func (m *MemStore) SetLockedPeriod(_ context.Context, userId, authKeyId int64, period int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	for i := range m.rows {
		if m.rows[i].UserId == userId && m.rows[i].AuthKeyId == authKeyId {
			m.rows[i].LockedPeriod = period
		}
	}
	return nil
}

func (m *MemStore) List(_ context.Context, userId int64) ([]*Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	var devices []*Device
	for _, r := range m.rows {
		if r.UserId == userId && !r.invalidated {
			d := r.Device
			d.Secret = bytes.Clone(r.Secret)
			d.OtherUids = slices.Clone(r.OtherUids)
			devices = append(devices, &d)
		}
	}
	return devices, nil
}

func (m *MemStore) DeleteAuthorizations(_ context.Context, userId int64, authKeyIds ...int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return 0, m.err
	}
	n := len(m.rows)
	m.rows = slices.DeleteFunc(m.rows, func(r memRow) bool {
		return r.UserId == userId && slices.Contains(authKeyIds, r.AuthKeyId)
	})
	return int64(n - len(m.rows)), nil
}

func (m *MemStore) Delete(_ context.Context, id int64) error {
	_, err := m.remove(func(r memRow) bool { return r.Id == id })
	return err
}

func (m *MemStore) Invalidate(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	for i := range m.rows {
		if m.rows[i].Id == id {
			m.rows[i].invalidated = true
		}
	}
	return nil
}

func (m *MemStore) remove(match func(memRow) bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	n := len(m.rows)
	m.rows = slices.DeleteFunc(m.rows, match)
	return len(m.rows) < n, nil
}

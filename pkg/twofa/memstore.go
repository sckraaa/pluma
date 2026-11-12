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

package twofa

import (
	"bytes"
	"context"
	"sync"
)

// MemStore is a Store in memory with the conditions of MySQLStore, for the
// tests of the packages that use the Service.
type MemStore struct {
	mu   sync.Mutex
	rows map[int64]Password
	err  error
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore { return &MemStore{rows: map[int64]Password{}} }

func (m *MemStore) Get(_ context.Context, userId int64) (*Password, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	p, ok := m.rows[userId]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (m *MemStore) Put(_ context.Context, p *Password) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	v := *p
	v.PendingResetDate, v.PendingDeleteDate, v.AwaitsEmail = 0, 0, false
	old, ok := m.rows[p.UserId]
	v.Email, v.EmailUnconfirmed, v.EmailSetAt = "", "", 0
	if ok {
		v.Email, v.EmailUnconfirmed, v.EmailSetAt = old.Email, old.EmailUnconfirmed, old.EmailSetAt
	}
	m.rows[p.UserId] = v
	return nil
}

func (m *MemStore) Replace(_ context.Context, prev, next *Password) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	old, ok := m.rows[next.UserId]
	if prev == nil && ok || prev != nil && (!ok || !sameRow(&old, prev)) {
		return false, nil
	}
	v := *next
	v.PendingResetDate, v.PendingDeleteDate = 0, 0
	m.rows[next.UserId] = v
	return true, nil
}

func (m *MemStore) RemoveIf(_ context.Context, prev *Password) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	old, ok := m.rows[prev.UserId]
	if !ok || !sameRow(&old, prev) {
		return false, nil
	}
	delete(m.rows, prev.UserId)
	return true, nil
}

// sameRow is the condition of Replace and RemoveIf.
func sameRow(a, b *Password) bool {
	return a.SetAt == b.SetAt && bytes.Equal(a.Verifier, b.Verifier) && a.emails() == b.emails() && a.AwaitsEmail == b.AwaitsEmail
}

func (m *MemStore) SwapEmail(_ context.Context, userId, setAt int64, prev, next Emails, emailSetAt int64) (bool, error) {
	return m.update(userId, func(p *Password) bool {
		if p.SetAt != setAt || p.AwaitsEmail || p.emails() != prev || prev == next {
			return false
		}
		p.Email, p.EmailUnconfirmed, p.EmailSetAt = next.Email, next.Unconfirmed, emailSetAt
		return true
	})
}

func (m *MemStore) Delete(_ context.Context, userId int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.rows[userId]
	delete(m.rows, userId)
	return ok, m.err
}

func (m *MemStore) update(userId int64, f func(p *Password) bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	p, ok := m.rows[userId]
	if !ok || !f(&p) {
		return false, nil
	}
	m.rows[userId] = p
	return true, nil
}

func (m *MemStore) StartReset(_ context.Context, userId, date int64) (bool, error) {
	return m.update(userId, func(p *Password) bool {
		if p.PendingResetDate != 0 {
			return false
		}
		p.PendingResetDate = date
		return true
	})
}

func (m *MemStore) FinishReset(_ context.Context, userId, now int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.rows[userId]
	if !ok || p.PendingResetDate == 0 || p.PendingResetDate > now {
		return false, nil
	}
	delete(m.rows, userId)
	return true, nil
}

func (m *MemStore) CancelReset(_ context.Context, userId int64) (bool, error) {
	return m.update(userId, func(p *Password) bool {
		if p.PendingResetDate == 0 {
			return false
		}
		p.PendingResetDate = 0
		return true
	})
}

func (m *MemStore) StartDelete(_ context.Context, userId, date int64) (bool, error) {
	return m.update(userId, func(p *Password) bool {
		if p.PendingDeleteDate != 0 {
			return false
		}
		p.PendingDeleteDate = date
		return true
	})
}

func (m *MemStore) CancelDelete(_ context.Context, userId int64) (bool, error) {
	return m.update(userId, func(p *Password) bool {
		if p.PendingDeleteDate == 0 {
			return false
		}
		p.PendingDeleteDate = 0
		return true
	})
}

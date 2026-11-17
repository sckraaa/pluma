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

package webbrowser

import (
	"context"
	"slices"
	"sync"
)

// MemStore is a Store in memory with the rules of MySQLStore, for the tests
// of the packages that use the settings.
type MemStore struct {
	mu    sync.Mutex
	users map[int64]*memUser
	err   error
}

type memUser struct {
	flagsSet           bool
	openExternal       bool
	displayCloseButton bool
	exceptions         []memException // in the order they were put
}

type memException struct {
	Exception
	external bool
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore { return &MemStore{users: map[int64]*memUser{}} }

// SetError makes every later call fail with err (nil restores).
func (m *MemStore) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *MemStore) user(userId int64) *memUser {
	u := m.users[userId]
	if u == nil {
		u = &memUser{}
		m.users[userId] = u
	}
	return u
}

func (m *MemStore) Get(_ context.Context, userId int64) (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return Settings{}, m.err
	}
	s := Defaults()
	u := m.users[userId]
	if u == nil {
		return s, nil
	}
	if u.flagsSet {
		s.OpenExternal, s.DisplayCloseButton = u.openExternal, u.displayCloseButton
	}
	for _, e := range u.exceptions {
		if e.external {
			s.External = append(s.External, e.Exception)
		} else {
			s.InApp = append(s.InApp, e.Exception)
		}
	}
	return s, nil
}

func (m *MemStore) SetFlags(_ context.Context, userId int64, openExternal, displayCloseButton bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	u := m.user(userId)
	u.flagsSet, u.openExternal, u.displayCloseButton = true, openExternal, displayCloseButton
	return nil
}

func (m *MemStore) PutException(_ context.Context, userId int64, e Exception, external bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	u := m.user(userId)
	next := slices.DeleteFunc(slices.Clone(u.exceptions), func(x memException) bool { return x.Domain == e.Domain })
	next = append(next, memException{Exception: e, external: external})
	n := 0
	for _, x := range next {
		if x.external == external {
			n++
		}
	}
	if n > MaxExceptions {
		return ErrLimit
	}
	u.exceptions = next
	return nil
}

func (m *MemStore) DeleteException(_ context.Context, userId int64, domain string, external *bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	u := m.users[userId]
	if u == nil {
		return false, nil
	}
	n := len(u.exceptions)
	u.exceptions = slices.DeleteFunc(u.exceptions, func(x memException) bool {
		return x.Domain == domain && (external == nil || x.external == *external)
	})
	return len(u.exceptions) != n, nil
}

func (m *MemStore) DeleteAllExceptions(_ context.Context, userId int64) (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return Settings{}, m.err
	}
	var removed Settings
	u := m.users[userId]
	if u == nil {
		return removed, nil
	}
	for _, e := range u.exceptions {
		if e.external {
			removed.External = append(removed.External, e.Exception)
		} else {
			removed.InApp = append(removed.InApp, e.Exception)
		}
	}
	u.exceptions = nil
	return removed, nil
}

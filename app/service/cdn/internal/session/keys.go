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

package session

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"

	"github.com/teamgram/teamgram-server/app/service/cdn/internal/metrics"
	"github.com/teamgram/teamgram-server/pkg/authkeys"
	"github.com/teamgram/teamgram-server/pkg/mtserver/handshake"
)

// maxSessionsPerKey bounds the sessions kept for one key; the least
// recently used goes first, and its next message starts it again
// (new_session_created).
const maxSessionsPerKey = 64

// AuthKey is a client key the node's handshakes created. Clients' keys
// live only in the node's memory (ADR 0065 §5): after a restart they get
// -404 and make new ones, as they do whenever a CDN key is dropped
// (Android ConnectionsManager.cpp:850-855, tdesktop
// session_private.cpp:2689-2711, iOS MTProto.m:2125-2132).
type AuthKey struct {
	ID   int64
	Type int
	// ExpiresAt is when a temporary key stops working (unix seconds); 0
	// for a permanent key.
	ExpiresAt int64
	// FirstSalt is the salt the handshake gave, valid for its first
	// firstSaltTTL; CreatedAt is the unix time of the handshake.
	FirstSalt int64
	CreatedAt int64

	ak *crypto.AuthKey

	mu       sync.Mutex
	sessions map[int64]*sessionState
	order    *list.List // of *sessionState, most recently used first

	// Owned by KeyStore.mu.
	elem *list.Element
}

// KeyStore holds the client keys in memory: at most max, the least
// recently used dropped first; a temporary key also goes when it expires.
// It is the handshake's handshake.KeySaver. It is safe for concurrent use.
type KeyStore struct {
	max int
	now func() time.Time

	mu   sync.Mutex
	byID map[int64]*AuthKey
	lru  *list.List // of *AuthKey, most recently used first
}

// NewKeyStore returns a store of at most max keys; now is time.Now when
// nil.
func NewKeyStore(max int, now func() time.Time) *KeyStore {
	if max < 1 {
		max = 1
	}
	if now == nil {
		now = time.Now
	}
	return &KeyStore{max: max, now: now, byID: map[int64]*AuthKey{}, lru: list.New()}
}

func keyTypeLabel(t int) string {
	switch t {
	case mtproto.AuthKeyTypePerm:
		return "perm"
	case mtproto.AuthKeyTypeTemp:
		return "temp"
	case mtproto.AuthKeyTypeMediaTemp:
		return "media_temp"
	}
	return "none"
}

// Save stores a key a handshake created (handshake.KeySaver). A temporary
// key expires after expires_in, at most authkeys.MaxTempExpiresIn, as at the
// home. It answers handshake.ErrKeyIdTaken when the node holds another key
// with the same id.
func (s *KeyStore) Save(_ context.Context, key *handshake.AuthKey, expiresIn int32) error {
	now := s.now().Unix()
	k := &AuthKey{
		ID:        key.AuthKeyId,
		Type:      key.AuthKeyType,
		FirstSalt: key.ServerSalt,
		CreatedAt: now,
		ak:        crypto.NewAuthKey(key.AuthKeyId, key.AuthKey),
		sessions:  map[int64]*sessionState{},
		order:     list.New(),
	}
	if expiresIn > 0 {
		k.ExpiresAt = now + int64(authkeys.TempExpiresIn(expiresIn))
	}
	label := keyTypeLabel(key.AuthKeyType)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[k.ID]; ok {
		metrics.Handshakes.WithLabelValues(label, "retry").Inc()
		return handshake.ErrKeyIdTaken
	}
	for len(s.byID) >= s.max {
		s.dropLocked(s.lru.Back().Value.(*AuthKey))
	}
	k.elem = s.lru.PushFront(k)
	s.byID[k.ID] = k
	metrics.AuthKeys.Set(float64(len(s.byID)))
	metrics.Handshakes.WithLabelValues(label, "ok").Inc()
	return nil
}

// Get returns the key with the id, or nil when the node does not hold it
// or it has expired (and is dropped).
func (s *KeyStore) Get(id int64) *AuthKey {
	now := s.now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.byID[id]
	if !ok {
		return nil
	}
	if k.ExpiresAt != 0 && now >= k.ExpiresAt {
		s.dropLocked(k)
		metrics.AuthKeys.Set(float64(len(s.byID)))
		return nil
	}
	s.lru.MoveToFront(k.elem)
	return k
}

// Sweep drops the expired temporary keys. It runs once a minute.
func (s *KeyStore) Sweep() {
	now := s.now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range s.byID {
		if k.ExpiresAt != 0 && now >= k.ExpiresAt {
			s.dropLocked(k)
		}
	}
	metrics.AuthKeys.Set(float64(len(s.byID)))
}

// Len returns how many keys the store holds.
func (s *KeyStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byID)
}

func (s *KeyStore) dropLocked(k *AuthKey) {
	s.lru.Remove(k.elem)
	delete(s.byID, k.ID)
}

// session returns the session state of id, creating it (created) when the
// key has none.
func (k *AuthKey) session(id int64) (st *sessionState, created bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if st, ok := k.sessions[id]; ok {
		k.order.MoveToFront(st.elem)
		return st, false
	}
	for len(k.sessions) >= maxSessionsPerKey {
		old := k.order.Back().Value.(*sessionState)
		k.order.Remove(old.elem)
		delete(k.sessions, old.id)
	}
	st = newSessionState(id)
	st.elem = k.order.PushFront(st)
	k.sessions[id] = st
	return st, true
}

// destroySession forgets the session id; it reports whether the key had it.
func (k *AuthKey) destroySession(id int64) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	st, ok := k.sessions[id]
	if ok {
		k.order.Remove(st.elem)
		delete(k.sessions, id)
	}
	return ok
}

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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teamgram/teamgram-server/pkg/cdn"
)

// receivedWindow is how many msg_ids of a session's received messages are
// remembered.
const receivedWindow = 512

// sessionState is what the node keeps of a client session: the messages
// received last and those in progress, and the count of content messages
// sent (for seqno).
type sessionState struct {
	id   int64
	elem *list.Element // owned by AuthKey.mu

	mu         sync.Mutex
	received   []int64 // ascending
	inFlight   map[int64]struct{}
	contentSeq int32
}

func newSessionState(id int64) *sessionState {
	return &sessionState{id: id, inFlight: map[int64]struct{}{}}
}

// receive records a client msg_id. It returns false (old) when the window
// is full and msg_id is below all of it: bad_msg_notification 20. A msg_id
// received before is reported as seen, and as running while its request
// is in progress.
func (st *sessionState) receive(msgID int64) (old, seen, running bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	i, found := slices.BinarySearch(st.received, msgID)
	if found {
		_, running = st.inFlight[msgID]
		return false, true, running
	}
	if i == 0 && len(st.received) >= receivedWindow {
		return true, false, false
	}
	st.received = slices.Insert(st.received, i, msgID)
	if len(st.received) > receivedWindow {
		st.received = slices.Delete(st.received, 0, 1)
	}
	return false, false, false
}

// start marks a request in progress; it returns false when it already is.
func (st *sessionState) start(msgID int64) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.inFlight[msgID]; ok {
		return false
	}
	st.inFlight[msgID] = struct{}{}
	return true
}

func (st *sessionState) finish(msgID int64) {
	st.mu.Lock()
	delete(st.inFlight, msgID)
	st.mu.Unlock()
}

func (st *sessionState) running(msgID int64) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	_, ok := st.inFlight[msgID]
	return ok
}

// stateInfo returns the msgs_state_info byte of a msg_id
// (https://core.telegram.org/mtproto/service_messages_about_messages):
// 4 + 8 + 32 (received, acknowledged, the query is being processed) for a
// request in progress; 3 for a msg_id too far in the future; 1 for one
// older than the window; 2 (not received) otherwise. The node keeps no
// answers, so for a request already answered it says "not received": the
// client sends it again (tdesktop resends on any state but 4,
// session_private.cpp:2292-2294), which is harmless, since every request
// the node serves only reads.
func (st *sessionState) stateInfo(msgID int64, now time.Time) byte {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.inFlight[msgID]; ok {
		return 4 | 8 | 32
	}
	if msgID>>32 > now.Unix()+msgIDFuture {
		return 3
	}
	if len(st.received) >= receivedWindow && msgID < st.received[0] {
		return 1
	}
	return 2
}

// seqNo returns the seqno of the next message sent in the session: odd for
// a content-related one, which also counts.
func (st *sessionState) seqNo(content bool) int32 {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !content {
		return st.contentSeq * 2
	}
	st.contentSeq++
	return st.contentSeq*2 - 1
}

// Limits of a client msg_id against the node's clock
// (https://core.telegram.org/mtproto/description#message-identifier-msg-id):
// at most 300 seconds in the past and 30 in the future.
const (
	msgIDPast   = 300
	msgIDFuture = 30
)

// msgIDs makes the msg_ids of the node's messages: about unixtime * 2^32,
// increasing across the process, 1 modulo 4 for an answer to a client
// message and 3 for any other.
type msgIDs struct {
	last atomic.Int64
}

func (g *msgIDs) next(now time.Time, answer bool) int64 {
	base := (now.Unix()<<32 | int64(uint64(now.Nanosecond())<<32/1e9)) &^ 3
	for {
		last := g.last.Load()
		id := base
		if id <= last {
			id = last + 4
		}
		if g.last.CompareAndSwap(last, id) {
			if answer {
				return id + 1
			}
			return id + 3
		}
	}
}

// Server salts (https://core.telegram.org/mtproto/description#server-salt):
// the node gives each key a salt per saltPeriod, derived from a secret made
// at start, so nothing is stored per key; the salt of the handshake stays
// valid for firstSaltTTL, and a salt is accepted saltGrace after its
// period ends (and before it starts, for a client clock running ahead).
const (
	saltPeriod   = 3600
	saltGrace    = 300
	firstSaltTTL = 30 * 60
	// maxFutureSalts bounds get_future_salts (Telegram answers at most 64).
	maxFutureSalts = 64
)

type salts struct {
	secret [32]byte
}

func (s *salts) salt(keyID, period int64) int64 {
	m := hmac.New(sha256.New, s.secret[:])
	var b [16]byte
	binary.LittleEndian.PutUint64(b[:], uint64(keyID))
	binary.LittleEndian.PutUint64(b[8:], uint64(period))
	m.Write(b[:])
	return int64(binary.LittleEndian.Uint64(m.Sum(nil)))
}

// current returns the salt of the key in force now.
func (s *salts) current(k *AuthKey, now time.Time) int64 {
	return s.salt(k.ID, now.Unix()/saltPeriod)
}

// valid reports whether a client may use salt now.
func (s *salts) valid(k *AuthKey, salt int64, now time.Time) bool {
	t := now.Unix()
	if salt == k.FirstSalt && t < k.CreatedAt+firstSaltTTL+saltGrace {
		return true
	}
	p := t / saltPeriod
	if salt == s.salt(k.ID, p) {
		return true
	}
	if t-p*saltPeriod < saltGrace && salt == s.salt(k.ID, p-1) {
		return true
	}
	return (p+1)*saltPeriod-t < saltGrace && salt == s.salt(k.ID, p+1)
}

// RequestTokens holds the key the node signs request_tokens with
// (SetRequestTokenKeys), in memory only.
type RequestTokens struct {
	mu      sync.RWMutex
	current []byte
}

// Set replaces the key.
func (r *RequestTokens) Set(current []byte) {
	r.mu.Lock()
	r.current = slices.Clone(current)
	r.mu.Unlock()
}

// Ready reports whether the node has a key.
func (r *RequestTokens) Ready() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current != nil
}

// Sign returns the request_token of fileToken (pkg/cdn), or false before
// the home has sent a key.
func (r *RequestTokens) Sign(fileToken []byte, now time.Time) ([]byte, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.current == nil {
		return nil, false
	}
	return cdn.SignRequestToken(r.current, fileToken, now), true
}

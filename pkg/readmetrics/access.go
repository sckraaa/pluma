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

package readmetrics

import (
	"context"
	"sync"
	"time"
)

const (
	// AccessTTL is how long a user's access to a chat is taken as known:
	// Desktop sends a batch every 5 s while a chat is open
	// (api/api_read_metrics.cpp:16), and each would read the chat from
	// biz otherwise.
	AccessTTL = time.Minute

	// maxAccessEntries bounds the cache: about 100 users with their open
	// chats are far fewer.
	maxAccessEntries = 10_000
)

// Chat is a user's chat whose metrics are reported.
type Chat struct {
	UserId   int64
	PeerType int32
	PeerId   int64
}

// Resolver reads whether the user may see the chat and the id of the
// chat's newest message the user sees. An error that is not the chat's
// refusal (PEER_ID_INVALID) is an error of the reading itself.
type Resolver func(ctx context.Context) (top int32, err error)

type accessEntry struct {
	top     int32
	expires time.Time
}

// AccessCache keeps the answers of Resolvers for AccessTTL. Only an access
// granted is kept: a refusal is the rare answer, and keeping it would hold
// back the metrics of a user who joined the chat a moment after.
type AccessCache struct {
	mu      sync.Mutex
	entries map[Chat]accessEntry
	now     func() time.Time
}

// NewAccessCache returns an empty cache.
func NewAccessCache() *AccessCache {
	return &AccessCache{entries: map[Chat]accessEntry{}, now: time.Now}
}

// Top returns the newest message of the chat that the user may see. It is
// the cached answer, unless there is none, it expired, or the caller needs
// a message newer than the one cached (atLeast): a message sent after the
// answer was cached is not refused for a minute.
func (a *AccessCache) Top(ctx context.Context, chat Chat, atLeast int32, resolve Resolver) (int32, error) {
	now := a.now()
	a.mu.Lock()
	e, ok := a.entries[chat]
	a.mu.Unlock()
	if ok && now.Before(e.expires) && e.top >= atLeast {
		return e.top, nil
	}
	top, err := resolve(ctx)
	if err != nil {
		a.mu.Lock()
		delete(a.entries, chat)
		a.mu.Unlock()
		return 0, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.entries) >= maxAccessEntries {
		a.sweep(now)
	}
	a.entries[chat] = accessEntry{top: top, expires: now.Add(AccessTTL)}
	return top, nil
}

// sweep drops the expired entries, and all of them if that frees nothing.
func (a *AccessCache) sweep(now time.Time) {
	for k, e := range a.entries {
		if !now.Before(e.expires) {
			delete(a.entries, k)
		}
	}
	if len(a.entries) >= maxAccessEntries {
		clear(a.entries)
	}
}

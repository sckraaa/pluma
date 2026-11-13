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

package svc

import (
	"context"
	"sync"

	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/tde2e"
)

// maxCachedChains bounds the cache; past it, it starts over.
const maxCachedChains = 1024

// ChainMembers caches who is in the group state of each call's chain at its
// height. The clients poll phone.getGroupCallChainBlocks every 1-5 s each
// (ADR 0060 §2, «Опрос»), and a read answers a member differently from
// anyone else; the last block, which holds the group state, changes only
// with a join or a removal.
type ChainMembers struct {
	mu    sync.Mutex
	calls map[int64]*cachedChain
}

type cachedChain struct {
	height  int32
	hash    [32]byte
	members map[int64]bool
}

// ChainReader restores the chain of a call.
type ChainReader interface {
	Chain(ctx context.Context, callId int64) (*tde2e.Chain, error)
}

// NewChainMembers returns an empty cache.
func NewChainMembers() *ChainMembers {
	return &ChainMembers{calls: map[int64]*cachedChain{}}
}

// IsMember reports whether userId is in the group state of the chain of the
// call of head, reading the chain when the cache does not have its height.
func (m *ChainMembers) IsMember(ctx context.Context, r ChainReader, head *groupcalls.ChainHead, userId int64) (bool, error) {
	if head.Height < 0 {
		return false, nil
	}
	m.mu.Lock()
	c := m.calls[head.Id]
	m.mu.Unlock()
	if c == nil || c.height != head.Height || c.hash != head.Hash {
		ch, err := r.Chain(ctx, head.Id)
		if err != nil {
			return false, err
		}
		c = &cachedChain{height: ch.Height(), hash: ch.LastHash(), members: map[int64]bool{}}
		for _, p := range ch.GroupState().Participants {
			c.members[p.UserID] = true
		}
		m.mu.Lock()
		if len(m.calls) >= maxCachedChains {
			m.calls = map[int64]*cachedChain{}
		}
		if old := m.calls[head.Id]; old == nil || old.height <= c.height {
			m.calls[head.Id] = c
		}
		m.mu.Unlock()
	}
	return c.members[userId], nil
}

// Forget drops a call, at its end.
func (m *ChainMembers) Forget(callId int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.calls, callId)
}

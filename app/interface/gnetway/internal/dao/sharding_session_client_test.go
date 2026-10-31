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

package dao

import (
	"testing"
	"time"

	sessionclient "github.com/teamgram/teamgram-server/app/interface/session/client"

	"github.com/zeromicro/go-zero/core/hash"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ring is a client of the session nodes as etcd would list them.
func ring(retryAfter time.Duration, nodes ...string) *ShardingSessionClient {
	sess := &ShardingSessionClient{
		dispatcher:   hash.NewConsistentHash(),
		sessions:     make(map[string]sessionclient.SessionClient),
		inRing:       make(map[string]bool),
		failCounters: make(map[string]int),
		retryAfter:   retryAfter,
	}
	for _, n := range nodes {
		sess.sessions[n] = nil
		sess.addToRing(n)
	}
	return sess
}

func failWith(code codes.Code) func(sessionclient.SessionClient) error {
	return func(sessionclient.SessionClient) error {
		return status.Error(code, code.String())
	}
}

func (sess *ShardingSessionClient) routed(t *testing.T, key string) string {
	t.Helper()
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	n, ok := sess.dispatcher.Get(key)
	if !ok {
		t.Fatalf("no node for %s", key)
	}
	return n.(string)
}

// Slow answers leave a node in the ring: they are not a node out of reach.
func TestSlowNodeStays(t *testing.T) {
	sess := ring(time.Hour, "a", "b")
	node := sess.routed(t, "key")
	for range 2 * maxNodeFailures {
		_ = sess.InvokeByKey("key", failWith(codes.DeadlineExceeded))
	}
	if !sess.inRing[node] || sess.routed(t, "key") != node {
		t.Errorf("node %s left the ring after slow answers", node)
	}
}

// The last node stays in the ring: no other node could take its keys, and
// without it every connection would fail.
func TestLastNodeStays(t *testing.T) {
	sess := ring(time.Hour, "a")
	for range 2 * maxNodeFailures {
		_ = sess.InvokeByKey("key", failWith(codes.Unavailable))
	}
	reached := false
	if err := sess.InvokeByKey("key", func(sessionclient.SessionClient) error {
		reached = true
		return nil
	}); err != nil || !reached {
		t.Errorf("the only node after failures: %v, reached %v", err, reached)
	}
}

// A node out of reach leaves the ring, its keys go to another node, and it
// comes back after retryAfter while etcd lists it.
func TestNodeComesBack(t *testing.T) {
	sess := ring(20*time.Millisecond, "a", "b")
	node := sess.routed(t, "key")
	for range maxNodeFailures {
		_ = sess.InvokeByKey("key", failWith(codes.Unavailable))
	}
	sess.mu.RLock()
	out := !sess.inRing[node]
	sess.mu.RUnlock()
	if !out || sess.routed(t, "key") == node {
		t.Fatalf("node %s still routes the key after %d failures", node, maxNodeFailures)
	}
	deadline := time.Now().Add(5 * time.Second)
	for sess.routed(t, "key") != node {
		if time.Now().After(deadline) {
			t.Fatalf("node %s did not come back", node)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A node etcd no longer lists does not come back.
func TestDroppedNodeStaysOut(t *testing.T) {
	sess := ring(10*time.Millisecond, "a", "b")
	node := sess.routed(t, "key")
	for range maxNodeFailures {
		_ = sess.InvokeByKey("key", failWith(codes.Unavailable))
	}
	sess.mu.Lock()
	delete(sess.sessions, node)
	sess.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	if sess.routed(t, "key") == node {
		t.Errorf("node %s came back without etcd listing it", node)
	}
}

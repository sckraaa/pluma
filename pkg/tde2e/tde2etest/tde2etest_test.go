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

package tde2etest

import (
	"testing"

	"github.com/teamgram/teamgram-server/pkg/tde2e"
)

// The blocks and broadcasts built here pass the server's checks.
func TestBuildersPassTheChecks(t *testing.T) {
	ch := tde2e.NewChain()
	for _, u := range []int64{1, 2, 3} {
		if _, err := ch.Join(tde2e.Join{UserID: u, PublicKey: Pub(Key(byte(u))), Block: JoinBlock(ch, u, Key(byte(u)))}); err != nil {
			t.Fatalf("join of %d: %v", u, err)
		}
	}
	if _, err := ch.Join(tde2e.Join{UserID: 2, PublicKey: Pub(Key(2)), Block: JoinBlock(ch, 2, Key(2))}); err != nil {
		t.Fatalf("a join again with the same key: %v", err)
	}
	if _, err := ch.Remove(1, []int64{3}, RemoveBlock(ch, Key(1), 3)); err != nil {
		t.Fatalf("removal: %v", err)
	}
	v := tde2e.NewVerification()
	v.Reset(ch)
	n := [32]byte{9}
	for _, data := range [][]byte{Commit(ch, 1, Key(1), n), Commit(ch, 2, Key(2), n), Reveal(ch, 1, Key(1), n), Reveal(ch, 2, Key(2), n)} {
		b, err := tde2e.ParseBroadcast(data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.Apply(b); err != nil {
			t.Fatal(err)
		}
	}
	if v.Phase() != tde2e.PhaseEnd {
		t.Fatal("the verification is not over")
	}
	// A follower of the server form ends where the chain is.
	server, err := tde2e.ToServer(JoinBlock(nil, 5, Key(5)))
	if err != nil {
		t.Fatal(err)
	}
	f := tde2e.NewChain()
	if err := Follow(f, server); err != nil || f.Height() != 0 {
		t.Fatalf("follow: %v", err)
	}
}

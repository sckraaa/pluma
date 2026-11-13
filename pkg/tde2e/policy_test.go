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

package tde2e

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func TestJoinReferenceZeroBlock(t *testing.T) {
	data := referenceZeroBlock(t)
	c := NewChain()
	a, err := c.Join(Join{UserID: 1000, PublicKey: pubKey(testKey(1)), Block: data})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes, data) || hex.EncodeToString(a.Hash[:]) != referenceZeroBlockHash || c.Height() != 0 {
		t.Fatal("reference block not stored as sent")
	}
	// The same block for another user or key is refused.
	for _, j := range []Join{
		{UserID: 1001, PublicKey: pubKey(testKey(1)), Block: data},
		{UserID: 1000, PublicKey: pubKey(testKey(2)), Block: data},
	} {
		_, err := NewChain().Join(j)
		wantErr(t, err, KindInvalid, CodePolicy)
	}
}

// joined returns a chain the users 1..n entered one by one, each with the
// key of the seed equal to his id and flags 3, as tde2e builds them
// (e2e_api.cpp:454-460).
func joined(t *testing.T, n int) *Chain {
	t.Helper()
	c := NewChain()
	var ps []Participant
	for id := 1; id <= n; id++ {
		k := testKey(byte(id))
		ps = append(ps, member(int64(id), 3, k))
		if _, err := c.Join(Join{UserID: int64(id), PublicKey: pubKey(k), Block: callBlock(c, k, ps...)}); err != nil {
			t.Fatalf("user %d joins: %v", id, err)
		}
	}
	return c
}

func TestJoin(t *testing.T) {
	alice, bob, carol, alice2 := testKey(1), testKey(2), testKey(3), testKey(11)
	a, b, c := member(1, 3, alice), member(2, 3, bob), member(3, 3, carol)
	tests := []struct {
		name    string
		members int // users in the chain before the block
		user    int64
		key     ed25519.PrivateKey
		block   func(ch *Chain) []byte
		owner   int64 // KeyOwner answers this user for the key; 0: unknown
		kind    Kind
		code    Code
	}{
		{name: "block 0", user: 1, key: alice, block: func(ch *Chain) []byte { return callBlock(ch, alice, a) }},
		{name: "block 0 with another user", user: 1, key: alice, block: func(ch *Chain) []byte { return callBlock(ch, alice, a, b) }, code: CodePolicy},
		{name: "block 0 without the caller", user: 1, key: alice, block: func(ch *Chain) []byte { return callBlock(ch, alice, member(2, 3, alice)) }, code: CodePolicy},
		{name: "block 0 closed to others", user: 1, key: alice, block: func(ch *Chain) []byte {
			blk := nextBlock(ch, setGroup(0, a), setKey(testSharedKey(1)))
			signBlock(blk, alice)
			return blk.Marshal()
		}, code: CodePolicy},
		{name: "join", members: 1, user: 2, key: bob, block: func(ch *Chain) []byte { return callBlock(ch, bob, a, b) }},
		{name: "join, members in another order", members: 2, user: 3, key: carol, block: func(ch *Chain) []byte { return callBlock(ch, carol, c, b, a) }},
		{name: "join adding another user", members: 1, user: 2, key: bob, block: func(ch *Chain) []byte { return callBlock(ch, bob, a, b, c) }, code: CodePolicy},
		{name: "join dropping a member", members: 2, user: 3, key: carol, block: func(ch *Chain) []byte { return callBlock(ch, carol, a, c) }, code: CodePolicy},
		{name: "join changing a member's flags", members: 1, user: 2, key: bob, block: func(ch *Chain) []byte { return callBlock(ch, bob, member(1, 1, alice), b) }, code: CodePolicy},
		{name: "join changing a member's version", members: 1, user: 2, key: bob, block: func(ch *Chain) []byte {
			moved := a
			moved.Version = 5
			return callBlock(ch, bob, moved, b)
		}},
		{name: "rejoin with a new key", members: 2, user: 1, key: alice2, block: func(ch *Chain) []byte { return callBlock(ch, alice2, b, member(1, 3, alice2)) }},
		{name: "rejoin with the same key", members: 2, user: 1, key: alice, owner: 1, block: func(ch *Chain) []byte { return callBlock(ch, alice, b, a) }},
		{name: "rejoin with the own key after removal", members: 1, user: 2, key: bob, owner: 2, block: func(ch *Chain) []byte { return callBlock(ch, bob, a, b) }},
		{name: "key of another user before", members: 1, user: 2, key: bob, owner: 7, block: func(ch *Chain) []byte { return callBlock(ch, bob, a, b) }, code: CodePolicy},
		{name: "key of another member", members: 1, user: 2, key: alice, block: func(ch *Chain) []byte { return callBlock(ch, alice, a, member(2, 3, alice)) }, code: CodeInvalidGroupState},
		{name: "signed by another key", members: 1, user: 2, key: bob, block: func(ch *Chain) []byte { return callBlock(ch, carol, a, b) }, code: CodePolicy},
		{name: "no signature key", members: 1, user: 2, key: bob, block: func(ch *Chain) []byte {
			blk := nextBlock(ch, setGroup(3, a, b), setKey(testSharedKey(1, 2)))
			signBlockNoKey(blk, bob)
			return blk.Marshal()
		}, code: CodePolicy},
		{name: "an extra change", members: 1, user: 2, key: bob, block: func(ch *Chain) []byte {
			blk := nextBlock(ch, setGroup(3, a, b), setKey(testSharedKey(1, 2)), Change{Kind: ChangeNoop})
			signBlock(blk, bob)
			return blk.Marshal()
		}, code: CodePolicy},
		{name: "empty shared key", members: 1, user: 2, key: bob, block: func(ch *Chain) []byte {
			blk := nextBlock(ch, setGroup(3, a, b), setKey(&SharedKey{}))
			signBlock(blk, bob)
			return blk.Marshal()
		}, code: CodePolicy},
		{name: "shared key without the joining user", members: 1, user: 2, key: bob, block: func(ch *Chain) []byte {
			blk := nextBlock(ch, setGroup(3, a, b), setKey(testSharedKey(1)))
			signBlock(blk, bob)
			return blk.Marshal()
		}, code: CodeInvalidSharedSecret},
		{name: "on an old block", members: 2, user: 3, key: carol, block: func(ch *Chain) []byte {
			blk := nextBlock(ch, setGroup(3, a, b, c), setKey(testSharedKey(1, 2, 3)))
			blk.Height--
			signBlock(blk, carol)
			return blk.Marshal()
		}, kind: KindStale, code: CodeHeightMismatch},
		{name: "too large", members: 1, user: 2, key: bob, block: func(*Chain) []byte { return make([]byte, MaxBlockSize+1) }, code: CodePolicy},
		{name: "not a block", members: 1, user: 2, key: bob, block: func(*Chain) []byte { return []byte{1, 2, 3, 4} }, code: CodeInvalidBlock},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := joined(t, tt.members)
			height, hash := ch.Height(), ch.LastHash()
			j := Join{UserID: tt.user, PublicKey: pubKey(tt.key), Block: tt.block(ch)}
			if tt.owner != 0 {
				j.KeyOwner = func([32]byte) (int64, bool) { return tt.owner, true }
			}
			_, err := ch.Join(j)
			kind := tt.kind
			if tt.code == 0 {
				kind = 0
			}
			wantErr(t, err, kind, tt.code)
			if err != nil && (ch.Height() != height || ch.LastHash() != hash) {
				t.Fatal("a refused block changed the chain")
			}
			if err == nil {
				if p, ok := ch.GroupState().Participant(tt.user); !ok || p.PublicKey != pubKey(tt.key) {
					t.Fatal("the user is not in the group state with his key")
				}
			}
		})
	}
}

func TestJoinRace(t *testing.T) {
	// Two users write their join on the same height: the first is accepted,
	// the second is stale and succeeds after reading the new last block.
	bob, carol := testKey(2), testKey(3)
	ch := joined(t, 1)
	a := member(1, 3, testKey(1))
	first := callBlock(ch, bob, a, member(2, 3, bob))
	second := callBlock(ch, carol, a, member(3, 3, carol))
	if _, err := ch.Join(Join{UserID: 2, PublicKey: pubKey(bob), Block: first}); err != nil {
		t.Fatal(err)
	}
	_, err := ch.Join(Join{UserID: 3, PublicKey: pubKey(carol), Block: second})
	wantErr(t, err, KindStale, CodeHeightMismatch)
	retry := callBlock(ch, carol, a, member(2, 3, bob), member(3, 3, carol))
	if _, err := ch.Join(Join{UserID: 3, PublicKey: pubKey(carol), Block: retry}); err != nil {
		t.Fatal(err)
	}
}

func TestJoinLimit(t *testing.T) {
	keys := make([]ed25519.PrivateKey, MaxParticipants+1)
	ps := make([]Participant, MaxParticipants)
	for i := range keys {
		keys[i] = testKey(byte(i + 1))
	}
	for i := range ps {
		ps[i] = member(int64(i+1), 3, keys[i])
	}
	// The chain rules let block 0 hold anyone; the policy only lets a caller
	// in alone, so the full call is built with Apply.
	ch := NewChain()
	mustApply(t, ch, callBlock(ch, keys[0], ps...))

	extra := keys[MaxParticipants]
	over := append(append([]Participant{}, ps...), member(MaxParticipants+1, 3, extra))
	_, err := ch.Join(Join{UserID: MaxParticipants + 1, PublicKey: pubKey(extra), Block: callBlock(ch, extra, over...)})
	wantErr(t, err, KindTooMany, CodePolicy)

	// A member coming back with a new key does not grow the call.
	newKey := testKey(200)
	back := append(append([]Participant{}, ps[1:]...), member(1, 3, newKey))
	if _, err := ch.Join(Join{UserID: 1, PublicKey: pubKey(newKey), Block: callBlock(ch, newKey, back...)}); err != nil {
		t.Fatal(err)
	}
}

func TestRemove(t *testing.T) {
	alice, bob, carol := testKey(1), testKey(2), testKey(3)
	a, b, c := member(1, 3, alice), member(2, 3, bob), member(3, 3, carol)
	tests := []struct {
		name   string
		caller int64
		ids    []int64
		block  func(ch *Chain) []byte
		kind   Kind
		code   Code
		left   int
	}{
		{name: "remove one", caller: 1, ids: []int64{2}, block: func(ch *Chain) []byte { return callBlock(ch, alice, a, c) }, left: 2},
		{name: "remove two", caller: 3, ids: []int64{1, 2}, block: func(ch *Chain) []byte { return callBlock(ch, carol, c) }, left: 1},
		{name: "remaining members in another order", caller: 1, ids: []int64{2}, block: func(ch *Chain) []byte { return callBlock(ch, alice, c, a) }, left: 2},
		{name: "remove fewer than asked", caller: 1, ids: []int64{2, 3}, block: func(ch *Chain) []byte { return callBlock(ch, alice, a, c) }, code: CodePolicy},
		{name: "remove more than asked", caller: 1, ids: []int64{2}, block: func(ch *Chain) []byte { return callBlock(ch, alice, a) }, code: CodePolicy},
		{name: "caller outside the group", caller: 4, ids: []int64{2}, block: func(ch *Chain) []byte { return callBlock(ch, testKey(4), a, c) }, kind: KindForbidden, code: CodePolicy},
		{name: "caller removes himself", caller: 1, ids: []int64{1}, block: func(ch *Chain) []byte { return callBlock(ch, alice, b, c) }, code: CodePolicy},
		{name: "user outside the group", caller: 1, ids: []int64{9}, block: func(ch *Chain) []byte { return callBlock(ch, alice, a, b, c) }, code: CodePolicy},
		{name: "no users", caller: 1, block: func(ch *Chain) []byte { return callBlock(ch, alice, a, b, c) }, code: CodePolicy},
		{name: "signed by another member", caller: 1, ids: []int64{2}, block: func(ch *Chain) []byte { return callBlock(ch, carol, a, c) }, code: CodePolicy},
		{name: "on an old block", caller: 1, ids: []int64{2}, block: func(ch *Chain) []byte {
			blk := nextBlock(ch, setGroup(3, a, c), setKey(testSharedKey(1, 3)))
			blk.PrevBlockHash = [32]byte{}
			signBlock(blk, alice)
			return blk.Marshal()
		}, kind: KindStale, code: CodeHashMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := joined(t, 3)
			height := ch.Height()
			_, err := ch.Remove(tt.caller, tt.ids, tt.block(ch))
			kind := tt.kind
			if tt.code == 0 {
				kind = 0
			}
			wantErr(t, err, kind, tt.code)
			if err != nil {
				if ch.Height() != height {
					t.Fatal("a refused block changed the chain")
				}
				return
			}
			g := ch.GroupState()
			if len(g.Participants) != tt.left {
				t.Fatalf("%d participants left", len(g.Participants))
			}
			for _, id := range tt.ids {
				if _, ok := g.Participant(id); ok {
					t.Fatalf("user %d still in the group state", id)
				}
			}
		})
	}
}

func TestRemoveRace(t *testing.T) {
	// Both remaining users remove the one who left: the first block is
	// accepted, the second is stale (ADR 0060 §2).
	alice, carol := testKey(1), testKey(3)
	ch := joined(t, 3)
	a, c := member(1, 3, alice), member(3, 3, carol)
	byAlice := callBlock(ch, alice, a, c)
	byCarol := callBlock(ch, carol, a, c)
	if _, err := ch.Remove(1, []int64{2}, byAlice); err != nil {
		t.Fatal(err)
	}
	_, err := ch.Remove(3, []int64{2}, byCarol)
	wantErr(t, err, KindStale, CodeHeightMismatch)
}

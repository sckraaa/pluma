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
	"testing"
)

func commitOf(c *Chain, userID int64, k ed25519.PrivateKey, nonce [32]byte) *Broadcast {
	b := &Broadcast{Kind: BroadcastCommit, UserID: userID, ChainHeight: c.Height(), ChainHash: c.LastHash(), Value: sha(nonce[:])}
	signBroadcast(b, k)
	return b
}

func revealOf(c *Chain, userID int64, k ed25519.PrivateKey, nonce [32]byte) *Broadcast {
	b := &Broadcast{Kind: BroadcastReveal, UserID: userID, ChainHeight: c.Height(), ChainHash: c.LastHash(), Value: nonce}
	signBroadcast(b, k)
	return b
}

// twoMembers is a chain at height 0 with alice (1) and bob (2).
func twoMembers(t *testing.T) *Chain {
	t.Helper()
	alice, bob := testKey(1), testKey(2)
	c := NewChain()
	mustApply(t, c, callBlock(c, alice, member(1, 3, alice), member(2, 3, bob)))
	return c
}

func TestBroadcastRoundTrip(t *testing.T) {
	c := twoMembers(t)
	for _, b := range []*Broadcast{commitOf(c, 1, testKey(1), [32]byte{7}), revealOf(c, -2, testKey(2), [32]byte{8})} {
		data := b.Marshal()
		if len(data) != 4+64+8+4+32+32 {
			t.Fatalf("%d bytes", len(data))
		}
		got, err := ParseBroadcast(data)
		if err != nil {
			t.Fatal(err)
		}
		if *got != *b || !bytes.Equal(got.Marshal(), data) {
			t.Fatalf("round trip: %+v", got)
		}
		server, err := ToServer(data)
		if err != nil {
			t.Fatal(err)
		}
		tests := [][]byte{nil, data[:len(data)-1], append(append([]byte{}, data...), 0), server}
		for _, bad := range tests {
			_, err := ParseBroadcast(bad)
			wantErr(t, err, KindInvalid, CodeInvalidBlock)
		}
	}
}

func TestVerification(t *testing.T) {
	alice, bob, carol := testKey(1), testKey(2), testKey(3)
	nonceA, nonceB := [32]byte{0xa}, [32]byte{0xb}
	c := twoMembers(t)
	v := NewVerification()
	v.Reset(c)
	otherHash := commitOf(c, 1, alice, nonceA)
	otherHash.ChainHash[0] ^= 1
	signBroadcast(otherHash, alice)
	future := commitOf(c, 1, alice, nonceA)
	future.ChainHeight++
	signBroadcast(future, alice)
	// One run of the emoji verification at height 0, step by step
	// (Call.cpp:181-257).
	steps := []struct {
		name  string
		b     *Broadcast
		kind  Kind
		code  Code
		phase Phase
	}{
		{"reveal before any commit", revealOf(c, 1, alice, nonceA), KindInvalid, CodeBroadcastNotInReveal, PhaseCommit},
		{"commit of a user outside the group", commitOf(c, 3, carol, nonceA), KindForbidden, CodeBroadcastUnknownUserID, PhaseCommit},
		{"commit signed by another key", commitOf(c, 1, bob, nonceA), KindInvalid, CodeInvalidSignature, PhaseCommit},
		{"commit for another block", otherHash, KindInvalid, CodeBroadcastInvalidBlockHash, PhaseCommit},
		{"commit for a later height", future, KindInvalid, CodeBroadcastInFuture, PhaseCommit},
		{"alice commits", commitOf(c, 1, alice, nonceA), 0, 0, PhaseCommit},
		{"alice commits again", commitOf(c, 1, alice, nonceB), KindInvalid, CodeBroadcastAlreadyApplied, PhaseCommit},
		{"reveal before all commits", revealOf(c, 1, alice, nonceA), KindInvalid, CodeBroadcastNotInReveal, PhaseCommit},
		{"bob commits", commitOf(c, 2, bob, nonceB), 0, 0, PhaseReveal},
		{"commit after all commits", commitOf(c, 2, bob, nonceB), KindInvalid, CodeBroadcastNotInCommit, PhaseReveal},
		{"reveal of another nonce", revealOf(c, 1, alice, nonceB), KindInvalid, CodeBroadcastInvalidReveal, PhaseReveal},
		{"reveal signed by another key", revealOf(c, 1, bob, nonceA), KindInvalid, CodeInvalidSignature, PhaseReveal},
		{"alice reveals", revealOf(c, 1, alice, nonceA), 0, 0, PhaseReveal},
		{"alice reveals again", revealOf(c, 1, alice, nonceA), KindInvalid, CodeBroadcastAlreadyApplied, PhaseReveal},
		{"bob reveals", revealOf(c, 2, bob, nonceB), 0, 0, PhaseEnd},
		{"reveal after the end", revealOf(c, 2, bob, nonceB), KindInvalid, CodeBroadcastNotInReveal, PhaseEnd},
	}
	// The steps share v and run in order.
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			stale, err := v.Apply(s.b)
			if stale {
				t.Fatal("stale")
			}
			wantErr(t, err, s.kind, s.code)
			if v.Phase() != s.phase {
				t.Fatalf("phase %d, want %d", v.Phase(), s.phase)
			}
		})
	}
	if !v.Committed(1) || !v.Committed(2) || !v.Revealed(1) || !v.Revealed(2) || v.Committed(3) {
		t.Fatal("progress not recorded")
	}
}

func TestVerificationNewBlock(t *testing.T) {
	alice, bob, carol := testKey(1), testKey(2), testKey(3)
	c := twoMembers(t)
	v := NewVerification()
	v.Reset(c)
	old := commitOf(c, 1, alice, [32]byte{1})
	if _, err := v.Apply(old); err != nil {
		t.Fatal(err)
	}
	mustApply(t, c, callBlock(c, carol, member(1, 3, alice), member(2, 3, bob), member(3, 3, carol)))
	v.Reset(c)
	if v.Height() != 1 || v.Phase() != PhaseCommit || v.Committed(1) {
		t.Fatal("the new block did not restart the verification")
	}
	// A broadcast for the previous block is stale: accepted unchecked,
	// nothing recorded (Call.cpp:112-116).
	stale, err := v.Apply(old)
	if err != nil || !stale || v.Committed(1) {
		t.Fatalf("stale broadcast: %v, %v", stale, err)
	}
	if _, err := v.Apply(commitOf(c, 3, carol, [32]byte{3})); err != nil {
		t.Fatal(err)
	}

	empty := NewVerification()
	empty.Reset(NewChain())
	_, err = empty.Apply(commitOf(c, 1, alice, [32]byte{1}))
	wantErr(t, err, KindInvalid, CodeBroadcastInFuture)
}

func TestVerificationAccept(t *testing.T) {
	alice := testKey(1)
	c := twoMembers(t)
	v := NewVerification()
	v.Reset(c)
	data := commitOf(c, 1, alice, [32]byte{1}).Marshal()

	_, err := v.Accept(2, data)
	wantErr(t, err, KindInvalid, CodePolicy)
	if v.Committed(1) {
		t.Fatal("a refused broadcast was recorded")
	}
	_, err = v.Accept(1, data[:10])
	wantErr(t, err, KindInvalid, CodeInvalidBlock)

	res, err := v.Accept(1, data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stale || !bytes.Equal(res.Bytes, data) || res.Broadcast.UserID != 1 {
		t.Fatalf("result %+v", res)
	}
	_, err = v.Accept(1, data)
	wantErr(t, err, KindInvalid, CodeBroadcastAlreadyApplied)
}

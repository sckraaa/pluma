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

// The cases of tde2e's BlockchainValidation tests (test/blockchain.cpp) that
// write no values, against a new chain, plus the rules those tests leave
// out. Participants of the tde2e tests have version 1; the version takes no
// part in the rules.

func TestChainZeroBlock(t *testing.T) {
	alice, bob := testKey(1), testKey(2)
	tests := []struct {
		name  string
		block func() *Block
		kind  Kind
		code  Code
	}{
		{
			// blockchain.cpp:21-30.
			name: "group state change only",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0, member(1, PermAll, alice)))
				signBlock(b, alice)
				return b
			},
		},
		{
			// blockchain.cpp:41-50.
			name: "wrong height",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0))
				b.Height = 1
				signBlock(b, alice)
				return b
			},
			kind: KindStale, code: CodeHeightMismatch,
		},
		{
			// blockchain.cpp:51-60.
			name: "wrong previous hash",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0))
				b.PrevBlockHash = [32]byte{1}
				signBlock(b, alice)
				return b
			},
			kind: KindStale, code: CodeHashMismatch,
		},
		{
			// blockchain.cpp:61-71.
			name: "zero signature",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0, member(1, PermAll, alice)))
				signBlock(b, alice)
				b.Signature = [64]byte{}
				return b
			},
			kind: KindInvalid, code: CodeInvalidSignature,
		},
		{
			name: "signed by another key",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0, member(1, PermAll, alice)))
				signBlock(b, alice)
				pk := pubKey(bob)
				b.SignaturePublicKey = &pk
				return b
			},
			kind: KindInvalid, code: CodeInvalidSignature,
		},
		{
			// Blockchain.cpp:521-527: block 0 has nobody to take a key from.
			name: "no signature key",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0, member(1, PermAll, alice)))
				signBlockNoKey(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeUnknown,
		},
		{
			// blockchain.cpp:93-101.
			name: "another user in the group state",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0, member(2, 3, bob)))
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeNoPermissions,
		},
		{
			// blockchain.cpp:103-110.
			name: "group state also in the proof",
			block: func() *Block {
				ch := setGroup(0, member(1, 3, alice))
				b := nextBlock(NewChain(), ch)
				b.StateProof.GroupState = ch.GroupState
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeStateProofGroup,
		},
		{
			// blockchain.cpp:111-121.
			name: "duplicate user_id",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0, member(1, 1, alice), member(1, 1, bob)), setKey(testSharedKey(1)))
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeInvalidGroupState,
		},
		{
			// blockchain.cpp:122-133.
			name: "duplicate public key",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0, member(1, 1, alice), member(2, 1, alice)), setKey(testSharedKey(1)))
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeInvalidGroupState,
		},
		{
			name: "participant flags out of range",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0, member(1, 1<<3, alice)))
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeInvalidGroupState,
		},
		{
			name: "external permissions out of range",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(1<<3, member(1, 3, alice)))
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeInvalidGroupState,
		},
		{
			name: "no changes",
			block: func() *Block {
				b := nextBlock(NewChain(), Change{Kind: ChangeNoop, Nonce: [32]byte{1}})
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeNoChanges,
		},
		{
			name: "key-value change",
			block: func() *Block {
				b := nextBlock(NewChain(), Change{Kind: ChangeSetValue, Key: bytes.Repeat([]byte{1}, 32), Value: []byte("b")})
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeInvalidBlock,
		},
		{
			name: "wrong key-value hash",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0, member(1, 3, alice)))
				b.StateProof.KVHash = [32]byte{}
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeUnknown,
		},
		{
			name: "shared key set twice",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0, member(1, 3, alice)), setKey(testSharedKey(1)), setKey(testSharedKey(1)))
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeUnknown,
		},
		{
			name: "shared key in the proof when it changes",
			block: func() *Block {
				b := nextBlock(NewChain(), setGroup(0, member(1, 3, alice)), setKey(testSharedKey(1)))
				b.StateProof.SharedKey = testSharedKey(1)
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeStateProofSecret,
		},
		{
			name: "shared key without a group state change",
			block: func() *Block {
				b := nextBlock(NewChain(), setKey(&SharedKey{}))
				signBlock(b, alice)
				return b
			},
			kind: KindInvalid, code: CodeNoPermissions,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChain()
			_, err := c.Apply(tt.block().Marshal())
			wantErr(t, err, tt.kind, tt.code)
			if err != nil && (c.Height() != -1 || c.LastHash() != [32]byte{}) {
				t.Fatal("a refused block changed the chain")
			}
		})
	}
}

func TestChainGroupStateChanges(t *testing.T) {
	alice, bob, carol := testKey(1), testKey(2), testKey(3)
	a, b, c := member(1, 1, alice), member(2, 2, bob), member(3, 2, carol)
	// The zero block of blockchain.cpp:138-149: alice may add, bob may
	// remove, and with external permissions 3 anyone may add and remove.
	zero := func(t *testing.T, external int32) *Chain {
		t.Helper()
		ch := NewChain()
		blk := nextBlock(ch, setGroup(external, a, b), setKey(testSharedKey(1, 2)))
		signBlock(blk, alice)
		mustApply(t, ch, blk.Marshal())
		return ch
	}
	headers := func(n int) [][]byte {
		h := make([][]byte, n)
		for i := range h {
			h[i] = []byte("??")
		}
		return h
	}
	tests := []struct {
		name string
		// external is the external permissions of the zero block, newExternal
		// those of the tested block (0 in the tde2e tests).
		external, newExternal int32
		signer                ed25519.PrivateKey
		group                 []Participant
		key                   *SharedKey
		code                  Code
	}{
		// blockchain.cpp:155-166.
		{name: "remove without the permission", external: 3, signer: alice, group: []Participant{a}, key: testSharedKey(1), code: CodeNoPermissions},
		// blockchain.cpp:167-178.
		{name: "add without the permission", external: 3, signer: bob, group: []Participant{c}, key: testSharedKey(3), code: CodeNoPermissions},
		// blockchain.cpp:179-190.
		{name: "raise own flags", external: 3, signer: alice, group: []Participant{member(1, 3, alice), b}, key: testSharedKey(1, 2), code: CodeNoPermissions},
		// blockchain.cpp:191-202.
		{name: "new shared key", external: 3, signer: alice, group: []Participant{a, b}, key: testSharedKey(1, 2)},
		// blockchain.cpp:203-214.
		{name: "remove self and set the shared key", external: 3, signer: bob, group: []Participant{a}, key: testSharedKey(1), code: CodeNoPermissions},
		// blockchain.cpp:215-228.
		{name: "self join", external: 3, signer: carol, group: []Participant{a, b, c}, key: testSharedKey(1, 2, 3)},
		// blockchain.cpp:229-249.
		{name: "self join without external permissions", external: 0, signer: carol, group: []Participant{a, b, member(3, 0, carol)}, key: testSharedKey(1, 2, 3), code: CodeNoPermissions},
		{name: "raise external permissions", external: 3, newExternal: PermAll, signer: alice, group: []Participant{a, b}, key: testSharedKey(1, 2), code: CodeNoPermissions},
		{name: "a new key for a user, signed by it", external: 3, signer: carol, group: []Participant{member(1, 1, carol), b}, key: testSharedKey(1, 2)},
		{name: "group state change without a shared key", external: 3, signer: alice, group: []Participant{a, b}},
		// blockchain.cpp:250-260.
		{name: "shared key for fewer users", external: 3, signer: alice, group: []Participant{a, b}, key: testSharedKey(1), code: CodeInvalidSharedSecret},
		// blockchain.cpp:261-274.
		{name: "users and headers differ", external: 3, signer: alice, group: []Participant{a, b}, key: &SharedKey{EncryptedSharedKey: []byte("dummy"), DestUserIDs: []int64{1, 2}, DestHeaders: headers(3)}, code: CodeInvalidSharedSecret},
		// blockchain.cpp:275-288.
		{name: "duplicate users in the shared key", external: 3, signer: alice, group: []Participant{a, b}, key: &SharedKey{EncryptedSharedKey: []byte("dummy"), DestUserIDs: []int64{1, 1}, DestHeaders: headers(2)}, code: CodeInvalidSharedSecret},
		// blockchain.cpp:289-302.
		{name: "unknown user in the shared key", external: 3, signer: alice, group: []Participant{a, b}, key: &SharedKey{EncryptedSharedKey: []byte("dummy"), DestUserIDs: []int64{1, 3}, DestHeaders: headers(2)}, code: CodeInvalidSharedSecret},
		// blockchain.cpp:303-312.
		{name: "duplicate users in the group state", external: 3, signer: alice, group: []Participant{a, member(1, 2, bob)}, code: CodeInvalidGroupState},
		// blockchain.cpp:313-322.
		{name: "duplicate keys in the group state", external: 3, signer: alice, group: []Participant{a, member(2, 2, alice)}, code: CodeInvalidGroupState},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := zero(t, tt.external)
			changes := []Change{setGroup(tt.newExternal, tt.group...)}
			if tt.key != nil {
				changes = append(changes, setKey(tt.key))
			}
			blk := nextBlock(ch, changes...)
			signBlock(blk, tt.signer)
			before := ch.LastHash()
			_, err := ch.Apply(blk.Marshal())
			if tt.code == 0 {
				wantErr(t, err, 0, 0)
				if ch.Height() != 1 || ch.LastHash() != sha(blk.Marshal()) {
					t.Fatal("the chain did not move to the block")
				}
				return
			}
			wantErr(t, err, KindInvalid, tt.code)
			if ch.Height() != 0 || ch.LastHash() != before {
				t.Fatal("a refused block changed the chain")
			}
		})
	}
}

func TestChainSignatureKeyFallback(t *testing.T) {
	// Without signature_public_key the block is checked with the key of the
	// first participant (Blockchain.cpp:521-524).
	alice, bob := testKey(1), testKey(2)
	tests := []struct {
		name   string
		signer ed25519.PrivateKey
		code   Code
	}{
		{"signed by the first participant", alice, 0},
		{"signed by another participant", bob, CodeInvalidSignature},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := NewChain()
			mustApply(t, ch, callBlock(ch, alice, member(1, 3, alice), member(2, 3, bob)))
			blk := nextBlock(ch, setGroup(3, member(1, 3, alice), member(2, 3, bob)), setKey(testSharedKey(1, 2)))
			signBlockNoKey(blk, tt.signer)
			_, err := ch.Apply(blk.Marshal())
			wantErr(t, err, KindInvalid, tt.code)
		})
	}
}

func TestChainState(t *testing.T) {
	alice, bob := testKey(1), testKey(2)
	ch := NewChain()
	a0 := mustApply(t, ch, callBlock(ch, alice, member(1, 3, alice)))
	if a0.Hash != sha(a0.Bytes) || ch.LastHash() != a0.Hash || ch.Height() != 0 {
		t.Fatal("block 0 not recorded")
	}
	// A block naming block 0 by another hash is stale.
	stale := nextBlock(ch, setGroup(3, member(1, 3, alice), member(2, 3, bob)), setKey(testSharedKey(1, 2)))
	stale.PrevBlockHash[0] ^= 1
	signBlock(stale, bob)
	_, err := ch.Apply(stale.Marshal())
	wantErr(t, err, KindStale, CodeHashMismatch)

	a1 := mustApply(t, ch, callBlock(ch, bob, member(1, 3, alice), member(2, 3, bob)))
	if a1.Block.PrevBlockHash != a0.Hash || ch.Height() != 1 {
		t.Fatal("block 1 not chained to block 0")
	}
	g := ch.GroupState()
	if len(g.Participants) != 2 || g.ExternalPermissions != 3 {
		t.Fatalf("group state %+v", g)
	}
	g.Participants[0].Flags = 0
	if ch.GroupState().Participants[0].Flags != 3 {
		t.Fatal("GroupState returned the chain's own state")
	}
	if k := ch.SharedKey(); len(k.DestUserIDs) != 2 {
		t.Fatalf("shared key for %v", k.DestUserIDs)
	}
	// Block 0 again is stale now.
	_, err = ch.Apply(a0.Bytes)
	wantErr(t, err, KindStale, CodeHeightMismatch)
}

func TestRestoreChain(t *testing.T) {
	alice, bob, carol := testKey(1), testKey(2), testKey(3)
	ch := NewChain()
	mustApply(t, ch, callBlock(ch, alice, member(1, 3, alice)))
	last := mustApply(t, ch, callBlock(ch, bob, member(1, 3, alice), member(2, 3, bob)))

	restored, err := RestoreChain(last.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Height() != ch.Height() || restored.LastHash() != ch.LastHash() {
		t.Fatal("height or hash differ")
	}
	if !restored.GroupState().equal(ch.GroupState()) || !restored.SharedKey().equal(ch.SharedKey()) {
		t.Fatal("state differs")
	}
	next := callBlock(ch, carol, member(1, 3, alice), member(2, 3, bob), member(3, 3, carol))
	a, err := ch.Apply(next)
	if err != nil {
		t.Fatal(err)
	}
	b, err := restored.Apply(next)
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash != b.Hash {
		t.Fatal("the next block hashes differently")
	}

	// The reference block restores to height 0.
	ref, err := RestoreChain(referenceZeroBlock(t))
	if err != nil {
		t.Fatal(err)
	}
	if ref.Height() != 0 || ref.LastHash() != sha(referenceZeroBlock(t)) {
		t.Fatal("reference block restored wrong")
	}

	noGroup := &Block{Height: 3, Changes: []Change{{Kind: ChangeNoop}}, StateProof: StateProof{KVHash: EmptyKVHash}}
	signBlock(noGroup, alice)
	negative := &Block{Height: -1, StateProof: StateProof{KVHash: EmptyKVHash}}
	signBlock(negative, alice)
	tests := []struct {
		name string
		data []byte
		code Code
	}{
		{"not a block", []byte{1, 2, 3, 4}, CodeInvalidBlock},
		{"negative height", negative.Marshal(), CodeInvalidBlock},
		{"no group state", noGroup.Marshal(), CodeStateProofGroup},
	}
	for _, tt := range tests {
		_, err := RestoreChain(tt.data)
		wantErr(t, err, KindInvalid, tt.code)
	}
}

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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

// referenceZeroBlockHex is block 0 of a call built by tdlib's tde2e
// (Call::create_zero_block): user 1000 with flags 3 and version 0, the
// Ed25519 key of the seed 32×0x01, external permissions 3. tweb captured it
// from a C++ runner and checks its own port against it
// (src/lib/calls/e2e/tests/zeroBlockByteCompat.test.ts:1-38); the tdlib
// commit of that runner is not stated there. Its shared key is random, so
// the block cannot be rebuilt, only checked.
const referenceZeroBlockHex = "" +
	"b63d9a631d942711f803e16ba800e57d73543e60f56d079e97b2513c7c0812be" +
	"987fce95eb7a32d9e92ec8db8ef4fbb9cf56c23b2ca8e3b29fe6144f1b9d8e9c" +
	"a97f920e01000000000000000000000000000000000000000000000000000000" +
	"0000000000000000020000004671f12c8475dc1d010000001f97f318e8030000" +
	"000000008a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801" +
	"b40f6f5c03000000000000000300000058217a987f7e848aa5152aa39d5dd41f" +
	"5dcde1d363cd528c89455955a053281d6ebc6c5d42d01dcc4063e412699f39e2" +
	"64e66e064ca1db05ab30034009da79a656f045e96b9eb2498acb952f126a9826" +
	"7088aecdf47c78b0b87ac19adc516cb940c1787e1d0423e51f00000001000000" +
	"e8030000000000000100000020bca3c7672aea6da60fded963d03e17764bfc8a" +
	"1271af1ee87ee411bd3b9a958600000000000000e679b6d600000000df3f6198" +
	"04a92fdb4057192dc43dd748ea778adc52bc498ce80524c014b811198a88e3dd" +
	"7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c"

// referenceZeroBlockHash is SHA-256 of the reference block.
const referenceZeroBlockHash = "7af41c7973c3e5d3da27739e9f167ec5f2076f1423adb48f792cf7c0f52be587"

func referenceZeroBlock(t *testing.T) []byte {
	t.Helper()
	b, err := hex.DecodeString(referenceZeroBlockHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testKey(seed byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
}

func pubKey(k ed25519.PrivateKey) (p [32]byte) {
	copy(p[:], k.Public().(ed25519.PublicKey))
	return p
}

// signBlock sets the signature key of b to k's and signs b.
func signBlock(b *Block, k ed25519.PrivateKey) {
	pk := pubKey(k)
	b.SignaturePublicKey = &pk
	copy(b.Signature[:], ed25519.Sign(k, b.signingBytes()))
}

// signBlockNoKey signs b with k and leaves the signature key out.
func signBlockNoKey(b *Block, k ed25519.PrivateKey) {
	b.SignaturePublicKey = nil
	copy(b.Signature[:], ed25519.Sign(k, b.signingBytes()))
}

func signBroadcast(b *Broadcast, k ed25519.PrivateKey) {
	copy(b.Signature[:], ed25519.Sign(k, b.signingBytes()))
}

func member(userID int64, flags int32, k ed25519.PrivateKey) Participant {
	return Participant{UserID: userID, PublicKey: pubKey(k), Flags: flags}
}

// testSharedKey is the shared key of tde2e's tests
// (BlockBuilder::make_shared_key, TestBlockchain.cpp:296-304): zero ek,
// "dummy" for the key, "??" for every header.
func testSharedKey(userIDs ...int64) *SharedKey {
	k := &SharedKey{EncryptedSharedKey: []byte("dummy")}
	for _, id := range userIDs {
		k.DestUserIDs = append(k.DestUserIDs, id)
		k.DestHeaders = append(k.DestHeaders, []byte("??"))
	}
	return k
}

func setGroup(external int32, ps ...Participant) Change {
	return Change{Kind: ChangeSetGroupState, GroupState: &GroupState{Participants: ps, ExternalPermissions: external}}
}

func setKey(k *SharedKey) Change {
	return Change{Kind: ChangeSetSharedKey, SharedKey: k}
}

// nextBlock starts a block on top of c with the empty key-value hash and no
// group state or shared key in the proof.
func nextBlock(c *Chain, changes ...Change) *Block {
	return &Block{PrevBlockHash: c.LastHash(), Height: c.Height() + 1, Changes: changes, StateProof: StateProof{KVHash: EmptyKVHash}}
}

// callBlock is the block a call client writes on top of c: the group state
// ps with the external permissions of a call, then a shared key for it,
// signed by k.
func callBlock(c *Chain, k ed25519.PrivateKey, ps ...Participant) []byte {
	ids := make([]int64, 0, len(ps))
	for _, p := range ps {
		ids = append(ids, p.UserID)
	}
	b := nextBlock(c, setGroup(callExternalPermissions, ps...), setKey(testSharedKey(ids...)))
	signBlock(b, k)
	return b.Marshal()
}

func mustApply(t *testing.T, c *Chain, data []byte) *Applied {
	t.Helper()
	a, err := c.Apply(data)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return a
}

// wantErr checks the kind and code of a refusal; code 0 means accepted.
func wantErr(t *testing.T, err error, kind Kind, code Code) {
	t.Helper()
	if code == 0 {
		if err != nil {
			t.Fatalf("refused: %v", err)
		}
		return
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v, want %v %v", err, kind, code)
	}
	if e.Kind != kind || e.Code != code {
		t.Fatalf("error %v (%v), want %v %v", err, e.Kind, kind, code)
	}
}

func sha(b []byte) [32]byte {
	return sha256.Sum256(b)
}

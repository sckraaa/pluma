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

// Package tde2etest builds the blocks and broadcasts a call client writes,
// for the tests of the server: the server checks them as it checks a
// client's. The shared key is tde2e's test one (BlockBuilder::make_shared_key,
// TestBlockchain.cpp:296-304): the server cannot open a key anyway, it only
// checks that every participant has a header.
package tde2etest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"

	"github.com/teamgram/teamgram-server/pkg/tde2e"
)

// Flags are the permissions every call client gives a participant
// (to_group_state, e2e_api.cpp:428-437).
const Flags = tde2e.PermAddUsers | tde2e.PermRemoveUsers

// Key is the Ed25519 key of a seed of 32 equal bytes: a throwaway key of a
// test.
func Key(seed byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
}

// Pub is the public key of k as the chain holds it.
func Pub(k ed25519.PrivateKey) [32]byte {
	return [32]byte(k.Public().(ed25519.PublicKey))
}

// Member is the entry of a user with key k.
func Member(userID int64, k ed25519.PrivateKey) tde2e.Participant {
	return tde2e.Participant{UserID: userID, PublicKey: Pub(k), Flags: Flags}
}

// CallBlock is the block a call client writes on top of c (nil for the
// empty chain): the group state of members with the external permissions
// of a call, then a shared key with a header for each, signed by signer.
func CallBlock(c *tde2e.Chain, signer ed25519.PrivateKey, members ...tde2e.Participant) []byte {
	if c == nil {
		c = tde2e.NewChain()
	}
	key := &tde2e.SharedKey{EncryptedSharedKey: []byte("dummy")}
	for _, p := range members {
		key.DestUserIDs = append(key.DestUserIDs, p.UserID)
		key.DestHeaders = append(key.DestHeaders, []byte("??"))
	}
	b := &tde2e.Block{
		PrevBlockHash: c.LastHash(),
		Height:        c.Height() + 1,
		Changes: []tde2e.Change{
			{Kind: tde2e.ChangeSetGroupState, GroupState: &tde2e.GroupState{Participants: members, ExternalPermissions: Flags}},
			{Kind: tde2e.ChangeSetSharedKey, SharedKey: key},
		},
		StateProof: tde2e.StateProof{KVHash: tde2e.EmptyKVHash},
	}
	b.Sign(signer)
	return b.Marshal()
}

// JoinBlock is the block userID writes to enter the call of c with k: the
// group state without his old entry and with him (Call.cpp:600-608).
func JoinBlock(c *tde2e.Chain, userID int64, k ed25519.PrivateKey) []byte {
	var members []tde2e.Participant
	if c != nil {
		for _, p := range c.GroupState().Participants {
			if p.UserID != userID {
				members = append(members, p)
			}
		}
	}
	return CallBlock(c, k, append(members, Member(userID, k))...)
}

// RemoveBlock is the block the holder of k writes to take ids out of the
// call of c.
func RemoveBlock(c *tde2e.Chain, k ed25519.PrivateKey, ids ...int64) []byte {
	var members []tde2e.Participant
	for _, p := range c.GroupState().Participants {
		gone := false
		for _, id := range ids {
			gone = gone || p.UserID == id
		}
		if !gone {
			members = append(members, p)
		}
	}
	return CallBlock(c, k, members...)
}

// Commit is the commit of userID at the last block of c to the hash of
// nonce (Call.cpp:538-540).
func Commit(c *tde2e.Chain, userID int64, k ed25519.PrivateKey, nonce [32]byte) []byte {
	return broadcast(c, tde2e.BroadcastCommit, userID, k, sha256.Sum256(nonce[:]))
}

// Reveal is the reveal of nonce by userID at the last block of c
// (Call.cpp:568-572).
func Reveal(c *tde2e.Chain, userID int64, k ed25519.PrivateKey, nonce [32]byte) []byte {
	return broadcast(c, tde2e.BroadcastReveal, userID, k, nonce)
}

func broadcast(c *tde2e.Chain, kind tde2e.BroadcastKind, userID int64, k ed25519.PrivateKey, value [32]byte) []byte {
	b := &tde2e.Broadcast{Kind: kind, UserID: userID, ChainHeight: c.Height(), ChainHash: c.LastHash(), Value: value}
	b.Sign(k)
	return b.Marshal()
}

// Follow applies to c blocks of sub-chain 0 in the server form, as a client
// applies what the server hands out.
func Follow(c *tde2e.Chain, blocks ...[]byte) error {
	for _, data := range blocks {
		local, err := tde2e.ToLocal(data)
		if err != nil {
			return err
		}
		if _, err := c.Apply(local); err != nil {
			return err
		}
	}
	return nil
}

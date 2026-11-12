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
	"fmt"
	"slices"
)

// The server policy over the chain (ADR 0060 §2). tde2e has none: the chain
// lets anyone add himself or remove others, because every client sets the
// external permissions to AddUsers|RemoveUsers. Who may write which block
// is decided here and by the caller (membership of the chat, admin rights,
// who left the media); a substituted key is caught by the emoji
// verification.

const (
	// MaxParticipants is the limit of the group state of a call,
	// conference_call_size_limit (ADR 0059 §9, ADR 0060 §2).
	MaxParticipants = 100
	// MaxBlockSize is the limit of a block in bytes. A block of 100
	// participants with their key headers takes about 15 KiB.
	MaxBlockSize = 256 << 10
)

// callExternalPermissions are the external permissions every call client
// writes (to_group_state, e2e_api.cpp:428-437; tweb carries them over,
// src/lib/calls/e2e/call.ts:473). A block that narrows them could lock
// everyone else out of the call.
const callExternalPermissions = PermAddUsers | PermRemoveUsers

// Join is a block written to enter a call: phone.joinGroupCall with
// public_key and block, or createConferenceCall with join.
type Join struct {
	UserID    int64
	PublicKey [32]byte
	Block     []byte
	// KeyUsed reports whether the key was in this call's chain before. A
	// client never reuses a key (Call.cpp:608-617). Nil checks only the
	// user's current entry; the current state of other users is checked by
	// the chain (unique public keys).
	KeyUsed func(publicKey [32]byte) bool
}

// Join checks and appends a join block. On the empty chain it is block 0
// with the caller alone; otherwise the new group state is the old one
// without the caller's previous entry and with the caller under the new
// key (Call::create_zero_block and create_self_add_block, Call.cpp:589-606).
// The block is signed by that key. On error the chain is unchanged.
func (c *Chain) Join(j Join) (*Applied, error) {
	b, err := c.parseNext(j.Block)
	if err != nil {
		return nil, err
	}
	g, err := callGroupState(b, j.PublicKey)
	if err != nil {
		return nil, err
	}
	if old, ok := c.st.group.Participant(j.UserID); ok && old.PublicKey == j.PublicKey {
		return nil, policy("the key was used before in this call")
	}
	if j.KeyUsed != nil && j.KeyUsed(j.PublicKey) {
		return nil, policy("the key was used before in this call")
	}
	if self, ok := g.Participant(j.UserID); !ok || self.PublicKey != j.PublicKey {
		return nil, policy("the joining user with his key is not in the group state")
	}
	if !sameMembers(without(g.Participants, j.UserID), without(c.st.group.Participants, j.UserID)) {
		return nil, policy("a join block may change only the joining user")
	}
	if len(g.Participants) > MaxParticipants {
		return nil, &Error{Kind: KindTooMany, Code: CodePolicy, Msg: fmt.Sprintf("%d participants, limit %d", len(g.Participants), MaxParticipants)}
	}
	return c.ApplyBlock(b)
}

// Remove checks and appends a block of phone.deleteConferenceCallParticipants
// written by callerID: the group state without exactly ids, signed by the
// caller's key in the chain. Whether ids may be removed (only_left: they
// left the media; kick: the caller administers the call) is the caller's
// check. Every remaining client tries to remove a user who left (ADR 0060
// §2): the first block is accepted and the others are stale. On error the chain is unchanged.
func (c *Chain) Remove(callerID int64, ids []int64, block []byte) (*Applied, error) {
	b, err := c.parseNext(block)
	if err != nil {
		return nil, err
	}
	caller, ok := c.st.group.Participant(callerID)
	if !ok {
		return nil, &Error{Kind: KindForbidden, Code: CodePolicy, Msg: "the writer is not in the group state"}
	}
	if len(ids) == 0 {
		return nil, policy("no users to remove")
	}
	for _, id := range ids {
		if id == callerID {
			return nil, policy("a user cannot remove himself")
		}
		if _, ok := c.st.group.Participant(id); !ok {
			return nil, policy("a user to remove is not in the group state")
		}
	}
	g, err := callGroupState(b, caller.PublicKey)
	if err != nil {
		return nil, err
	}
	if !sameMembers(g.Participants, without(c.st.group.Participants, ids...)) {
		return nil, policy("a removal block must remove exactly the given users")
	}
	return c.ApplyBlock(b)
}

// BroadcastResult is an accepted broadcast.
type BroadcastResult struct {
	Broadcast *Broadcast
	// Bytes is the broadcast in the local form, to store and, with
	// ToServer, hand out.
	Bytes []byte
	// Stale is set for a broadcast for an earlier height: it was not checked
	// and changes nothing (see Verification.Apply).
	Stale bool
}

// Accept checks a broadcast of phone.sendConferenceCallBroadcast sent by
// senderID: it must be the sender's own (one commit and one reveal per user
// and height follow from Apply). On error nothing changes.
func (v *Verification) Accept(senderID int64, data []byte) (*BroadcastResult, error) {
	b, err := ParseBroadcast(data)
	if err != nil {
		return nil, err
	}
	if b.UserID != senderID {
		return nil, policy("a broadcast of another user")
	}
	stale, err := v.Apply(b)
	if err != nil {
		return nil, err
	}
	return &BroadcastResult{Broadcast: b, Bytes: b.Marshal(), Stale: stale}, nil
}

// parseNext reads a block written over the RPC and checks that it goes on
// top of the chain, so that a stale block is answered as stale before
// anything else.
func (c *Chain) parseNext(data []byte) (*Block, error) {
	if len(data) > MaxBlockSize {
		return nil, policy(fmt.Sprintf("block of %d bytes, limit %d", len(data), MaxBlockSize))
	}
	b, err := ParseBlock(data)
	if err != nil {
		return nil, err
	}
	if err := c.checkPosition(b); err != nil {
		return nil, err
	}
	return b, nil
}

// callGroupState checks the shape every call client gives a block: a new
// group state, then a new shared key for it (Call::make_changes_for_new_state,
// Call.cpp:631-656; tweb src/lib/calls/e2e/blockchain.ts:718-719), with the
// external permissions of a call and the signature key set to signer. It
// returns the new group state.
func callGroupState(b *Block, signer [32]byte) (*GroupState, error) {
	if len(b.Changes) != 2 || b.Changes[0].Kind != ChangeSetGroupState || b.Changes[1].Kind != ChangeSetSharedKey {
		return nil, policy("the changes must be a group state and a shared key")
	}
	if b.SignaturePublicKey == nil || *b.SignaturePublicKey != signer {
		return nil, policy("the block must be signed by the writer's key")
	}
	g := b.Changes[0].GroupState
	if g.ExternalPermissions != callExternalPermissions {
		return nil, policy("the external permissions of a call must be AddUsers|RemoveUsers")
	}
	if b.Changes[1].SharedKey.Empty() {
		return nil, policy("the shared key is empty")
	}
	return g, nil
}

// without returns the participants other than the given users.
func without(ps []Participant, userIDs ...int64) []Participant {
	out := make([]Participant, 0, len(ps))
	for _, p := range ps {
		if !slices.Contains(userIDs, p.UserID) {
			out = append(out, p)
		}
	}
	return out
}

// sameMembers reports whether a and b hold the same users with the same
// keys and flags, in any order. Version is not compared: tde2e does not
// check it (State::set_group_state compares flags only), and a removal block
// rebuilds every entry with version 0 (to_group_state, e2e_api.cpp:428-437).
func sameMembers(a, b []Participant) bool {
	if len(a) != len(b) {
		return false
	}
	byUser := make(map[int64]Participant, len(b))
	for _, p := range b {
		byUser[p.UserID] = p
	}
	if len(byUser) != len(b) {
		return false
	}
	for _, p := range a {
		q, ok := byUser[p.UserID]
		if !ok || q.PublicKey != p.PublicKey || q.Flags != p.Flags {
			return false
		}
		delete(byUser, p.UserID)
	}
	return true
}

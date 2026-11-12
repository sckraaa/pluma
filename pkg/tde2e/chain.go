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
	"crypto/sha256"
	"fmt"
	"math"
)

// EmptyKVHash is the hash of the empty key-value trie: SHA-256 of the node
// type Empty (0) stored as an int32 (TrieNode::compute_hash, Trie.cpp:23-25,
// 72-101). Every call block carries it in its state proof.
var EmptyKVHash = sha256.Sum256([]byte{0, 0, 0, 0})

// Chain is sub-chain 0 of a call as the reference server keeps it
// (ServerBlockchain, TestBlockchain.cpp:161-178): every block is checked
// with the signature and the state hash validated and all permissions
// allowed. A Chain is not safe for concurrent use.
type Chain struct {
	height   int32
	lastHash [32]byte
	st       state
}

// Applied is an accepted block.
type Applied struct {
	Block *Block
	// Bytes is the block in the local form as the server stores it and,
	// with ToServer, hands it out: the serialization tde2e rebuilds from the
	// parsed block (TestBlockchain.cpp:204). For a block a client built with
	// tde2e it is the bytes the client sent.
	Bytes []byte
	// Hash is SHA-256 of Bytes, the prev_block_hash of the next block.
	Hash [32]byte
}

// NewChain returns the chain of a call without blocks: height -1, the hash
// of the missing block zero (Blockchain::create_empty, Blockchain.h:283-285).
func NewChain() *Chain {
	return &Chain{height: -1, st: state{group: &GroupState{}, sharedKey: &SharedKey{}, kvHash: EmptyKVHash}}
}

// RestoreChain rebuilds the chain from its last block in the local form, the
// way tde2e starts a chain from the last block (Blockchain::create_from_block
// and State::create_from_block, Blockchain.cpp:552-605, 708-718). The block
// is trusted: it is one this server accepted.
func RestoreChain(lastBlock []byte) (*Chain, error) {
	b, err := ParseBlock(lastBlock)
	if err != nil {
		return nil, err
	}
	if b.Height < 0 {
		return nil, fail(CodeInvalidBlock, "negative height")
	}
	st := state{kvHash: b.StateProof.KVHash}
	if b.Height == 0 {
		st.group = &GroupState{ExternalPermissions: PermAll}
	}
	for i := range b.Changes {
		switch c := &b.Changes[i]; c.Kind {
		case ChangeSetValue:
			st.hasSetValue = true
		case ChangeSetGroupState:
			st.group = c.GroupState.clone()
			st.sharedKey = &SharedKey{}
			st.hasGroupStateChange = true
		case ChangeSetSharedKey:
			st.sharedKey = c.SharedKey.clone()
			st.hasSharedKeyChange = true
		}
	}
	if b.StateProof.GroupState != nil {
		st.group = b.StateProof.GroupState.clone()
	}
	if b.StateProof.SharedKey != nil {
		st.sharedKey = b.StateProof.SharedKey.clone()
	}
	if st.group == nil {
		return nil, fail(CodeStateProofGroup, "no group state proof")
	}
	if st.sharedKey == nil {
		return nil, fail(CodeStateProofSecret, "no shared key")
	}
	if err := st.validate(&b.StateProof); err != nil {
		return nil, err
	}
	return &Chain{height: b.Height, lastHash: b.Hash(), st: st}, nil
}

// Height is the height of the last block, -1 without blocks.
func (c *Chain) Height() int32 {
	return c.height
}

// LastHash is the hash of the last block, zeros without blocks.
func (c *Chain) LastHash() [32]byte {
	return c.lastHash
}

// GroupState returns a copy of the current group state.
func (c *Chain) GroupState() *GroupState {
	return c.st.group.clone()
}

// SharedKey returns a copy of the current shared key.
func (c *Chain) SharedKey() *SharedKey {
	return c.st.sharedKey.clone()
}

// Apply checks a block in the local form and appends it
// (ServerBlockchain::try_apply_block). On error the chain is unchanged.
func (c *Chain) Apply(data []byte) (*Applied, error) {
	b, err := ParseBlock(data)
	if err != nil {
		return nil, err
	}
	return c.ApplyBlock(b)
}

// ApplyBlock checks a parsed block and appends it (Blockchain::
// try_apply_block, Blockchain.cpp:660-686). On error the chain is unchanged.
func (c *Chain) ApplyBlock(b *Block) (*Applied, error) {
	if err := c.checkPosition(b); err != nil {
		return nil, err
	}
	st := c.st
	if err := st.apply(b); err != nil {
		return nil, err
	}
	raw := b.Marshal()
	hash := sha256.Sum256(raw)
	c.st, c.height, c.lastHash = st, b.Height, hash
	return &Applied{Block: b, Bytes: raw, Hash: hash}, nil
}

// checkPosition checks that the block goes right after the last one
// (Blockchain.cpp:666-673). A block that does not is stale: the client reads
// the last block again (CONF_WRITE_CHAIN_INVALID, ADR 0060 §2).
func (c *Chain) checkPosition(b *Block) error {
	if c.height == math.MaxInt32 || b.Height != c.height+1 {
		return &Error{Kind: KindStale, Code: CodeHeightMismatch, Msg: fmt.Sprintf("block height %d, chain height %d", b.Height, c.height)}
	}
	if b.PrevBlockHash != c.lastHash {
		return &Error{Kind: KindStale, Code: CodeHashMismatch}
	}
	return nil
}

// state is State of Blockchain.h:220-253. group and sharedKey are never
// changed in place, only replaced, so copying a state copies it.
type state struct {
	group     *GroupState
	sharedKey *SharedKey
	kvHash    [32]byte

	hasSetValue         bool
	hasGroupStateChange bool
	hasSharedKeyChange  bool
}

// permissions are the permissions of the holder of a key
// (GroupState::get_permissions, Blockchain.cpp:71-78), with the limit of
// ServerBlockchain: all permissions (TestBlockchain.cpp:164).
func (g *GroupState) permissions(publicKey [32]byte) int32 {
	for _, p := range g.Participants {
		if p.PublicKey == publicKey {
			return p.Flags&PermAll | permIsParticipant
		}
	}
	return g.ExternalPermissions & PermAll
}

// mayChangeSharedKey is Permissions::may_change_shared_key
// (Blockchain.h:89-91).
func mayChangeSharedKey(perm int32) bool {
	return perm&permIsParticipant != 0 && perm&(PermAddUsers|PermRemoveUsers) != 0
}

// validateGroupState is State::validate_group_state (Blockchain.cpp:334-354).
func validateGroupState(g *GroupState) error {
	userIDs := make(map[int64]struct{}, len(g.Participants))
	keys := make(map[[32]byte]struct{}, len(g.Participants))
	for _, p := range g.Participants {
		userIDs[p.UserID] = struct{}{}
		keys[p.PublicKey] = struct{}{}
		if p.Flags&^PermAll != 0 {
			return fail(CodeInvalidGroupState, "invalid permissions")
		}
	}
	if g.ExternalPermissions&^PermAll != 0 {
		return fail(CodeInvalidGroupState, "invalid external permissions")
	}
	if len(userIDs) != len(g.Participants) {
		return fail(CodeInvalidGroupState, "duplicate user_id")
	}
	if len(keys) != len(g.Participants) {
		return fail(CodeInvalidGroupState, "duplicate public_key")
	}
	return nil
}

// participantKey identifies a participant for the permission rules: the
// same user with a new key is a removal and an addition.
type participantKey struct {
	userID    int64
	publicKey [32]byte
}

// setGroupState is State::set_group_state (Blockchain.cpp:356-401).
func (s *state) setGroupState(g *GroupState, perm int32) error {
	if err := validateGroupState(g); err != nil {
		return err
	}
	oldFlags := make(map[participantKey]int32, len(s.group.Participants))
	for _, p := range s.group.Participants {
		oldFlags[participantKey{p.UserID, p.PublicKey}] = p.Flags
	}
	newFlags := make(map[participantKey]int32, len(g.Participants))
	for _, p := range g.Participants {
		newFlags[participantKey{p.UserID, p.PublicKey}] = p.Flags
	}
	if ^s.group.ExternalPermissions&g.ExternalPermissions != 0 {
		return fail(CodeNoPermissions, "can't increase external permissions")
	}
	for k := range oldFlags {
		if _, ok := newFlags[k]; !ok && perm&PermRemoveUsers == 0 {
			return fail(CodeNoPermissions, "can't remove users")
		}
	}
	var needed int32
	for k, flags := range newFlags {
		old, ok := oldFlags[k]
		switch {
		case !ok:
			if perm&PermAddUsers == 0 {
				return fail(CodeNoPermissions, "can't add users")
			}
			needed |= flags
		case flags != old:
			if perm&PermAddUsers == 0 || perm&PermRemoveUsers == 0 {
				return fail(CodeNoPermissions, "can't change permissions of users")
			}
			needed |= flags &^ old
		}
	}
	if needed&^(perm&PermAll) != 0 {
		return fail(CodeNoPermissions, "can't give more permissions than the signer has")
	}
	s.group = g
	return nil
}

// clearSharedKey is State::clear_shared_key (Blockchain.cpp:403-409).
func (s *state) clearSharedKey(perm int32) error {
	if !mayChangeSharedKey(perm) {
		return fail(CodeNoPermissions, "can't clear shared key")
	}
	s.sharedKey = &SharedKey{}
	return nil
}

// validateSharedKey is State::validate_shared_key (Blockchain.cpp:411-435):
// a non-empty key has exactly one header for every participant.
func validateSharedKey(k *SharedKey, g *GroupState) error {
	if k.Empty() {
		return nil
	}
	if len(k.DestUserIDs) != len(k.DestHeaders) {
		return fail(CodeInvalidSharedSecret, "different number of users and headers")
	}
	if len(k.DestUserIDs) != len(g.Participants) {
		return fail(CodeInvalidSharedSecret, "wrong number of users")
	}
	dest := make(map[int64]struct{}, len(k.DestUserIDs))
	for _, id := range k.DestUserIDs {
		dest[id] = struct{}{}
	}
	if len(dest) != len(k.DestUserIDs) {
		return fail(CodeInvalidSharedSecret, "duplicate users")
	}
	for _, p := range g.Participants {
		if _, ok := dest[p.UserID]; !ok {
			return fail(CodeInvalidSharedSecret, "unknown user_id")
		}
	}
	return nil
}

// setSharedKey is State::set_shared_key (Blockchain.cpp:437-447): a key is
// set only after a group state change cleared the previous one.
func (s *state) setSharedKey(k *SharedKey, perm int32) error {
	if !s.sharedKey.Empty() {
		return fail(CodeUnknown, "shared key is already set")
	}
	if !mayChangeSharedKey(perm) {
		return fail(CodeNoPermissions, "can't set shared key")
	}
	if err := validateSharedKey(k, s.group); err != nil {
		return err
	}
	s.sharedKey = k
	return nil
}

// validate is State::validate_state (Blockchain.cpp:449-484): the proof
// holds the group state and the shared key exactly when the block leaves
// them as they were.
func (s *state) validate(p *StateProof) error {
	if p.KVHash != s.kvHash {
		return fail(CodeUnknown, "state hash mismatch")
	}
	if !s.hasGroupStateChange && !s.hasSetValue {
		return fail(CodeNoChanges, "there must be a SetValue or SetGroupState change")
	}
	switch {
	case s.hasGroupStateChange && p.GroupState != nil:
		return fail(CodeStateProofGroup, "group state must be omitted when it changes")
	case !s.hasGroupStateChange && p.GroupState == nil:
		return fail(CodeStateProofGroup, "group state must be provided when it does not change")
	case !s.hasGroupStateChange && !p.GroupState.equal(s.group):
		return fail(CodeStateProofGroup, "group state differs")
	}
	omitKey := s.hasGroupStateChange || s.hasSharedKeyChange
	switch {
	case omitKey && p.SharedKey != nil:
		return fail(CodeStateProofSecret, "shared key must be omitted")
	case !omitKey && p.SharedKey == nil:
		return fail(CodeStateProofSecret, "shared key must be provided")
	case !omitKey && !p.SharedKey.equal(s.sharedKey):
		return fail(CodeStateProofSecret, "shared key differs")
	}
	if err := validateGroupState(s.group); err != nil {
		return err
	}
	return validateSharedKey(s.sharedKey, s.group)
}

// applyChange is State::apply_change (Blockchain.cpp:486-511). The
// permissions are those of the signer in the group state at the moment of
// each step: after a group state change the shared key is cleared with the
// signer's place in the new state.
func (s *state) applyChange(c *Change, signer [32]byte) error {
	switch c.Kind {
	case ChangeNoop:
		return nil
	case ChangeSetValue:
		s.hasSetValue = true
		if s.group.permissions(signer)&PermSetValue == 0 {
			return fail(CodeNoPermissions, "can't set value")
		}
		// The key-value trie is not ported: calls never write values.
		return fail(CodeInvalidBlock, "key-value changes are not served")
	case ChangeSetGroupState:
		s.hasGroupStateChange = true
		if err := s.setGroupState(c.GroupState.clone(), s.group.permissions(signer)); err != nil {
			return err
		}
		return s.clearSharedKey(s.group.permissions(signer))
	case ChangeSetSharedKey:
		s.hasSharedKeyChange = true
		return s.setSharedKey(c.SharedKey.clone(), s.group.permissions(signer))
	}
	return fail(CodeInvalidBlock, "unknown change")
}

// apply is State::apply (Blockchain.cpp:513-550) with validate_signature and
// validate_state_hash both set, as ServerBlockchain has them.
func (s *state) apply(b *Block) error {
	if b.Height == 0 {
		// Block 0 is applied on top of an implicit block -1 whose group
		// state lets anyone do anything.
		s.group = &GroupState{ExternalPermissions: PermAll}
	}
	var signer [32]byte
	switch {
	case b.SignaturePublicKey != nil:
		signer = *b.SignaturePublicKey
	case len(s.group.Participants) > 0:
		signer = s.group.Participants[0].PublicKey
	default:
		return fail(CodeUnknown, "unknown public key")
	}
	if err := b.VerifySignature(signer); err != nil {
		return err
	}
	s.hasSetValue, s.hasGroupStateChange, s.hasSharedKeyChange = false, false, false
	for i := range b.Changes {
		if err := s.applyChange(&b.Changes[i], signer); err != nil {
			return err
		}
	}
	return s.validate(&b.StateProof)
}

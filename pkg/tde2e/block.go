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
	"fmt"
	"slices"
)

// Participant flags (Blockchain.h:34-40). The schema names bit 0 add_users
// and bit 1 remove_users (e2e_api.tl:70).
const (
	PermAddUsers    int32 = 1 << 0
	PermRemoveUsers int32 = 1 << 1
	PermSetValue    int32 = 1 << 2
	PermAll         int32 = 1<<3 - 1

	permIsParticipant int32 = 1 << 30
)

// Participant is e2e.chain.groupParticipant. Flags is kept as sent: the
// add_users and remove_users fields are its bits and take no bytes of their
// own, and tde2e serializes the field back verbatim (Blockchain.cpp:32-40).
type Participant struct {
	UserID    int64
	PublicKey [32]byte
	Flags     int32
	Version   int32
}

// GroupState is e2e.chain.groupState: the participants of the call and the
// permissions of everyone else.
type GroupState struct {
	Participants        []Participant
	ExternalPermissions int32
}

// Participant returns the entry of a user.
func (g *GroupState) Participant(userID int64) (Participant, bool) {
	for _, p := range g.Participants {
		if p.UserID == userID {
			return p, true
		}
	}
	return Participant{}, false
}

func (g *GroupState) clone() *GroupState {
	return &GroupState{Participants: slices.Clone(g.Participants), ExternalPermissions: g.ExternalPermissions}
}

// equal is GroupState::operator== (Blockchain.h:107-109): the same
// participants in the same order.
func (g *GroupState) equal(o *GroupState) bool {
	return g.ExternalPermissions == o.ExternalPermissions && slices.Equal(g.Participants, o.Participants)
}

// SharedKey is e2e.chain.sharedKey: the call key encrypted once, and a
// header per participant that opens it with that participant's key. The
// server cannot open it.
type SharedKey struct {
	EK                 [32]byte
	EncryptedSharedKey []byte
	DestUserIDs        []int64
	DestHeaders        [][]byte
}

// Empty reports whether k is the empty shared key a group state change
// leaves behind (Blockchain.h:125-128, Keys.cpp:52-58).
func (k *SharedKey) Empty() bool {
	return k.EK == [32]byte{} && len(k.EncryptedSharedKey) == 0 && len(k.DestUserIDs) == 0 && len(k.DestHeaders) == 0
}

func (k *SharedKey) clone() *SharedKey {
	c := &SharedKey{EK: k.EK, EncryptedSharedKey: bytes.Clone(k.EncryptedSharedKey), DestUserIDs: slices.Clone(k.DestUserIDs)}
	if k.DestHeaders != nil {
		c.DestHeaders = make([][]byte, len(k.DestHeaders))
		for i, h := range k.DestHeaders {
			c.DestHeaders[i] = bytes.Clone(h)
		}
	}
	return c
}

// equal is GroupSharedKey::operator== (Blockchain.h:129-132).
func (k *SharedKey) equal(o *SharedKey) bool {
	return k.EK == o.EK && bytes.Equal(k.EncryptedSharedKey, o.EncryptedSharedKey) &&
		slices.Equal(k.DestUserIDs, o.DestUserIDs) && slices.EqualFunc(k.DestHeaders, o.DestHeaders, bytes.Equal)
}

// ChangeKind is the constructor of an e2e.chain.Change (e2e_api.tl:74-77).
type ChangeKind uint8

const (
	ChangeNoop ChangeKind = iota + 1
	ChangeSetValue
	ChangeSetGroupState
	ChangeSetSharedKey
)

// Change is one e2e.chain.Change; the fields of its kind are set.
type Change struct {
	Kind ChangeKind
	// Nonce of ChangeNoop.
	Nonce [32]byte
	// Key and Value of ChangeSetValue.
	Key, Value []byte
	// GroupState of ChangeSetGroupState.
	GroupState *GroupState
	// SharedKey of ChangeSetSharedKey.
	SharedKey *SharedKey
}

// StateProof is e2e.chain.stateProof: the state after the block, with the
// group state and the shared key only when the block does not change them.
type StateProof struct {
	KVHash     [32]byte
	GroupState *GroupState
	SharedKey  *SharedKey
}

// Block is e2e.chain.block (e2e_api.tl:81), a block of sub-chain 0.
type Block struct {
	Signature     [64]byte
	PrevBlockHash [32]byte
	Changes       []Change
	Height        int32
	StateProof    StateProof
	// SignaturePublicKey is the key that signed the block; without it the
	// key of the first participant is used.
	SignaturePublicKey *[32]byte
}

// ParseBlock reads a block in the local form, the one clients send
// (Block::from_tl_serialized, Blockchain.cpp:196-207). Flag bits the schema
// does not define are read and dropped, as tde2e drops them; a negative
// flags field is refused, as tde2e's generated parser refuses it.
func ParseBlock(data []byte) (*Block, error) {
	r := reader{buf: data}
	b := readBlock(&r)
	if err := r.end(); err != nil {
		return nil, fail(CodeInvalidBlock, "block: "+err.Error())
	}
	return b, nil
}

// Marshal returns the boxed serialization of the block, the form tde2e
// rebuilds from a parsed block (Block::to_tl, Blockchain.cpp:209-221) and the
// reference server stores and hands out (TestBlockchain.cpp:204). It panics
// on a Change whose kind and fields disagree.
func (b *Block) Marshal() []byte {
	return b.marshal(false)
}

// Hash is the hash the next block names in prev_block_hash: SHA-256 of
// Marshal (Block::calc_hash, Blockchain.cpp:170-178).
func (b *Block) Hash() [32]byte {
	return sha256.Sum256(b.Marshal())
}

// VerifySignature checks the Ed25519 signature: it is made over Marshal with
// the signature field set to zeros (verify_signature, utils.h:116-127).
func (b *Block) VerifySignature(publicKey [32]byte) error {
	if !ed25519.Verify(publicKey[:], b.signingBytes(), b.Signature[:]) {
		return fail(CodeInvalidSignature, "")
	}
	return nil
}

// signingBytes is what the signature of the block covers.
func (b *Block) signingBytes() []byte {
	return b.marshal(true)
}

func (b *Block) marshal(zeroSignature bool) []byte {
	var w writer
	w.uint32(idBlock)
	if zeroSignature {
		w.raw(make([]byte, len(b.Signature)))
	} else {
		w.raw(b.Signature[:])
	}
	var flags uint32
	if b.SignaturePublicKey != nil {
		flags |= 1
	}
	w.uint32(flags)
	w.raw(b.PrevBlockHash[:])
	w.uint32(uint32(len(b.Changes)))
	for i := range b.Changes {
		writeChange(&w, &b.Changes[i])
	}
	w.int32(b.Height)
	writeStateProof(&w, &b.StateProof)
	if b.SignaturePublicKey != nil {
		w.raw(b.SignaturePublicKey[:])
	}
	return w.buf
}

func readBlock(r *reader) *Block {
	b := &Block{}
	if !r.expect(idBlock) {
		return b
	}
	b.Signature = r.int512()
	flags := r.flags()
	b.PrevBlockHash = r.int256()
	n := r.count(4)
	for i := 0; i < n && r.err == nil; i++ {
		b.Changes = append(b.Changes, readChange(r))
	}
	b.Height = r.int32()
	b.StateProof = readStateProof(r)
	if flags&1 != 0 {
		pk := r.int256()
		b.SignaturePublicKey = &pk
	}
	return b
}

func readChange(r *reader) Change {
	id := r.uint32()
	if r.err != nil {
		return Change{}
	}
	switch id {
	case idChangeNoop:
		return Change{Kind: ChangeNoop, Nonce: r.int256()}
	case idChangeSetValue:
		key := r.bytes()
		value := r.bytes()
		return Change{Kind: ChangeSetValue, Key: key, Value: value}
	case idChangeSetGroupState:
		return Change{Kind: ChangeSetGroupState, GroupState: readGroupState(r)}
	case idChangeSetSharedKey:
		return Change{Kind: ChangeSetSharedKey, SharedKey: readSharedKey(r)}
	}
	r.fail(fmt.Errorf("unknown change constructor %08x", id))
	return Change{}
}

func writeChange(w *writer, c *Change) {
	switch c.Kind {
	case ChangeNoop:
		w.uint32(idChangeNoop)
		w.raw(c.Nonce[:])
	case ChangeSetValue:
		w.uint32(idChangeSetValue)
		w.bytes(c.Key)
		w.bytes(c.Value)
	case ChangeSetGroupState:
		w.uint32(idChangeSetGroupState)
		writeGroupState(w, c.GroupState)
	case ChangeSetSharedKey:
		w.uint32(idChangeSetSharedKey)
		writeSharedKey(w, c.SharedKey)
	default:
		panic(fmt.Sprintf("tde2e: change of kind %d", c.Kind))
	}
}

// participantSize is the size of a boxed groupParticipant: id, user_id,
// public_key, flags, version.
const participantSize = 4 + 8 + 32 + 4 + 4

func readGroupState(r *reader) *GroupState {
	g := &GroupState{}
	if !r.expect(idGroupState) {
		return g
	}
	n := r.count(participantSize)
	if n > 0 {
		g.Participants = make([]Participant, 0, n)
	}
	for i := 0; i < n && r.expect(idGroupParticipant); i++ {
		var p Participant
		p.UserID = r.int64()
		p.PublicKey = r.int256()
		p.Flags = r.flags()
		p.Version = r.int32()
		g.Participants = append(g.Participants, p)
	}
	g.ExternalPermissions = r.int32()
	return g
}

func writeGroupState(w *writer, g *GroupState) {
	w.uint32(idGroupState)
	w.uint32(uint32(len(g.Participants)))
	for _, p := range g.Participants {
		w.uint32(idGroupParticipant)
		w.int64(p.UserID)
		w.raw(p.PublicKey[:])
		w.int32(p.Flags)
		w.int32(p.Version)
	}
	w.int32(g.ExternalPermissions)
}

func readSharedKey(r *reader) *SharedKey {
	k := &SharedKey{}
	if !r.expect(idSharedKey) {
		return k
	}
	k.EK = r.int256()
	k.EncryptedSharedKey = r.bytes()
	n := r.count(8)
	for i := 0; i < n && r.err == nil; i++ {
		k.DestUserIDs = append(k.DestUserIDs, r.int64())
	}
	n = r.count(4)
	for i := 0; i < n && r.err == nil; i++ {
		k.DestHeaders = append(k.DestHeaders, r.bytes())
	}
	return k
}

func writeSharedKey(w *writer, k *SharedKey) {
	w.uint32(idSharedKey)
	w.raw(k.EK[:])
	w.bytes(k.EncryptedSharedKey)
	w.uint32(uint32(len(k.DestUserIDs)))
	for _, id := range k.DestUserIDs {
		w.int64(id)
	}
	w.uint32(uint32(len(k.DestHeaders)))
	for _, h := range k.DestHeaders {
		w.bytes(h)
	}
}

// State proof flags (e2e_api.tl:79).
const (
	proofGroupState = 1 << 0
	proofSharedKey  = 1 << 1
)

func readStateProof(r *reader) StateProof {
	var p StateProof
	if !r.expect(idStateProof) {
		return p
	}
	flags := r.flags()
	p.KVHash = r.int256()
	if flags&proofGroupState != 0 {
		p.GroupState = readGroupState(r)
	}
	if flags&proofSharedKey != 0 {
		p.SharedKey = readSharedKey(r)
	}
	return p
}

func writeStateProof(w *writer, p *StateProof) {
	w.uint32(idStateProof)
	var flags uint32
	if p.GroupState != nil {
		flags |= proofGroupState
	}
	if p.SharedKey != nil {
		flags |= proofSharedKey
	}
	w.uint32(flags)
	w.raw(p.KVHash[:])
	if p.GroupState != nil {
		writeGroupState(w, p.GroupState)
	}
	if p.SharedKey != nil {
		writeSharedKey(w, p.SharedKey)
	}
}

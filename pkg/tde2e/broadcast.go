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
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
)

// BroadcastKind is the constructor of an e2e.chain.GroupBroadcast
// (e2e_api.tl:67-68).
type BroadcastKind uint8

const (
	BroadcastCommit BroadcastKind = iota + 1
	BroadcastReveal
)

// Broadcast is a message of sub-chain 1, the emoji verification: every
// participant commits to a random nonce, then reveals it once all have
// committed (Call.cpp:181-257).
type Broadcast struct {
	Kind        BroadcastKind
	Signature   [64]byte
	UserID      int64
	ChainHeight int32
	ChainHash   [32]byte
	// Value is nonce_hash of a commit and nonce of a reveal.
	Value [32]byte
}

// ParseBroadcast reads a broadcast in the local form
// (e2e_chain_GroupBroadcast::fetch and fetch_end, Call.cpp:102-107).
func ParseBroadcast(data []byte) (*Broadcast, error) {
	r := reader{buf: data}
	b := &Broadcast{}
	switch id := r.uint32(); id {
	case idBroadcastCommit:
		b.Kind = BroadcastCommit
	case idBroadcastReveal:
		b.Kind = BroadcastReveal
	default:
		// A short read keeps its own error: fail records only the first.
		r.fail(fmt.Errorf("unknown broadcast constructor %08x", id))
	}
	b.Signature = r.int512()
	b.UserID = r.int64()
	b.ChainHeight = r.int32()
	b.ChainHash = r.int256()
	b.Value = r.int256()
	if err := r.end(); err != nil {
		return nil, fail(CodeInvalidBlock, "broadcast: "+err.Error())
	}
	return b, nil
}

// Marshal returns the boxed serialization of the broadcast. A broadcast has
// no flags, so it is the bytes ParseBroadcast read.
func (b *Broadcast) Marshal() []byte {
	return b.marshal(false)
}

// VerifySignature checks the Ed25519 signature, made over Marshal with the
// signature set to zeros (utils.h:116-127).
func (b *Broadcast) VerifySignature(publicKey [32]byte) error {
	if !ed25519.Verify(publicKey[:], b.signingBytes(), b.Signature[:]) {
		return fail(CodeInvalidSignature, "")
	}
	return nil
}

// Sign signs the broadcast with key, as a client does (Call.cpp:539, 571).
func (b *Broadcast) Sign(key ed25519.PrivateKey) {
	copy(b.Signature[:], ed25519.Sign(key, b.signingBytes()))
}

func (b *Broadcast) signingBytes() []byte {
	return b.marshal(true)
}

func (b *Broadcast) marshal(zeroSignature bool) []byte {
	var w writer
	switch b.Kind {
	case BroadcastCommit:
		w.uint32(idBroadcastCommit)
	case BroadcastReveal:
		w.uint32(idBroadcastReveal)
	default:
		panic(fmt.Sprintf("tde2e: broadcast of kind %d", b.Kind))
	}
	if zeroSignature {
		w.raw(make([]byte, len(b.Signature)))
	} else {
		w.raw(b.Signature[:])
	}
	w.int64(b.UserID)
	w.int32(b.ChainHeight)
	w.raw(b.ChainHash[:])
	w.raw(b.Value[:])
	return w.buf
}

// Phase is the step of the emoji verification at the current height
// (CallVerificationChain::State, Call.h:31-35).
type Phase uint8

const (
	PhaseEnd Phase = iota
	PhaseCommit
	PhaseReveal
)

// Verification is sub-chain 1 as the reference server checks it
// (CallVerificationChain of ServerBlockchain, TestBlockchain.cpp:180-189):
// signatures are checked and broadcasts for a later height are refused. A
// Verification is not safe for concurrent use.
type Verification struct {
	height    int32
	hash      [32]byte
	phase     Phase
	keys      map[int64][32]byte
	committed map[int64][32]byte
	revealed  map[int64][32]byte
}

// NewVerification returns the verification of a chain without blocks.
func NewVerification() *Verification {
	return &Verification{height: -1}
}

// Reset starts the verification of the last block of c: every participant
// of its group state commits and then reveals
// (CallVerificationChain::on_new_main_block, Call.cpp:65-100). Call it after
// every accepted block of sub-chain 0, and after a restart replay the
// broadcasts of the current height through Apply.
func (v *Verification) Reset(c *Chain) {
	if c.height < 0 {
		*v = Verification{height: -1}
		return
	}
	v.height, v.hash, v.phase = c.height, c.lastHash, PhaseCommit
	v.keys = make(map[int64][32]byte, len(c.st.group.Participants))
	for _, p := range c.st.group.Participants {
		v.keys[p.UserID] = p.PublicKey
	}
	v.committed = make(map[int64][32]byte, len(v.keys))
	v.revealed = make(map[int64][32]byte, len(v.keys))
}

// Height is the height of the block being verified.
func (v *Verification) Height() int32 {
	return v.height
}

// Phase is the current step.
func (v *Verification) Phase() Phase {
	return v.phase
}

// Committed reports whether the user committed at the current height.
func (v *Verification) Committed(userID int64) bool {
	_, ok := v.committed[userID]
	return ok
}

// Revealed reports whether the user revealed at the current height.
func (v *Verification) Revealed(userID int64) bool {
	_, ok := v.revealed[userID]
	return ok
}

// Apply checks a broadcast and records it (CallVerificationChain::
// try_apply_block, Call.cpp:102-130). A broadcast for an earlier height is
// stale: tde2e accepts it unchecked and the reference server keeps it, while
// clients skip it (Call.cpp:112-116); Apply reports it and changes nothing.
// On error nothing changes.
func (v *Verification) Apply(b *Broadcast) (stale bool, err error) {
	if b.ChainHeight < v.height {
		return true, nil
	}
	if b.ChainHeight > v.height {
		return false, fail(CodeBroadcastInFuture, fmt.Sprintf("broadcast height %d, chain height %d", b.ChainHeight, v.height))
	}
	// Call.cpp:150-171.
	if b.ChainHash != v.hash {
		return false, fail(CodeBroadcastInvalidBlockHash, "")
	}
	switch b.Kind {
	case BroadcastCommit:
		return false, v.commit(b)
	case BroadcastReveal:
		return false, v.reveal(b)
	}
	return false, fail(CodeInvalidBlock, "unknown broadcast")
}

// commit is process_broadcast of a commit (Call.cpp:181-209).
func (v *Verification) commit(b *Broadcast) error {
	if v.phase != PhaseCommit {
		return fail(CodeBroadcastNotInCommit, "")
	}
	key, ok := v.keys[b.UserID]
	if !ok {
		return &Error{Kind: KindForbidden, Code: CodeBroadcastUnknownUserID}
	}
	if err := b.VerifySignature(key); err != nil {
		return err
	}
	if _, ok := v.committed[b.UserID]; ok {
		return fail(CodeBroadcastAlreadyApplied, "")
	}
	v.committed[b.UserID] = b.Value
	if len(v.committed) == len(v.keys) {
		v.phase = PhaseReveal
	}
	return nil
}

// reveal is process_broadcast of a reveal (Call.cpp:211-257): the nonce must
// hash to the user's commit.
func (v *Verification) reveal(b *Broadcast) error {
	if v.phase != PhaseReveal {
		return fail(CodeBroadcastNotInReveal, "")
	}
	key, ok := v.keys[b.UserID]
	if !ok {
		return &Error{Kind: KindForbidden, Code: CodeBroadcastUnknownUserID}
	}
	if err := b.VerifySignature(key); err != nil {
		return err
	}
	if _, ok := v.revealed[b.UserID]; ok {
		return fail(CodeBroadcastAlreadyApplied, "")
	}
	if sha256.Sum256(b.Value[:]) != v.committed[b.UserID] {
		return fail(CodeBroadcastInvalidReveal, "")
	}
	v.revealed[b.UserID] = b.Value
	if len(v.revealed) == len(v.keys) {
		v.phase = PhaseEnd
	}
	return nil
}

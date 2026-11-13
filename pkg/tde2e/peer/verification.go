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

package peer

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"slices"

	"github.com/teamgram/teamgram-server/pkg/tde2e"
)

// verification is CallVerification of a participant (Call.cpp:521-577):
// on every block it commits to a fresh nonce, reveals it once everyone
// committed, and with all nonces revealed makes the hash of the emoji.
// Broadcasts for a later height wait for their block (allow_delay,
// Call.cpp:118-127).
type verification struct {
	userID int64
	key    ed25519.PrivateKey

	chain      *tde2e.Verification
	height     int32
	hash       [32]byte
	nonce      [32]byte
	sentReveal bool
	outbound   [][]byte
	revealed   map[int64][32]byte
	delayed    map[int32][]*tde2e.Broadcast
	emoji      []byte
}

// onBlock starts the verification of the chain's last block
// (CallVerification::on_new_main_block, Call.cpp:531-549).
func (v *verification) onBlock(c *tde2e.Chain) error {
	if v.chain == nil {
		v.chain = tde2e.NewVerification()
		v.delayed = map[int32][]*tde2e.Broadcast{}
	}
	if _, err := rand.Read(v.nonce[:]); err != nil {
		return err
	}
	v.height, v.hash = c.Height(), c.LastHash()
	commit := &tde2e.Broadcast{Kind: tde2e.BroadcastCommit, UserID: v.userID, ChainHeight: v.height, ChainHash: v.hash, Value: sha256.Sum256(v.nonce[:])}
	commit.Sign(v.key)
	v.outbound = [][]byte{commit.Marshal()}
	v.sentReveal = false
	v.revealed = map[int64][32]byte{}
	v.emoji = nil
	v.chain.Reset(c)
	pending := v.delayed[v.height]
	for h := range v.delayed {
		if h <= v.height {
			delete(v.delayed, h)
		}
	}
	for _, b := range pending {
		// A delayed broadcast that fails is dropped, as tde2e logs and
		// drops it (Call.cpp:93-99).
		_ = v.apply(b)
	}
	return nil
}

// receive takes a broadcast in the local form
// (CallVerification::receive_inbound_message, Call.cpp:565-577).
func (v *verification) receive(local []byte) error {
	b, err := tde2e.ParseBroadcast(local)
	if err != nil {
		return err
	}
	switch {
	case b.ChainHeight < v.height:
		return nil
	case b.ChainHeight > v.height:
		v.delayed[b.ChainHeight] = append(v.delayed[b.ChainHeight], b)
		return nil
	}
	return v.apply(b)
}

func (v *verification) apply(b *tde2e.Broadcast) error {
	if _, err := v.chain.Apply(b); err != nil {
		return err
	}
	if b.Kind == tde2e.BroadcastReveal {
		v.revealed[b.UserID] = b.Value
		if v.chain.Phase() == tde2e.PhaseEnd {
			nonces := make([][]byte, 0, len(v.revealed))
			for _, n := range v.revealed {
				nonces = append(nonces, n[:])
			}
			slices.SortFunc(nonces, bytes.Compare)
			v.emoji = hmacSHA512(bytes.Join(nonces, nil), v.hash[:])
		}
	}
	if v.chain.Phase() == tde2e.PhaseReveal && !v.sentReveal {
		v.sentReveal = true
		reveal := &tde2e.Broadcast{Kind: tde2e.BroadcastReveal, UserID: v.userID, ChainHeight: v.height, ChainHash: v.hash, Value: v.nonce}
		reveal.Sign(v.key)
		v.outbound = append(v.outbound, reveal.Marshal())
	}
	return nil
}

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

package secretchat

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestDHPrime(t *testing.T) {
	p := new(big.Int).SetBytes(DHPrime)
	if p.BitLen() != 2048 || !p.ProbablyPrime(20) {
		t.Fatalf("prime: %d bits", p.BitLen())
	}
	q := new(big.Int).Rsh(p, 1)
	if !q.ProbablyPrime(20) {
		t.Fatal("(p-1)/2 is not prime")
	}
	// Android accepts the group of g = 3 when p mod 3 = 2
	// (Utilities.isGoodPrime).
	if new(big.Int).Mod(p, big.NewInt(3)).Int64() != 2 {
		t.Fatal("p mod 3")
	}
}

func TestValidDH(t *testing.T) {
	p := new(big.Int).SetBytes(DHPrime)
	good := new(big.Int).Exp(big.NewInt(int64(DHG)), big.NewInt(1234567), p).FillBytes(make([]byte, 256))
	low := new(big.Int).Lsh(big.NewInt(1), 2048-64).Bytes()
	high := new(big.Int).Sub(p, new(big.Int).Lsh(big.NewInt(1), 2048-64)).Bytes()
	for _, tc := range []struct {
		name string
		v    []byte
		ok   bool
	}{
		{"g^a", good, true},
		{"empty", nil, false},
		{"one", []byte{1}, false},
		{"longer than the group", bytes.Repeat([]byte{0xff}, 257), false},
		{"the low bound", low, false},
		{"the high bound", high, false},
	} {
		if got := ValidDH(tc.v); got != tc.ok {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}

func TestChatView(t *testing.T) {
	admin := Device{UserId: 1, AuthKey: 10}
	other := Device{UserId: 2, AuthKey: 21}
	accepted := Device{UserId: 2, AuthKey: 20}
	stranger := Device{UserId: 3, AuthKey: 30}
	chat := func(st int8) *Chat {
		return &Chat{Id: 5, AccessHash: 7, AdminId: 1, AdminAuthKey: 10, ParticipantId: 2, ParticipantAuthKey: 20, GA: []byte("ga"), GB: []byte("gb"), KeyFingerprint: 9, State: st}
	}

	c := chat(StateRequested)
	c.ParticipantAuthKey = 0
	if v := c.View(admin); v.GetPredicateName() != mtproto.Predicate_encryptedChatWaiting {
		t.Errorf("request, admin: %v", v)
	}
	if v := c.View(other); v.GetPredicateName() != mtproto.Predicate_encryptedChatRequested || !bytes.Equal(v.GetGA(), []byte("ga")) {
		t.Errorf("request, participant: %v", v)
	}
	if v := c.View(stranger); v.GetPredicateName() != mtproto.Predicate_encryptedChatEmpty {
		t.Errorf("request, stranger: %v", v)
	}

	c = chat(StateActive)
	if v := c.View(admin); v.GetPredicateName() != mtproto.Predicate_encryptedChat || !bytes.Equal(v.GetGAOrB(), []byte("gb")) || v.GetKeyFingerprint() != 9 {
		t.Errorf("active, admin: %v", v)
	}
	if v := c.View(accepted); v.GetPredicateName() != mtproto.Predicate_encryptedChat || !bytes.Equal(v.GetGAOrB(), []byte("ga")) {
		t.Errorf("active, accepting device: %v", v)
	}
	if v := c.View(other); v.GetPredicateName() != mtproto.Predicate_encryptedChatDiscarded || v.GetHistoryDeleted() {
		t.Errorf("active, another device: %v", v)
	}

	c = chat(StateDiscarded)
	c.HistoryDeleted, c.DiscardedBy = true, 20
	if v := c.View(admin); !v.GetHistoryDeleted() {
		t.Errorf("discarded with history, the other end: %v", v)
	}
	if v := c.View(accepted); v.GetHistoryDeleted() {
		t.Errorf("discarded by it: %v", v)
	}
	if v := c.View(other); v.GetHistoryDeleted() {
		t.Errorf("discarded, another device of the participant: %v", v)
	}
}

func TestShouldTell(t *testing.T) {
	admin := Device{UserId: 1, AuthKey: 10}
	device := Device{UserId: 2, AuthKey: 20}
	c := &Chat{AdminId: 1, AdminAuthKey: 10, ParticipantId: 2, ParticipantAuthKey: 20}

	c.State = StateRequested
	c.ParticipantAuthKey = 0
	if shouldTell(c, admin, true, false) || !shouldTell(c, device, false, false) || shouldTell(c, device, false, true) {
		t.Error("a request is the participant's, unless it takes none")
	}

	c.State, c.ParticipantAuthKey = StateActive, 20
	if !shouldTell(c, admin, true, false) || shouldTell(c, device, false, false) || !shouldTell(c, Device{UserId: 2, AuthKey: 21}, false, false) {
		t.Error("accepted: the requester and the other authorizations are told, the acceptor knows")
	}

	c.State, c.DiscardedBy = StateDiscarded, 20
	if !shouldTell(c, admin, true, false) || shouldTell(c, device, false, false) {
		t.Error("discarded: the other end is told, the side that discarded knows")
	}
}

func TestDifferenceEmpty(t *testing.T) {
	if d := (&Difference{Qts: 3}); !d.Empty(3) {
		t.Error("nothing new at the same qts")
	}
	if d := (&Difference{Qts: 3}); d.Empty(5) {
		t.Error("a client ahead of the queue takes the lower qts")
	}
	if d := (&Difference{Messages: []*QueuedMessage{{}}, Qts: 3}); d.Empty(3) {
		t.Error("messages")
	}
	if d := (&Difference{Qts: firstQts}); !d.Empty(0) {
		t.Error("no events yet at qts 0")
	}
	if d := (&Difference{Qts: firstQts + 1}); d.Empty(0) {
		t.Error("an event since qts 0")
	}
	if d := (&Difference{}); !d.Empty(-1) {
		t.Error("a client that asks with qts -1 has nothing new")
	}
	if d := (&Difference{Messages: []*QueuedMessage{{}}, Qts: 3}); d.Empty(-1) {
		t.Error("messages at qts -1")
	}
}

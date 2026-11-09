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

package core

import (
	"context"
	"math/big"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// The checks Android, iOS and tdesktop run on the answer before a call goes
// on: a safe 2048-bit prime for which 3 generates the group
// (tdesktop mtproto_dh_utils.cpp:17-80).
func TestDhGroupIsGood(t *testing.T) {
	p := new(big.Int).SetBytes(dhPrime)
	if p.BitLen() != 2048 || !p.ProbablyPrime(20) {
		t.Fatalf("p: %d bits", p.BitLen())
	}
	q := new(big.Int).Rsh(p, 1)
	if !q.ProbablyPrime(20) {
		t.Fatal("(p-1)/2 is not prime")
	}
	if dhG != 3 || new(big.Int).Mod(p, big.NewInt(3)).Int64() != 2 {
		t.Fatal("3 does not generate the group")
	}
}

func TestGetDhConfig(t *testing.T) {
	c := New(context.Background())
	// Telegram-iOS: version 0, no random.
	r, err := c.MessagesGetDhConfig(&mtproto.TLMessagesGetDhConfig{Version: 0, RandomLength: 0})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_messages_dhConfig || len(r.GetRandom()) != 0 || r.GetG() != dhG || len(r.GetP()) != 256 || r.GetVersion() == 0 {
		t.Fatalf("version 0: %v %v", r, err)
	}
	r, err = c.MessagesGetDhConfig(&mtproto.TLMessagesGetDhConfig{Version: 0, RandomLength: 256})
	if err != nil || len(r.GetRandom()) != 256 {
		t.Fatalf("random: %v %v", r, err)
	}
	r, err = c.MessagesGetDhConfig(&mtproto.TLMessagesGetDhConfig{Version: dhVersion, RandomLength: 256})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_messages_dhConfigNotModified || len(r.GetRandom()) != 256 {
		t.Fatalf("known version: %v %v", r, err)
	}
	// A version the server never gave out is not "the same".
	r, err = c.MessagesGetDhConfig(&mtproto.TLMessagesGetDhConfig{Version: dhVersion + 1, RandomLength: 256})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_messages_dhConfig {
		t.Fatalf("other version: %v %v", r, err)
	}
	for _, n := range []int32{-1, maxRandomLength + 1} {
		if _, err := c.MessagesGetDhConfig(&mtproto.TLMessagesGetDhConfig{RandomLength: n}); err != mtproto.ErrRandomLengthInvalid {
			t.Errorf("random_length %d: %v", n, err)
		}
	}
}

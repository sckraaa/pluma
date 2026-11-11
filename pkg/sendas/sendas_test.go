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

package sendas

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

const (
	user  = int64(10)
	group = int64(100)
)

func peerIds(peers []*mtproto.Peer) []int64 {
	out := make([]int64, 0, len(peers))
	for _, p := range peers {
		if id := p.GetUserId(); id != 0 {
			out = append(out, id)
		} else {
			out = append(out, -p.GetChannelId())
		}
	}
	return out
}

func TestChoices(t *testing.T) {
	owned := []int64{200, 300}
	for _, tc := range []struct {
		name string
		m    Member
		want []int64
	}{
		{"broadcast channel", Member{Admin: true, Public: true}, []int64{-group}},
		{"private group, member", Member{Megagroup: true}, []int64{user}},
		{"public group, member", Member{Megagroup: true, Public: true}, []int64{user, -200, -300}},
		{"discussion group, member", Member{Megagroup: true, Discussion: true}, []int64{user, -200, -300}},
		{"private group, admin", Member{Megagroup: true, Admin: true}, []int64{user, -200, -300}},
		{"private group, anonymous admin", Member{Megagroup: true, Admin: true, Anonymous: true}, []int64{-group, -200, -300}},
	} {
		got := peerIds(Choices(user, group, tc.m, owned))
		if len(got) != len(tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
				break
			}
		}
	}
}

func TestChoicesLeaveOutTheGroup(t *testing.T) {
	// A supergroup is no broadcast channel; were its id among owned it
	// would not come twice.
	got := peerIds(Choices(user, group, Member{Megagroup: true, Admin: true, Anonymous: true}, []int64{group, 200}))
	if len(got) != 2 || got[0] != -group || got[1] != -200 {
		t.Errorf("%v, want [%d -200]", got, -group)
	}
}

func TestAllowed(t *testing.T) {
	choices := Choices(user, group, Member{Megagroup: true, Public: true}, []int64{200})
	for _, tc := range []struct {
		as   *mtproto.Peer
		want bool
	}{
		{mtproto.MakePeerUser(user), true},
		{mtproto.MakePeerChannel(200), true},
		{mtproto.MakePeerChannel(group), false},
		{mtproto.MakePeerChannel(300), false},
		{mtproto.MakePeerUser(11), false},
		// A user and a channel with the same id are two peers.
		{mtproto.MakePeerUser(200), false},
		{nil, false},
	} {
		if got := Allowed(choices, tc.as); got != tc.want {
			t.Errorf("Allowed(%v) = %v, want %v", tc.as, got, tc.want)
		}
	}
}

func TestAuthor(t *testing.T) {
	if p := Author(user, 0); p.GetPredicateName() != mtproto.Predicate_peerUser || p.GetUserId() != user {
		t.Errorf("Author(user, 0) = %v", p)
	}
	if p := (AuthorRow{SenderUserId: user, SendAsId: 200}).Peer(); p.GetPredicateName() != mtproto.Predicate_peerChannel || p.GetChannelId() != 200 {
		t.Errorf("Author(user, 200) = %v", p)
	}
}

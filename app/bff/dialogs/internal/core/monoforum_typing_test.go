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
	"slices"
	"testing"

	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// TestMonoforumTypingTo: in the direct messages of a channel a subscriber's
// typing reaches the admins of the direct messages and no other subscriber,
// and an admin's typing reaches nobody.
func TestMonoforumTypingTo(t *testing.T) {
	const (
		owner, admin, bob, carol = 1, 2, 3, 4
	)
	row := func(id int64, role int32) *channelpb.ImmutableChannelParticipant {
		return &channelpb.ImmutableChannelParticipant{UserId: id, Role: role, State: channelpb.StateIn}
	}
	mc := &channelpb.MutableChannel{
		Channel: &channelpb.ImmutableChannel{Id: 10, Megagroup: true, Monoforum: true},
		Participants: []*channelpb.ImmutableChannelParticipant{
			row(owner, channelpb.RoleAdmin), row(admin, channelpb.RoleAdmin),
			row(bob, channelpb.RoleMember), row(carol, channelpb.RoleMember),
		},
	}
	if got := monoforumTypingTo(mc, bob); !slices.Equal(got, []int64{owner, admin}) {
		t.Errorf("Bob types to %v, want the admins alone", got)
	}
	if got := monoforumTypingTo(mc, admin); len(got) != 0 {
		t.Errorf("an admin types to %v, want nobody", got)
	}
}

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

package monoforum

import (
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

// Who manages the direct messages of a channel: its creator and its admins
// with manage_direct_messages, while they are in it.
func TestManages(t *testing.T) {
	for _, c := range []struct {
		name               string
		role, state, right int32
		want               bool
	}{
		{"the creator", channelperm.RoleCreator, channelperm.StateIn, 0, true},
		{"an admin with the right", channelperm.RoleAdmin, channelperm.StateIn, mtproto.ADMIN_MANAGE_DIRECT_MESSAGES | mtproto.ADMIN_POST_MESSAGES, true},
		{"an admin without it", channelperm.RoleAdmin, channelperm.StateIn, mtproto.ADMIN_POST_MESSAGES | mtproto.ADMIN_DELETE_MESSAGES, false},
		{"a subscriber", channelperm.RoleMember, channelperm.StateIn, mtproto.ADMIN_MANAGE_DIRECT_MESSAGES, false},
		{"the creator who left", channelperm.RoleCreator, channelperm.StateLeft, 0, false},
		{"an admin kicked", channelperm.RoleAdmin, channelperm.StateKicked, mtproto.ADMIN_MANAGE_DIRECT_MESSAGES, false},
	} {
		if got := Manages(c.role, c.state, c.right); got != c.want {
			t.Errorf("%s manages: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSide(t *testing.T) {
	if id := (Side{Monoforum: true, LinkedId: 5}).BroadcastId(); id != 5 {
		t.Errorf("the channel of a monoforum: %d, want 5", id)
	}
	if id := (Side{LinkedId: 5}).BroadcastId(); id != 0 {
		t.Errorf("the channel of a channel: %d, want none", id)
	}
	if got := Peers(map[int32]int64{1: 9, 2: 0, 3: 7, 4: 9}); !slices.Equal(got, []int64{7, 9}) {
		t.Errorf("the sublists of messages: %v, want 7 9", got)
	}
}

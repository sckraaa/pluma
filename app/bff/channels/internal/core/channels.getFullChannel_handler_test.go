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
	"testing"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// TestAvailableMinId: the field names the last hidden message, and only for
// a member: the row of one who left keeps its mark, yet the history of a
// public channel is whole for it.
func TestAvailableMinId(t *testing.T) {
	for _, c := range []struct {
		name string
		p    *channelpb.ImmutableChannelParticipant
		want int32
	}{
		{"never joined", nil, 0},
		{"sees everything", &channelpb.ImmutableChannelParticipant{State: channelpb.StateIn}, 0},
		{"sees from the first", &channelpb.ImmutableChannelParticipant{State: channelpb.StateIn, AvailableMinId: 1}, 0},
		{"hid up to 41", &channelpb.ImmutableChannelParticipant{State: channelpb.StateIn, AvailableMinId: 42}, 41},
		{"left", &channelpb.ImmutableChannelParticipant{State: channelpb.StateLeft, AvailableMinId: 42}, 0},
	} {
		if got := availableMinId(c.p).GetValue(); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

// TestStatsFlags: can_view_stats and stats_dc go together, to the creator
// in the channel and to an admin with post_messages in a broadcast channel
// or with ban_users in a supergroup, and to nobody while the statistics are
// off.
func TestStatsFlags(t *testing.T) {
	broadcast := &channelpb.ImmutableChannel{Broadcast: true}
	megagroup := &channelpb.ImmutableChannel{Megagroup: true}
	monoforum := &channelpb.ImmutableChannel{Megagroup: true, Monoforum: true}
	blocked := &channelpb.ImmutableChannel{Broadcast: true, Blocked: true}
	creator := &channelpb.ImmutableChannelParticipant{Role: channelpb.RoleCreator, State: channelpb.StateIn}
	poster := &channelpb.ImmutableChannelParticipant{Role: channelpb.RoleAdmin, State: channelpb.StateIn, AdminRights: mtproto.ADMIN_POST_MESSAGES}
	banner := &channelpb.ImmutableChannelParticipant{Role: channelpb.RoleAdmin, State: channelpb.StateIn, AdminRights: mtproto.ADMIN_BAN_USERS}
	member := &channelpb.ImmutableChannelParticipant{State: channelpb.StateIn}
	left := &channelpb.ImmutableChannelParticipant{Role: channelpb.RoleCreator, State: channelpb.StateLeft}
	for _, c := range []struct {
		name string
		ch   *channelpb.ImmutableChannel
		p    *channelpb.ImmutableChannelParticipant
		dc   int32
		want bool
	}{
		{"creator", broadcast, creator, 1, true},
		{"creator, statistics off", broadcast, creator, 0, false},
		{"poster of a channel", broadcast, poster, 1, true},
		{"banner of a channel", broadcast, banner, 1, false},
		{"banner of a group", megagroup, banner, 1, true},
		{"poster of a group", megagroup, poster, 1, false},
		{"member", broadcast, member, 1, false},
		{"not a participant", broadcast, nil, 1, false},
		{"creator who left", broadcast, left, 1, false},
		{"monoforum", monoforum, creator, 1, false},
		{"blocked", blocked, creator, 1, false},
	} {
		got, dc := statsFlags(c.ch, c.p, c.dc)
		if got != c.want {
			t.Errorf("%s: can_view_stats %v, want %v", c.name, got, c.want)
		}
		switch {
		case c.want && dc.GetValue() != c.dc:
			t.Errorf("%s: stats_dc %v, want %d", c.name, dc, c.dc)
		case !c.want && dc != nil:
			t.Errorf("%s: stats_dc %v without can_view_stats", c.name, dc)
		}
	}
}

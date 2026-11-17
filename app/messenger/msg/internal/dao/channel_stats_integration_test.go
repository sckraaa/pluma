//go:build integration

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

package dao_test

import (
	"testing"

	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

// TestChannelSendReaders: a member who writes to a supergroup is one of its
// readers of the day (task 103), once however much it writes; a post of a
// broadcast channel makes none, nor does a ghost's message.
func TestChannelSendReaders(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	creator, member := newUser(), newUser()
	group := w.newChannelRow(creator, false)
	w.member(group, member, channelperm.RoleMember)
	for range 2 {
		if _, err := w.sendOutgoing(member, group, channelText(member, group, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_stats_readers WHERE channel_id = ? AND user_id = ?", group, member); n != 1 {
		t.Fatalf("readers of the group: %d, want 1", n)
	}

	broadcast := w.newChannelRow(creator, true)
	if _, err := w.sendOutgoing(creator, broadcast, channelText(creator, broadcast, 0)); err != nil {
		t.Fatal(err)
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_stats_readers WHERE channel_id = ?", broadcast); n != 0 {
		t.Fatalf("readers of the broadcast channel: %d, want 0", n)
	}

	ghost := w.newGhost()
	w.exec("INSERT INTO channel_participants (channel_id, user_id, role, hidden, `date`) VALUES (?, ?, ?, 1, 1)", group, ghost, channelperm.RoleMember)
	if _, err := w.sendOutgoing(ghost, group, channelText(ghost, group, 0)); err != nil {
		t.Fatal(err)
	}
	if n := w.countRows("SELECT COUNT(*) FROM channel_stats_readers WHERE channel_id = ? AND user_id = ?", group, ghost); n != 0 {
		t.Fatalf("the ghost is a reader: %d", n)
	}
}

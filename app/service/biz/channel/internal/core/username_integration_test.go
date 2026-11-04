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

package core

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
)

func (f *diffFixture) checkUsername(self, channelId int64, name string) (bool, error) {
	r, err := f.c.ChannelCheckUsername(&channel.TLChannelCheckUsername{SelfId: self, ChannelId: channelId, Username: name})
	return mtproto.FromBool(r), err
}

func (f *diffFixture) setUsername(self, channelId int64, name string) error {
	_, err := f.c.ChannelUpdateUsername(&channel.TLChannelUpdateUsername{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Username: name})
	return err
}

// TestUsername: a channel takes a free username, not one a user or another
// channel has; the same one again is not modified; "" makes it private;
// a member who may not change the info may not set it; past the limit of
// public channels the owner may make no other public; only the creator
// sets the link.
func TestUsername(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id, other := f.create(creator), f.create(creator)
	name := fmt.Sprintf("chan%d", id)
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)",
		fmt.Sprintf("user%d", bob), mtproto.PEER_USER, bob)

	if free, err := f.checkUsername(creator, 0, name); err != nil || !free {
		t.Fatalf("a free name for a channel not made yet: %v, %v", free, err)
	}
	if free, err := f.checkUsername(creator, id, fmt.Sprintf("user%d", bob)); err != nil || free {
		t.Errorf("a user's name: %v, %v", free, err)
	}
	if err := f.setUsername(creator, id, name); err != nil {
		t.Fatal(err)
	}
	if free, err := f.checkUsername(creator, id, name); err != nil || !free {
		t.Errorf("the channel's own name: %v, %v", free, err)
	}
	if free, err := f.checkUsername(creator, other, name); err != nil || free {
		t.Errorf("another channel's name: %v, %v", free, err)
	}
	wantErr(t, "the same name", f.setUsername(creator, id, name), mtproto.ErrUsernameNotModified)
	wantErr(t, "another channel's name", f.setUsername(creator, other, name), mtproto.ErrUsernameOccupied)
	wantErr(t, "a user's name", f.setUsername(creator, other, fmt.Sprintf("user%d", bob)), mtproto.ErrUsernameOccupied)

	mc, err := f.c.svcCtx.Dao.GetMutableChannel(f.c.ctx, id, []int64{creator})
	if err != nil || mc.GetChannel().GetUsername() != name || !mc.GetChannel().Public() {
		t.Errorf("the channel's username %q, %v", mc.GetChannel().GetUsername(), err)
	}

	f.public(id) // a second row is ignored: the editable one is the channel's
	// Only the creator sets the link: neither a member where members may
	// change the info nor an admin who may.
	f.exec("INSERT INTO channel_participants (channel_id, user_id, `date`) VALUES (?, ?, 0)", id, bob)
	wantErr(t, "a member", f.setUsername(bob, id, "bobs_link"), mtproto.ErrChatAdminRequired)
	admin := f.user("Admin")
	f.exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, 0)",
		id, admin, channel.RoleAdmin, mtproto.ADMIN_CHANGE_INFO)
	wantErr(t, "an admin who may change the info", f.setUsername(admin, id, "admins_link"), mtproto.ErrChatAdminRequired)
	_, err = f.checkUsername(admin, id, "admins_link")
	wantErr(t, "an admin's check", err, mtproto.ErrChatAdminRequired)

	if err = f.setUsername(creator, id, ""); err != nil {
		t.Fatalf("make it private: %v", err)
	}
	if free, err := f.checkUsername(creator, other, name); err != nil || !free {
		t.Errorf("the name freed: %v, %v", free, err)
	}

	// The limit of public channels.
	owner := f.user("Owner")
	for i := range dao.PublicChannelsLimit {
		c := f.create(owner)
		if err = f.setUsername(owner, c, fmt.Sprintf("pub%d_%d", owner, i)); err != nil {
			t.Fatalf("public channel %d: %v", i, err)
		}
	}
	last := f.create(owner)
	_, err = f.checkUsername(owner, 0, "")
	wantErr(t, "a check past the limit", err, mtproto.ErrChannelsAdminPublicTooMuch)
	wantErr(t, "a link past the limit", f.setUsername(owner, last, fmt.Sprintf("pub%d_x", owner)), mtproto.ErrChannelsAdminPublicTooMuch)
	if ids, err := f.c.svcCtx.Dao.AdminedPublicChannels(f.c.ctx, owner); err != nil || len(ids) != dao.PublicChannelsLimit {
		t.Errorf("admined public channels: %d, %v", len(ids), err)
	}
}

// TestUsernameRace: two channels take one name at once: one gets it, the
// other USERNAME_OCCUPIED.
func TestUsernameRace(t *testing.T) {
	f := newDiffFixture(t)
	a, b := f.user("A"), f.user("B")
	ca, cb := f.create(a), f.create(b)
	name := fmt.Sprintf("race%d", ca)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, p := range [][2]int64{{a, ca}, {b, cb}} {
		wg.Add(1)
		go func(i int, self, c int64) {
			defer wg.Done()
			errs[i] = f.setUsername(self, c, name)
		}(i, p[0], p[1])
	}
	wg.Wait()
	won, lost := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, mtproto.ErrUsernameOccupied):
			lost++
		default:
			t.Errorf("unexpected: %v", err)
		}
	}
	if won != 1 || lost != 1 {
		t.Errorf("won %d, lost %d", won, lost)
	}
}

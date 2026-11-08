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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

func (f *diffFixture) signatures(self, channelId int64, enabled, profiles bool) (*mtproto.Updates, error) {
	return f.c.ChannelToggleSignatures(&channel.TLChannelToggleSignatures{
		SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Enabled: mtproto.ToBool(enabled), Profiles: mtproto.ToBool(profiles),
	})
}

func (f *diffFixture) slowMode(self, channelId int64, seconds int32) (*mtproto.Updates, error) {
	return f.c.ChannelToggleSlowMode(&channel.TLChannelToggleSlowMode{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Seconds: seconds})
}

func (f *diffFixture) broadcast(channelId int64) {
	f.t.Helper()
	f.exec("UPDATE channels SET broadcast = 1, megagroup = 0 WHERE id = ?", channelId)
}

func (f *diffFixture) member(channelId, userId int64) {
	f.t.Helper()
	f.exec("INSERT INTO channel_participants (channel_id, user_id, `date`) VALUES (?, ?, 1)", channelId, userId)
}

func (f *diffFixture) admin(channelId, userId int64, rights int32) {
	f.t.Helper()
	f.exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, 1)", channelId, userId, channel.RoleAdmin, rights)
}

// TestToggleSignatures: the links go only with signatures and go off with
// them; the answer carries the channel with both flags; only one who may
// change the info changes them, and only in a broadcast channel.
func TestToggleSignatures(t *testing.T) {
	f := newDiffFixture(t)
	creator, member := itId(), itId()
	id := f.create(creator)
	f.broadcast(id)
	f.member(id, member)

	_, err := f.signatures(member, id, true, true)
	wantErr(t, "a subscriber", err, mtproto.ErrChatAdminRequired)

	check := func(r *mtproto.Updates, signatures, profiles bool) {
		t.Helper()
		c := r.GetChats()[0]
		if c.GetSignatures() != signatures || c.GetSignatureProfiles() != profiles {
			t.Errorf("channel: signatures %v, profiles %v, want %v, %v", c.GetSignatures(), c.GetSignatureProfiles(), signatures, profiles)
		}
	}
	r, err := f.signatures(creator, id, true, true)
	if err != nil {
		t.Fatal(err)
	}
	check(r, true, true)
	_, err = f.signatures(creator, id, true, true)
	wantErr(t, "the same again", err, mtproto.ErrChatNotModified)
	if r, err = f.signatures(creator, id, true, false); err != nil {
		t.Fatal(err)
	}
	check(r, true, false)
	if r, err = f.signatures(creator, id, true, true); err != nil {
		t.Fatal(err)
	}
	check(r, true, true)
	// Signatures off with the links asked for: the links go off too.
	if r, err = f.signatures(creator, id, false, true); err != nil {
		t.Fatal(err)
	}
	check(r, false, false)
	_, err = f.signatures(creator, id, false, false)
	wantErr(t, "off again", err, mtproto.ErrChatNotModified)

	group := f.create(creator)
	_, err = f.signatures(creator, group, true, false)
	wantErr(t, "a supergroup", err, mtproto.ErrChannelInvalid)
}

// TestToggleSlowMode: the intervals of the clients; those who may ban change
// it; the flag and the seconds are on the channel; a member's last send is
// read through the participant.
func TestToggleSlowMode(t *testing.T) {
	f := newDiffFixture(t)
	creator, banner, editor, member := itId(), itId(), itId(), itId()
	id := f.create(creator)
	f.admin(id, banner, mtproto.ADMIN_BAN_USERS)
	f.admin(id, editor, mtproto.ADMIN_CHANGE_INFO)
	f.member(id, member)

	for _, s := range []int32{-1, 1, 7, 45, 3601} {
		_, err := f.slowMode(creator, id, s)
		wantErr(t, "an invalid interval", err, mtproto.ErrSecondsInvalid)
	}
	_, err := f.slowMode(member, id, 30)
	wantErr(t, "a member", err, mtproto.ErrChatAdminRequired)
	_, err = f.slowMode(editor, id, 30)
	wantErr(t, "an admin who may not ban", err, mtproto.ErrChatAdminRequired)

	r, err := f.slowMode(banner, id, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !r.GetChats()[0].GetSlowmodeEnabled() {
		t.Errorf("slowmode_enabled unset: %v", r.GetChats()[0])
	}
	if u := r.GetUpdates()[0]; u.GetPredicateName() != mtproto.Predicate_updateChannel || u.GetChannelId() != id {
		t.Errorf("updates: %v", r.GetUpdates())
	}
	_, err = f.slowMode(creator, id, 30)
	wantErr(t, "the same interval", err, mtproto.ErrChatNotModified)

	f.exec("UPDATE channel_participants SET last_send_date = 1234 WHERE channel_id = ? AND user_id = ?", id, member)
	mc, err := f.c.ChannelGetMutableChannel(&channel.TLChannelGetMutableChannel{ChannelId: id, Id: []int64{member}})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := mc.GetParticipant(member)
	if mc.GetChannel().GetSlowmodeSeconds() != 30 || p.GetLastSendDate() != 1234 {
		t.Errorf("seconds %d, last send %d", mc.GetChannel().GetSlowmodeSeconds(), p.GetLastSendDate())
	}

	if r, err = f.slowMode(creator, id, 0); err != nil {
		t.Fatal(err)
	}
	if r.GetChats()[0].GetSlowmodeEnabled() {
		t.Error("slowmode_enabled set after turning it off")
	}

	post := f.create(creator)
	f.broadcast(post)
	_, err = f.slowMode(creator, post, 30)
	wantErr(t, "a broadcast channel", err, mtproto.ErrChannelInvalid)
}

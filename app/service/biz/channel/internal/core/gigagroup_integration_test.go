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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

// TestConvertToGigagroup: only the creator of a supergroup of GigagroupMin
// members turns it into a broadcast group, not of a forum or a broadcast
// channel. The flag and the restrictions of sending, added to those the
// group had, are on the channel and in the answer, the members load it
// again, the recent actions keep the change of the restrictions; from then
// on a member may not write and an admin may, the conversion is not undone
// or repeated, and neither the default restrictions nor a forum come.
func TestConvertToGigagroup(t *testing.T) {
	f := newDiffFixture(t)
	creator, editor, member := itId(), itId(), itId()
	id := f.create(creator)
	f.admin(id, editor, channelperm.CreatorRights)
	f.member(id, member)
	f.exec("UPDATE channels SET default_banned_rights = ? WHERE id = ?", mtproto.BANNED_PIN_MESSAGES, id)
	convert := func(self int64, on bool) (*mtproto.Updates, error) {
		return f.toggleSetting(self, id, channel.SettingGigagroup, on)
	}

	_, err := convert(creator, true)
	wantErr(t, "a small group", err, mtproto.ErrParticipantsTooFew)
	f.exec("UPDATE channels SET participants_count = ? WHERE id = ?", dao.GigagroupMin-1, id)
	_, err = convert(creator, true)
	wantErr(t, "one member short", err, mtproto.ErrParticipantsTooFew)
	f.exec("UPDATE channels SET participants_count = ? WHERE id = ?", dao.GigagroupMin, id)
	_, err = convert(member, true)
	wantErr(t, "a member", err, mtproto.ErrChatAdminRequired)
	_, err = convert(editor, true)
	wantErr(t, "an admin with every right", err, mtproto.ErrChatAdminRequired)
	_, err = convert(creator, false)
	wantErr(t, "turning it off", err, mtproto.ErrInputRequestInvalid)
	f.exec("UPDATE channels SET forum = 1 WHERE id = ?", id)
	_, err = convert(creator, true)
	wantErr(t, "a forum", err, mtproto.ErrForumEnabled)
	f.exec("UPDATE channels SET forum = 0 WHERE id = ?", id)

	r, err := convert(creator, true)
	if err != nil {
		t.Fatal(err)
	}
	want := int32(mtproto.BANNED_PIN_MESSAGES | channelperm.SendBanned)
	rights := func(r *mtproto.ChatBannedRights) int32 { return int32(r.ToBannedRights()) }
	c := r.GetChats()[0]
	if !c.GetGigagroup() || !c.GetMegagroup() || rights(c.GetDefaultBannedRights()) != want {
		t.Errorf("the answer's channel: gigagroup %v, megagroup %v, default %v", c.GetGigagroup(), c.GetMegagroup(), c.GetDefaultBannedRights())
	}
	if u := r.GetUpdates()[0]; u.GetPredicateName() != mtproto.Predicate_updateChannel || u.GetChannelId() != id {
		t.Errorf("updates: %v", r.GetUpdates())
	}
	logged := f.logOf(creator, id, -1, mtproto.Predicate_channelAdminLogEventActionDefaultBannedRights)
	if len(logged) != 1 || rights(logged[0].GetAction().GetPrevBannedRights()) != mtproto.BANNED_PIN_MESSAGES ||
		rights(logged[0].GetAction().GetNewBannedRights()) != want || logged[0].GetUserId() != creator {
		t.Errorf("the recent actions: %v", logged)
	}

	mc, err := f.c.ChannelGetMutableChannel(&channel.TLChannelGetMutableChannel{ChannelId: id, Id: []int64{member, editor}})
	if err != nil {
		t.Fatal(err)
	}
	if !mc.GetChannel().GetGigagroup() || !mc.ToChat(member, 0).GetGigagroup() || !mc.GetChannel().ToMinChat().GetGigagroup() {
		t.Errorf("the channel as the members load it: %v", mc.GetChannel())
	}
	m, _ := mc.GetParticipant(member)
	e, _ := mc.GetParticipant(editor)
	perm := mc.GetChannel().Perm()
	wantErr(t, "a member writes", channelperm.Can(perm, m.Perm(), channelperm.SendPlain, 0), mtproto.ErrChatWriteForbidden)
	if err = channelperm.Can(perm, e.Perm(), channelperm.SendPlain, 0); err != nil {
		t.Errorf("an admin writes: %v", err)
	}

	_, err = convert(creator, true)
	wantErr(t, "again", err, mtproto.ErrChatNotModified)
	_, err = convert(creator, false)
	wantErr(t, "back", err, mtproto.ErrInputRequestInvalid)
	_, err = f.c.ChannelEditDefaultBannedRights(&channel.TLChannelEditDefaultBannedRights{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, BannedRights: 0})
	wantErr(t, "lifting the restrictions", err, mtproto.ErrChannelInvalid)
	_, err = f.c.ChannelToggleForum(&channel.TLChannelToggleForum{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, Enabled: mtproto.BoolTrue, Tabs: mtproto.BoolFalse})
	wantErr(t, "a forum of it", err, mtproto.ErrChannelInvalid)

	other := f.create(creator)
	f.broadcast(other)
	f.exec("UPDATE channels SET participants_count = ? WHERE id = ?", dao.GigagroupMin, other)
	_, err = f.toggleSetting(creator, other, channel.SettingGigagroup, true)
	wantErr(t, "a broadcast channel", err, mtproto.ErrChannelInvalid)
}

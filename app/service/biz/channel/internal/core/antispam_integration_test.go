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
	"encoding/json"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/adminlog"
	"github.com/teamgram/teamgram-server/pkg/antispam"
	"github.com/teamgram/teamgram-server/pkg/moderation"
)

func (f *diffFixture) antispamDelete(userId, channelId int64, ids ...int32) {
	f.t.Helper()
	r, err := f.c.ChannelAntispamDelete(&channel.TLChannelAntispamDelete{UserId: userId, ChannelId: channelId, Id: ids, Rule: int32(antispam.RuleLink)})
	if err != nil || !mtproto.FromBool(r) {
		f.t.Fatalf("channel.antispamDelete: %v, %v", r, err)
	}
}

func (f *diffFixture) falsePositive(self, channelId int64, id int32) (bool, error) {
	r, err := f.c.ChannelAntispamFalsePositive(&channel.TLChannelAntispamFalsePositive{SelfId: self, ChannelId: channelId, MsgId: id})
	return mtproto.FromBool(r), err
}

func (f *diffFixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.box.QueryRow(query, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestToggleAntispam: an admin who may delete the messages of others turns
// the antispam of a supergroup of antispam.MembersMin members on, which the
// channel, the answer and the recent actions show; it goes off in a group
// of any size; a broadcast channel has none.
func TestToggleAntispam(t *testing.T) {
	f := newDiffFixture(t)
	creator, deleter, editor, member := itId(), itId(), itId(), itId()
	id := f.create(creator)
	f.admin(id, deleter, mtproto.ADMIN_DELETE_MESSAGES)
	f.admin(id, editor, mtproto.ADMIN_CHANGE_INFO)
	f.member(id, member)
	toggle := func(self int64, on bool) (*mtproto.Updates, error) {
		return f.toggleSetting(self, id, channel.SettingAntispam, on)
	}

	f.exec("UPDATE channels SET participants_count = ? WHERE id = ?", antispam.MembersMin-1, id)
	_, err := toggle(creator, true)
	wantErr(t, "a small group", err, mtproto.ErrParticipantsTooFew)
	f.exec("UPDATE channels SET participants_count = ? WHERE id = ?", antispam.MembersMin, id)
	_, err = toggle(member, true)
	wantErr(t, "a member", err, mtproto.ErrChatAdminRequired)
	_, err = toggle(editor, true)
	wantErr(t, "an admin who changes the info only", err, mtproto.ErrChatAdminRequired)

	r, err := toggle(deleter, true)
	if err != nil {
		t.Fatal(err)
	}
	if u := r.GetUpdates()[0]; u.GetPredicateName() != mtproto.Predicate_updateChannel || u.GetChannelId() != id {
		t.Errorf("updates: %v", r.GetUpdates())
	}
	mc, err := f.c.ChannelGetMutableChannel(&channel.TLChannelGetMutableChannel{ChannelId: id})
	if err != nil {
		t.Fatal(err)
	}
	if !mc.GetChannel().GetAntispam() {
		t.Error("the channel has no antispam")
	}
	logged := f.logOf(creator, id, adminlog.Settings, mtproto.Predicate_channelAdminLogEventActionToggleAntiSpam)
	if len(logged) != 1 || !mtproto.FromBool(logged[0].GetAction().GetNewValue_BOOL()) || logged[0].GetUserId() != deleter {
		t.Errorf("the recent actions: %v", logged)
	}
	_, err = toggle(creator, true)
	wantErr(t, "again", err, mtproto.ErrChatNotModified)

	f.exec("UPDATE channels SET participants_count = 1 WHERE id = ?", id)
	if _, err = toggle(creator, false); err != nil {
		t.Errorf("off in a small group: %v", err)
	}
	if mc, err = f.c.ChannelGetMutableChannel(&channel.TLChannelGetMutableChannel{ChannelId: id}); err != nil || mc.GetChannel().GetAntispam() {
		t.Errorf("the channel after off: %v, %v", mc.GetChannel(), err)
	}

	other := f.create(creator)
	f.broadcast(other)
	f.exec("UPDATE channels SET participants_count = ? WHERE id = ?", antispam.MembersMin, other)
	_, err = f.toggleSetting(creator, other, channel.SettingAntispam, true)
	wantErr(t, "a broadcast channel", err, mtproto.ErrChannelInvalid)
}

// TestToggleAutotranslation: one who may change the info of a broadcast
// channel translates it for all; the members see the flag, in the min
// channel too, and the recent actions keep it; a supergroup has none.
func TestToggleAutotranslation(t *testing.T) {
	f := newDiffFixture(t)
	creator, member := itId(), itId()
	id := f.create(creator)
	f.broadcast(id)
	f.member(id, member)
	toggle := func(self, channelId int64, on bool) (*mtproto.Updates, error) {
		return f.toggleSetting(self, channelId, channel.SettingAutotranslation, on)
	}

	_, err := toggle(member, id, true)
	wantErr(t, "a subscriber", err, mtproto.ErrChatAdminRequired)
	r, err := toggle(creator, id, true)
	if err != nil {
		t.Fatal(err)
	}
	if !r.GetChats()[0].GetAutotranslation() {
		t.Errorf("the answer's channel: %v", r.GetChats()[0])
	}
	mc, err := f.c.ChannelGetMutableChannel(&channel.TLChannelGetMutableChannel{ChannelId: id, Id: []int64{member}})
	if err != nil {
		t.Fatal(err)
	}
	if !mc.ToChat(member, 0).GetAutotranslation() || !mc.GetChannel().ToMinChat().GetAutotranslation() {
		t.Errorf("the channel as the members load it: %v", mc.GetChannel())
	}
	logged := f.logOf(creator, id, adminlog.Settings, mtproto.Predicate_channelAdminLogEventActionToggleAutotranslation)
	if len(logged) != 1 || !mtproto.FromBool(logged[0].GetAction().GetNewValue_BOOL()) {
		t.Errorf("the recent actions: %v", logged)
	}
	_, err = toggle(creator, id, true)
	wantErr(t, "again", err, mtproto.ErrChatNotModified)
	if _, err = toggle(creator, id, false); err != nil {
		t.Errorf("off: %v", err)
	}

	group := f.create(creator)
	_, err = toggle(creator, group, true)
	wantErr(t, "a supergroup", err, mtproto.ErrChannelInvalid)
}

// TestAntispamDelete: the antispam deletes the sender's messages it was
// asked to, keeps each in the recent actions as the antispam account's
// deletion and in channel_antispam_deletions; a repeat changes nothing, and
// another's message, or a group whose antispam is off, stays.
func TestAntispamDelete(t *testing.T) {
	f := newDiffFixture(t)
	creator, spammer, other := itId(), itId(), itId()
	id := f.create(creator)
	f.member(id, spammer)
	f.member(id, other)
	f.exec("UPDATE channels SET antispam = 1 WHERE id = ?", id)
	f.send(id, spammer, 2)
	first, second := int32(1), int32(2)
	f.send(id, other, 1)
	foreign := int32(3)

	f.antispamDelete(spammer, id, first, second, foreign)
	if !f.isDeleted(id, first) || !f.isDeleted(id, second) || f.isDeleted(id, foreign) {
		t.Errorf("deleted: %v %v, another's %v", f.isDeleted(id, first), f.isDeleted(id, second), f.isDeleted(id, foreign))
	}
	logged := f.logOf(creator, id, adminlog.Delete, mtproto.Predicate_channelAdminLogEventActionDeleteMessage)
	if len(logged) != 2 {
		t.Fatalf("the recent actions: %v", logged)
	}
	for _, e := range logged {
		if e.GetUserId() != antispam.UserId || e.GetAction().GetMessage().GetFromId().GetUserId() != spammer {
			t.Errorf("the deletion is not the antispam's: %v", e)
		}
	}
	byAdmins, err := f.c.ChannelGetAdminLog(&channel.TLChannelGetAdminLog{SelfId: creator, ChannelId: id, Filter: -1, Admins: []int64{antispam.UserId}, Limit: 100})
	if err != nil || len(byAdmins.GetDatas()) != 2 {
		t.Errorf("the recent actions of the antispam account: %v, %v", byAdmins.GetDatas(), err)
	}
	if n := f.count("SELECT COUNT(*) FROM channel_antispam_deletions WHERE channel_id = ? AND sender_user_id = ? AND `rule` = ? AND rules_version = ?",
		id, spammer, antispam.RuleLink, antispam.RulesVersion); n != 2 {
		t.Errorf("%d deletions kept, want 2", n)
	}

	f.antispamDelete(spammer, id, first, second)
	if n := len(f.logOf(creator, id, adminlog.Delete, mtproto.Predicate_channelAdminLogEventActionDeleteMessage)); n != 2 {
		t.Errorf("a repeat wrote %d recent actions, want none more", n-2)
	}

	f.send(id, spammer, 1)
	f.exec("UPDATE channels SET antispam = 0 WHERE id = ?", id)
	f.antispamDelete(spammer, id, 4)
	if f.isDeleted(id, 4) {
		t.Error("deleted with the antispam off")
	}
	f.antispamDelete(spammer, itId(), 1)
}

// TestAntispamFalsePositive: an admin who may delete messages reports a
// deletion of the antispam as wrong: the sender is exempt in the group and
// the moderators get one report with the message as it was, however often
// the admin reports it; a message the antispam did not delete is false.
func TestAntispamFalsePositive(t *testing.T) {
	f := newDiffFixture(t)
	creator := f.user("Creator")
	spammer, member, editor := f.user("Sam"), itId(), itId()
	id := f.create(creator)
	f.member(id, spammer)
	f.member(id, member)
	f.admin(id, editor, mtproto.ADMIN_CHANGE_INFO)
	f.exec("UPDATE channels SET antispam = 1 WHERE id = ?", id)
	f.send(id, spammer, 2)
	f.antispamDelete(spammer, id, 1)

	_, err := f.falsePositive(member, id, 1)
	wantErr(t, "a member", err, mtproto.ErrChatAdminRequired)
	_, err = f.falsePositive(editor, id, 1)
	wantErr(t, "an admin who changes the info only", err, mtproto.ErrChatAdminRequired)
	if ok, err := f.falsePositive(creator, id, 2); err != nil || ok {
		t.Errorf("a message the antispam left: %v, %v", ok, err)
	}

	for range 2 {
		if ok, err := f.falsePositive(creator, id, 1); err != nil || !ok {
			t.Fatalf("the report: %v, %v", ok, err)
		}
	}
	if n := f.count("SELECT COUNT(*) FROM channel_antispam_exempt WHERE channel_id = ? AND user_id = ?", id, spammer); n != 1 {
		t.Errorf("exempt rows %d, want 1", n)
	}
	if n := f.count("SELECT COUNT(*) FROM channel_antispam_deletions WHERE channel_id = ? AND message_id = 1 AND reported = 1", id); n != 1 {
		t.Error("the deletion is not marked reported")
	}
	if n := f.count("SELECT COUNT(*) FROM moderation_reports WHERE kind = ? AND peer_type = ? AND peer_id = ?",
		moderation.KindAntispamFalsePositive, mtproto.PEER_CHANNEL, id); n != 1 {
		t.Fatalf("%d reports, want 1", n)
	}
	var (
		reporter, target int64
		reason, data     string
	)
	if err = f.box.QueryRow("SELECT reporter_user_id, target_user_id, reason, snapshot FROM moderation_reports WHERE kind = ? AND peer_type = ? AND peer_id = ?",
		moderation.KindAntispamFalsePositive, mtproto.PEER_CHANNEL, id).Scan(&reporter, &target, &reason, &data); err != nil {
		t.Fatal(err)
	}
	if reporter != creator || target != spammer || reason != "not_spam" {
		t.Fatalf("the report: reporter %d, target %d, reason %q", reporter, target, reason)
	}
	var s moderation.Snapshot
	if err = json.Unmarshal([]byte(data), &s); err != nil {
		t.Fatal(err)
	}
	if s.PeerTitle != "t" || len(s.Users) != 1 || s.Users[0].FirstName != "Sam" ||
		len(s.Messages) != 1 || s.Messages[0].Id != 1 || s.Messages[0].FromId != spammer || s.Messages[0].Text != "text" {
		t.Errorf("snapshot %+v", s)
	}

	broadcast := f.create(creator)
	f.broadcast(broadcast)
	_, err = f.falsePositive(creator, broadcast, 1)
	wantErr(t, "a broadcast channel", err, mtproto.ErrChannelInvalid)
}

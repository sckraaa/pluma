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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/adminlog"
)

// logOf is the recent actions of the channel of one predicate, oldest first,
// as self reads them with filter (-1: every kind).
func (f *diffFixture) logOf(self, channelId int64, filter int64, predicate string) []*mtproto.ChannelAdminLogEvent {
	f.t.Helper()
	r, err := f.c.ChannelGetAdminLog(&channel.TLChannelGetAdminLog{SelfId: self, ChannelId: channelId, Filter: filter, Limit: 100})
	if err != nil {
		f.t.Fatal(err)
	}
	var out []*mtproto.ChannelAdminLogEvent
	for i := len(r.GetDatas()) - 1; i >= 0; i-- {
		if e := r.GetDatas()[i]; e.GetAction().GetPredicateName() == predicate {
			out = append(out, e)
		}
	}
	return out
}

func (f *diffFixture) toggleSetting(self, channelId int64, setting int32, on bool) (*mtproto.Updates, error) {
	return f.c.ChannelToggleSetting(&channel.TLChannelToggleSetting{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Setting: setting, Enabled: mtproto.ToBool(on)})
}

// TestAdminLogSettings: slow mode, the hidden history, copy protection, the
// reactions, the auto-delete period and the wallpaper of a supergroup are
// kept with what they were and became, each under its flag of the filter;
// signatures and their profiles of a channel are kept one action per flag.
func TestAdminLogSettings(t *testing.T) {
	f := newDiffFixture(t)
	creator := itId()
	id := f.create(creator)
	f.send(id, creator, 1)

	if _, err := f.slowMode(creator, id, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := f.slowMode(creator, id, 0); err != nil {
		t.Fatal(err)
	}
	slow := f.logOf(creator, id, adminlog.Info, mtproto.Predicate_channelAdminLogEventActionToggleSlowMode)
	if len(slow) != 2 || slow[0].GetAction().GetPrevValue_INT32() != 0 || slow[0].GetAction().GetNewValue_INT32() != 60 ||
		slow[1].GetAction().GetPrevValue_INT32() != 60 || slow[1].GetAction().GetNewValue_INT32() != 0 || slow[0].GetUserId() != creator {
		t.Errorf("slow mode: %v", slow)
	}
	if got := f.logOf(creator, id, adminlog.Settings, mtproto.Predicate_channelAdminLogEventActionToggleSlowMode); len(got) != 0 {
		t.Errorf("slow mode under settings: %v", got)
	}

	for _, s := range []struct {
		setting   int32
		predicate string
	}{
		{channel.SettingPrehistoryHidden, mtproto.Predicate_channelAdminLogEventActionTogglePreHistoryHidden},
		{channel.SettingNoforwards, mtproto.Predicate_channelAdminLogEventActionToggleNoForwards},
	} {
		if _, err := f.toggleSetting(creator, id, s.setting, true); err != nil {
			t.Fatal(err)
		}
		_, err := f.toggleSetting(creator, id, s.setting, true)
		wantErr(t, "the same setting again", err, mtproto.ErrChatNotModified)
		if _, err = f.toggleSetting(creator, id, s.setting, false); err != nil {
			t.Fatal(err)
		}
		got := f.logOf(creator, id, adminlog.Settings, s.predicate)
		if len(got) != 2 || !mtproto.FromBool(got[0].GetAction().GetNewValue_BOOL()) || mtproto.FromBool(got[1].GetAction().GetNewValue_BOOL()) {
			t.Errorf("%s: %v", s.predicate, got)
		}
	}

	if _, err := f.c.ChannelSetAvailableReactions(&channel.TLChannelSetAvailableReactions{
		SelfId: creator, PermAuthKeyId: 1, ChannelId: id, ReactionsType: mtproto.ChatReactionsTypeSome, Reactions: []string{"👍"},
	}); err != nil {
		t.Fatal(err)
	}
	// The same again changes nothing to keep.
	if _, err := f.c.ChannelSetAvailableReactions(&channel.TLChannelSetAvailableReactions{
		SelfId: creator, PermAuthKeyId: 1, ChannelId: id, ReactionsType: mtproto.ChatReactionsTypeSome, Reactions: []string{"👍"},
	}); err != nil {
		t.Fatal(err)
	}
	reactions := f.logOf(creator, id, adminlog.Settings, mtproto.Predicate_channelAdminLogEventActionChangeAvailableReactions)
	if len(reactions) != 1 {
		t.Fatalf("reactions: %v", reactions)
	}
	if a := reactions[0].GetAction(); a.GetPrevValue_CHATREACTIONS().GetPredicateName() != mtproto.Predicate_chatReactionsAll ||
		a.GetNewValue_CHATREACTIONS().GetPredicateName() != mtproto.Predicate_chatReactionsSome || len(a.GetNewValue_CHATREACTIONS().GetReactions()) != 1 {
		t.Errorf("reactions: %v", a)
	}

	if _, err := f.c.ChannelSetHistoryTTL(&channel.TLChannelSetHistoryTTL{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, TtlPeriod: 86400}); err != nil {
		t.Fatal(err)
	}
	ttl := f.logOf(creator, id, adminlog.Info, mtproto.Predicate_channelAdminLogEventActionChangeHistoryTTL)
	if len(ttl) != 1 || ttl[0].GetAction().GetPrevValue_INT32() != 0 || ttl[0].GetAction().GetNewValue_INT32() != 86400 {
		t.Errorf("history ttl: %v", ttl)
	}

	paper := mtproto.MakeTLWallPaperNoFile(&mtproto.WallPaper{Id: 0, Settings: mtproto.MakeTLWallPaperSettings(&mtproto.WallPaperSettings{
		BackgroundColor: mtproto.MakeFlagsInt32(0x112233),
	}).To_WallPaperSettings()}).To_WallPaper()
	if _, err := f.c.ChannelSetChatWallpaper(&channel.TLChannelSetChatWallpaper{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, Wallpaper: paper}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.ChannelSetChatWallpaper(&channel.TLChannelSetChatWallpaper{SelfId: creator, PermAuthKeyId: 1, ChannelId: id}); err != nil {
		t.Fatal(err)
	}
	papers := f.logOf(creator, id, adminlog.Info, mtproto.Predicate_channelAdminLogEventActionChangeWallpaper)
	if len(papers) != 2 {
		t.Fatalf("wallpaper: %v", papers)
	}
	// None is wallPaperNoFile with id 0 and no settings.
	if w := papers[0].GetAction().GetPrevValue_WALLPAPER(); w.GetPredicateName() != mtproto.Predicate_wallPaperNoFile || w.GetSettings() != nil {
		t.Errorf("wallpaper before: %v", w)
	}
	if w := papers[1].GetAction().GetNewValue_WALLPAPER(); w.GetPredicateName() != mtproto.Predicate_wallPaperNoFile || w.GetSettings() != nil ||
		papers[1].GetAction().GetPrevValue_WALLPAPER().GetSettings() == nil {
		t.Errorf("wallpaper removed: %v", papers[1].GetAction())
	}

	broadcast := f.create(creator)
	f.broadcast(broadcast)
	if _, err := f.signatures(creator, broadcast, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.signatures(creator, broadcast, false, false); err != nil {
		t.Fatal(err)
	}
	signatures := f.logOf(creator, broadcast, adminlog.Settings, mtproto.Predicate_channelAdminLogEventActionToggleSignatures)
	profiles := f.logOf(creator, broadcast, adminlog.Settings, mtproto.Predicate_channelAdminLogEventActionToggleSignatureProfiles)
	if len(signatures) != 2 || len(profiles) != 2 || !mtproto.FromBool(profiles[0].GetAction().GetNewValue_BOOL()) || mtproto.FromBool(profiles[1].GetAction().GetNewValue_BOOL()) {
		t.Errorf("signatures %v, profiles %v", signatures, profiles)
	}
	if _, err := f.signatures(creator, broadcast, true, false); err != nil {
		t.Fatal(err)
	}
	if got := f.logOf(creator, broadcast, -1, mtproto.Predicate_channelAdminLogEventActionToggleSignatureProfiles); len(got) != 2 {
		t.Errorf("profiles kept when they did not change: %v", got)
	}
}

// TestAdminLogInvites: a link edited, revoked and deleted is kept under
// invites; who joins by a link is kept with it, and who is let in on a
// request is kept as the one who joined, with the admin as approved_by.
func TestAdminLogInvites(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol, dave := f.user("Creator"), f.user("Bob"), f.user("Carol"), f.user("Dave")
	id := f.create(creator)
	f.send(id, creator, 1)

	e, err := f.export(creator, id, false, 0, 0, "first")
	if err != nil {
		t.Fatal(err)
	}
	hash := inviteHash(t, e)
	if _, err = f.c.ChannelEditExportedChatInvite(&channel.TLChannelEditExportedChatInvite{
		SelfId: creator, ChannelId: id, Link: hash, Revoked: mtproto.BoolFalse, RequestNeeded: mtproto.BoolFalse,
		Fields: channel.InviteFieldTitle, Title: "second",
	}); err != nil {
		t.Fatal(err)
	}
	edits := f.logOf(creator, id, adminlog.Invites, mtproto.Predicate_channelAdminLogEventActionExportedInviteEdit)
	if len(edits) != 1 || edits[0].GetAction().GetPrevInvite().GetTitle().GetValue() != "first" || edits[0].GetAction().GetNewInvite().GetTitle().GetValue() != "second" {
		t.Errorf("edit: %v", edits)
	}
	// The same title again changes nothing the log could show.
	if _, err = f.c.ChannelEditExportedChatInvite(&channel.TLChannelEditExportedChatInvite{
		SelfId: creator, ChannelId: id, Link: hash, Revoked: mtproto.BoolFalse, RequestNeeded: mtproto.BoolFalse,
		Fields: channel.InviteFieldTitle, Title: "second",
	}); err != nil {
		t.Fatal(err)
	}
	if edits = f.logOf(creator, id, adminlog.Invites, mtproto.Predicate_channelAdminLogEventActionExportedInviteEdit); len(edits) != 1 {
		t.Errorf("an edit without a change kept: %v", edits)
	}

	if _, err = f.join(bob, 0, hash); err != nil {
		t.Fatal(err)
	}
	joins := f.logOf(creator, id, adminlog.Join, mtproto.Predicate_channelAdminLogEventActionParticipantJoinByInvite)
	if len(joins) != 1 || joins[0].GetUserId() != bob || joins[0].GetAction().GetInvite().GetLink() != e.GetLink() ||
		joins[0].GetAction().GetViaChatlist() || joins[0].GetAction().GetInvite().GetUsage().GetValue() != 1 {
		t.Errorf("join by link: %v", joins)
	}
	if got := f.logOf(creator, id, -1, mtproto.Predicate_channelAdminLogEventActionParticipantJoin); len(got) != 0 {
		t.Errorf("a join by link kept as a plain join: %v", got)
	}

	// A link of a shared folder marks the join via_chatlist.
	folder, err := f.export(creator, id, false, 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	f.exec("INSERT INTO chatlist_invite_peers (slug, channel_id, link) VALUES (?, ?, ?)", "s"+inviteHash(t, folder), id, inviteHash(t, folder))
	if _, err = f.join(dave, 0, inviteHash(t, folder)); err != nil {
		t.Fatal(err)
	}
	joins = f.logOf(creator, id, adminlog.Join, mtproto.Predicate_channelAdminLogEventActionParticipantJoinByInvite)
	if len(joins) != 2 || !joins[1].GetAction().GetViaChatlist() {
		t.Errorf("join via a folder: %v", joins)
	}

	asking, err := f.export(creator, id, true, 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.join(carol, 0, inviteHash(t, asking))
	wantErr(t, "a request", err, mtproto.ErrInviteRequestSent)
	if _, err = f.hide(creator, id, carol, true); err != nil {
		t.Fatal(err)
	}
	approved := f.logOf(creator, id, adminlog.Join, mtproto.Predicate_channelAdminLogEventActionParticipantJoinByRequest)
	if len(approved) != 1 || approved[0].GetUserId() != carol || approved[0].GetAction().GetApprovedBy() != creator ||
		approved[0].GetAction().GetInvite().GetLink() != asking.GetLink() {
		t.Errorf("approved: %v", approved)
	}

	if _, err = f.c.ChannelEditExportedChatInvite(&channel.TLChannelEditExportedChatInvite{
		SelfId: creator, ChannelId: id, Link: hash, Revoked: mtproto.BoolTrue, RequestNeeded: mtproto.BoolFalse,
	}); err != nil {
		t.Fatal(err)
	}
	revoked := f.logOf(creator, id, adminlog.Invites, mtproto.Predicate_channelAdminLogEventActionExportedInviteRevoke)
	if len(revoked) != 1 || !revoked[0].GetAction().GetInvite().GetRevoked() {
		t.Errorf("revoke: %v", revoked)
	}
	if _, err = f.c.ChannelDeleteExportedChatInvite(&channel.TLChannelDeleteExportedChatInvite{SelfId: creator, ChannelId: id, Link: hash}); err != nil {
		t.Fatal(err)
	}
	deleted := f.logOf(creator, id, adminlog.Invites, mtproto.Predicate_channelAdminLogEventActionExportedInviteDelete)
	if len(deleted) != 1 || deleted[0].GetAction().GetInvite().GetLink() != e.GetLink() {
		t.Errorf("delete: %v", deleted)
	}
	// The joins are not invites.
	if got := f.logOf(creator, id, adminlog.Invites, mtproto.Predicate_channelAdminLogEventActionParticipantJoinByInvite); len(got) != 0 {
		t.Errorf("joins under invites: %v", got)
	}
}

// TestAdminLogPublicRequest: a request to a public group without a link is
// kept with chatInvitePublicJoinRequests.
func TestAdminLogPublicRequest(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.create(creator)
	f.send(id, creator, 1)
	f.public(id)
	if _, err := f.toggleRequests(creator, id, true, false); err != nil {
		t.Fatal(err)
	}
	_, err := f.join(bob, id, "")
	wantErr(t, "a request", err, mtproto.ErrInviteRequestSent)
	if _, err = f.hide(creator, id, bob, true); err != nil {
		t.Fatal(err)
	}
	approved := f.logOf(creator, id, -1, mtproto.Predicate_channelAdminLogEventActionParticipantJoinByRequest)
	if len(approved) != 1 || approved[0].GetAction().GetInvite().GetPredicateName() != mtproto.Predicate_chatInvitePublicJoinRequests {
		t.Errorf("approved: %v", approved)
	}
}

// TestAdminLogForum: the forum turned on, a topic created, renamed, closed,
// pinned, unpinned and deleted are kept, the topics under forums.
func TestAdminLogForum(t *testing.T) {
	f := newDiffFixture(t)
	creator := itId()
	id := f.forum(creator)
	if got := f.logOf(creator, id, adminlog.Settings, mtproto.Predicate_channelAdminLogEventActionToggleForum); len(got) != 1 || !mtproto.FromBool(got[0].GetAction().GetNewValue_BOOL()) {
		t.Errorf("forum on: %v", got)
	}
	topic := f.createTopic(creator, id, "A", 0)
	created := f.logOf(creator, id, adminlog.Forums, mtproto.Predicate_channelAdminLogEventActionCreateTopic)
	if len(created) != 1 || created[0].GetAction().GetTopic().GetId() != topic || created[0].GetAction().GetTopic().GetTitle() != "A" {
		t.Errorf("created: %v", created)
	}
	edit := &channel.TLChannelEditForumTopic{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, TopicId: topic, Fields: channel.TopicFieldTitle, Title: "B", Closed: mtproto.BoolFalse, Hidden: mtproto.BoolFalse}
	if _, err := f.c.ChannelEditForumTopic(edit); err != nil {
		t.Fatal(err)
	}
	edit.Fields, edit.Closed = channel.TopicFieldClosed, mtproto.BoolTrue
	if _, err := f.c.ChannelEditForumTopic(edit); err != nil {
		t.Fatal(err)
	}
	edits := f.logOf(creator, id, adminlog.Forums, mtproto.Predicate_channelAdminLogEventActionEditTopic)
	if len(edits) != 2 || edits[0].GetAction().GetPrevTopic().GetTitle() != "A" || edits[0].GetAction().GetNewTopic().GetTitle() != "B" ||
		edits[1].GetAction().GetPrevTopic().GetClosed() || !edits[1].GetAction().GetNewTopic().GetClosed() {
		t.Errorf("edits: %v", edits)
	}
	pin := func(on bool) {
		t.Helper()
		if _, err := f.c.ChannelUpdatePinnedForumTopic(&channel.TLChannelUpdatePinnedForumTopic{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, TopicId: topic, Pinned: mtproto.ToBool(on)}); err != nil {
			t.Fatal(err)
		}
	}
	pin(true)
	pin(false)
	pins := f.logOf(creator, id, adminlog.Forums, mtproto.Predicate_channelAdminLogEventActionPinTopic)
	if len(pins) != 2 || !pins[0].GetAction().GetNewTopic().GetPinned() || pins[0].GetAction().GetPrevTopic() != nil ||
		pins[1].GetAction().GetNewTopic() != nil || pins[1].GetAction().GetPrevTopic().GetId() != topic {
		t.Errorf("pins: %v", pins)
	}
	if _, err := f.c.ChannelDeleteTopicHistory(&channel.TLChannelDeleteTopicHistory{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, TopMsgId: topic}); err != nil {
		t.Fatal(err)
	}
	deleted := f.logOf(creator, id, adminlog.Forums, mtproto.Predicate_channelAdminLogEventActionDeleteTopic)
	if len(deleted) != 1 || deleted[0].GetAction().GetTopic().GetTitle() != "B" {
		t.Errorf("deleted: %v", deleted)
	}
	// The layout alone is not kept.
	if _, err := f.c.ChannelToggleForum(&channel.TLChannelToggleForum{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, Enabled: mtproto.BoolTrue, Tabs: mtproto.BoolTrue}); err != nil {
		t.Fatal(err)
	}
	if got := f.logOf(creator, id, -1, mtproto.Predicate_channelAdminLogEventActionToggleForum); len(got) != 1 {
		t.Errorf("the tabs kept: %v", got)
	}
}

// TestAdminLogLinkedChat: linking a group to a channel is kept in both,
// and moving the group to another channel is kept in the three it touched.
func TestAdminLogLinkedChat(t *testing.T) {
	f := newDiffFixture(t)
	creator := itId()
	first, second, group := f.create(creator), f.create(creator), f.create(creator)
	f.broadcast(first)
	f.broadcast(second)
	link := func(channelId, groupId int64) {
		t.Helper()
		if _, err := f.c.ChannelSetDiscussionGroup(&channel.TLChannelSetDiscussionGroup{SelfId: creator, PermAuthKeyId: 1, ChannelId: channelId, GroupId: groupId}); err != nil {
			t.Fatal(err)
		}
	}
	link(first, group)
	link(second, group)
	changes := func(channelId int64) [][2]int64 {
		var out [][2]int64
		for _, e := range f.logOf(creator, channelId, adminlog.Info, mtproto.Predicate_channelAdminLogEventActionChangeLinkedChat) {
			out = append(out, [2]int64{e.GetAction().GetPrevValue_INT64(), e.GetAction().GetNewValue_INT64()})
		}
		return out
	}
	if got := changes(first); len(got) != 2 || got[0] != [2]int64{0, group} || got[1] != [2]int64{group, 0} {
		t.Errorf("first channel: %v", got)
	}
	if got := changes(second); len(got) != 1 || got[0] != [2]int64{0, group} {
		t.Errorf("second channel: %v", got)
	}
	if got := changes(group); len(got) != 2 || got[0] != [2]int64{0, first} || got[1] != [2]int64{first, second} {
		t.Errorf("group: %v", got)
	}
	_, err := f.c.ChannelSetDiscussionGroup(&channel.TLChannelSetDiscussionGroup{SelfId: creator, PermAuthKeyId: 1, ChannelId: second, GroupId: group})
	if !errors.Is(err, mtproto.ErrLinkNotModified) {
		t.Errorf("the same link: %v", err)
	}
	if got := changes(group); len(got) != 2 {
		t.Errorf("an unchanged link kept: %v", got)
	}
}

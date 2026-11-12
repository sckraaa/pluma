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

package channel

import (
	"math"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

const (
	viewNow     int32 = 1_800_000_000
	viewCreated int64 = 1_700_000_000
	viewJoined  int64 = 1_750_000_000
	viewer      int64 = 7
)

func testChannel(username string) *ImmutableChannel {
	return &ImmutableChannel{
		Id:                  100,
		AccessHash:          555,
		CreatorUserId:       1,
		Title:               "c",
		Megagroup:           true,
		Noforwards:          true,
		JoinRequest:         true,
		Username:            username,
		ParticipantsCount:   3,
		DefaultBannedRights: mtproto.BANNED_SEND_POLLS,
		SlowmodeSeconds:     30,
		Date:                viewCreated,
	}
}

func participant(role, state, adminRights, bannedRights, untilDate int32) *ImmutableChannelParticipant {
	return &ImmutableChannelParticipant{
		ChannelId:    100,
		UserId:       viewer,
		Role:         role,
		State:        state,
		AdminRights:  adminRights,
		BannedRights: bannedRights,
		UntilDate:    untilDate,
		Date:         viewJoined,
	}
}

// want is what a test expects of the object.
type want struct {
	predicate    string
	left         bool
	creator      bool
	adminRights  int32 // 0: none
	bannedRights int32 // -1: none
	bannedUntil  int32
	untilDate    int32 // channelForbidden; 0: none
	date         int32
}

// The view of a shared folder's link: a private channel of which the
// viewer is no member shows with left and its count; banned stays out.
func TestToInvitedChat(t *testing.T) {
	ch := testChannel("")
	ch.ParticipantsCount = 12
	if got := (&MutableChannel{Channel: ch}).ToInvitedChat(viewer, viewNow); got.GetPredicateName() != mtproto.Predicate_channel || !got.GetLeft() || got.GetParticipantsCount_FLAGINT32().GetValue() != 12 {
		t.Fatalf("a stranger: %v", got)
	}
	banned := &MutableChannel{Channel: ch, Participants: []*ImmutableChannelParticipant{participant(RoleMember, StateKicked, 0, mtproto.BANNED_VIEW_MESSAGES|mtproto.BANNED_SEND_MESSAGES, 0)}}
	banned.Participants[0].UserId = viewer
	if got := banned.ToInvitedChat(viewer, viewNow); got.GetPredicateName() != mtproto.Predicate_channelForbidden {
		t.Fatalf("banned: %v", got)
	}
	member := &MutableChannel{Channel: ch, Participants: []*ImmutableChannelParticipant{participant(RoleMember, StateIn, 0, 0, 0)}}
	member.Participants[0].UserId = viewer
	if got := member.ToInvitedChat(viewer, viewNow); got.GetPredicateName() != mtproto.Predicate_channel || got.GetLeft() {
		t.Fatalf("a member: %v", got)
	}
}

func TestToChat(t *testing.T) {
	const (
		adminRights = mtproto.ADMIN_CHANGE_INFO | mtproto.ADMIN_BAN_USERS
		kick        = mtproto.BANNED_VIEW_MESSAGES | mtproto.BANNED_SEND_MESSAGES
		mute        = mtproto.BANNED_SEND_MESSAGES
		later       = viewNow + 3600
		earlier     = viewNow - 3600
	)
	member := want{predicate: mtproto.Predicate_channel, bannedRights: -1, date: int32(viewJoined)}
	cases := []struct {
		name     string
		username string
		p        *ImmutableChannelParticipant
		want     want
	}{
		{"creator", "", participant(RoleCreator, StateIn, adminRights, 0, 0),
			want{predicate: mtproto.Predicate_channel, creator: true, adminRights: channelperm.CreatorRights, bannedRights: -1, date: int32(viewJoined)}},
		{"admin", "", participant(RoleAdmin, StateIn, adminRights, 0, 0),
			want{predicate: mtproto.Predicate_channel, adminRights: adminRights, bannedRights: -1, date: int32(viewJoined)}},
		{"member", "", participant(RoleMember, StateIn, 0, 0, 0), member},
		{"restricted for good", "", participant(RoleMember, StateIn, 0, mute, 0),
			want{predicate: mtproto.Predicate_channel, bannedRights: mute, date: int32(viewJoined)}},
		{"restricted until later", "", participant(RoleMember, StateIn, 0, mute, later),
			want{predicate: mtproto.Predicate_channel, bannedRights: mute, bannedUntil: later, date: int32(viewJoined)}},
		{"restriction over", "", participant(RoleMember, StateIn, 0, mute, earlier), member},
		{"left a private channel", "", participant(RoleMember, StateLeft, 0, 0, 0),
			want{predicate: mtproto.Predicate_channelForbidden}},
		{"the owner who left a private channel", "", participant(RoleCreator, StateLeft, 0, 0, 0),
			want{predicate: mtproto.Predicate_channel, left: true, bannedRights: -1, date: int32(viewCreated)}},
		{"left a public channel", "pub", participant(RoleMember, StateLeft, 0, 0, 0),
			want{predicate: mtproto.Predicate_channel, left: true, bannedRights: -1, date: int32(viewCreated)}},
		{"kicked for good", "pub", participant(RoleMember, StateKicked, 0, kick, 0),
			want{predicate: mtproto.Predicate_channelForbidden}},
		{"kicked for good, Int32.max", "", participant(RoleMember, StateKicked, 0, kick, math.MaxInt32),
			want{predicate: mtproto.Predicate_channelForbidden}},
		{"banned until later", "pub", participant(RoleMember, StateKicked, 0, kick, later),
			want{predicate: mtproto.Predicate_channelForbidden, untilDate: later}},
		{"ban over, public", "pub", participant(RoleMember, StateKicked, 0, kick, earlier),
			want{predicate: mtproto.Predicate_channel, left: true, bannedRights: -1, date: int32(viewCreated)}},
		{"ban over, private", "", participant(RoleMember, StateKicked, 0, kick, earlier),
			want{predicate: mtproto.Predicate_channelForbidden}},
		{"never joined a public channel", "pub", nil,
			want{predicate: mtproto.Predicate_channel, left: true, bannedRights: -1, date: int32(viewCreated)}},
		{"never joined a private channel", "", nil,
			want{predicate: mtproto.Predicate_channelForbidden}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := testChannel(tc.username)
			got := ch.ToChat(tc.p, viewNow)
			checkChat(t, got, ch, tc.want)

			// The same through MutableChannel, with another user's row
			// beside the viewer's.
			mc := &MutableChannel{Channel: ch, Participants: []*ImmutableChannelParticipant{{ChannelId: 100, UserId: 1, Role: RoleCreator}}}
			if tc.p != nil {
				mc.Participants = append(mc.Participants, tc.p)
			}
			checkChat(t, mc.ToChat(viewer, viewNow), ch, tc.want)

			// Every object must encode at the server's layer.
			x := mtproto.NewEncodeBuf(512)
			if err := got.Encode(x, 228); err != nil {
				t.Fatalf("encode: %v", err)
			}
		})
	}
}

func checkChat(t *testing.T, got *mtproto.Chat, ch *ImmutableChannel, w want) {
	t.Helper()
	if got.GetPredicateName() != w.predicate {
		t.Fatalf("predicate %s, want %s", got.GetPredicateName(), w.predicate)
	}
	if got.GetId() != ch.Id || got.GetTitle() != ch.Title || got.GetMegagroup() != ch.Megagroup || got.GetBroadcast() != ch.Broadcast {
		t.Errorf("id, title or kind: %v", got)
	}
	if w.predicate == mtproto.Predicate_channelForbidden {
		if got.GetAccessHash_INT64() != ch.AccessHash {
			t.Errorf("access_hash %d, want %d", got.GetAccessHash_INT64(), ch.AccessHash)
		}
		if w.untilDate == 0 && got.GetUntilDate() != nil {
			t.Errorf("until_date %v, want none", got.GetUntilDate())
		}
		if w.untilDate != 0 && got.GetUntilDate().GetValue() != w.untilDate {
			t.Errorf("until_date %v, want %d", got.GetUntilDate(), w.untilDate)
		}
		return
	}
	if got.GetMin() {
		t.Error("min set")
	}
	if got.GetAccessHash_FLAGINT64().GetValue() != ch.AccessHash {
		t.Errorf("access_hash %v, want %d", got.GetAccessHash_FLAGINT64(), ch.AccessHash)
	}
	if got.GetLeft() != w.left || got.GetCreator() != w.creator {
		t.Errorf("left %v creator %v, want %v %v", got.GetLeft(), got.GetCreator(), w.left, w.creator)
	}
	if got.GetDate() != w.date {
		t.Errorf("date %d, want %d", got.GetDate(), w.date)
	}
	if r := got.GetAdminRights(); int32(mtproto.MakeChatAdminRightsHelper(r)) != w.adminRights || (w.adminRights == 0) != (r == nil) {
		t.Errorf("admin_rights %v, want %d", r, w.adminRights)
	}
	switch r := got.GetBannedRights(); {
	case w.bannedRights < 0:
		if r != nil {
			t.Errorf("banned_rights %v, want none", r)
		}
	case r == nil || int32(r.ToBannedRights()) != w.bannedRights || r.GetUntilDate() != w.bannedUntil:
		t.Errorf("banned_rights %v, want %d until %d", r, w.bannedRights, w.bannedUntil)
	}
	if got.GetParticipantsCount_FLAGINT32().GetValue() != ch.ParticipantsCount {
		t.Errorf("participants_count %v, want %d", got.GetParticipantsCount_FLAGINT32(), ch.ParticipantsCount)
	}
	if !got.GetNoforwards() || !got.GetJoinRequest() || !got.GetSlowmodeEnabled() {
		t.Errorf("channel flags lost: %v", got)
	}
	if d := got.GetDefaultBannedRights(); d == nil || !d.GetSendPolls() || d.GetUntilDate() != 0 {
		t.Errorf("default_banned_rights %v", d)
	}
	if (ch.Username != "") != (got.GetUsername() != nil) || got.GetUsername().GetValue() != ch.Username {
		t.Errorf("username %v, want %q", got.GetUsername(), ch.Username)
	}
}

func TestSlowmodeNextSendDate(t *testing.T) {
	ch := testChannel("")
	ch.SlowmodeSeconds = 30
	now := int64(viewNow)
	member := participant(RoleMember, StateIn, 0, 0, 0)
	member.LastSendDate = now - 10
	if got := ch.SlowmodeNextSendDate(member, now); got != viewNow+20 {
		t.Errorf("waiting member: %d, want %d", got, viewNow+20)
	}
	member.LastSendDate = now - 30
	if got := ch.SlowmodeNextSendDate(member, now); got != 0 {
		t.Errorf("window over: %d", got)
	}
	member.LastSendDate = now
	admin := participant(RoleAdmin, StateIn, mtproto.ADMIN_BAN_USERS, 0, 0)
	admin.LastSendDate = now
	creator := participant(RoleCreator, StateIn, 0, 0, 0)
	creator.LastSendDate = now
	if ch.SlowmodeNextSendDate(admin, now) != 0 || ch.SlowmodeNextSendDate(creator, now) != 0 {
		t.Error("an admin or the creator waits")
	}
	if ch.SlowmodeNextSendDate(nil, now) != 0 {
		t.Error("one never joined waits")
	}
	guest := &ImmutableChannelParticipant{State: StateLinked, LastSendDate: now}
	if got := ch.SlowmodeNextSendDate(guest, now); got != viewNow+30 {
		t.Errorf("guest of a discussion group: %d", got)
	}
	ch.SlowmodeSeconds = 0
	if ch.SlowmodeNextSendDate(member, now) != 0 {
		t.Error("waits with the slow mode off")
	}
}

func TestSignatureProfilesView(t *testing.T) {
	ch := testChannel("")
	ch.Broadcast, ch.Megagroup, ch.Signatures, ch.SignatureProfiles = true, false, true, true
	if got := ch.ToChat(participant(RoleMember, StateIn, 0, 0, 0), viewNow); !got.GetSignatures() || !got.GetSignatureProfiles() {
		t.Errorf("channel: %v", got)
	}
	if got := ch.ToMinChat(); !got.GetSignatureProfiles() {
		t.Errorf("min channel: %v", got)
	}
	x := mtproto.NewEncodeBuf(512)
	if err := ch.ToMinChat().Encode(x, 228); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

// TestGigagroupView: a broadcast group is a supergroup with gigagroup, in
// the full and the min channel (tdesktop applies it from a min one,
// data_session.cpp:1110-1116), and channelperm reads it.
func TestGigagroupView(t *testing.T) {
	ch := testChannel("pub")
	ch.Gigagroup = true
	for _, got := range []*mtproto.Chat{ch.ToChat(participant(RoleMember, StateIn, 0, 0, 0), viewNow), ch.ToMinChat()} {
		if !got.GetGigagroup() || !got.GetMegagroup() {
			t.Errorf("channel: %v", got)
		}
		x := mtproto.NewEncodeBuf(512)
		if err := got.Encode(x, 228); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	if !ch.Perm().Gigagroup {
		t.Error("channelperm does not read gigagroup")
	}
}

// TestAutotranslationView: a channel translated for all says so in the
// full and the min channel (flags2.15).
func TestAutotranslationView(t *testing.T) {
	ch := testChannel("pub")
	ch.Megagroup, ch.Broadcast, ch.Autotranslation = false, true, true
	for _, got := range []*mtproto.Chat{ch.ToChat(participant(RoleMember, StateIn, 0, 0, 0), viewNow), ch.ToMinChat()} {
		if !got.GetAutotranslation() {
			t.Errorf("channel: %v", got)
		}
		x := mtproto.NewEncodeBuf(512)
		if err := got.Encode(x, 228); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
}

func TestToChatDeleted(t *testing.T) {
	ch := testChannel("pub")
	ch.Deleted = true
	got := ch.ToChat(participant(RoleCreator, StateIn, mtproto.ADMIN_CHANGE_INFO, 0, 0), viewNow)
	if got.GetPredicateName() != mtproto.Predicate_channelForbidden || got.GetUntilDate() != nil {
		t.Fatalf("deleted channel: %v", got)
	}
}

func TestToMinChat(t *testing.T) {
	ch := testChannel("pub")
	ch.Broadcast, ch.Megagroup, ch.Signatures = true, false, true
	got := ch.ToMinChat()
	if got.GetPredicateName() != mtproto.Predicate_channel || !got.GetMin() {
		t.Fatalf("min: %v", got)
	}
	if got.GetAccessHash_FLAGINT64() != nil {
		t.Errorf("access_hash %v in a min channel", got.GetAccessHash_FLAGINT64())
	}
	// Nothing of a viewer, nor what a client keeps from the full object.
	if got.GetLeft() || got.GetCreator() || got.GetAdminRights() != nil || got.GetBannedRights() != nil ||
		got.GetParticipantsCount_FLAGINT32() != nil || got.GetSignatures() {
		t.Errorf("viewer's or full fields in a min channel: %v", got)
	}
	if got.GetUsername().GetValue() != "pub" || !got.GetBroadcast() || got.GetMegagroup() || got.GetTitle() != ch.Title ||
		got.GetDate() != int32(viewCreated) || got.GetPhoto() == nil {
		t.Errorf("min channel: %v", got)
	}
	x := mtproto.NewEncodeBuf(512)
	if err := got.Encode(x, 228); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

func TestVectorChannelListByIdList(t *testing.T) {
	pub, priv := testChannel("pub"), testChannel("")
	priv.Id = 101
	v := &Vector_MutableChannel{Datas: []*MutableChannel{
		{Channel: pub},
		{Channel: priv, Participants: []*ImmutableChannelParticipant{participant(RoleMember, StateIn, 0, 0, 0)}},
		{}, // no channel: skipped
	}}
	got := v.GetChannelListByIdList(viewer)
	if len(got) != 2 || got[0].GetId() != 100 || !got[0].GetLeft() || got[1].GetId() != 101 || got[1].GetLeft() {
		t.Fatalf("got %v", got)
	}
}

func TestCodecRoundTrip(t *testing.T) {
	in := MakeTLMutableChannel(&MutableChannel{
		Channel:      MakeTLImmutableChannel(testChannel("pub")).To_ImmutableChannel(),
		Participants: []*ImmutableChannelParticipant{MakeTLImmutableChannelParticipant(participant(RoleAdmin, StateIn, 3, 0, 0)).To_ImmutableChannelParticipant()},
	}).To_MutableChannel()
	in.Channel.Forum, in.Channel.Deleted, in.Channel.Gigagroup = true, true, true
	in.Channel.Antispam, in.Channel.Autotranslation = true, true
	in.Participants[0].Rank = "boss"
	x := mtproto.NewEncodeBuf(512)
	if err := in.Encode(x, 228); err != nil {
		t.Fatal(err)
	}
	out := &MutableChannel{}
	if err := out.Decode(mtproto.NewDecodeBuf(x.GetBuf())); err != nil {
		t.Fatal(err)
	}
	c, p := out.GetChannel(), out.GetParticipants()
	if c.GetId() != 100 || c.GetUsername() != "pub" || !c.GetForum() || !c.GetDeleted() || !c.GetGigagroup() || !c.GetMegagroup() || c.GetBroadcast() ||
		!c.GetAntispam() || !c.GetAutotranslation() ||
		c.GetDefaultBannedRights() != mtproto.BANNED_SEND_POLLS || c.GetDate() != viewCreated || c.GetSlowmodeSeconds() != 30 {
		t.Errorf("channel %v", c)
	}
	if len(p) != 1 || p[0].GetUserId() != viewer || p[0].GetRole() != RoleAdmin || p[0].GetRank() != "boss" || p[0].GetDate() != viewJoined {
		t.Errorf("participants %v", p)
	}

	// A vector length the input cannot hold is refused before allocation.
	y := mtproto.NewEncodeBuf(64)
	y.Int(int32(mtproto.CRC32_vector))
	y.Int(1 << 30)
	if err := (&Vector_MutableChannel{}).Decode(mtproto.NewDecodeBuf(y.GetBuf())); err == nil {
		t.Error("an impossible vector length was accepted")
	}
}

// TestMember: only a row in the channel, restricted or not, is a member;
// no row (never joined) is none, though the zero state is StateIn.
func TestMember(t *testing.T) {
	for _, c := range []struct {
		name string
		p    *ImmutableChannelParticipant
		want bool
	}{
		{"no row", nil, false},
		{"in", participant(RoleMember, StateIn, 0, 0, 0), true},
		{"left", participant(RoleMember, StateLeft, 0, 0, 0), false},
		{"restricted", participant(RoleMember, StateIn, 0, mtproto.BANNED_SEND_MESSAGES, 0), true},
		{"kicked", participant(RoleMember, StateKicked, 0, mtproto.BANNED_VIEW_MESSAGES, 0), false},
	} {
		if got := c.p.Member(); got != c.want {
			t.Errorf("%s: member %v, want %v", c.name, got, c.want)
		}
	}
}

func TestToChatBlocked(t *testing.T) {
	ch := testChannel("name")
	ch.Blocked = true
	c := ch.ToChat(participant(RoleCreator, StateIn, 0, 0, 0), int32(viewJoined))
	if !c.GetRestricted() || len(c.GetRestrictionReason()) != 1 {
		t.Fatalf("blocked: %+v", c)
	}
	if r := c.GetRestrictionReason()[0]; r.GetPlatform() != "all" || r.GetText() == "" {
		t.Errorf("reason: %+v", r)
	}
	if err := ch.Can(participant(RoleCreator, StateIn, 0, 0, 0), channelperm.SendPlain, int32(viewJoined)); err != mtproto.ErrChannelPrivate {
		t.Errorf("the creator of a blocked channel posts: %v", err)
	}
	ch.Blocked = false
	if c := ch.ToChat(participant(RoleCreator, StateIn, 0, 0, 0), int32(viewJoined)); c.GetRestricted() {
		t.Error("unblocked channel is restricted")
	}
}

// The stories of a channel in its object (task 084): the newest one in the
// feeds, the same for every viewer, in both forms of stories_max_id, and
// stories_hidden of the viewer's own row. A channel without stories in the
// feeds carries no stories_max_id and says stories_unavailable.
func TestToChatStories(t *testing.T) {
	ch := testChannel("news")
	ch.StoriesMaxId = 5
	p := participant(RoleMember, StateIn, 0, 0, 0)
	p.StoriesHidden = true
	got := ch.ToChat(p, viewNow)
	if got.GetStoriesMaxId_FLAGRECENTSTORY().GetMaxId().GetValue() != 5 || got.GetStoriesMaxId_FLAGINT32().GetValue() != 5 || !got.GetStoriesHidden() || got.GetStoriesUnavailable() {
		t.Errorf("a member who hid the stories: %v / %v, hidden %v", got.GetStoriesMaxId_FLAGRECENTSTORY(), got.GetStoriesMaxId_FLAGINT32(), got.GetStoriesHidden())
	}
	// One who never joined the public channel sees the stories too.
	if got = ch.ToChat(nil, viewNow); got.GetStoriesMaxId_FLAGRECENTSTORY().GetMaxId().GetValue() != 5 || got.GetStoriesHidden() {
		t.Errorf("a stranger: %v, hidden %v", got.GetStoriesMaxId_FLAGRECENTSTORY(), got.GetStoriesHidden())
	}
	ch.StoriesMaxId = 0
	if got = ch.ToChat(participant(RoleMember, StateIn, 0, 0, 0), viewNow); got.GetStoriesMaxId_FLAGRECENTSTORY() != nil || got.GetStoriesMaxId_FLAGINT32() != nil || !got.GetStoriesUnavailable() {
		t.Errorf("no stories: %v / %v, unavailable %v", got.GetStoriesMaxId_FLAGRECENTSTORY(), got.GetStoriesMaxId_FLAGINT32(), got.GetStoriesUnavailable())
	}
}

// The group call of a channel in its object (task 093): call_active and
// call_not_empty for every viewer, a min channel included, since tdesktop
// clears what a min channel leaves out (data_session.cpp:1110-1172).
func TestToChatGroupCall(t *testing.T) {
	ch := testChannel("news")
	ch.CallActive, ch.CallNotEmpty = true, true
	for name, got := range map[string]*mtproto.Chat{
		"member":   ch.ToChat(participant(RoleMember, StateIn, 0, 0, 0), viewNow),
		"stranger": ch.ToChat(nil, viewNow),
		"min":      ch.ToMinChat(),
	} {
		if !got.GetCallActive() || !got.GetCallNotEmpty() {
			t.Errorf("%s: call_active %v call_not_empty %v", name, got.GetCallActive(), got.GetCallNotEmpty())
		}
	}
	ch.CallActive, ch.CallNotEmpty = false, false
	if got := ch.ToChat(nil, viewNow); got.GetCallActive() || got.GetCallNotEmpty() {
		t.Errorf("no call: %v %v", got.GetCallActive(), got.GetCallNotEmpty())
	}
}

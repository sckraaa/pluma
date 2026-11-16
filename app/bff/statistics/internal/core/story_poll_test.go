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
	"reflect"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/polls"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"
)

func answer(text string, option string) *mtproto.PollAnswer {
	return mtproto.MakeTLPollAnswer(&mtproto.PollAnswer{
		Text_TEXTWITHENTITIES: mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: text}).To_TextWithEntities(),
		Option:                []byte(option),
	}).To_PollAnswer()
}

// The graph of a poll is in fives of minutes for its first 48 hours, in
// hours up to 60 days, in days after; it ends at the closing date when
// that has passed, and has two points at least.
func TestPollBuckets(t *testing.T) {
	for _, c := range []struct {
		name        string
		date, close int64
		now         int64
		scale       statsgraph.Scale
		points      int
	}{
		{"an hour old", day0, 0, day0 + hour, statsgraph.FiveMin, 13},
		{"two minutes old", day0, 0, day0 + 120, statsgraph.FiveMin, 2},
		{"48 hours old", day0, 0, day0 + 48*hour, statsgraph.FiveMin, 577},
		{"three days old", day0, 0, day0 + 3*day, statsgraph.Hour, 73},
		{"closed after a day", day0, day0 + day, day0 + 90*day, statsgraph.FiveMin, 289},
		{"not closed yet", day0, day0 + 100*day, day0 + 10*day, statsgraph.Hour, 241},
		{"61 days old", day0, 0, day0 + 61*day, statsgraph.Day, 62},
	} {
		b := pollBuckets(&polls.Record{Date: c.date, CloseDate: c.close}, c.now)
		if b.Scale != c.scale || b.Len() != c.points {
			t.Errorf("%s: %v of %d points, want %v of %d", c.name, b.Scale, b.Len(), c.scale, c.points)
		}
	}
}

// A series for each option in the order of the poll, named by its text;
// the votes of an option the poll no longer has are not drawn. A poll
// without votes is the statsGraphError of its text.
func TestVotesGraph(t *testing.T) {
	b := statsgraph.Span(statsgraph.FiveMin, day0, day0+600)
	f := statsgraph.FiveMin.Bucket(day0)
	answers := []*mtproto.PollAnswer{answer("Yes", "0"), answer("No", "1")}
	g := votesGraph(b, answers, []dao.KeyCount{
		{B: f, K: "1", N: 2}, {B: f + 2, K: "0", N: 1}, {B: f + 1, K: "7", N: 5}, {B: f + 9, K: "0", N: 4},
	}, "none")
	if len(g.Series) != 2 || g.Series[0].Name != "Yes" || g.Series[1].Name != "No" {
		t.Fatalf("series %+v", g.Series)
	}
	if !reflect.DeepEqual(g.Series[0].Values, []int64{0, 0, 1}) || !reflect.DeepEqual(g.Series[1].Values, []int64{2, 0, 0}) {
		t.Errorf("values %v %v", g.Series[0].Values, g.Series[1].Values)
	}
	if g.Type != statsgraph.Bar || !g.Stacked || g.YScaled || g.Subchart {
		t.Errorf("kind %+v", g)
	}
	out, err := g.Build(langpack.StatsText, "en", false)
	if err != nil || out.GetPredicateName() != mtproto.Predicate_statsGraph {
		t.Fatalf("build %v %v", out, err)
	}

	empty := votesGraph(b, answers, []dao.KeyCount{{B: f + 1, K: "7", N: 5}}, langpack.StatsText(langpack.StatsNoVotes, "ru"))
	out, err = empty.Build(langpack.StatsText, "ru", false)
	if err != nil {
		t.Fatal(err)
	}
	if out.GetPredicateName() != mtproto.Predicate_statsGraphError || out.GetError() != langpack.StatsText(langpack.StatsNoVotes, "ru") {
		t.Errorf("no votes: %v", out)
	}
}

func TestForwardsLimit(t *testing.T) {
	for in, want := range map[int32]int{-5: 1, 0: 1, 1: 1, 20: 20, 100: 100, 101: 100, 1 << 30: 100} {
		if got := forwardsLimit(in); got != want {
			t.Errorf("limit %d: %d, want %d", in, got, want)
		}
	}
}

func inputPeerChannel(h int64) *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: channelId, AccessHash: h}).To_InputPeer()
}

// The access to the statistics of stories and of their public forwards:
// the story of a user has none, the rest is the access to the statistics
// of the channel, of a supergroup too. Nothing is read before the access
// is checked.
func TestStoryAccess(t *testing.T) {
	user := mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 5, AccessHash: 1}).To_InputPeer()
	member := participant(channelpb.RoleMember, 0)
	for _, c := range []struct {
		name string
		peer *mtproto.InputPeer
		mc   *channelpb.MutableChannel
		want error
	}{
		{"a user", user, mutable(channelOpts{broadcast: true}, participant(channelpb.RoleCreator, 0)), mtproto.ErrPeerIdInvalid},
		{"no peer", nil, mutable(channelOpts{broadcast: true}, participant(channelpb.RoleCreator, 0)), mtproto.ErrPeerIdInvalid},
		{"a wrong hash", inputPeerChannel(hash + 1), mutable(channelOpts{broadcast: true}, participant(channelpb.RoleCreator, 0)), mtproto.ErrChannelInvalid},
		{"a member", inputPeerChannel(hash), mutable(channelOpts{broadcast: true}, member), errChatAdminRequired},
		{"a supergroup admin without ban_users", inputPeerChannel(hash), mutable(channelOpts{}, participant(channelpb.RoleAdmin, mtproto.ADMIN_POST_STORIES)), errChatAdminRequired},
		{"a private channel never joined", inputPeerChannel(hash), mutable(channelOpts{broadcast: true}, nil), mtproto.ErrChannelPrivate},
	} {
		core := testCore(t, fakeChannels{mc: c.mc}, newSigner(t), day0)
		if _, err := core.StatsGetStoryStats(&mtproto.TLStatsGetStoryStats{Peer: c.peer, Id: 1}); !sameError(err, c.want) {
			t.Errorf("story stats, %s: %v, want %v", c.name, err, c.want)
		}
		if _, err := core.StatsGetStoryPublicForwards(&mtproto.TLStatsGetStoryPublicForwards{Peer: c.peer, Id: 1, Limit: 10}); !sameError(err, c.want) {
			t.Errorf("story forwards, %s: %v, want %v", c.name, err, c.want)
		}
	}
}

// The public forwards of a post are those of a broadcast channel's post,
// for whoever sees its statistics; an offset this server did not give is
// OFFSET_INVALID.
func TestMessageForwardsAccess(t *testing.T) {
	creator := participant(channelpb.RoleCreator, 0)
	for _, c := range []struct {
		name string
		mc   *channelpb.MutableChannel
		in   *mtproto.TLStatsGetMessagePublicForwards5F150144
		want error
	}{
		{"a wrong hash", mutable(channelOpts{broadcast: true}, creator), &mtproto.TLStatsGetMessagePublicForwards5F150144{Channel: inputChannel(hash + 1), MsgId: 1}, mtproto.ErrChannelInvalid},
		{"a supergroup", mutable(channelOpts{}, creator), &mtproto.TLStatsGetMessagePublicForwards5F150144{Channel: inputChannel(hash), MsgId: 1}, mtproto.ErrBroadcastRequired},
		{"a subscriber", mutable(channelOpts{broadcast: true}, participant(channelpb.RoleMember, 0)), &mtproto.TLStatsGetMessagePublicForwards5F150144{Channel: inputChannel(hash), MsgId: 1}, errChatAdminRequired},
		{"no post", mutable(channelOpts{broadcast: true}, creator), &mtproto.TLStatsGetMessagePublicForwards5F150144{Channel: inputChannel(hash), MsgId: 0}, mtproto.ErrMessageIdInvalid},
	} {
		core := testCore(t, fakeChannels{mc: c.mc}, newSigner(t), day0)
		if _, err := core.StatsGetMessagePublicForwards5F150144(c.in); !sameError(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	core := testCore(t, fakeChannels{mc: mutable(channelOpts{broadcast: true}, creator)}, newSigner(t), day0)
	for _, offset := range []string{"x", "MTox", "MToyOjM6NA"} {
		if _, err := core.publicForwards(dao.ForwardSource{ChannelId: channelId, Id: 1}, offset, 10); !sameError(err, mtproto.ErrOffsetInvalid) {
			t.Errorf("offset %q: %v", offset, err)
		}
	}
}

// Every refusal of stats.getPollStats is PEER_ID_INVALID, the one error of
// its page, read before any poll.
func TestPollAccess(t *testing.T) {
	creator := participant(channelpb.RoleCreator, 0)
	for _, c := range []struct {
		name string
		peer *mtproto.InputPeer
		id   int32
		mc   *channelpb.MutableChannel
	}{
		{"no peer", mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(), 1, mutable(channelOpts{broadcast: true}, creator)},
		{"no message", inputPeerChannel(hash), 0, mutable(channelOpts{broadcast: true}, creator)},
		{"a wrong hash", inputPeerChannel(hash + 1), 1, mutable(channelOpts{broadcast: true}, creator)},
		{"a deleted channel", inputPeerChannel(hash), 1, mutable(channelOpts{broadcast: true, deleted: true}, creator)},
		{"a private channel never joined", inputPeerChannel(hash), 1, mutable(channelOpts{broadcast: true}, nil)},
	} {
		core := testCore(t, fakeChannels{mc: c.mc}, newSigner(t), day0)
		if _, err := core.StatsGetPollStats(&mtproto.TLStatsGetPollStats{Peer: c.peer, MsgId: c.id}); !sameError(err, mtproto.ErrPeerIdInvalid) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

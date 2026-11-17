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
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	viewer    = int64(1001)
	channelId = int64(77)
	hash      = int64(4242)
	// day0 is a Monday, 2026-11-02 00:00 UTC.
	day0 = int64(1793577600)
	hour = int64(3600)
	day  = int64(secondsPerDay)
)

// fakeChannels answers channel.getMutableChannel with mc, or err.
type fakeChannels struct {
	mc  *channelpb.MutableChannel
	err error
}

func (f fakeChannels) ChannelGetMutableChannel(_ context.Context, in *channelpb.TLChannelGetMutableChannel) (*channelpb.MutableChannel, error) {
	if f.err != nil {
		return nil, f.err
	}
	if in.ChannelId != f.mc.GetChannel().GetId() {
		return nil, mtproto.ErrChannelInvalid
	}
	return f.mc, nil
}

func newSigner(t *testing.T) *statsgraph.Signer {
	t.Helper()
	key := make([]byte, statsgraph.MinKeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	s, err := statsgraph.NewSigner(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func testCore(t *testing.T, ch svc.Channels, signer *statsgraph.Signer, now int64) *StatisticsCore {
	t.Helper()
	ctx := context.Background()
	return &StatisticsCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Channels: ch, Signer: signer, Flight: new(singleflight.Group)},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: viewer, LangCode: "ru"},
		now:    now,
	}
}

type channelOpts struct {
	broadcast, deleted, blocked bool
	username                    string
}

func mutable(o channelOpts, me *channelpb.ImmutableChannelParticipant) *channelpb.MutableChannel {
	mc := &channelpb.MutableChannel{
		Channel: channelpb.MakeTLImmutableChannel(&channelpb.ImmutableChannel{
			Id: channelId, AccessHash: hash, Broadcast: o.broadcast, Megagroup: !o.broadcast,
			Deleted: o.deleted, Blocked: o.blocked, Username: o.username, Title: "c", Date: day0,
		}).To_ImmutableChannel(),
	}
	if me != nil {
		mc.Participants = []*channelpb.ImmutableChannelParticipant{me}
	}
	return channelpb.MakeTLMutableChannel(mc).To_MutableChannel()
}

func participant(role, rights int32) *channelpb.ImmutableChannelParticipant {
	return channelpb.MakeTLImmutableChannelParticipant(&channelpb.ImmutableChannelParticipant{
		ChannelId: channelId, UserId: viewer, Role: role, AdminRights: rights, Date: day0,
	}).To_ImmutableChannelParticipant()
}

func inputChannel(h int64) *mtproto.InputChannel {
	return mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: channelId, AccessHash: h}).To_InputChannel()
}

func sameError(got, want error) bool {
	if got == nil || want == nil {
		return got == want
	}
	a, b := status.Convert(got), status.Convert(want)
	return a.Code() == b.Code() && a.Message() == b.Message()
}

// The access to the statistics: every error of the methods' pages in its
// case, and the rights of channelperm.CanViewStats.
func TestStatsChannel(t *testing.T) {
	creator := participant(channelpb.RoleCreator, 0)
	poster := participant(channelpb.RoleAdmin, mtproto.ADMIN_POST_MESSAGES)
	banner := participant(channelpb.RoleAdmin, mtproto.ADMIN_BAN_USERS)
	member := participant(channelpb.RoleMember, 0)
	left := participant(channelpb.RoleCreator, 0)
	left.State = channelpb.StateLeft
	for _, c := range []struct {
		name string
		mc   *channelpb.MutableChannel
		in   *mtproto.InputChannel
		kind int
		want error
	}{
		{"creator", mutable(channelOpts{broadcast: true}, creator), inputChannel(hash), broadcastOnly, nil},
		{"admin who posts", mutable(channelOpts{broadcast: true}, poster), inputChannel(hash), broadcastOnly, nil},
		{"admin who only bans, channel", mutable(channelOpts{broadcast: true}, banner), inputChannel(hash), broadcastOnly, errChatAdminRequired},
		{"admin who bans, group", mutable(channelOpts{}, banner), inputChannel(hash), megagroupOnly, nil},
		{"subscriber", mutable(channelOpts{broadcast: true}, member), inputChannel(hash), broadcastOnly, errChatAdminRequired},
		{"reader of a public channel", mutable(channelOpts{broadcast: true, username: "pub"}, nil), inputChannel(hash), broadcastOnly, errChatAdminRequired},
		{"stranger to a private channel", mutable(channelOpts{broadcast: true}, nil), inputChannel(hash), broadcastOnly, mtproto.ErrChannelPrivate},
		{"creator who left", mutable(channelOpts{broadcast: true}, left), inputChannel(hash), broadcastOnly, mtproto.ErrChannelPrivate},
		{"deleted", mutable(channelOpts{broadcast: true, deleted: true}, creator), inputChannel(hash), broadcastOnly, mtproto.ErrChannelPrivate},
		{"blocked", mutable(channelOpts{broadcast: true, blocked: true}, creator), inputChannel(hash), broadcastOnly, mtproto.ErrChannelPrivate},
		{"wrong access hash", mutable(channelOpts{broadcast: true}, creator), inputChannel(hash + 1), broadcastOnly, mtproto.ErrChannelInvalid},
		{"empty input", mutable(channelOpts{broadcast: true}, creator), mtproto.MakeTLInputChannelEmpty(nil).To_InputChannel(), broadcastOnly, mtproto.ErrChannelInvalid},
		{"group for a channel's method", mutable(channelOpts{}, creator), inputChannel(hash), broadcastOnly, mtproto.ErrBroadcastRequired},
		{"channel for a group's method", mutable(channelOpts{broadcast: true}, creator), inputChannel(hash), megagroupOnly, mtproto.ErrMegagroupRequired},
	} {
		core := testCore(t, fakeChannels{mc: c.mc}, newSigner(t), day0+10*day)
		if _, err := core.statsChannel(c.in, c.kind); !sameError(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

// Without the key the statistics are off: ERR_ENTERPRISE_IS_BLOCKED, before
// anything is read.
func TestDisabled(t *testing.T) {
	core := testCore(t, fakeChannels{err: fmt.Errorf("not to be read")}, nil, day0)
	if _, err := core.StatsGetBroadcastStats(&mtproto.TLStatsGetBroadcastStats{Channel: inputChannel(hash)}); !sameError(err, mtproto.ErrEnterpriseIsBlocked) {
		t.Errorf("getBroadcastStats: %v", err)
	}
	if _, err := core.StatsGetMessageStats(&mtproto.TLStatsGetMessageStats{Channel: inputChannel(hash), MsgId: 1}); !sameError(err, mtproto.ErrEnterpriseIsBlocked) {
		t.Errorf("getMessageStats: %v", err)
	}
	if _, err := core.StatsLoadAsyncGraph(&mtproto.TLStatsLoadAsyncGraph{Token: "v1.x.y"}); !sameError(err, mtproto.ErrEnterpriseIsBlocked) {
		t.Errorf("loadAsyncGraph: %v", err)
	}
}

// A zoom token of another key, of another viewer, of a kind not served or
// of another peer type is GRAPH_INVALID_RELOAD; an old one
// GRAPH_EXPIRED_RELOAD; all before the channel is read.
func TestLoadAsyncGraphTokens(t *testing.T) {
	now := day0 + 10*day
	signer := newSigner(t)
	core := testCore(t, fakeChannels{err: fmt.Errorf("not to be read")}, signer, now)
	token := statsgraph.Token{Kind: "growth", PeerType: mtproto.PEER_CHANNEL, PeerID: channelId, Viewer: viewer, Lang: "ru", Expires: now + 60}
	sign := func(s *statsgraph.Signer, tok statsgraph.Token) string {
		str, err := s.Sign(tok)
		if err != nil {
			t.Fatal(err)
		}
		return str
	}
	other := token
	other.Viewer = viewer + 1
	unknown := token
	unknown.Kind = "unknown"
	user := token
	user.PeerType = mtproto.PEER_USER
	old := token
	old.Expires = now
	for _, c := range []struct {
		name, token string
		want        error
	}{
		{"forged", sign(newSigner(t), token), mtproto.ErrGraphInvalidReload},
		{"garbage", "not a token", mtproto.ErrGraphInvalidReload},
		{"another viewer's", sign(signer, other), mtproto.ErrGraphInvalidReload},
		{"unknown kind", sign(signer, unknown), mtproto.ErrGraphInvalidReload},
		{"peer of a user", sign(signer, user), mtproto.ErrGraphInvalidReload},
		{"expired", sign(signer, old), mtproto.ErrGraphExpiredReload},
	} {
		if _, err := core.StatsLoadAsyncGraph(&mtproto.TLStatsLoadAsyncGraph{Token: c.token, X: wrapperspb.Int64(now * 1000)}); !sameError(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

// The members at the end of each day: the count of now less the joins and
// leaves after each day.
func TestGrowth(t *testing.T) {
	b := statsgraph.Span(statsgraph.Day, day0, day0+3*day)
	first := statsgraph.Day.Bucket(day0)
	rows := []dao.MemberCount{
		{B: first, Joined: true, N: 3},
		{B: first + 1, Joined: true, N: 2},
		{B: first + 1, Joined: false, N: 1},
		{B: first + 3, Joined: false, N: 1},
		{B: first + 9, Joined: true, N: 50}, // out of the window
	}
	if got, want := growth(b, rows, 10), []int64{10, 11, 11, 10}; !reflect.DeepEqual(got, want) {
		t.Errorf("growth %v, want %v", got, want)
	}
	// Never below zero, whatever the events say.
	if got, want := growth(b, rows, 0), []int64{0, 1, 1, 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("growth from nothing %v, want %v", got, want)
	}
}

// Mutings and unmutings by day: a muting for good, a muting for a time that
// runs out (an unmuting at its time, also after the last row), a change of
// the time of a muting (no new muting), the first row of a member as it
// is, and nothing counted after end.
func TestMutes(t *testing.T) {
	b := statsgraph.Span(statsgraph.Day, day0, day0+4*day)
	events := []dao.MuteEvent{
		// 1: muted for good on day 0, unmuted on day 2.
		{UserId: 1, Date: day0 + hour, MuteUntil: math.MaxInt32},
		{UserId: 1, Date: day0 + 2*day, MuteUntil: 0},
		// 2: muted on day 0 until day 1; muted again on day 3 until day 3,
		// moved to day 9 (no new muting).
		{UserId: 2, Date: day0 + 2*hour, MuteUntil: int32(day0 + day + hour)},
		{UserId: 2, Date: day0 + 3*day, MuteUntil: int32(day0 + 3*day + 5*hour)},
		{UserId: 2, Date: day0 + 3*day + hour, MuteUntil: int32(day0 + 9*day)},
		// 3: the first row is an unmuting, on day 1.
		{UserId: 3, Date: day0 + day, MuteUntil: 0},
		// 4: muted on day 4 until a time after end.
		{UserId: 4, Date: day0 + 4*day, MuteUntil: int32(day0 + 4*day + 20*hour)},
	}
	muted, unmuted := mutes(b, events, day0+4*day+hour)
	if want := []int64{2, 0, 0, 1, 1}; !reflect.DeepEqual(muted, want) {
		t.Errorf("muted %v, want %v", muted, want)
	}
	if want := []int64{0, 2, 1, 0, 0}; !reflect.DeepEqual(unmuted, want) {
		t.Errorf("unmuted %v, want %v", unmuted, want)
	}
}

// The zoom: seven days of hours around the day of x, cut to the graph's
// window and to now, an x out of the window taken as its nearest day, and
// two hours at least.
func TestZoomHours(t *testing.T) {
	now := day0 + 20*day + 10*hour
	check := func(name string, b statsgraph.Buckets, from, to int64) {
		t.Helper()
		if b.Scale != statsgraph.Hour || statsgraph.Hour.Start(b.First) != from || statsgraph.Hour.Start(b.Last) != to {
			t.Errorf("%s: hours %d..%d, want %d..%d", name, statsgraph.Hour.Start(b.First), statsgraph.Hour.Start(b.Last), from, to)
		}
	}
	noon := func(d int64) int64 { return (day0+d*day)*1000 + 43200*1000 }
	check("middle", zoomHours(day0, now, noon(10)), day0+7*day, day0+14*day-hour)
	check("today", zoomHours(day0, now, noon(20)), day0+17*day, day0+20*day+10*hour)
	check("first day", zoomHours(day0+5*hour, now, noon(0)), day0+5*hour, day0+4*day-hour)
	check("after now", zoomHours(day0, now, noon(400)), day0+17*day, day0+20*day+10*hour)
	check("before the window", zoomHours(day0+10*day, now, noon(-5)), day0+10*day, day0+14*day-hour)
	// A graph that began a minute ago has one hour: the one before is
	// added.
	later := now + 30*60
	check("within an hour", zoomHours(later-60, later, noon(20)), day0+20*day+9*hour, day0+20*day+10*hour)
}

// The emotions of reactions: the standard ones by the table, with or
// without the variation selector, everything else (custom emoji, other
// emoji) among the others; the two tables are apart and hold no selector.
func TestEmotionOf(t *testing.T) {
	for key, want := range map[string]int{
		"e👍":       emotionPositive,
		"e❤":       emotionPositive,
		"e❤\uFE0F": emotionPositive,
		"e❤‍🔥":     emotionPositive,
		"e👎":       emotionNegative,
		"e😢":       emotionNegative,
		"e🤔":       emotionOther,
		"c5368324": emotionOther,
		"":         emotionOther,
	} {
		if got := emotionOf(key); got != want {
			t.Errorf("%q: %d, want %d", key, got, want)
		}
	}
	seen := map[string]bool{}
	for _, e := range append(append([]string{}, positiveReactions...), negativeReactions...) {
		if seen[e] {
			t.Errorf("%q twice", e)
		}
		seen[e] = true
		if strings.ContainsRune(e, '\uFE0F') {
			t.Errorf("%q has the variation selector", e)
		}
	}
}

// The members by language: those of now, with the joins and leaves after
// each day undone; before the recording, those of then that joined by
// that day; the languages not named and the users without a session among
// the others.
func TestLanguageShares(t *testing.T) {
	b := statsgraph.Span(statsgraph.Day, day0, day0+3*day)
	since := day0 + day + 12*hour
	// Now: 1 (en), 2 (ru), 4 (ru); 3 (en) left on day 2; 4 joined on day 3;
	// 5 (xx) joined on day 0, before the recording, and is in.
	members := map[int64]bool{1: true, 2: true, 4: true, 5: true}
	joinedAt := map[int64]int64{1: day0 - day, 2: day0 + day, 3: day0 - day, 4: day0 + 3*day, 5: day0 + hour}
	events := []dao.MemberEvent{
		{UserId: 4, Date: day0 + 3*day + hour, Joined: true},
		{UserId: 3, Date: day0 + 2*day + hour, Joined: false},
	}
	lang := map[int64]string{1: "PlumaStatsLang_en", 2: "PlumaStatsLang_ru", 3: "PlumaStatsLang_en", 4: "PlumaStatsLang_ru", 5: ""}
	g := languageShares(b, members, joinedAt, events, since, func(u int64) string { return lang[u] })
	got := map[string][]int64{}
	for _, s := range g.Series {
		got[s.NameKey] = s.Values
	}
	want := map[string][]int64{
		// Day 0 is before the recording: 1, 3 and 5 had joined; day 1
		// ends after it: 1, 2, 3, 5; day 2: 3 left; day 3: 4 joined.
		"PlumaStatsLang_en":          {2, 2, 1, 1},
		"PlumaStatsLang_ru":          {0, 1, 1, 2},
		langpack.StatsOtherLanguages: {1, 1, 1, 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("languages %v, want %v", got, want)
	}
	if !g.Percentage || !g.Stacked || g.Type != statsgraph.Area {
		t.Errorf("graph %+v", g)
	}
	if _, err := g.Build(langpack.StatsText, "en", false); err != nil {
		t.Fatal(err)
	}
}

// More than the top languages: the rest summed into the others, with the
// languages not named.
func TestLanguageSharesCollapse(t *testing.T) {
	b := statsgraph.Span(statsgraph.Day, day0, day0+day)
	codes := []string{"en", "ru", "uk", "de", "fr", "es", "it", "pl", "tr", "ar", "fa"}
	members := map[int64]bool{}
	lang := map[int64]string{}
	for i, c := range codes {
		key, ok := langpack.StatsLanguageKey(c)
		if !ok {
			t.Fatalf("no key of %s", c)
		}
		for j := 0; j <= i; j++ {
			u := int64(100*i + j)
			members[u] = true
			lang[u] = key
		}
	}
	members[9999] = true
	g := languageShares(b, members, map[int64]int64{}, nil, 0, func(u int64) string { return lang[u] })
	if len(g.Series) != topLanguages+1 {
		t.Fatalf("%d series, want %d", len(g.Series), topLanguages+1)
	}
	other := g.Series[topLanguages]
	// The two smallest languages (1 and 2 members) and the user without one.
	if other.NameKey != langpack.StatsOtherLanguages || other.Values[0] != 4 {
		t.Errorf("others %+v", other)
	}
}

// series puts each count in its key's series, the unknown keys in the
// other's, and leaves out the buckets outside.
func TestSeries(t *testing.T) {
	b := statsgraph.Span(statsgraph.Day, day0, day0+day)
	first := statsgraph.Day.Bucket(day0)
	rows := []dao.Count{{B: first, K: 1, N: 2}, {B: first + 1, K: 0, N: 3}, {B: first + 1, K: 7, N: 1}, {B: first + 5, K: 1, N: 9}}
	if got, want := series(b, rows, []int64{1, 0}, 1), [][]int64{{2, 0}, {0, 4}}; !reflect.DeepEqual(got, want) {
		t.Errorf("series %v, want %v", got, want)
	}
	if got, want := total(b, rows), []int64{2, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("total %v, want %v", got, want)
	}
}

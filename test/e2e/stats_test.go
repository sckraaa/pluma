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

//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The statistics of channels, supergroups, posts, stories and polls (task
// 103, ADR 0066) end to end, through the session's routing of
// RPCStatistics to bff. The statistics count from tables written as things
// happen, and a graph of days needs two days to be drawn: the tests move
// the date of the channel, the post or the story into the past in the
// stack's MySQL, and the start of the recording of events
// (stats_collection) at least as far, so that the graphs of a channel made
// a minute ago have their days. The admin reads in Russian, so the names of
// the series are checked against the server's Russian texts.

const (
	// statsPast is how far the tests move a channel into the past: four
	// days of graph.
	statsPast = 3 * 86400
	// statsPostPast is how far they move a post or a story: four hours of
	// graph, still hourly (48 hours or younger).
	statsPostPast = 3 * 3600
	// statsHourMs is the step of an hourly graph, which iOS takes for one
	// (MessageStatistics.swift:104-116).
	statsHourMs = 3600 * 1000
)

// statsRecordSince moves the start of the recording of every kind of
// events of the statistics to statsPast ago at least, for the graphs of
// joins, mutes, views by source and readers; an earlier start stays. The
// stand is the tests' own.
func statsRecordSince(t *testing.T) {
	t.Helper()
	storiesSQL(t, fmt.Sprintf("UPDATE stats_collection SET since = LEAST(since, UNIX_TIMESTAMP() - %d)", statsPast))
}

// statsChannelPast moves the date of the channel statsPast into the past.
func statsChannelPast(t *testing.T, channelId int64) {
	t.Helper()
	storiesSQL(t, fmt.Sprintf("UPDATE channels SET date = date - %d WHERE id = %d", statsPast, channelId))
}

func statsInput(u *user, ch *mtproto.Chat) *mtproto.InputChannel {
	return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
}

func statsPeer(u *user, ch *mtproto.Chat) *mtproto.InputPeer {
	return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
}

// statsJoinByLink brings the users into the channel of admin by its
// permanent link.
func statsJoinByLink(ctx context.Context, t *testing.T, admin *user, ch *mtproto.Chat, users ...*user) {
	t.Helper()
	hash := channelInviteHash(ctx, t, admin, statsInput(admin, ch))
	for _, u := range users {
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        hash,
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}
}

// statsPublicChannel makes a broadcast channel of u with a public name
// built from u's phone and suffix.
func statsPublicChannel(ctx context.Context, t *testing.T, u *user, suffix string) *mtproto.Chat {
	t.Helper()
	_, ch := createChannel(ctx, t, u, "public "+suffix, false)
	name := "st" + strings.TrimPrefix(u.phone, "+") + suffix
	if _, err := u.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{
		Constructor: u.c.Constructor(mtproto.Predicate_channels_updateUsername),
		Channel:     statsInput(u, ch),
		Username:    name,
	}); err != nil {
		t.Fatalf("channels.updateUsername: %v", err)
	}
	return ch
}

// statsViews counts u's views of the posts ids of the channel, as the
// clients ask for the posts on the screen.
func statsViews(ctx context.Context, t *testing.T, u *user, ch *mtproto.Chat, ids ...int32) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetMessagesViews{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getMessagesViews),
		Peer:        statsPeer(u, ch), Id: ids, Increment: mtproto.BoolTrue,
	}); err != nil {
		t.Fatalf("messages.getMessagesViews: %v", err)
	}
}

// statsForward forwards the post of from to the channel to as u.
func statsForward(ctx context.Context, t *testing.T, u *user, from *mtproto.Chat, post int32, to *mtproto.Chat) int32 {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_forwardMessages),
		FromPeer:    statsPeer(u, from), Id: []int32{post}, RandomId: []int64{randomID()},
		ToPeer: statsPeer(u, to),
	})
	if err != nil {
		t.Fatalf("messages.forwardMessages: %v", err)
	}
	return sentID(t, o)
}

// statsChart is a statsGraph as the clients read it.
type statsChart struct {
	x []int64
	// series are the y columns by their names; colors their colors.
	series    map[string][]int64
	colors    map[string]string
	zoomToken string
}

// sum is the total of the series named name.
func (c *statsChart) sum(name string) int64 {
	var n int64
	for _, v := range c.series[name] {
		n += v
	}
	return n
}

// checkStep checks that the points of c are step milliseconds apart.
func (c *statsChart) checkStep(t *testing.T, field string, step int64) {
	t.Helper()
	for i := 1; i < len(c.x); i++ {
		if c.x[i]-c.x[i-1] != step {
			t.Errorf("%s: x %d and %d are %d ms apart, want %d", field, c.x[i-1], c.x[i], c.x[i]-c.x[i-1], step)
			return
		}
	}
}

// statsGraphOf reads g as the clients do: nil for a statsGraphError, which
// they hide; otherwise the JSON must hold what their parsers need (the
// rules of ADR 0066, p. 4): compact, columns first with x first (iOS looks
// for `["x",`, MessageStatistics.swift:106-110; Android reads x's length
// for every column, ChartData.java:72), types, names and colors for every
// y column (iOS ChartsCollection.swift:40-86, Android ChartData.java:
// 90-108), a color as #hex after an optional key (statsColor), columns of equal length
// (ChartsCollection.swift:91-95), two points at least (StatisticActivity.
// java:1015-1016), defaultZoom on exact x values (tdesktop statistics_data_
// deserialize.cpp:112-140), and no column of zeros beside another in a graph
// of two axes (Android DoubleLinearChartData.java:34 divides by its max).
func statsGraphOf(t *testing.T, field string, g *mtproto.StatsGraph) *statsChart {
	t.Helper()
	switch g.GetPredicateName() {
	case mtproto.Predicate_statsGraphError:
		if g.GetError() == "" {
			t.Errorf("%s: statsGraphError without a text", field)
		}
		return nil
	case mtproto.Predicate_statsGraph:
	default:
		t.Errorf("%s: %v, want statsGraph or statsGraphError", field, g)
		return nil
	}
	data := g.GetJson().GetData()
	if !strings.HasPrefix(data, `{"columns":[["x",`) {
		t.Errorf("%s: the JSON does not begin with the x column: %.80s", field, data)
		return nil
	}
	var j struct {
		Columns  [][]json.RawMessage `json:"columns"`
		Types    map[string]string   `json:"types"`
		Names    map[string]string   `json:"names"`
		Colors   map[string]string   `json:"colors"`
		YScaled  bool                `json:"y_scaled"`
		Subchart *struct {
			Show        bool     `json:"show"`
			DefaultZoom *[]int64 `json:"defaultZoom"`
		} `json:"subchart"`
		XTickFormatter    *string `json:"xTickFormatter"`
		XTooltipFormatter *string `json:"xTooltipFormatter"`
	}
	if err := json.Unmarshal([]byte(data), &j); err != nil {
		t.Errorf("%s: JSON: %v", field, err)
		return nil
	}
	if j.Types == nil || j.Names == nil || j.Colors == nil || j.XTickFormatter == nil || j.XTooltipFormatter == nil {
		t.Errorf("%s: a key the clients need is missing: %.300s", field, data)
		return nil
	}
	if len(j.Columns) < 2 {
		t.Errorf("%s: %d columns, want x and a y column at least", field, len(j.Columns))
		return nil
	}
	c := &statsChart{series: map[string][]int64{}, colors: map[string]string{}, zoomToken: g.GetZoomToken().GetValue()}
	var zero, nonzero int
	for i, col := range j.Columns {
		var id string
		if len(col) == 0 || json.Unmarshal(col[0], &id) != nil {
			t.Errorf("%s: column %d has no id", field, i)
			return nil
		}
		values := make([]int64, 0, len(col)-1)
		for _, raw := range col[1:] {
			var v int64
			if err := json.Unmarshal(raw, &v); err != nil {
				t.Errorf("%s: column %s: %s is not an integer", field, id, raw)
				return nil
			}
			values = append(values, v)
		}
		if i == 0 {
			if id != "x" || j.Types["x"] != "x" {
				t.Errorf("%s: the first column is %q of type %q, want x", field, id, j.Types["x"])
				return nil
			}
			c.x = values
			continue
		}
		if len(values) != len(c.x) {
			t.Errorf("%s: column %s has %d values for %d points", field, id, len(values), len(c.x))
			return nil
		}
		name, color := j.Names[id], j.Colors[id]
		if j.Types[id] == "" || name == "" || !statsColor(color) {
			t.Errorf("%s: column %s: type %q, name %q, color %q", field, id, j.Types[id], name, color)
		}
		if _, dup := c.series[name]; dup {
			t.Errorf("%s: two columns named %q", field, name)
		}
		c.series[name], c.colors[name] = values, color
		if slices.ContainsFunc(values, func(v int64) bool { return v != 0 }) {
			nonzero++
		} else {
			zero++
		}
	}
	if len(c.x) < 2 {
		t.Errorf("%s: %d points, want 2 at least", field, len(c.x))
	}
	for i := 1; i < len(c.x); i++ {
		if c.x[i] <= c.x[i-1] {
			t.Errorf("%s: x not increasing at %d", field, i)
			break
		}
	}
	if j.YScaled && zero > 0 && nonzero > 0 {
		t.Errorf("%s: a graph of two axes with a column of zeros beside a column of values", field)
	}
	if s := j.Subchart; s != nil && s.Show {
		if s.DefaultZoom == nil || len(*s.DefaultZoom) != 2 || !slices.Contains(c.x, (*s.DefaultZoom)[0]) || !slices.Contains(c.x, (*s.DefaultZoom)[1]) {
			t.Errorf("%s: defaultZoom %v not on the points %v", field, s.DefaultZoom, c.x)
		}
	}
	return c
}

// statsColor tells whether c is a color of a series as the clients read
// it: an optional key and "#RRGGBB" (Android ChartData.java:90-104 splits
// "(.*)(#.*)" and takes the key only when there is one; iOS
// UIColor+Utils.swift:30-80 the same on "#"). The series past the eight
// named colors have no key (pkg/statsgraph palette.go).
func statsColor(c string) bool {
	i := strings.LastIndexByte(c, '#')
	if i < 0 || len(c)-i != 7 {
		return false
	}
	for _, r := range c[i+1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return !strings.Contains(c[:i], "#")
}

// requireGraph is statsGraphOf of a graph that must be drawn.
func requireGraph(t *testing.T, field string, g *mtproto.StatsGraph) *statsChart {
	t.Helper()
	c := statsGraphOf(t, field, g)
	if c == nil {
		t.Fatalf("%s: %v, want a graph", field, g)
	}
	return c
}

func statsRu(key string) string { return langpack.StatsText(key, "ru") }

func broadcastStats(ctx context.Context, u *user, ch *mtproto.InputChannel, dark bool) (*mtproto.Stats_BroadcastStats, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLStatsGetBroadcastStats{
		Constructor: u.c.Constructor(mtproto.Predicate_stats_getBroadcastStats),
		Dark:        dark,
		Channel:     ch,
	})
	if err != nil {
		return nil, err
	}
	r, ok := o.(interface {
		To_Stats_BroadcastStats() *mtproto.Stats_BroadcastStats
	})
	if !ok {
		return nil, fmt.Errorf("stats.getBroadcastStats returned %T", o)
	}
	return r.To_Stats_BroadcastStats(), nil
}

func megagroupStats(ctx context.Context, u *user, ch *mtproto.InputChannel) (*mtproto.Stats_MegagroupStats, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLStatsGetMegagroupStats{
		Constructor: u.c.Constructor(mtproto.Predicate_stats_getMegagroupStats),
		Channel:     ch,
	})
	if err != nil {
		return nil, err
	}
	r, ok := o.(interface {
		To_Stats_MegagroupStats() *mtproto.Stats_MegagroupStats
	})
	if !ok {
		return nil, fmt.Errorf("stats.getMegagroupStats returned %T", o)
	}
	return r.To_Stats_MegagroupStats(), nil
}

func loadAsyncGraph(ctx context.Context, u *user, token string, x int64) (*mtproto.StatsGraph, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLStatsLoadAsyncGraph{
		Constructor: u.c.Constructor(mtproto.Predicate_stats_loadAsyncGraph),
		Token:       token,
		X:           wrapperspb.Int64(x),
	})
	if err != nil {
		return nil, err
	}
	r, ok := o.(interface{ To_StatsGraph() *mtproto.StatsGraph })
	if !ok {
		return nil, fmt.Errorf("stats.loadAsyncGraph returned %T", o)
	}
	return r.To_StatsGraph(), nil
}

func messageStats(ctx context.Context, u *user, ch *mtproto.InputChannel, msgId int32) (*mtproto.Stats_MessageStats, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLStatsGetMessageStats{
		Constructor: u.c.Constructor(mtproto.Predicate_stats_getMessageStats),
		Channel:     ch,
		MsgId:       msgId,
	})
	if err != nil {
		return nil, err
	}
	r, ok := o.(interface {
		To_Stats_MessageStats() *mtproto.Stats_MessageStats
	})
	if !ok {
		return nil, fmt.Errorf("stats.getMessageStats returned %T", o)
	}
	return r.To_Stats_MessageStats(), nil
}

func publicForwardsOf(o mtproto.TLObject, err error, method string) (*mtproto.Stats_PublicForwards, error) {
	if err != nil {
		return nil, err
	}
	r, ok := o.(interface {
		To_Stats_PublicForwards() *mtproto.Stats_PublicForwards
	})
	if !ok {
		return nil, fmt.Errorf("%s returned %T", method, o)
	}
	return r.To_Stats_PublicForwards(), nil
}

func messagePublicForwards(ctx context.Context, u *user, ch *mtproto.InputChannel, msgId int32, offset string, limit int32) (*mtproto.Stats_PublicForwards, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLStatsGetMessagePublicForwards5F150144{
		Constructor: mtproto.CRC32_stats_getMessagePublicForwards5F150144,
		Channel:     ch,
		MsgId:       msgId,
		Offset:      offset,
		Limit:       limit,
	})
	return publicForwardsOf(o, err, "stats.getMessagePublicForwards")
}

func storyPublicForwards(ctx context.Context, u *user, peer *mtproto.InputPeer, id int32, offset string, limit int32) (*mtproto.Stats_PublicForwards, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLStatsGetStoryPublicForwards{
		Constructor: u.c.Constructor(mtproto.Predicate_stats_getStoryPublicForwards),
		Peer:        peer,
		Id:          id,
		Offset:      offset,
		Limit:       limit,
	})
	return publicForwardsOf(o, err, "stats.getStoryPublicForwards")
}

func storyStats(ctx context.Context, u *user, peer *mtproto.InputPeer, id int32) (*mtproto.Stats_StoryStats, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLStatsGetStoryStats{
		Constructor: u.c.Constructor(mtproto.Predicate_stats_getStoryStats),
		Peer:        peer,
		Id:          id,
	})
	if err != nil {
		return nil, err
	}
	r, ok := o.(interface {
		To_Stats_StoryStats() *mtproto.Stats_StoryStats
	})
	if !ok {
		return nil, fmt.Errorf("stats.getStoryStats returned %T", o)
	}
	return r.To_Stats_StoryStats(), nil
}

func pollStats(ctx context.Context, u *user, peer *mtproto.InputPeer, msgId int32) (*mtproto.Stats_PollStats, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLStatsGetPollStats{
		Constructor: u.c.Constructor(mtproto.Predicate_stats_getPollStats),
		Peer:        peer,
		MsgId:       msgId,
	})
	if err != nil {
		return nil, err
	}
	r, ok := o.(interface {
		To_Stats_PollStats() *mtproto.Stats_PollStats
	})
	if !ok {
		return nil, fmt.Errorf("stats.getPollStats returned %T", o)
	}
	return r.To_Stats_PollStats(), nil
}

// statsRefused checks that err is the RPC error code.
func statsRefused(t *testing.T, what string, err error, code string) {
	t.Helper()
	if !mtclient.IsRPCError(err, code) {
		t.Errorf("%s: %v, want %s", what, err, code)
	}
}

// TestStatsBroadcast: the creator of a channel sees its statistics, a
// subscriber does not. Bob joins by the link, views the two posts, puts a
// reaction on one and mutes the channel; the overview counts it all in
// its values and graphs, every graph is one the clients parse, the names
// are Russian, and a day zooms into its hours with the graph's token, which
// is the viewer's own and cannot be forged.
func TestStatsBroadcast(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := loginIn(ctx, t, phones[0], "Alice", "ru")
	bob := login(ctx, t, phones[1], "Bob")

	_, ch := createChannel(ctx, t, alice, "stats", false)
	statsJoinByLink(ctx, t, alice, ch, bob)
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSetChatAvailableReactions{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_setChatAvailableReactions),
		Peer:        statsPeer(alice, ch),
		AvailableReactions_CHATREACTIONS: mtproto.MakeTLChatReactionsSome(&mtproto.ChatReactions{
			Reactions: []*mtproto.Reaction{emojiReaction(alice, "👍")},
		}).To_ChatReactions(),
	}); err != nil {
		t.Fatalf("messages.setChatAvailableReactions: %v", err)
	}
	first := sentID(t, statsSend(ctx, t, alice, statsPeer(alice, ch), "first post"))
	second := sentID(t, statsSend(ctx, t, alice, statsPeer(alice, ch), "second post"))
	statsViews(ctx, t, bob, ch, first, second)
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSendReaction{
		Constructor:                 bob.c.Constructor(mtproto.Predicate_messages_sendReaction),
		Peer:                        statsPeer(bob, ch),
		MsgId:                       first,
		Reaction_FLAGVECTORREACTION: []*mtproto.Reaction{emojiReaction(bob, "👍")},
	}); err != nil {
		t.Fatalf("messages.sendReaction: %v", err)
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLAccountUpdateNotifySettings{
		Constructor: bob.c.Constructor(mtproto.Predicate_account_updateNotifySettings),
		Peer:        &mtproto.InputNotifyPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputNotifyPeer), Peer: statsPeer(bob, ch)},
		Settings: &mtproto.InputPeerNotifySettings{
			Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerNotifySettings),
			MuteUntil:   wrapperspb.Int32(math.MaxInt32),
		},
	}); err != nil {
		t.Fatalf("account.updateNotifySettings: %v", err)
	}

	// The flags the clients offer the statistics by: the creator's full
	// channel has can_view_stats and the DC to ask (stats_dc, Web K needs
	// both, appProfileManager.ts:1444-1455); a subscriber's has neither.
	if full, _ := fullChannel(ctx, t, alice, statsInput(alice, ch)); !full.GetCanViewStats() || full.GetStatsDc().GetValue() != 1 {
		t.Errorf("the creator's full channel: can_view_stats %v, stats_dc %v, want true and 1", full.GetCanViewStats(), full.GetStatsDc())
	}
	if full, _ := fullChannel(ctx, t, bob, statsInput(bob, ch)); full.GetCanViewStats() || full.GetStatsDc() != nil {
		t.Errorf("a subscriber's full channel: can_view_stats %v, stats_dc %v, want neither", full.GetCanViewStats(), full.GetStatsDc())
	}

	statsRecordSince(t)
	statsChannelPast(t, ch.GetId())
	st, err := broadcastStats(ctx, alice, statsInput(alice, ch), false)
	if err != nil {
		t.Fatalf("stats.getBroadcastStats: %v", err)
	}
	if p := st.GetPeriod(); p.GetMaxDate()-p.GetMinDate() != 30*86400 || int64(p.GetMaxDate()) > time.Now().Unix()+5 {
		t.Errorf("period %v, want the last 30 days", p)
	}
	if f := st.GetFollowers(); f.GetCurrent() != 2 || f.GetPrevious() != 1 {
		t.Errorf("followers %v, want 2 now and 1 before Bob joined", f)
	}
	if r := st.GetReactionsPerPost(); r.GetCurrent() != 0.5 {
		t.Errorf("reactions per post %v, want one reaction on two posts", r)
	}
	if v := st.GetViewsPerPost(); v.GetCurrent() <= 0 {
		t.Errorf("views per post %v, want some", v)
	}
	if n := st.GetEnabledNotifications(); n.GetPart() != 1 || n.GetTotal() != 2 {
		t.Errorf("enabled notifications %v, want 1 of 2 (Bob muted)", n)
	}
	for _, v := range []*mtproto.StatsAbsValueAndPrev{st.GetSharesPerPost(), st.GetViewsPerStory(), st.GetSharesPerStory(), st.GetReactionsPerStory()} {
		if v == nil {
			t.Errorf("a value of the overview is missing: %v", st)
		}
	}

	// Every graph is drawn or hidden as a whole; those with something in
	// them count what happened.
	graphs := map[string]*mtproto.StatsGraph{
		"growth_graph":                     st.GetGrowthGraph(),
		"followers_graph":                  st.GetFollowersGraph(),
		"mute_graph":                       st.GetMuteGraph(),
		"top_hours_graph":                  st.GetTopHoursGraph(),
		"interactions_graph":               st.GetInteractionsGraph(),
		"views_by_source_graph":            st.GetViewsBySourceGraph(),
		"new_followers_by_source_graph":    st.GetNewFollowersBySourceGraph(),
		"languages_graph":                  st.GetLanguagesGraph(),
		"reactions_by_emotion_graph":       st.GetReactionsByEmotionGraph(),
		"story_interactions_graph":         st.GetStoryInteractionsGraph(),
		"story_reactions_by_emotion_graph": st.GetStoryReactionsByEmotionGraph(),
	}
	for field, g := range graphs {
		statsGraphOf(t, field, g)
	}
	if g := st.GetIvInteractionsGraph(); g.GetPredicateName() != mtproto.Predicate_statsGraphError {
		t.Errorf("iv_interactions_graph %v, want statsGraphError (no Instant View)", g)
	}
	growth := requireGraph(t, "growth_graph", st.GetGrowthGraph())
	if v := growth.series[statsRu(langpack.StatsSubscribers)]; len(v) == 0 || v[len(v)-1] != 2 {
		t.Errorf("growth_graph: subscribers %v, want 2 at the end", v)
	}
	if c := requireGraph(t, "followers_graph", st.GetFollowersGraph()); c.sum(statsRu(langpack.StatsJoined)) != 1 || c.sum(statsRu(langpack.StatsLeft)) != 0 {
		t.Errorf("followers_graph %v, want one join and no leave", c.series)
	}
	if c := requireGraph(t, "new_followers_by_source_graph", st.GetNewFollowersBySourceGraph()); c.sum(statsRu(langpack.StatsJoinSourceLink)) != 1 {
		t.Errorf("new_followers_by_source_graph %v, want one join by a link", c.series)
	}
	if c := requireGraph(t, "mute_graph", st.GetMuteGraph()); c.sum(statsRu(langpack.StatsMuted)) != 1 {
		t.Errorf("mute_graph %v, want one muting", c.series)
	}
	if c := requireGraph(t, "views_by_source_graph", st.GetViewsBySourceGraph()); c.sum(statsRu(langpack.StatsSourceSubscribers)) != 2 {
		t.Errorf("views_by_source_graph %v, want Bob's two views as a subscriber", c.series)
	}
	if c := requireGraph(t, "reactions_by_emotion_graph", st.GetReactionsByEmotionGraph()); c.sum(statsRu(langpack.StatsReactionsPositive)) != 1 {
		t.Errorf("reactions_by_emotion_graph %v, want one positive reaction", c.series)
	}
	// The hours of the day: 0..23, the x Web K, Android and iOS take for
	// an hour (Web K lib/tchart/format.ts:13-19, Android
	// LegendSignatureView.java:163-170, iOS ChartsCollection.swift:68-77).
	hours := requireGraph(t, "top_hours_graph", st.GetTopHoursGraph())
	if len(hours.x) != 24 || hours.x[0] != 0 || hours.x[23] != 23 {
		t.Errorf("top_hours_graph: x %v, want the hours 0..23", hours.x)
	}
	if hours.sum(statsRu(langpack.StatsThisPeriod)) != 2 {
		t.Errorf("top_hours_graph %v, want Bob's two views in this period", hours.series)
	}
	inter := requireGraph(t, "interactions_graph", st.GetInteractionsGraph())
	if inter.sum(statsRu(langpack.StatsViews)) != 2 {
		t.Errorf("interactions_graph %v, want two views", inter.series)
	}
	if len(inter.x) < 4 || inter.zoomToken == "" {
		t.Fatalf("interactions_graph: %d days, zoom_token %q, want four days and a token", len(inter.x), inter.zoomToken)
	}
	ids := []int32{}
	for _, p := range st.GetRecentPostsInteractions() {
		if p.GetPredicateName() != mtproto.Predicate_postInteractionCountersMessage {
			t.Errorf("recent interactions: %v, want posts only", p)
			continue
		}
		ids = append(ids, p.GetMsgId())
		if p.GetMsgId() == first && p.GetReactions() != 1 {
			t.Errorf("recent interactions of the first post: %v, want one reaction", p)
		}
	}
	if !slices.Equal(ids, []int32{second, first}) {
		t.Errorf("recent interactions of posts %v, want %v, the latest first", ids, []int32{second, first})
	}

	// The dark theme has its own colors.
	darkSt, err := broadcastStats(ctx, alice, statsInput(alice, ch), true)
	if err != nil {
		t.Fatalf("stats.getBroadcastStats dark: %v", err)
	}
	dark := requireGraph(t, "top_hours_graph dark", darkSt.GetTopHoursGraph())
	if name := statsRu(langpack.StatsThisPeriod); dark.colors[name] == hours.colors[name] {
		t.Errorf("top_hours_graph: the same color %q in the dark theme", hours.colors[name])
	}

	// The zoom of the last day: its hours and the three days before, with
	// Bob's views in them.
	g, err := loadAsyncGraph(ctx, alice, inter.zoomToken, inter.x[len(inter.x)-1])
	if err != nil {
		t.Fatalf("stats.loadAsyncGraph: %v", err)
	}
	zoom := requireGraph(t, "zoom", g)
	zoom.checkStep(t, "zoom", statsHourMs)
	if len(zoom.x) < 24 || zoom.sum(statsRu(langpack.StatsViews)) != 2 || zoom.zoomToken != "" {
		t.Errorf("zoom: %d hours, views %d, token %q, want a day of hours at least, two views and no token", len(zoom.x), zoom.sum(statsRu(langpack.StatsViews)), zoom.zoomToken)
	}
	parts := strings.Split(inter.zoomToken, ".")
	if len(parts) != 3 {
		t.Fatalf("zoom_token %q is not version.payload.mac", inter.zoomToken)
	}
	forged := []byte(parts[1])
	forged[0] ^= 1
	_, err = loadAsyncGraph(ctx, alice, parts[0]+"."+string(forged)+"."+parts[2], inter.x[0])
	statsRefused(t, "a forged token", err, "GRAPH_INVALID_RELOAD")
	_, err = loadAsyncGraph(ctx, alice, "v9."+parts[1]+"."+parts[2], inter.x[0])
	statsRefused(t, "a token of another version", err, "GRAPH_OUTDATED_RELOAD")
	_, err = loadAsyncGraph(ctx, bob, inter.zoomToken, inter.x[0])
	statsRefused(t, "the creator's token used by a subscriber", err, "GRAPH_INVALID_RELOAD")

	_, err = broadcastStats(ctx, bob, statsInput(bob, ch), false)
	statsRefused(t, "a subscriber's statistics", err, "CHAT_ADMIN_REQUIRED")
	_, err = megagroupStats(ctx, alice, statsInput(alice, ch))
	statsRefused(t, "the statistics of a supergroup for a channel", err, "MEGAGROUP_REQUIRED")
	_, err = broadcastStats(ctx, alice, &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue() + 1}, false)
	statsRefused(t, "a wrong access hash", err, "CHANNEL_INVALID")
	// The methods of the statistics Pluma does not serve answer 400, which
	// the clients do not retry: the revenue (RPCChannelAdRevenue, which the
	// session routes nowhere; layer 233 has no such method, so its form of
	// layer 205 is sent), and the form of getMessagePublicForwards of the
	// layers before 170 (RPCStatistics, refused by bff).
	_, err = alice.c.Invoke(ctx, &mtproto.TLStatsGetBroadcastRevenueStats{
		Constructor: mtproto.CRC32_stats_getBroadcastRevenueStats_f788ee19,
		Peer:        statsPeer(alice, ch),
		Channel:     statsInput(alice, ch),
	})
	statsRefused(t, "stats.getBroadcastRevenueStats", err, "ERR_ENTERPRISE_IS_BLOCKED")
	_, err = alice.c.Invoke(ctx, &mtproto.TLStatsGetMessagePublicForwards5630281B{
		Constructor: mtproto.CRC32_stats_getMessagePublicForwards5630281B,
		Channel:     statsInput(alice, ch),
		MsgId:       first,
		OffsetPeer:  &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Limit:       10,
	})
	statsRefused(t, "the old stats.getMessagePublicForwards", err, "ERR_ENTERPRISE_IS_BLOCKED")
}

// statsSend sends text from u to peer and returns the answer.
func statsSend(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, text string) mtproto.TLObject {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer, Message: text, RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	return o
}

// TestStatsMessage: the statistics of a post three hours old are hourly
// (iOS takes a step of 3600 for an hourly graph) with the views and the
// forwards of the post; its public forwards are the copies in public
// channels only, paged by next_offset.
func TestStatsMessage(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := loginIn(ctx, t, phones[0], "Alice", "ru")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	_, ch := createChannel(ctx, t, alice, "posts", false)
	statsJoinByLink(ctx, t, alice, ch, bob, carol)
	post := sentID(t, statsSend(ctx, t, alice, statsPeer(alice, ch), "the post"))
	statsViews(ctx, t, bob, ch, post)
	public := statsPublicChannel(ctx, t, carol, "pub")
	_, private := createChannel(ctx, t, carol, "private copies", false)
	copies := []int32{statsForward(ctx, t, carol, ch, post, public), statsForward(ctx, t, carol, ch, post, public)}
	statsForward(ctx, t, carol, ch, post, private)

	storiesSQL(t, fmt.Sprintf("UPDATE channel_messages SET date2 = date2 - %d WHERE channel_id = %d AND id = %d", statsPostPast, ch.GetId(), post))
	st, err := messageStats(ctx, alice, statsInput(alice, ch), post)
	if err != nil {
		t.Fatalf("stats.getMessageStats: %v", err)
	}
	views := requireGraph(t, "views_graph", st.GetViewsGraph())
	views.checkStep(t, "views_graph", statsHourMs)
	if len(views.x) < 4 || views.sum(statsRu(langpack.StatsViews)) != 1 || views.sum(statsRu(langpack.StatsShares)) != 3 {
		t.Errorf("views_graph: %d hours, %v, want four hours, Bob's view and Carol's three forwards", len(views.x), views.series)
	}
	statsGraphOf(t, "reactions_by_emotion_graph", st.GetReactionsByEmotionGraph())

	// The copies in the public channel, one a page; the one in the private
	// channel is not listed.
	var got []int32
	offset := ""
	for page := 0; ; page++ {
		if page > len(copies) {
			t.Fatalf("stats.getMessagePublicForwards: more pages than copies")
		}
		f, err := messagePublicForwards(ctx, alice, statsInput(alice, ch), post, offset, 1)
		if err != nil {
			t.Fatalf("stats.getMessagePublicForwards: %v", err)
		}
		if f.GetCount() != int32(len(copies)) {
			t.Errorf("public forwards: count %d, want %d", f.GetCount(), len(copies))
		}
		for _, c := range f.GetChats() {
			if c.GetId() == private.GetId() {
				t.Errorf("public forwards name the private channel")
			}
		}
		for _, fw := range f.GetForwards() {
			m := fw.GetMessage()
			if fw.GetPredicateName() != mtproto.Predicate_publicForwardMessage || m.GetPeerId().GetChannelId() != public.GetId() ||
				m.GetFwdFrom().GetChannelPost().GetValue() != post {
				t.Errorf("public forward %v, want a copy of the post in the public channel", fw)
				continue
			}
			got = append(got, m.GetId())
		}
		if len(f.GetForwards()) == 0 {
			if f.GetNextOffset() != nil {
				t.Errorf("an empty page with next_offset %v", f.GetNextOffset())
			}
			break
		}
		if f.GetNextOffset().GetValue() == "" {
			t.Fatalf("a full page without next_offset")
		}
		offset = f.GetNextOffset().GetValue()
	}
	slices.Sort(got)
	if !slices.Equal(got, copies) {
		t.Errorf("public forwards %v, want the copies %v", got, copies)
	}

	_, err = messageStats(ctx, alice, statsInput(alice, ch), 1)
	statsRefused(t, "the statistics of the service message of the creation", err, "MESSAGE_ID_INVALID")
	_, err = messageStats(ctx, alice, statsInput(alice, ch), post+1000)
	statsRefused(t, "the statistics of a post that is not there", err, "MESSAGE_ID_INVALID")
	_, err = messageStats(ctx, bob, statsInput(bob, ch), post)
	statsRefused(t, "a subscriber's statistics of a post", err, "CHAT_ADMIN_REQUIRED")
	_, err = messagePublicForwards(ctx, alice, statsInput(alice, ch), post, "not an offset", 10)
	statsRefused(t, "an offset the server did not give", err, "OFFSET_INVALID")
}

// TestStatsMegagroup: the statistics of a supergroup count its members by
// how they came, its messages by type, and name the members who wrote,
// deleted and added the most.
func TestStatsMegagroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := loginIn(ctx, t, phones[0], "Alice", "ru")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	_, gr := createChannel(ctx, t, alice, "stats group", true)
	statsJoinByLink(ctx, t, alice, gr, bob)
	inviteToChannel(ctx, t, alice, statsInput(alice, gr), carol)
	statsSend(ctx, t, alice, statsPeer(alice, gr), "hello")
	statsSend(ctx, t, bob, statsPeer(bob, gr), "one")
	statsSend(ctx, t, bob, statsPeer(bob, gr), "two")
	gone := sentID(t, statsSend(ctx, t, bob, statsPeer(bob, gr), "three"))
	sendMedia(ctx, t, bob, statsPeer(bob, gr), uploadPhoto(ctx, t, bob))
	statsSend(ctx, t, carol, statsPeer(carol, gr), "hi")
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsDeleteMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_deleteMessages),
		Channel:     statsInput(alice, gr),
		Id:          []int32{gone},
	}); err != nil {
		t.Fatalf("channels.deleteMessages: %v", err)
	}

	if full, _ := fullChannel(ctx, t, alice, statsInput(alice, gr)); !full.GetCanViewStats() || full.GetStatsDc().GetValue() != 1 {
		t.Errorf("the creator's full supergroup: can_view_stats %v, stats_dc %v", full.GetCanViewStats(), full.GetStatsDc())
	}
	if full, _ := fullChannel(ctx, t, carol, statsInput(carol, gr)); full.GetCanViewStats() {
		t.Errorf("a member's full supergroup has can_view_stats")
	}

	statsRecordSince(t)
	statsChannelPast(t, gr.GetId())
	st, err := megagroupStats(ctx, alice, statsInput(alice, gr))
	if err != nil {
		t.Fatalf("stats.getMegagroupStats: %v", err)
	}
	if m := st.GetMembers(); m.GetCurrent() != 3 || m.GetPrevious() != 1 {
		t.Errorf("members %v, want 3 now and 1 before the joins", m)
	}
	if m := st.GetMessages(); m.GetCurrent() != 5 {
		t.Errorf("messages %v, want 5 (the deleted one not counted)", m)
	}
	if p := st.GetPosters(); p.GetCurrent() != 3 {
		t.Errorf("posters %v, want 3", p)
	}
	if v := st.GetViewers(); v.GetCurrent() != 3 {
		t.Errorf("viewers %v, want the three who wrote today", v)
	}
	for field, g := range map[string]*mtproto.StatsGraph{
		"growth_graph":                st.GetGrowthGraph(),
		"members_graph":               st.GetMembersGraph(),
		"new_members_by_source_graph": st.GetNewMembersBySourceGraph(),
		"languages_graph":             st.GetLanguagesGraph(),
		"messages_graph":              st.GetMessagesGraph(),
		"actions_graph":               st.GetActionsGraph(),
		"top_hours_graph":             st.GetTopHoursGraph(),
		"weekdays_graph":              st.GetWeekdaysGraph(),
	} {
		statsGraphOf(t, field, g)
	}
	msgs := requireGraph(t, "messages_graph", st.GetMessagesGraph())
	if msgs.sum(statsRu(langpack.StatsMessagesText)) != 4 || msgs.sum(statsRu(langpack.StatsMessagesPhotos)) != 1 {
		t.Errorf("messages_graph %v, want 4 texts and a photo", msgs.series)
	}
	if msgs.zoomToken == "" {
		t.Errorf("messages_graph without a zoom token")
	}
	if c := requireGraph(t, "members_graph", st.GetMembersGraph()); c.sum(statsRu(langpack.StatsJoined)) != 2 {
		t.Errorf("members_graph %v, want two joins", c.series)
	}
	if c := requireGraph(t, "new_members_by_source_graph", st.GetNewMembersBySourceGraph()); c.sum(statsRu(langpack.StatsJoinSourceLink)) != 1 || c.sum(statsRu(langpack.StatsJoinSourceAdded)) != 1 {
		t.Errorf("new_members_by_source_graph %v, want one by a link and one added", c.series)
	}
	if c := requireGraph(t, "top_hours_graph", st.GetTopHoursGraph()); len(c.x) != 24 || c.sum(statsRu(langpack.StatsMessages)) != 5 {
		t.Errorf("top_hours_graph: %d hours, %v, want 24 and the five messages", len(c.x), c.series)
	}

	aliceId, bobId, carolId := alice.self.GetId(), bob.self.GetId(), carol.self.GetId()
	posters := map[int64]int32{}
	for _, p := range st.GetTopPosters() {
		posters[p.GetUserId()] = p.GetMessages()
	}
	if len(st.GetTopPosters()) == 0 || st.GetTopPosters()[0].GetUserId() != bobId || posters[bobId] != 3 || posters[aliceId] != 1 || posters[carolId] != 1 {
		t.Errorf("top posters %v, want Bob with 3 first, Alice and Carol with 1", st.GetTopPosters())
	}
	if a := st.GetTopAdmins(); len(a) != 1 || a[0].GetUserId() != aliceId || a[0].GetDeleted() != 1 || a[0].GetKicked() != 0 || a[0].GetBanned() != 0 {
		t.Errorf("top admins %v, want Alice who deleted one", a)
	}
	if i := st.GetTopInviters(); len(i) != 1 || i[0].GetUserId() != aliceId || i[0].GetInvitations() != 1 {
		t.Errorf("top inviters %v, want Alice who added Carol", i)
	}
	users := map[int64]bool{}
	for _, u := range st.GetUsers() {
		users[u.GetId()] = true
	}
	if !users[aliceId] || !users[bobId] || !users[carolId] {
		t.Errorf("users %v, want the three named in the lists", st.GetUsers())
	}

	_, err = megagroupStats(ctx, bob, statsInput(bob, gr))
	statsRefused(t, "a member's statistics", err, "CHAT_ADMIN_REQUIRED")
	_, err = broadcastStats(ctx, alice, statsInput(alice, gr), false)
	statsRefused(t, "the statistics of a channel for a supergroup", err, "BROADCAST_REQUIRED")
}

// TestStatsStoryAndPoll: the statistics of a story of a channel count its
// views and its repost in a public channel, which its public forwards list;
// a poll's creator has can_view_stats in its results and its votes by
// option, another member neither.
func TestStatsStoryAndPoll(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := loginIn(ctx, t, phones[0], "Alice", "ru")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	_, ch := createChannel(ctx, t, alice, "stories", false)
	statsJoinByLink(ctx, t, alice, ch, bob, carol)
	story, _ := sendStory(ctx, t, alice, statsPeer(alice, ch), randomID(), "the story")
	if read := invokeInts(ctx, t, bob, "stories.readStories", &mtproto.TLStoriesReadStories{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_readStories),
		Peer:        statsPeer(bob, ch),
		MaxId:       story,
	}); !slices.Equal(read, []int32{story}) {
		t.Errorf("stories.readStories: %v", read)
	}
	public := statsPublicChannel(ctx, t, carol, "rep")
	o, err := carol.c.Invoke(ctx, &mtproto.TLStoriesSendStory{
		Constructor:  carol.c.Constructor(mtproto.Predicate_stories_sendStory),
		Peer:         statsPeer(carol, public),
		Media:        uploadPhoto(ctx, t, carol),
		PrivacyRules: []*mtproto.InputPrivacyRule{},
		RandomId:     randomID(),
		FwdFromId:    statsPeer(carol, ch),
		FwdFromStory: wrapperspb.Int32(story),
	})
	if err != nil {
		t.Fatalf("stories.sendStory of the repost: %v", err)
	}
	var repost int32
	for _, x := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateStoryID {
			repost = x.GetId_INT32()
		}
	}
	if repost == 0 {
		t.Fatal("stories.sendStory of the repost: no updateStoryID")
	}

	storiesSQL(t, fmt.Sprintf("UPDATE stories SET date = date - %d WHERE peer_type = %d AND peer_id = %d AND story_id = %d", statsPostPast, mtproto.PEER_CHANNEL, ch.GetId(), story))
	st, err := storyStats(ctx, alice, statsPeer(alice, ch), story)
	if err != nil {
		t.Fatalf("stats.getStoryStats: %v", err)
	}
	views := requireGraph(t, "story views_graph", st.GetViewsGraph())
	views.checkStep(t, "story views_graph", statsHourMs)
	if len(views.x) < 4 || views.sum(statsRu(langpack.StatsViews)) != 1 || views.sum(statsRu(langpack.StatsShares)) != 1 {
		t.Errorf("story views_graph: %d hours, %v, want four hours, Bob's view and Carol's repost", len(views.x), views.series)
	}
	statsGraphOf(t, "story reactions_by_emotion_graph", st.GetReactionsByEmotionGraph())

	f, err := storyPublicForwards(ctx, alice, statsPeer(alice, ch), story, "", 10)
	if err != nil {
		t.Fatalf("stats.getStoryPublicForwards: %v", err)
	}
	if f.GetCount() != 1 || len(f.GetForwards()) != 1 || f.GetNextOffset() != nil {
		t.Fatalf("story public forwards: %v, want the repost alone", f)
	}
	if fw := f.GetForwards()[0]; fw.GetPredicateName() != mtproto.Predicate_publicForwardStory ||
		fw.GetPeer().GetChannelId() != public.GetId() || fw.GetStory().GetId() != repost {
		t.Errorf("story public forward %v, want the repost %d in the public channel", fw, repost)
	}
	if !slices.ContainsFunc(f.GetChats(), func(c *mtproto.Chat) bool { return c.GetId() == public.GetId() }) {
		t.Errorf("story public forwards without the public channel in chats")
	}

	_, err = storyStats(ctx, alice, statsPeer(alice, ch), story+1000)
	statsRefused(t, "a story that is not there", err, "STORY_ID_INVALID")
	_, err = storyStats(ctx, carol, statsPeer(carol, public), repost)
	if err != nil {
		t.Errorf("the statistics of the repost for its channel's creator: %v", err)
	}
	_, empty := createChannel(ctx, t, carol, "no stories", false)
	_, err = storyStats(ctx, carol, statsPeer(carol, empty), 1)
	statsRefused(t, "a channel that never posted a story", err, "STORIES_NEVER_CREATED")
	_, err = storyStats(ctx, alice, &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}, story)
	statsRefused(t, "the story of a user", err, "PEER_ID_INVALID")
	_, err = storyStats(ctx, bob, statsPeer(bob, ch), story)
	statsRefused(t, "a subscriber's statistics of a story", err, "CHAT_ADMIN_REQUIRED")

	// A poll in a private chat: the statistics are its creator's.
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	toBob := inputPeerOf(ctx, t, alice, bob)
	reply, aliceMsg := sendPollMedia(ctx, t, alice, toBob, inputPoll(alice, nil))
	var poll int64
	for _, u := range reply.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			media := u.GetMessage_MESSAGE().GetMedia()
			poll = media.GetPoll().GetId()
			if !media.GetResults().GetCanViewStats() {
				t.Errorf("the creator's results: %v, want can_view_stats", media.GetResults())
			}
		}
	}
	noVotes, err := pollStats(ctx, alice, toBob, aliceMsg)
	if err != nil {
		t.Fatalf("stats.getPollStats without votes: %v", err)
	}
	if g := noVotes.GetVotesGraph(); g.GetPredicateName() != mtproto.Predicate_statsGraphError || g.GetError() != statsRu(langpack.StatsNoVotes) {
		t.Errorf("votes_graph of a poll without votes: %v, want statsGraphError %q", g, statsRu(langpack.StatsNoVotes))
	}
	var bobsMsg int32
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		if u.GetMessage_MESSAGE().GetMedia().GetPoll().GetId() == poll {
			bobsMsg = u.GetMessage_MESSAGE().GetId()
			return true
		}
		return false
	})
	toAlice := inputPeerOf(ctx, t, bob, alice)
	r, err := vote(ctx, bob, toAlice, bobsMsg, "1")
	if err != nil {
		t.Fatalf("messages.sendVote: %v", err)
	}
	if mine := pollUpdateOf(r.GetUpdates()); mine == nil || mine.GetResults().GetCanViewStats() {
		t.Errorf("the voter's results: %v, want them without can_view_stats", mine)
	}
	ps, err := pollStats(ctx, alice, toBob, aliceMsg)
	if err != nil {
		t.Fatalf("stats.getPollStats: %v", err)
	}
	votes := requireGraph(t, "votes_graph", ps.GetVotesGraph())
	if len(votes.series) != 3 || votes.sum("A") != 0 || votes.sum("B") != 1 || votes.sum("C") != 0 {
		t.Errorf("votes_graph %v, want a series for each option and Bob's vote for B", votes.series)
	}
	_, err = pollStats(ctx, bob, toAlice, bobsMsg)
	statsRefused(t, "the statistics of another's poll", err, "PEER_ID_INVALID")
	_, err = pollStats(ctx, alice, toBob, aliceMsg+1000)
	statsRefused(t, "the statistics of a message that is not there", err, "PEER_ID_INVALID")
}

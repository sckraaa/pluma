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
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"
)

// target is what a graph counts.
type target struct {
	channelId int64
	// object is the post of a post's graphs, 0 for a channel's.
	object int32
	// broadcast: a channel, not a supergroup.
	broadcast bool
	// start is when the counted object began: the channel was made, the
	// post was posted.
	start int64
	// members is the channel's participants_count now (no ghosts).
	members int64
	// now is the time of the request.
	now int64
}

// graphSpec is a graph that has a point a day and zooms into the hours of
// a day: the overview builds it over days, stats.loadAsyncGraph over
// hours, with the same function.
type graphSpec struct {
	// events is the kind of events of pkg/channelstats the graph counts:
	// it starts when they began to be recorded (ADR 0066, p. 3). Empty
	// for the tables that keep their whole history.
	events channelstats.Kind
	// object: the graph is of a post (target.object), not of a channel.
	object bool
	// kind is the kind of channel whose statistics have the graph.
	kind  int
	build func(ctx context.Context, d *dao.Dao, t target, b statsgraph.Buckets) (*statsgraph.Graph, error)
}

// The graphs that zoom, by the Kind of their tokens. The statistics of
// supergroups and of stories add theirs here.
var graphs = map[string]graphSpec{
	"growth":                  {events: channelstats.MemberEvents, kind: broadcastOnly, build: growthGraph},
	"followers":               {events: channelstats.MemberEvents, kind: broadcastOnly, build: joinsGraph},
	"mute":                    {events: channelstats.MuteEvents, kind: broadcastOnly, build: muteGraph},
	"interactions":            {kind: broadcastOnly, build: interactionsGraph},
	"views_by_source":         {events: channelstats.ViewSources, kind: broadcastOnly, build: viewsBySourceGraph},
	"new_followers_by_source": {events: channelstats.MemberEvents, kind: broadcastOnly, build: joinsBySourceGraph},
	"reactions_by_emotion":    {kind: broadcastOnly, build: reactionsGraph},
	"story_interactions":      {kind: broadcastOnly, build: storyInteractionsGraph},
	"story_reactions":         {kind: broadcastOnly, build: storyReactionsGraph},
	"post_views":              {object: true, kind: broadcastOnly, build: interactionsGraph},
	"post_reactions":          {object: true, kind: broadcastOnly, build: reactionsGraph},
}

// graphStart is when the graph spec of t begins: when its object began, a
// year ago at most, and not before its events are recorded.
func (c *StatisticsCore) graphStart(ctx context.Context, spec graphSpec, t target) (int64, error) {
	from := max(t.start, t.now-windowDays*secondsPerDay)
	if spec.events != "" {
		since, err := c.since(ctx, spec.events)
		if err != nil {
			return 0, err
		}
		from = max(from, since)
	}
	return from, nil
}

// since is since when the events of kind are recorded, read once a
// request.
func (c *StatisticsCore) since(ctx context.Context, kind channelstats.Kind) (int64, error) {
	if s, ok := c.sinces[kind]; ok {
		return s, nil
	}
	s, err := c.svcCtx.Dao.Since(ctx, kind)
	if err != nil {
		return 0, err
	}
	if c.sinces == nil {
		c.sinces = make(map[channelstats.Kind]int64)
	}
	c.sinces[kind] = s
	return s, nil
}

// view is how the viewer reads the graphs: the language of the names and
// the theme of the colors.
type view struct {
	lang string
	dark bool
}

// tokenLang is the language a token keeps: the viewer's, when it fits.
func tokenLang(lang string) string {
	if len(lang) > statsgraph.MaxTokenField {
		return ""
	}
	return lang
}

// daily is the graph of kind for t over days with its subchart and the
// token of its zoom.
func (c *StatisticsCore) daily(ctx context.Context, kind string, t target, v view) (*mtproto.StatsGraph, error) {
	spec, ok := graphs[kind]
	if !ok {
		return nil, fmt.Errorf("stats: graph %q", kind)
	}
	from, err := c.graphStart(ctx, spec, t)
	if err != nil {
		return nil, err
	}
	g, err := spec.build(ctx, c.svcCtx.Dao, t, days(from, t.now))
	if err != nil {
		return nil, err
	}
	g.Subchart = true
	object := int64(0)
	if spec.object {
		object = int64(t.object)
	}
	if g.ZoomToken, err = c.svcCtx.Signer.Sign(statsgraph.Token{
		Kind:     kind,
		PeerType: mtproto.PEER_CHANNEL,
		PeerID:   t.channelId,
		Object:   object,
		Viewer:   c.MD.GetUserId(),
		Lang:     tokenLang(v.lang),
		Dark:     v.dark,
		Expires:  t.now + statsgraph.TokenLifetime,
	}); err != nil {
		return nil, err
	}
	return g.Build(langpack.StatsText, v.lang, v.dark)
}

// hourly is the graph of kind for t over the hours of b, without a
// subchart or a zoom.
func (c *StatisticsCore) hourly(ctx context.Context, kind string, t target, b statsgraph.Buckets, v view) (*mtproto.StatsGraph, error) {
	spec, ok := graphs[kind]
	if !ok {
		return nil, fmt.Errorf("stats: graph %q", kind)
	}
	g, err := spec.build(ctx, c.svcCtx.Dao, t, b)
	if err != nil {
		return nil, err
	}
	return g.Build(langpack.StatsText, v.lang, v.dark)
}

// growthGraph: the subscribers (members of a supergroup) at the end of
// each bucket, the count now less the joins and leaves after it.
func growthGraph(ctx context.Context, d *dao.Dao, t target, b statsgraph.Buckets) (*statsgraph.Graph, error) {
	r := rangeOf(b)
	rows, err := d.MemberCounts(ctx, t.channelId, r)
	if err != nil {
		return nil, err
	}
	after, err := d.MemberDelta(ctx, t.channelId, r.To)
	if err != nil {
		return nil, err
	}
	name := langpack.StatsMembers
	if t.broadcast {
		name = langpack.StatsSubscribers
	}
	return &statsgraph.Graph{
		Type: statsgraph.Line, Scale: b.Scale, X: b.X(),
		Series: []statsgraph.Series{{NameKey: name, Values: growth(b, rows, t.members-after)}},
	}, nil
}

// growth are the members at the end of each bucket of b, last the members
// at its end, from the joins and leaves of rows; never below zero.
func growth(b statsgraph.Buckets, rows []dao.MemberCount, last int64) []int64 {
	net := make([]int64, b.Len())
	for _, r := range rows {
		if j, ok := b.Index(r.B); ok {
			if r.Joined {
				net[j] += r.N
			} else {
				net[j] -= r.N
			}
		}
	}
	out := make([]int64, b.Len())
	n := last
	for j := len(out) - 1; j >= 0; j-- {
		out[j] = max(n, 0)
		n -= net[j]
	}
	return out
}

// joinsGraph: the joins and the leaves in each bucket.
func joinsGraph(ctx context.Context, d *dao.Dao, t target, b statsgraph.Buckets) (*statsgraph.Graph, error) {
	rows, err := d.MemberCounts(ctx, t.channelId, rangeOf(b))
	if err != nil {
		return nil, err
	}
	joined, left := make([]int64, b.Len()), make([]int64, b.Len())
	for _, r := range rows {
		if j, ok := b.Index(r.B); ok {
			if r.Joined {
				joined[j] += r.N
			} else {
				left[j] += r.N
			}
		}
	}
	return &statsgraph.Graph{
		Type: statsgraph.Line, Scale: b.Scale, X: b.X(),
		Series: []statsgraph.Series{
			{NameKey: langpack.StatsJoined, Color: statsgraph.Green, Values: joined},
			{NameKey: langpack.StatsLeft, Color: statsgraph.Red, Values: left},
		},
	}, nil
}

// joinSources are the sources of joins in the order of their series, the
// other last.
var joinSources = []struct {
	source channelstats.JoinSource
	key    string
}{
	{channelstats.JoinLink, langpack.StatsJoinSourceLink},
	{channelstats.JoinFolder, langpack.StatsJoinSourceFolder},
	{channelstats.JoinRequest, langpack.StatsJoinSourceRequest},
	{channelstats.JoinPublic, langpack.StatsJoinSourcePublic},
	{channelstats.JoinAdded, langpack.StatsJoinSourceAdded},
	{channelstats.JoinOther, langpack.StatsSourceOther},
}

// joinsBySourceGraph: the joins in each bucket by how they joined.
func joinsBySourceGraph(ctx context.Context, d *dao.Dao, t target, b statsgraph.Buckets) (*statsgraph.Graph, error) {
	rows, err := d.MemberCounts(ctx, t.channelId, rangeOf(b))
	if err != nil {
		return nil, err
	}
	counts := make([]dao.Count, 0, len(rows))
	for _, r := range rows {
		if r.Joined {
			counts = append(counts, dao.Count{B: r.B, K: int64(r.Source), N: r.N})
		}
	}
	keys := make([]int64, len(joinSources))
	for i, s := range joinSources {
		keys[i] = int64(s.source)
	}
	values := series(b, counts, keys, len(keys)-1)
	g := &statsgraph.Graph{Type: statsgraph.Bar, Stacked: true, Scale: b.Scale, X: b.X()}
	for i, s := range joinSources {
		g.Series = append(g.Series, statsgraph.Series{NameKey: s.key, Values: values[i]})
	}
	return g, nil
}

// muteGraph: who muted the channel and who unmuted it in each bucket, an
// unmuting also when a muting for a time ran out.
func muteGraph(ctx context.Context, d *dao.Dao, t target, b statsgraph.Buckets) (*statsgraph.Graph, error) {
	r := rangeOf(b)
	events, err := d.MuteEvents(ctx, t.channelId, r.To)
	if err != nil {
		return nil, err
	}
	muted, unmuted := mutes(b, events, min(r.To, t.now+1))
	return &statsgraph.Graph{
		Type: statsgraph.Line, Scale: b.Scale, X: b.X(),
		Series: []statsgraph.Series{
			{NameKey: langpack.StatsMuted, Color: statsgraph.Red, Values: muted},
			{NameKey: langpack.StatsUnmuted, Color: statsgraph.Green, Values: unmuted},
		},
	}, nil
}

// interactionsGraph: the views and the forwards of the posts of the
// channel, or of the post of t, in each bucket; two axes.
func interactionsGraph(ctx context.Context, d *dao.Dao, t target, b statsgraph.Buckets) (*statsgraph.Graph, error) {
	r := rangeOf(b)
	views, err := d.ViewCounts(ctx, t.channelId, t.object, r)
	if err != nil {
		return nil, err
	}
	forwards, err := d.ForwardCounts(ctx, t.channelId, t.object, r)
	if err != nil {
		return nil, err
	}
	return &statsgraph.Graph{
		Type: statsgraph.Step, YScaled: true, Scale: b.Scale, X: b.X(),
		Series: []statsgraph.Series{
			{NameKey: langpack.StatsViews, Values: total(b, views)},
			{NameKey: langpack.StatsShares, Values: total(b, forwards)},
		},
	}, nil
}

// viewSources are the sources of views in the order of their series, the
// other last.
var viewSources = []struct {
	source channelstats.ViewSource
	key    string
}{
	{channelstats.ViewSubscriber, langpack.StatsSourceSubscribers},
	{channelstats.ViewChannel, langpack.StatsSourceChannels},
	{channelstats.ViewGroup, langpack.StatsSourceGroups},
	{channelstats.ViewPrivate, langpack.StatsSourcePrivate},
	{channelstats.ViewOther, langpack.StatsSourceOther},
}

// viewsBySourceGraph: the views of the posts in each bucket by where they
// were viewed.
func viewsBySourceGraph(ctx context.Context, d *dao.Dao, t target, b statsgraph.Buckets) (*statsgraph.Graph, error) {
	rows, err := d.ViewCounts(ctx, t.channelId, 0, rangeOf(b))
	if err != nil {
		return nil, err
	}
	keys := make([]int64, len(viewSources))
	for i, s := range viewSources {
		keys[i] = int64(s.source)
	}
	values := series(b, rows, keys, len(keys)-1)
	g := &statsgraph.Graph{Type: statsgraph.Bar, Stacked: true, Scale: b.Scale, X: b.X()}
	for i, s := range viewSources {
		g.Series = append(g.Series, statsgraph.Series{NameKey: s.key, Values: values[i]})
	}
	return g, nil
}

// emotionGraph is the graph of reactions by emotion of values.
func emotionGraph(b statsgraph.Buckets, values [][]int64) *statsgraph.Graph {
	return &statsgraph.Graph{
		Type: statsgraph.Bar, Stacked: true, Scale: b.Scale, X: b.X(),
		Series: []statsgraph.Series{
			{NameKey: langpack.StatsReactionsPositive, Color: statsgraph.Green, Values: values[emotionPositive]},
			{NameKey: langpack.StatsReactionsNegative, Color: statsgraph.Red, Values: values[emotionNegative]},
			{NameKey: langpack.StatsReactionsOther, Color: statsgraph.Blue, Values: values[emotionOther]},
		},
	}
}

// reactionsGraph: the reactions put on the posts of the channel, or on the
// post of t, in each bucket by emotion.
func reactionsGraph(ctx context.Context, d *dao.Dao, t target, b statsgraph.Buckets) (*statsgraph.Graph, error) {
	rows, err := d.ReactionCounts(ctx, t.channelId, t.object, rangeOf(b))
	if err != nil {
		return nil, err
	}
	return emotionGraph(b, byEmotion(b, rows)), nil
}

// storyInteractionsGraph: the views and the shares of the stories of the
// channel in each bucket; two axes.
func storyInteractionsGraph(ctx context.Context, d *dao.Dao, t target, b statsgraph.Buckets) (*statsgraph.Graph, error) {
	r := rangeOf(b)
	views, err := d.StoryViewCounts(ctx, t.channelId, 0, r)
	if err != nil {
		return nil, err
	}
	shares, err := d.StoryShareCounts(ctx, t.channelId, 0, r)
	if err != nil {
		return nil, err
	}
	return &statsgraph.Graph{
		Type: statsgraph.Step, YScaled: true, Scale: b.Scale, X: b.X(),
		Series: []statsgraph.Series{
			{NameKey: langpack.StatsViews, Values: total(b, views)},
			{NameKey: langpack.StatsShares, Values: total(b, shares)},
		},
	}, nil
}

// storyReactionsGraph: the reactions put on the stories of the channel in
// each bucket by emotion.
func storyReactionsGraph(ctx context.Context, d *dao.Dao, t target, b statsgraph.Buckets) (*statsgraph.Graph, error) {
	rows, err := d.StoryReactionCounts(ctx, t.channelId, 0, rangeOf(b))
	if err != nil {
		return nil, err
	}
	return emotionGraph(b, byEmotion(b, rows)), nil
}

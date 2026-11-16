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
	"sort"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"
)

// StatsGetBroadcastStats
// stats.getBroadcastStats#ab42441a flags:# dark:flags.0?true channel:InputChannel = stats.BroadcastStats;
//
// The statistics of a broadcast channel for an admin who may see them
// (channelperm.CanViewStats): the last 30 days against the 30 before, the
// graphs of a year of days (from the start of the recording of their
// events for the graphs of events, ADR 0066, p. 3) and the latest posts
// and stories. iv_interactions_graph is always a statsGraphError: there is
// no Instant View.
func (c *StatisticsCore) StatsGetBroadcastStats(in *mtproto.TLStatsGetBroadcastStats) (_ *mtproto.Stats_BroadcastStats, err error) {
	defer c.observe("stats.getBroadcastStats", time.Now(), &err)
	if err = c.enabled(); err != nil {
		return nil, err
	}
	mc, err := c.statsChannel(in.GetChannel(), broadcastOnly)
	if err != nil {
		return nil, err
	}
	ch := mc.GetChannel()
	t := target{
		channelId: ch.GetId(),
		broadcast: true,
		start:     ch.GetDate(),
		members:   int64(ch.GetParticipantsCount()),
		now:       c.now,
	}
	v := view{lang: c.MD.GetLangCode(), dark: in.GetDark()}
	return build(c, c.flightKey("broadcast", t.channelId, 0, v), func(ctx context.Context) (*mtproto.Stats_BroadcastStats, error) {
		return c.broadcastStats(ctx, t, v)
	})
}

func absValue(current, previous float64) *mtproto.StatsAbsValueAndPrev {
	return mtproto.MakeTLStatsAbsValueAndPrev(&mtproto.StatsAbsValueAndPrev{Current: current, Previous: previous}).To_StatsAbsValueAndPrev()
}

// perItem is sum per item, 0 without items.
func perItem(sum, items int64) float64 {
	if items == 0 {
		return 0
	}
	return float64(sum) / float64(items)
}

func (c *StatisticsCore) broadcastStats(ctx context.Context, t target, v view) (*mtproto.Stats_BroadcastStats, error) {
	d := c.svcCtx.Dao
	periodFrom := t.now - periodDays*secondsPerDay
	prevFrom := periodFrom - periodDays*secondsPerDay
	out := &mtproto.Stats_BroadcastStats{
		Period: mtproto.MakeTLStatsDateRangeDays(&mtproto.StatsDateRangeDays{
			MinDate: int32(periodFrom),
			MaxDate: int32(t.now),
		}).To_StatsDateRangeDays(),
	}

	// The followers 30 days ago: those of now less the joins and leaves
	// since, as far as they are recorded.
	since, err := c.since(ctx, channelstats.MemberEvents)
	if err != nil {
		return nil, err
	}
	delta, err := d.MemberDelta(ctx, t.channelId, max(periodFrom, since))
	if err != nil {
		return nil, err
	}
	out.Followers = absValue(float64(t.members), float64(max(t.members-delta, 0)))

	posts, err := d.PostTotals(ctx, t.channelId, periodFrom, t.now+1)
	if err != nil {
		return nil, err
	}
	prevPosts, err := d.PostTotals(ctx, t.channelId, prevFrom, periodFrom)
	if err != nil {
		return nil, err
	}
	out.ViewsPerPost = absValue(perItem(posts.Views, posts.Items), perItem(prevPosts.Views, prevPosts.Items))
	out.SharesPerPost = absValue(perItem(posts.Forwards, posts.Items), perItem(prevPosts.Forwards, prevPosts.Items))
	out.ReactionsPerPost = absValue(perItem(posts.Reactions, posts.Items), perItem(prevPosts.Reactions, prevPosts.Items))

	stories, err := d.StoryTotals(ctx, t.channelId, periodFrom, t.now+1)
	if err != nil {
		return nil, err
	}
	prevStories, err := d.StoryTotals(ctx, t.channelId, prevFrom, periodFrom)
	if err != nil {
		return nil, err
	}
	out.ViewsPerStory = absValue(perItem(stories.Views, stories.Items), perItem(prevStories.Views, prevStories.Items))
	out.SharesPerStory = absValue(perItem(stories.Forwards, stories.Items), perItem(prevStories.Forwards, prevStories.Items))
	out.ReactionsPerStory = absValue(perItem(stories.Reactions, stories.Items), perItem(prevStories.Reactions, prevStories.Items))

	if out.EnabledNotifications, err = c.enabledNotifications(ctx, t); err != nil {
		return nil, err
	}

	for _, g := range []struct {
		kind string
		to   **mtproto.StatsGraph
	}{
		{"growth", &out.GrowthGraph},
		{"followers", &out.FollowersGraph},
		{"mute", &out.MuteGraph},
		{"interactions", &out.InteractionsGraph},
		{"views_by_source", &out.ViewsBySourceGraph},
		{"new_followers_by_source", &out.NewFollowersBySourceGraph},
		{"reactions_by_emotion", &out.ReactionsByEmotionGraph},
		{"story_interactions", &out.StoryInteractionsGraph},
		{"story_reactions", &out.StoryReactionsByEmotionGraph},
	} {
		if *g.to, err = c.daily(ctx, g.kind, t, v); err != nil {
			return nil, err
		}
	}

	if out.TopHoursGraph, err = c.topHoursGraph(ctx, t, periodFrom, prevFrom, v); err != nil {
		return nil, err
	}
	out.IvInteractionsGraph = statsgraph.Error(langpack.StatsText(langpack.StatsNoInstantView, v.lang))

	langs, err := c.languagesGraph(ctx, t, max(t.start, t.now-windowDays*secondsPerDay))
	if err != nil {
		return nil, err
	}
	if out.LanguagesGraph, err = langs.Build(langpack.StatsText, v.lang, v.dark); err != nil {
		return nil, err
	}

	if out.RecentPostsInteractions, err = c.recentInteractions(ctx, t); err != nil {
		return nil, err
	}
	return mtproto.MakeTLStatsBroadcastStats(out).To_Stats_BroadcastStats(), nil
}

// enabledNotifications: the members of now whose notifications of the
// channel are on (its own mute_until, else that of all channels; one that
// passed mutes nothing) out of all of them.
func (c *StatisticsCore) enabledNotifications(ctx context.Context, t target) (*mtproto.StatsPercentValue, error) {
	rows, err := c.svcCtx.Dao.MuteSettings(ctx, t.channelId, t.broadcast)
	if err != nil {
		return nil, err
	}
	var on int64
	for _, r := range rows {
		m := r.PeerMute
		if m == -1 {
			m = r.ClassMute
		}
		if int64(m) <= t.now {
			on++
		}
	}
	return mtproto.MakeTLStatsPercentValue(&mtproto.StatsPercentValue{Part: float64(on), Total: float64(len(rows))}).To_StatsPercentValue(), nil
}

// topHoursGraph: the views of the posts by the hour of the day (UTC) in
// the period and in the one before.
func (c *StatisticsCore) topHoursGraph(ctx context.Context, t target, periodFrom, prevFrom int64, v view) (*mtproto.StatsGraph, error) {
	hours := statsgraph.Span(statsgraph.HourOfDay, 0, 0)
	byHour := func(from, to int64) ([]int64, error) {
		rows, err := c.svcCtx.Dao.ViewCounts(ctx, t.channelId, 0, dao.Range{From: from, To: to, Bucket: 3600})
		if err != nil {
			return nil, err
		}
		out := make([]int64, hours.Len())
		for _, r := range rows {
			out[statsgraph.HourOfDay.Bucket(statsgraph.Hour.Start(r.B))] += r.N
		}
		return out, nil
	}
	now, err := byHour(periodFrom, t.now+1)
	if err != nil {
		return nil, err
	}
	before, err := byHour(prevFrom, periodFrom)
	if err != nil {
		return nil, err
	}
	g := &statsgraph.Graph{
		Type: statsgraph.Step, Scale: statsgraph.HourOfDay, X: hours.X(),
		Series: []statsgraph.Series{
			{NameKey: langpack.StatsThisPeriod, Values: now},
			{NameKey: langpack.StatsPreviousPeriod, Values: before},
		},
	}
	return g.Build(langpack.StatsText, v.lang, v.dark)
}

// recentInteractions are the counters of the latest posts and stories,
// together, the latest first.
func (c *StatisticsCore) recentInteractions(ctx context.Context, t target) ([]*mtproto.PostInteractionCounters, error) {
	posts, err := c.svcCtx.Dao.RecentPosts(ctx, t.channelId, recentItems)
	if err != nil {
		return nil, err
	}
	stories, err := c.svcCtx.Dao.RecentStories(ctx, t.channelId, recentItems)
	if err != nil {
		return nil, err
	}
	type item struct {
		date int64
		c    *mtproto.PostInteractionCounters
	}
	items := make([]item, 0, len(posts)+len(stories))
	for _, p := range posts {
		items = append(items, item{p.Date, mtproto.MakeTLPostInteractionCountersMessage(&mtproto.PostInteractionCounters{
			MsgId: p.Id, Views: int32(p.Views), Forwards: int32(p.Forwards), Reactions: int32(p.Reactions),
		}).To_PostInteractionCounters()})
	}
	for _, s := range stories {
		items = append(items, item{s.Date, mtproto.MakeTLPostInteractionCountersStory(&mtproto.PostInteractionCounters{
			StoryId: s.Id, Views: int32(s.Views), Forwards: int32(s.Forwards), Reactions: int32(s.Reactions),
		}).To_PostInteractionCounters()})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].date > items[j].date })
	out := make([]*mtproto.PostInteractionCounters, len(items))
	for i, it := range items {
		out[i] = it.c
	}
	return out, nil
}

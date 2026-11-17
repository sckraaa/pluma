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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"
)

// topListSize is how many members each list of the top members of a
// supergroup names. Android shows ten of the top posters and the rest
// behind "show more" (StatisticActivity.java:394-404); the number Telegram
// gives is not documented (unverified).
const topListSize = 10

// The graphs of supergroups that zoom into the hours of a day. The graph of
// actions does not: its viewers are kept by the day
// (channel_stats_readers), so its hours would have no second series.
func init() {
	for kind, spec := range map[string]graphSpec{
		"group_growth":                {events: channelstats.MemberEvents, kind: megagroupOnly, build: growthGraph},
		"group_members":               {events: channelstats.MemberEvents, kind: megagroupOnly, build: joinsGraph},
		"group_new_members_by_source": {events: channelstats.MemberEvents, kind: megagroupOnly, build: joinsBySourceGraph},
		"group_messages":              {kind: megagroupOnly, build: messagesGraph},
	} {
		if _, ok := graphs[kind]; ok {
			panic(fmt.Sprintf("stats: graph %q twice", kind))
		}
		graphs[kind] = spec
	}
}

// StatsGetMegagroupStats
// stats.getMegagroupStats#dcdf8607 flags:# dark:flags.0?true channel:InputChannel = stats.MegagroupStats;
//
// The statistics of a supergroup for an admin who may see them
// (channelperm.CanViewStats: the creator, an admin who bans): the last 30
// days against the 30 before, the graphs of a year of days (from the start
// of the recording of their events for the graphs of events, ADR 0066,
// p. 3), the hours and the days of the week the members write at, and the
// members who wrote, moderated and added the most in the period, with the
// users they are as the viewer sees them. A broadcast channel is
// MEGAGROUP_REQUIRED.
func (c *StatisticsCore) StatsGetMegagroupStats(in *mtproto.TLStatsGetMegagroupStats) (_ *mtproto.Stats_MegagroupStats, err error) {
	defer c.observe("stats.getMegagroupStats", time.Now(), &err)
	if err = c.enabled(); err != nil {
		return nil, err
	}
	mc, err := c.statsChannel(in.GetChannel(), megagroupOnly)
	if err != nil {
		return nil, err
	}
	ch := mc.GetChannel()
	t := target{
		channelId: ch.GetId(),
		start:     ch.GetDate(),
		members:   int64(ch.GetParticipantsCount()),
		now:       c.now,
	}
	v := view{lang: c.MD.GetLangCode(), dark: in.GetDark()}
	return build(c, c.flightKey("megagroup", t.channelId, 0, v), func(ctx context.Context) (*mtproto.Stats_MegagroupStats, error) {
		return c.megagroupStats(ctx, t, v)
	})
}

// periodDayRange is the days [first, last] of the readers and the admins'
// actions (kept by the UTC day) that stand for the period (from, to]: the
// day of to and those before it, as many as the period has.
func periodDayRange(from, to int64) (first, last int32) {
	return channelstats.Day(from) + 1, channelstats.Day(to)
}

func (c *StatisticsCore) megagroupStats(ctx context.Context, t target, v view) (*mtproto.Stats_MegagroupStats, error) {
	d := c.svcCtx.Dao
	periodFrom := t.now - periodDays*secondsPerDay
	prevFrom := periodFrom - periodDays*secondsPerDay
	out := &mtproto.Stats_MegagroupStats{
		Period: mtproto.MakeTLStatsDateRangeDays(&mtproto.StatsDateRangeDays{
			MinDate: int32(periodFrom),
			MaxDate: int32(t.now),
		}).To_StatsDateRangeDays(),
	}

	// The members 30 days ago: those of now less the joins and leaves
	// since, as far as they are recorded.
	since, err := c.since(ctx, channelstats.MemberEvents)
	if err != nil {
		return nil, err
	}
	delta, err := d.MemberDelta(ctx, t.channelId, max(periodFrom, since))
	if err != nil {
		return nil, err
	}
	out.Members = absValue(float64(t.members), float64(max(t.members-delta, 0)))

	totals, err := d.GroupTotals(ctx, t.channelId, periodFrom, t.now+1)
	if err != nil {
		return nil, err
	}
	prevTotals, err := d.GroupTotals(ctx, t.channelId, prevFrom, periodFrom)
	if err != nil {
		return nil, err
	}
	out.Messages = absValue(float64(totals.Messages), float64(prevTotals.Messages))
	out.Posters = absValue(float64(totals.Posters), float64(prevTotals.Posters))

	if out.Viewers, err = c.viewers(ctx, t, periodFrom, prevFrom); err != nil {
		return nil, err
	}

	// The messages of the window by day and kind, read once: the graphs of
	// messages, actions and weekdays count them.
	window := max(t.start, t.now-windowDays*secondsPerDay)
	windowRange := rangeOf(days(window, t.now))
	kinds, err := d.MessageKinds(ctx, t.channelId, windowRange)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, dayKindsKey{}, &dayKinds{r: windowRange, rows: kinds})

	for _, g := range []struct {
		kind string
		to   **mtproto.StatsGraph
	}{
		{"group_growth", &out.GrowthGraph},
		{"group_members", &out.MembersGraph},
		{"group_new_members_by_source", &out.NewMembersBySourceGraph},
		{"group_messages", &out.MessagesGraph},
	} {
		if *g.to, err = c.daily(ctx, g.kind, t, v); err != nil {
			return nil, err
		}
	}

	langs, err := c.languagesGraph(ctx, t, window)
	if err != nil {
		return nil, err
	}
	if out.LanguagesGraph, err = langs.Build(langpack.StatsText, v.lang, v.dark); err != nil {
		return nil, err
	}
	if out.ActionsGraph, err = c.actionsGraph(ctx, t, window, kinds, v); err != nil {
		return nil, err
	}
	if out.TopHoursGraph, err = c.groupTopHoursGraph(ctx, t, periodFrom, v); err != nil {
		return nil, err
	}
	if out.WeekdaysGraph, err = weekdaysGraph(t, window, kinds, v); err != nil {
		return nil, err
	}

	if err = c.topMembers(ctx, t, periodFrom, out); err != nil {
		return nil, err
	}
	return mtproto.MakeTLStatsMegagroupStats(out).To_Stats_MegagroupStats(), nil
}

// viewers: the members who read or wrote to the group in the period and in
// the one before. The readers are recorded since stats_collection's
// readers: a period before that has none to count, and a period recorded
// in part would show a fall that was not, so the previous counts only when
// all its days are recorded (against a previous of 0 Android shows no
// change, StatisticActivity.java:2998-3006).
func (c *StatisticsCore) viewers(ctx context.Context, t target, periodFrom, prevFrom int64) (*mtproto.StatsAbsValueAndPrev, error) {
	since, err := c.since(ctx, channelstats.Readers)
	if err != nil {
		return nil, err
	}
	d := c.svcCtx.Dao
	first, last := periodDayRange(periodFrom, t.now)
	now, err := d.Readers(ctx, t.channelId, first, last)
	if err != nil {
		return nil, err
	}
	var before int64
	prevFirst, prevLast := periodDayRange(prevFrom, periodFrom)
	if since <= statsgraph.Day.Start(int64(prevFirst)) {
		if before, err = d.Readers(ctx, t.channelId, prevFirst, prevLast); err != nil {
			return nil, err
		}
	}
	return absValue(float64(now), float64(before)), nil
}

// The kinds of messages of a supergroup, the series of its graph of
// messages in their order: the values of channel_messages.stats_kind, which
// the migration (migrate-20261119-channel-stats.sql) defines from the type
// of media of each message (mtproto.GetMediaType) and, for the types that
// hold more than one kind, its media. A message with no media or a link
// preview is text; GetMediaType gives voice messages and round videos one
// type, and a round video is a video/mp4 document
// (mtproto.IsRoundVideoDocument); a poll, a location, a contact, a dice, a
// story and every other media is of the others.
const (
	messageText = iota
	messagePhoto
	messageVideo
	messageVoice
	messageRound
	messageSticker
	messageGif
	messageFile
	messageMusic
	messageOther
	messageKinds
)

// messageKindKeys are the names of the series of the kinds of messages.
var messageKindKeys = [messageKinds]string{
	messageText:    langpack.StatsMessagesText,
	messagePhoto:   langpack.StatsMessagesPhotos,
	messageVideo:   langpack.StatsMessagesVideos,
	messageVoice:   langpack.StatsMessagesVoice,
	messageRound:   langpack.StatsMessagesRound,
	messageSticker: langpack.StatsMessagesStickers,
	messageGif:     langpack.StatsMessagesGifs,
	messageFile:    langpack.StatsMessagesFiles,
	messageMusic:   langpack.StatsMessagesMusic,
	messageOther:   langpack.StatsMessagesOther,
}

// dayKindsKey keys in the context of a request the messages of its group
// by day and kind that it read already (dayKinds).
type dayKindsKey struct{}

// dayKinds are the messages of the group over r by bucket and kind.
type dayKinds struct {
	r    dao.Range
	rows []dao.Count
}

// groupMessageKinds are the messages of the group of t over r by bucket and
// kind: those the request read already (getMegagroupStats reads the window
// of its graphs once), else read now (a zoom into hours).
func groupMessageKinds(ctx context.Context, d *dao.Dao, t target, r dao.Range) ([]dao.Count, error) {
	if k, ok := ctx.Value(dayKindsKey{}).(*dayKinds); ok && k.r == r {
		return k.rows, nil
	}
	return d.MessageKinds(ctx, t.channelId, r)
}

// byKind spreads the messages of rows (K the kind) over b by kind.
func byKind(b statsgraph.Buckets, rows []dao.Count) ([][]int64, error) {
	out := make([][]int64, messageKinds)
	for i := range out {
		out[i] = make([]int64, b.Len())
	}
	for _, r := range rows {
		if r.K < 0 || r.K >= messageKinds {
			return nil, fmt.Errorf("stats: message kind %d", r.K)
		}
		if j, ok := b.Index(r.B); ok {
			out[r.K][j] += r.N
		}
	}
	return out, nil
}

// messagesGraph: the messages of the group in each bucket by kind.
func messagesGraph(ctx context.Context, d *dao.Dao, t target, b statsgraph.Buckets) (*statsgraph.Graph, error) {
	rows, err := groupMessageKinds(ctx, d, t, rangeOf(b))
	if err != nil {
		return nil, err
	}
	values, err := byKind(b, rows)
	if err != nil {
		return nil, err
	}
	g := &statsgraph.Graph{Type: statsgraph.Bar, Stacked: true, Scale: b.Scale, X: b.X()}
	for i, key := range messageKindKeys {
		g.Series = append(g.Series, statsgraph.Series{NameKey: key, Values: values[i]})
	}
	return g, nil
}

// actionsGraph: the messages (dayMessages: those of the window from
// window, by the day) and the viewers (the members who read or wrote,
// channel_stats_readers) of each day, two axes; from when the readers are
// recorded, so the two series start together. The readers are kept by the
// day, so it does not zoom into hours.
func (c *StatisticsCore) actionsGraph(ctx context.Context, t target, window int64, dayMessages []dao.Count, v view) (*mtproto.StatsGraph, error) {
	since, err := c.since(ctx, channelstats.Readers)
	if err != nil {
		return nil, err
	}
	b := days(max(window, since), t.now)
	readers, err := c.svcCtx.Dao.ReaderCounts(ctx, t.channelId, rangeOf(b))
	if err != nil {
		return nil, err
	}
	g := &statsgraph.Graph{
		Type: statsgraph.Line, YScaled: true, Subchart: true, Scale: b.Scale, X: b.X(),
		Series: []statsgraph.Series{
			{NameKey: langpack.StatsMessages, Values: total(b, dayMessages)},
			{NameKey: langpack.StatsViewers, Values: total(b, readers)},
		},
	}
	return g.Build(langpack.StatsText, v.lang, v.dark)
}

// groupTopHoursGraph: the messages of the period by the hour of the day
// (UTC). The clients draw it as one line of 24 hours (Android type 0 with
// the hours' format, StatisticActivity.java:380-384; iOS .hourlyStep,
// GroupStatsController.swift:523).
func (c *StatisticsCore) groupTopHoursGraph(ctx context.Context, t target, periodFrom int64, v view) (*mtproto.StatsGraph, error) {
	rows, err := c.svcCtx.Dao.MessageCounts(ctx, t.channelId, dao.Range{From: periodFrom, To: t.now + 1, Bucket: statsgraph.Hour.Seconds()})
	if err != nil {
		return nil, err
	}
	hours := statsgraph.Span(statsgraph.HourOfDay, 0, 0)
	values := make([]int64, hours.Len())
	for _, r := range rows {
		values[statsgraph.HourOfDay.Bucket(statsgraph.Hour.Start(r.B))] += r.N
	}
	g := &statsgraph.Graph{
		Type: statsgraph.Line, Scale: statsgraph.HourOfDay, X: hours.X(),
		Series: []statsgraph.Series{{NameKey: langpack.StatsMessages, Values: values}},
	}
	return g.Build(langpack.StatsText, v.lang, v.dark)
}

// weekdaysGraph: the messages of each week of the window from window
// (dayMessages: those of the window, by the day) by the day of the week,
// Monday first, as shares. The clients draw it as a pie of the days (iOS
// .pie, GroupStatsController.swift:528; Android type 4 with the week's
// format, StatisticActivity.java:381-387), so it does not zoom; tdesktop
// knows the weeks by 'week' in the tooltip's formatter
// (statistics_data_deserialize.cpp:150-157), which statsgraph writes for
// the scale of weeks.
func weekdaysGraph(t target, window int64, dayMessages []dao.Count, v view) (*mtproto.StatsGraph, error) {
	weeks := statsgraph.Span(statsgraph.Week, window, t.now)
	g := &statsgraph.Graph{
		Type: statsgraph.Area, Percentage: true, Stacked: true, Subchart: true, Scale: weeks.Scale, X: weeks.X(),
	}
	for i, values := range byWeekday(weeks, dayMessages) {
		g.Series = append(g.Series, statsgraph.Series{NameKey: langpack.StatsWeekdays[i], Values: values})
	}
	return g.Build(langpack.StatsText, v.lang, v.dark)
}

// byWeekday spreads the counts of rows, by the day, over weeks by the day
// of the week: seven series, Monday first.
func byWeekday(weeks statsgraph.Buckets, rows []dao.Count) [][]int64 {
	out := make([][]int64, len(langpack.StatsWeekdays))
	for i := range out {
		out[i] = make([]int64, weeks.Len())
	}
	for _, r := range rows {
		start := statsgraph.Day.Start(r.B)
		w := statsgraph.Week.Bucket(start)
		j, ok := weeks.Index(w)
		if !ok {
			continue
		}
		out[r.B-statsgraph.Day.Bucket(statsgraph.Week.Start(w))][j] += r.N
	}
	return out
}

// topMembers fills the members of the group who wrote the most in the
// period, the admins who did the most and the members who added the most,
// and the users they are as the viewer sees them. One the viewer cannot
// be given (an account no longer there) is left out of the lists: Android
// takes the user of each entry from users and draws it
// (StatisticActivity.java:3223-3226, :3277-3284).
func (c *StatisticsCore) topMembers(ctx context.Context, t target, periodFrom int64, out *mtproto.Stats_MegagroupStats) error {
	d := c.svcCtx.Dao
	posters, err := d.TopPosters(ctx, t.channelId, periodFrom, t.now+1, topListSize)
	if err != nil {
		return err
	}
	first, last := periodDayRange(periodFrom, t.now)
	admins, err := d.TopAdmins(ctx, t.channelId, first, last, topListSize)
	if err != nil {
		return err
	}
	inviters, err := d.TopInviters(ctx, t.channelId, periodFrom, t.now+1, topListSize)
	if err != nil {
		return err
	}

	ids := make([]int64, 0, len(posters)+len(admins)+len(inviters))
	seen := make(map[int64]bool, cap(ids))
	add := func(id int64) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, p := range posters {
		add(p.UserId)
	}
	for _, a := range admins {
		add(a.UserId)
	}
	for _, i := range inviters {
		add(i.UserId)
	}

	out.TopPosters = []*mtproto.StatsGroupTopPoster{}
	out.TopAdmins = []*mtproto.StatsGroupTopAdmin{}
	out.TopInviters = []*mtproto.StatsGroupTopInviter{}
	out.Users = []*mtproto.User{}
	if len(ids) == 0 {
		return nil
	}
	me := c.MD.GetUserId()
	users, err := c.svcCtx.Users.UserGetMutableUsersV2(ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      append([]int64{me}, ids...),
		Privacy: true,
		HasTo:   true,
		To:      []int64{me},
	})
	if err != nil {
		return err
	}
	known := func(id int64) bool {
		_, ok := users.GetImmutableUser(id)
		return ok
	}
	given := make([]int64, 0, len(ids))
	for _, id := range ids {
		if known(id) {
			given = append(given, id)
		}
	}
	out.Users = users.GetUserListByIdList(me, given...)

	for _, p := range posters {
		if known(p.UserId) {
			out.TopPosters = append(out.TopPosters, mtproto.MakeTLStatsGroupTopPoster(&mtproto.StatsGroupTopPoster{
				UserId: p.UserId, Messages: int32(p.Messages), AvgChars: int32(p.AvgChars),
			}).To_StatsGroupTopPoster())
		}
	}
	for _, a := range admins {
		if known(a.UserId) {
			out.TopAdmins = append(out.TopAdmins, mtproto.MakeTLStatsGroupTopAdmin(&mtproto.StatsGroupTopAdmin{
				UserId: a.UserId, Deleted: int32(a.Deleted), Kicked: int32(a.Kicked), Banned: int32(a.Banned),
			}).To_StatsGroupTopAdmin())
		}
	}
	for _, i := range inviters {
		if known(i.UserId) {
			out.TopInviters = append(out.TopInviters, mtproto.MakeTLStatsGroupTopInviter(&mtproto.StatsGroupTopInviter{
				UserId: i.UserId, Invitations: int32(i.Invitations),
			}).To_StatsGroupTopInviter())
		}
	}
	return nil
}

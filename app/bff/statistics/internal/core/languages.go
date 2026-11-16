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

	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"
)

// topLanguages is how many languages the graph names; the others are
// summed into one series (ADR 0066, p. 4).
const topLanguages = 9

// languagesGraph: the members of the channel at the end of each day of the
// window from from, by the language of the session each used last
// (lang_code of initConnection, its base language), as shares. The members
// of a day are those of now with the joins and leaves after it undone; for
// a day before the joins and leaves were recorded, the members of then
// with a participant row that joined that day or before (task 103). The
// clients draw it as a pie, so it does not zoom.
func (c *StatisticsCore) languagesGraph(ctx context.Context, t target, from int64) (*statsgraph.Graph, error) {
	since, err := c.since(ctx, channelstats.MemberEvents)
	if err != nil {
		return nil, err
	}
	d := c.svcCtx.Dao
	participants, err := d.Participants(ctx, t.channelId)
	if err != nil {
		return nil, err
	}
	events, err := d.MemberEvents(ctx, t.channelId, from)
	if err != nil {
		return nil, err
	}
	members := make(map[int64]bool, len(participants))
	joinedAt := make(map[int64]int64, len(participants))
	users := make([]int64, 0, len(participants))
	for _, p := range participants {
		joinedAt[p.UserId] = p.Date
		users = append(users, p.UserId)
		if p.State == 0 {
			members[p.UserId] = true
		}
	}
	for _, e := range events {
		if _, ok := joinedAt[e.UserId]; !ok {
			joinedAt[e.UserId] = 0
			users = append(users, e.UserId)
		}
	}
	codes, err := d.LangCodes(ctx, users)
	if err != nil {
		return nil, err
	}
	b := days(from, t.now)
	return languageShares(b, members, joinedAt, events, since, func(u int64) string {
		key, _ := langpack.StatsLanguageKey(codes[u])
		return key
	}), nil
}

// languageShares counts the members by language at the end of each day of
// b; members are those of now, events their joins and leaves from the
// first day on (the latest first), since when they are recorded,
// joinedAt when each joined last, keyOf the key of the name of a user's
// language ("" for one not named).
func languageShares(b statsgraph.Buckets, members map[int64]bool, joinedAt map[int64]int64, events []dao.MemberEvent, since int64, keyOf func(int64) string) *statsgraph.Graph {
	keys := map[string][]int64{}
	count := func(j int, u int64) {
		k := keyOf(u)
		if keys[k] == nil {
			keys[k] = make([]int64, b.Len())
		}
		keys[k][j]++
	}
	in := make(map[int64]bool, len(members))
	for u, ok := range members {
		in[u] = ok
	}
	next := 0
	undo := func(before int64) {
		for ; next < len(events) && events[next].Date >= before; next++ {
			// The members before a join had not the joined; before a leave
			// they had the one who left.
			in[events[next].UserId] = !events[next].Joined
		}
	}
	for j := b.Len() - 1; j >= 0; j-- {
		end := statsgraph.Day.Start(b.First + int64(j) + 1)
		undo(end)
		recorded := end > since
		if !recorded {
			undo(since)
		}
		for u, ok := range in {
			if ok && (recorded || joinedAt[u] < end) {
				count(j, u)
			}
		}
	}

	names := make([]string, 0, len(keys))
	for k := range keys {
		if k != "" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	known := make([]statsgraph.Series, 0, len(names))
	for _, k := range names {
		known = append(known, statsgraph.Series{NameKey: k, Values: keys[k]})
	}
	all := statsgraph.Collapse(known, topLanguages, statsgraph.Series{NameKey: langpack.StatsOtherLanguages})
	if unnamed := keys[""]; unnamed != nil {
		last := len(all) - 1
		if len(known) > topLanguages {
			for j, v := range unnamed {
				all[last].Values[j] += v
			}
		} else {
			all = append(all, statsgraph.Series{NameKey: langpack.StatsOtherLanguages, Values: unnamed})
		}
	}
	return &statsgraph.Graph{
		Type: statsgraph.Area, Percentage: true, Stacked: true, Subchart: true, Scale: b.Scale, X: b.X(),
		Series: all,
	}
}

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
	"strings"

	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"
)

// The windows of time of the statistics (ADR 0066, p. 6; decisions to
// confirm of task 103).
const (
	secondsPerDay = 86400
	// periodDays is the period of the overview, and the one before it is
	// its previous.
	periodDays = 30
	// windowDays bounds the graphs of days.
	windowDays = 365
	// zoomDays is how many days around the one asked for a zoom gives,
	// in hours: three on each side.
	zoomDays = 3
	// hourlyAge is how old a post may be for its graph to be in hours, not
	// days: iOS takes a step of 3600 for an hourly graph
	// (MessageStatistics.swift:104-116, MessageStatsController.swift:
	// 265-277).
	hourlyAge = 48 * 3600
	// recentItems is how many of the latest posts, and of the latest
	// stories, the overview lists.
	recentItems = 50
)

// rangeOf is the unix times the buckets b span, the end excluded.
func rangeOf(b statsgraph.Buckets) dao.Range {
	return dao.Range{From: b.Scale.Start(b.First), To: b.Scale.Start(b.Last + 1), Bucket: b.Scale.Seconds()}
}

// days are the days of a graph from unix time from to now.
func days(from, now int64) statsgraph.Buckets {
	return statsgraph.Span(statsgraph.Day, from, now)
}

// zoomHours are the hours of the zoom into the day of x (milliseconds, the
// x of the point of a graph of days) for a graph that starts at from: the
// zoomDays on each side, not before from nor after now. An x out of the
// graph is taken as its nearest day. Two hours at least, so a zoom is
// always drawn: Android shows a statsGraphError of a zoom as a toast from
// the network callback (StatisticActivity.java:1693) and tdesktop stops
// zooming after one (api_statistics.cpp:268-270).
func zoomHours(from, now, x int64) statsgraph.Buckets {
	first, last := statsgraph.Day.Bucket(from), statsgraph.Day.Bucket(now)
	d := statsgraph.Day.Bucket(floorDiv(x, 1000))
	d = max(first, min(last, d))
	start := max(from, statsgraph.Day.Start(d-zoomDays))
	end := min(now, statsgraph.Day.Start(d+zoomDays+1)-1)
	b := statsgraph.Span(statsgraph.Hour, start, end)
	if b.Len() < statsgraph.MinPoints {
		b.First = b.Last - 1
	}
	return b
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// series spreads the counts of rows over b: one series for each key of
// keys, in their order; the count of a key not among them goes to the
// series of other, when it is not negative.
func series(b statsgraph.Buckets, rows []dao.Count, keys []int64, other int) [][]int64 {
	out := make([][]int64, len(keys))
	for i := range out {
		out[i] = make([]int64, b.Len())
	}
	for _, r := range rows {
		j, ok := b.Index(r.B)
		if !ok {
			continue
		}
		i := indexOf(keys, r.K)
		if i < 0 {
			if other < 0 {
				continue
			}
			i = other
		}
		out[i][j] += r.N
	}
	return out
}

// total is the counts of rows over b, whatever their key.
func total(b statsgraph.Buckets, rows []dao.Count) []int64 {
	out := make([]int64, b.Len())
	for _, r := range rows {
		if j, ok := b.Index(r.B); ok {
			out[j] += r.N
		}
	}
	return out
}

func indexOf(keys []int64, k int64) int {
	for i, v := range keys {
		if v == k {
			return i
		}
	}
	return -1
}

// The emotions of reactions (ADR 0066, p. 6): the series of the graphs of
// reactions by emotion, in their order.
const (
	emotionPositive = iota
	emotionNegative
	emotionOther
	emotions
)

// positiveReactions and negativeReactions sort the standard reactions;
// every other one, and every custom emoji, is of the others. The emoticons
// are without the variation selector U+FE0F, which emotionOf drops.
var (
	positiveReactions = []string{
		"👍", "❤", "🔥", "🥰", "👏", "😁", "🎉", "🤩", "🙏", "👌", "😍", "❤‍🔥", "💯", "🤣", "🏆",
		"🍾", "💋", "😇", "🤝", "🤗", "🫡", "😘", "😎", "💘", "🆒", "⚡", "🕊", "🐳", "🦄", "✍", "🍓",
	}
	negativeReactions = []string{
		"👎", "😱", "🤬", "😢", "🤮", "💩", "🥱", "🥴", "💔", "😐", "🖕", "😭", "😨", "😡", "🤡",
	}
)

// emotionOf is the emotion of the reaction of key (pkg/reactions Key: "e"
// and the emoticon, "c" and the id of a custom emoji).
func emotionOf(key string) int {
	e, ok := strings.CutPrefix(key, "e")
	if !ok {
		return emotionOther
	}
	e = strings.ReplaceAll(e, "\uFE0F", "")
	for _, p := range positiveReactions {
		if e == p {
			return emotionPositive
		}
	}
	for _, n := range negativeReactions {
		if e == n {
			return emotionNegative
		}
	}
	return emotionOther
}

// byEmotion spreads the reactions of rows over b by emotion.
func byEmotion(b statsgraph.Buckets, rows []dao.KeyCount) [][]int64 {
	out := make([][]int64, emotions)
	for i := range out {
		out[i] = make([]int64, b.Len())
	}
	for _, r := range rows {
		if j, ok := b.Index(r.B); ok {
			out[emotionOf(r.K)][j] += r.N
		}
	}
	return out
}

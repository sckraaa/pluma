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

package statsgraph

import "fmt"

// Scale is the step of the x axis of a graph.
type Scale int

const (
	// Day: a point a day, UTC. x is noon UTC of the day, so the date under
	// the point is the local one from UTC-11 to UTC+11 (ADR 0066, p. 4).
	Day Scale = iota + 1
	// Week: a point a week, x is noon UTC of its Monday.
	Week
	// Hour: a point an hour, x is the start of the hour.
	Hour
	// FiveMin: a point every five minutes, x is the start of the five.
	FiveMin
	// HourOfDay: the 24 hours of a day, x is the hour 0-23 itself (UTC).
	// Web K multiplies it by an hour (lib/tchart/format.ts:13-19), Android
	// prints it as "%02d:00" (LegendSignatureView.java:163-164), iOS takes
	// an x below 24 for seconds (ChartsCollection.swift:68-77).
	HourOfDay
)

const (
	secondsPerDay   = 86400
	msPerSecond     = 1000
	noonOffsetMs    = 43200 * msPerSecond
	hoursPerDay     = 24
	daysPerWeek     = 7
	mondayFromEpoch = 3 // 1970-01-01 was a Thursday, the Monday before is day -3
)

func (s Scale) valid() bool { return s >= Day && s <= HourOfDay }

func (s Scale) String() string {
	switch s {
	case Day:
		return "day"
	case Week:
		return "week"
	case Hour:
		return "hour"
	case FiveMin:
		return "5min"
	case HourOfDay:
		return "hour-of-day"
	}
	return fmt.Sprintf("Scale(%d)", int(s))
}

// Seconds is the length of a bucket of s; for HourOfDay, an hour.
func (s Scale) Seconds() int64 {
	switch s {
	case Day:
		return secondsPerDay
	case Week:
		return daysPerWeek * secondsPerDay
	case Hour, HourOfDay:
		return 3600
	case FiveMin:
		return 300
	}
	panic(fmt.Sprintf("statsgraph: bad scale %d", int(s)))
}

// floorDiv is a / b rounded down, so the times before 1970 fall into their
// own buckets, not into the one of zero.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// Bucket is the number of the bucket of s that unix time t falls into:
// days since 1970-01-01, weeks since Monday 1969-12-29, hours or fives of
// minutes since the epoch, or the hour of the day 0-23 (UTC).
func (s Scale) Bucket(t int64) int64 {
	switch s {
	case Day, Hour, FiveMin:
		return floorDiv(t, s.Seconds())
	case Week:
		return floorDiv(floorDiv(t, secondsPerDay)+mondayFromEpoch, daysPerWeek)
	case HourOfDay:
		return floorDiv(t, 3600) - floorDiv(t, secondsPerDay)*hoursPerDay
	}
	panic(fmt.Sprintf("statsgraph: bad scale %d", int(s)))
}

// X is the value of the x axis of bucket b of s: milliseconds since the
// epoch (noon UTC for days and weeks, the start for hours and fives), or
// the hour 0-23 for HourOfDay.
func (s Scale) X(b int64) int64 {
	switch s {
	case Day:
		return b*secondsPerDay*msPerSecond + noonOffsetMs
	case Week:
		return (b*daysPerWeek-mondayFromEpoch)*secondsPerDay*msPerSecond + noonOffsetMs
	case Hour, FiveMin:
		return b * s.Seconds() * msPerSecond
	case HourOfDay:
		return b
	}
	panic(fmt.Sprintf("statsgraph: bad scale %d", int(s)))
}

// Start is the unix time bucket b of s begins at; for HourOfDay, the
// offset of the hour into the day.
func (s Scale) Start(b int64) int64 {
	if s == Week {
		return (b*daysPerWeek - mondayFromEpoch) * secondsPerDay
	}
	return b * s.Seconds()
}

// Buckets is a run of consecutive buckets of one scale, First to Last
// inclusive: the x axis of a graph and the place of each count on it.
type Buckets struct {
	Scale       Scale
	First, Last int64
}

// Span returns the buckets of s from the one of unix time from to the one
// of to, inclusive; from after to gives no buckets. For HourOfDay it gives
// the 24 hours whatever from and to are.
func Span(s Scale, from, to int64) Buckets {
	if !s.valid() {
		panic(fmt.Sprintf("statsgraph: bad scale %d", int(s)))
	}
	if s == HourOfDay {
		return Buckets{Scale: s, First: 0, Last: hoursPerDay - 1}
	}
	return Buckets{Scale: s, First: s.Bucket(from), Last: s.Bucket(to)}
}

// Len is the number of buckets.
func (b Buckets) Len() int {
	if b.Last < b.First {
		return 0
	}
	return int(b.Last - b.First + 1)
}

// X is the x axis of the buckets, one value each.
func (b Buckets) X() []int64 {
	x := make([]int64, b.Len())
	for i := range x {
		x[i] = b.Scale.X(b.First + int64(i))
	}
	return x
}

// Index is the position of bucket n among b, false when it is outside.
func (b Buckets) Index(n int64) (int, bool) {
	if n < b.First || n > b.Last {
		return 0, false
	}
	return int(n - b.First), true
}

// IndexOf is the position of the bucket unix time t falls into, false
// when it is outside b.
func (b Buckets) IndexOf(t int64) (int, bool) {
	return b.Index(b.Scale.Bucket(t))
}

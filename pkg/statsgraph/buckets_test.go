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

import (
	"testing"
	"time"
)

func utc(s string) int64 {
	t, err := time.Parse(time.DateTime, s)
	if err != nil {
		panic(err)
	}
	return t.Unix()
}

func ms(s string) int64 { return utc(s) * 1000 }

func TestBucketX(t *testing.T) {
	tests := []struct {
		scale Scale
		at    string
		x     int64
	}{
		// A day's point is its noon UTC, from its first second to its last.
		{Day, "2026-11-16 00:00:00", ms("2026-11-16 12:00:00")},
		{Day, "2026-11-16 23:59:59", ms("2026-11-16 12:00:00")},
		{Day, "1969-12-31 23:00:00", ms("1969-12-31 12:00:00")},
		// A week's point is noon UTC of its Monday; 2026-11-16 is a Monday.
		{Week, "2026-11-16 00:00:00", ms("2026-11-16 12:00:00")},
		{Week, "2026-11-22 23:59:59", ms("2026-11-16 12:00:00")},
		{Week, "2026-11-23 00:00:00", ms("2026-11-23 12:00:00")},
		{Week, "1970-01-01 00:00:00", ms("1969-12-29 12:00:00")},
		{Week, "1969-12-28 23:59:59", ms("1969-12-22 12:00:00")},
		{Hour, "2026-11-16 13:59:59", ms("2026-11-16 13:00:00")},
		{FiveMin, "2026-11-16 13:04:59", ms("2026-11-16 13:00:00")},
		{FiveMin, "2026-11-16 13:05:00", ms("2026-11-16 13:05:00")},
		{HourOfDay, "2026-11-16 00:59:59", 0},
		{HourOfDay, "2026-11-16 23:00:00", 23},
		{HourOfDay, "1969-12-31 22:30:00", 22},
	}
	for _, tt := range tests {
		if got := tt.scale.X(tt.scale.Bucket(utc(tt.at))); got != tt.x {
			t.Errorf("%v %s: %d, want %d", tt.scale, tt.at, got, tt.x)
		}
	}
}

func TestBucketStart(t *testing.T) {
	for _, s := range []Scale{Day, Week, Hour, FiveMin} {
		at := utc("2026-11-19 17:43:12")
		b := s.Bucket(at)
		start := s.Start(b)
		if start > at || at >= start+s.Seconds() || s.Bucket(start) != b || s.Bucket(start-1) != b-1 {
			t.Errorf("%v: start %d of %d", s, start, at)
		}
	}
	if got := Week.Start(Week.Bucket(utc("2026-11-19 17:43:12"))); got != utc("2026-11-16 00:00:00") {
		t.Errorf("week start %d", got)
	}
}

func TestSpan(t *testing.T) {
	b := Span(Day, utc("2026-11-01 23:00:00"), utc("2026-11-03 01:00:00"))
	x := b.X()
	if b.Len() != 3 || len(x) != 3 || x[0] != ms("2026-11-01 12:00:00") || x[2] != ms("2026-11-03 12:00:00") {
		t.Fatalf("days: %d %v", b.Len(), x)
	}
	if i, ok := b.IndexOf(utc("2026-11-02 00:00:00")); !ok || i != 1 {
		t.Errorf("index of the second day: %d %v", i, ok)
	}
	if _, ok := b.IndexOf(utc("2026-11-04 00:00:00")); ok {
		t.Error("a day after the span is in it")
	}
	if _, ok := b.IndexOf(utc("2026-10-31 23:59:59")); ok {
		t.Error("a day before the span is in it")
	}
	if n := Span(Day, utc("2026-11-03 00:00:00"), utc("2026-11-01 00:00:00")).Len(); n != 0 {
		t.Errorf("reversed span: %d", n)
	}

	h := Span(HourOfDay, 5, 1)
	if h.Len() != 24 || h.X()[0] != 0 || h.X()[23] != 23 {
		t.Errorf("hours of the day: %v", h.X())
	}
	if i, ok := h.IndexOf(utc("2026-11-16 07:15:00")); !ok || i != 7 {
		t.Errorf("hour 7: %d %v", i, ok)
	}

	// Seven days of hours, the zoom of a day.
	z := Span(Hour, utc("2026-11-13 00:00:00"), utc("2026-11-19 23:59:59"))
	if z.Len() != 168 {
		t.Errorf("a week of hours: %d", z.Len())
	}
	x = z.X()
	for i := 1; i < len(x); i++ {
		if x[i]-x[i-1] != 3600_000 {
			t.Fatalf("step at %d: %d", i, x[i]-x[i-1])
		}
	}
}

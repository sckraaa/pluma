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

package business

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

func iv(pairs ...int32) []Interval {
	var out []Interval
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Interval{Start: pairs[i], End: pairs[i+1]})
	}
	return out
}

const (
	sunday = 6 * MinutesPerDay
	week   = MinutesPerWeek
)

// The normalization of core.telegram.org/api/business, step by step as
// tdesktop's WorkingIntervals::normalized does it.
func TestNormalizeIntervals(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []Interval
		want []Interval
	}{
		{"one", iv(540, 1080), iv(540, 1080)},
		{"sorted", iv(2000, 2100, 540, 1080), iv(540, 1080, 2000, 2100)},
		{"overlapping merged", iv(540, 800, 700, 1080), iv(540, 1080)},
		{"adjacent merged", iv(540, 800, 800, 1080), iv(540, 1080)},
		{"inside another", iv(540, 1080, 600, 700), iv(540, 1080)},
		{"Sunday night to Monday", iv(sunday+1320, week+120), iv(0, 120, sunday+1320, week)},
		{"the tail joins Monday morning", iv(60, 600, sunday+1320, week+120), iv(0, 600, sunday+1320, week)},
		{"all of the following Monday", iv(week+20, week+120), iv(20, 120)},
		{"ends at the end of the week", iv(sunday, week), iv(sunday, week)},
		{"longer than a week cut to the week", iv(0, week+MinutesPerDay), iv(0, week)},
		{"open all week from Monday 09:00", iv(540, week+540), iv(0, week)},
		{"invalid ones dropped", iv(-1, 10, 100, 100, 200, 100, 0, week+MinutesPerDay+1, 600, 700), iv(600, 700)},
		{"one minute", iv(0, 1), iv(0, 1)},
		{"29 merged into one", func() []Interval {
			var in []Interval
			for i := int32(0); i < 29; i++ {
				in = append(in, Interval{Start: i * 10, End: i*10 + 10})
			}
			return in
		}(), iv(0, 290)},
	} {
		got, err := NormalizeIntervals(slices.Clone(c.in))
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: %v %v, want %v", c.name, got, err, c.want)
		}
	}

	many := func(n int32) []Interval {
		var in []Interval
		for i := int32(0); i < n; i++ {
			in = append(in, Interval{Start: i * 300, End: i*300 + 60})
		}
		return in
	}
	if got, err := NormalizeIntervals(many(28)); err != nil || len(got) != 28 {
		t.Errorf("28 intervals: %d %v", len(got), err)
	}
	for _, c := range []struct {
		name string
		in   []Interval
		want error
	}{
		{"none", nil, mtproto.ErrBusinessWorkHoursEmpty},
		{"none valid", iv(10, 10, -1, 5), mtproto.ErrBusinessWorkHoursPeriodInvalid},
		{"29 apart", many(29), mtproto.ErrBusinessWorkHoursPeriodInvalid},
	} {
		if got, err := NormalizeIntervals(c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v %v, want %v", c.name, got, err, c.want)
		}
	}
}

func TestMinuteOfWeek(t *testing.T) {
	for _, c := range []struct {
		t    time.Time
		want int32
	}{
		{time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), 0}, // a Monday
		{time.Date(2026, 11, 16, 1, 1, 59, 0, time.UTC), 61},
		{time.Date(2026, 11, 18, 12, 30, 0, 0, time.UTC), 2*MinutesPerDay + 750},
		{time.Date(2026, 11, 22, 23, 59, 0, 0, time.UTC), week - 1}, // Sunday
	} {
		if got := MinuteOfWeek(c.t); got != c.want {
			t.Errorf("%v: minute %d, want %d", c.t, got, c.want)
		}
	}
}

func TestOpenAt(t *testing.T) {
	night := iv(0, 120, sunday+1320, week) // Sunday 22:00 to Monday 02:00
	for _, c := range []struct {
		m    int32
		want bool
	}{
		{week - 1, true}, // Sunday 23:59
		{0, true},        // Monday 00:00
		{119, true},
		{120, false},
		{sunday + 1319, false},
		{sunday + 1320, true},
	} {
		if got := OpenAt(night, c.m); got != c.want {
			t.Errorf("minute %d: open %v, want %v", c.m, got, c.want)
		}
	}
	// Intervals as a client sends them, past the end of the week.
	if !OpenAt(iv(sunday+1320, week+120), 60) {
		t.Error("Monday 01:00 within Sunday 22:00 to Monday 02:00 not kept normalized")
	}
}

func TestOpenNow(t *testing.T) {
	berlin := &Hours{TimezoneID: "Europe/Berlin", Intervals: iv(540, 1080)} // Monday 09:00-18:00
	for _, c := range []struct {
		name string
		h    *Hours
		now  time.Time
		want bool
	}{
		{"Monday 09:30 in Berlin", berlin, time.Date(2026, 11, 16, 8, 30, 0, 0, time.UTC), true},
		{"Monday 08:30 in Berlin", berlin, time.Date(2026, 11, 16, 7, 30, 0, 0, time.UTC), false},
		{"Monday 18:00 in Berlin", berlin, time.Date(2026, 11, 16, 17, 0, 0, 0, time.UTC), false},
		// Monday 01:00 UTC is still Sunday in New York.
		{"Sunday evening in New York", &Hours{TimezoneID: "America/New_York", Intervals: iv(sunday+1080, sunday+1260)},
			time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC), true},
		// The clocks of Berlin go to summer time on 29 March 2026 at 01:00
		// UTC: 01:30 UTC is 03:30 there, within Sunday 03:00-04:00, and
		// 00:30 UTC is 01:30, outside it.
		{"after the change to summer time", &Hours{TimezoneID: "Europe/Berlin", Intervals: iv(sunday+180, sunday+240)},
			time.Date(2026, 3, 29, 1, 30, 0, 0, time.UTC), true},
		{"before the change to summer time", &Hours{TimezoneID: "Europe/Berlin", Intervals: iv(sunday+180, sunday+240)},
			time.Date(2026, 3, 29, 0, 30, 0, 0, time.UTC), false},
	} {
		got, err := c.h.OpenNow(c.now)
		if err != nil || got != c.want {
			t.Errorf("%s: open %v %v, want %v", c.name, got, err, c.want)
		}
	}
	if _, err := (&Hours{TimezoneID: "Mars/Olympus", Intervals: iv(0, 60)}).OpenNow(time.Now()); !errors.Is(err, ErrTimezoneUnknown) {
		t.Errorf("an unknown time zone: %v", err)
	}
}

func TestLoadTimezone(t *testing.T) {
	for _, id := range []string{"", "Local", "Mars/Olympus", "../etc/passwd", "/etc/localtime"} {
		if _, err := LoadTimezone(id); !errors.Is(err, ErrTimezoneUnknown) {
			t.Errorf("%q: %v, want ErrTimezoneUnknown", id, err)
		}
	}
	for _, id := range []string{"Europe/Berlin", "Asia/Kolkata", "America/Argentina/Buenos_Aires", "UTC"} {
		loc, err := LoadTimezone(id)
		if err != nil || loc.String() != id {
			t.Errorf("%q: %v %v", id, loc, err)
		}
		again, err := LoadTimezone(id)
		if err != nil || again != loc {
			t.Errorf("%q a second time: %p %v, want the same %p", id, again, err, loc)
		}
	}
}

func TestHoursToTL(t *testing.T) {
	h := &Hours{TimezoneID: "Europe/Berlin", Intervals: iv(540, 1080, 2000, 2100)}
	got, err := h.ToTL(time.Date(2026, 11, 16, 8, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.GetPredicateName() != mtproto.Predicate_businessWorkHours || !got.GetOpenNow() || got.GetTimezoneId() != "Europe/Berlin" ||
		len(got.GetWeeklyOpen()) != 2 || got.GetWeeklyOpen()[1].GetStartMinute() != 2000 || got.GetWeeklyOpen()[1].GetEndMinute() != 2100 ||
		got.GetWeeklyOpen()[0].GetPredicateName() != mtproto.Predicate_businessWeeklyOpen {
		t.Errorf("businessWorkHours %v", got)
	}
	if back := IntervalsFromTL(got.GetWeeklyOpen()); !slices.Equal(back, h.Intervals) {
		t.Errorf("the intervals back: %v", back)
	}
}

func TestIntervalJSON(t *testing.T) {
	data, err := json.Marshal(iv(0, 120, sunday+1320, week))
	if err != nil || string(data) != "[[0,120],[9960,10080]]" {
		t.Fatalf("%s %v", data, err)
	}
	var back []Interval
	if err = json.Unmarshal(data, &back); err != nil || !slices.Equal(back, iv(0, 120, sunday+1320, week)) {
		t.Errorf("%v %v", back, err)
	}
	if err = json.Unmarshal([]byte(`[[1,2,3]]`), &back); err == nil {
		t.Error("an interval of three numbers read")
	}
}

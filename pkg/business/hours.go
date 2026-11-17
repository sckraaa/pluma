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
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// The minutes of the week of work hours (core.telegram.org/api/business):
// minute 0 is Monday 00:00 ("61 is Monday 01:01"), an interval may end on
// the following Monday, up to 8×24×60, to cross the night from Sunday to
// Monday.
const (
	MinutesPerDay  = 24 * 60
	MinutesPerWeek = 7 * MinutesPerDay
	endMinuteMax   = MinutesPerWeek + MinutesPerDay
	// WorkHoursIntervalsMax is the most intervals of work hours, after
	// normalization ("a maximum of 28 intervals", /api/business).
	WorkHoursIntervalsMax = 28
)

// Interval is the open time [Start, End) of work hours, in minutes of the
// week.
type Interval struct {
	Start int32
	End   int32
}

func (i Interval) empty() bool { return i.Start >= i.End }

func (i Interval) intersected(o Interval) Interval {
	r := Interval{Start: max(i.Start, o.Start), End: min(i.End, o.End)}
	if r.empty() {
		return Interval{}
	}
	return r
}

// MarshalJSON keeps an interval as [start, end].
func (i Interval) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]int32{i.Start, i.End})
}

// UnmarshalJSON reads an interval kept as [start, end].
func (i *Interval) UnmarshalJSON(data []byte) error {
	var v []int32
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if len(v) != 2 {
		return fmt.Errorf("business: an interval of %d numbers", len(v))
	}
	i.Start, i.End = v[0], v[1]
	return nil
}

// Hours are the work hours of an account: its time zone, an id of
// help.getTimezonesList, and its normalized intervals.
type Hours struct {
	TimezoneID string
	Intervals  []Interval
}

// NormalizeIntervals returns in as the server keeps it, as
// core.telegram.org/api/business asks: intervals out of range or shorter
// than a minute are dropped, the rest sorted, overlapping and adjacent ones
// merged, the part past the end of the week moved to its start and merged
// again, so that an interval longer than a week is cut to the week. It is
// the normalization of tdesktop, WorkingIntervals::normalized
// (data/business/data_business_common.cpp:20-50, 284-286), in minutes.
// No intervals is BUSINESS_WORK_HOURS_EMPTY; none valid, or more than
// WorkHoursIntervalsMax after merging, BUSINESS_WORK_HOURS_PERIOD_INVALID.
func NormalizeIntervals(in []Interval) ([]Interval, error) {
	if len(in) == 0 {
		return nil, mtproto.ErrBusinessWorkHoursEmpty
	}
	list := make([]Interval, 0, len(in))
	for _, i := range in {
		if i.Start < 0 || i.End > endMinuteMax || i.empty() {
			continue
		}
		list = append(list, i)
	}
	list = sortAndMerge(moveTailToFront(sortAndMerge(list)))
	if len(list) == 0 || len(list) > WorkHoursIntervalsMax {
		return nil, mtproto.ErrBusinessWorkHoursPeriodInvalid
	}
	return list, nil
}

// sortAndMerge sorts list by start and merges the intervals that overlap
// or touch (SortAndMerge of tdesktop).
func sortAndMerge(list []Interval) []Interval {
	slices.SortStableFunc(list, func(a, b Interval) int { return int(a.Start - b.Start) })
	out := list[:0]
	for _, i := range list {
		if n := len(out); n > 0 && i.Start <= out[n-1].End {
			out[n-1].End = max(out[n-1].End, i.End)
			continue
		}
		out = append(out, i)
	}
	return out
}

// moveTailToFront moves the part of the sorted, merged list that lies in
// the following Monday to the start of the week (MoveTailToFront of
// tdesktop).
func moveTailToFront(list []Interval) []Interval {
	after := Interval{Start: MinutesPerWeek, End: endMinuteMax}
	for len(list) > 0 {
		last := &list[len(list)-1]
		tail := last.intersected(after)
		if tail.empty() {
			break
		}
		last.End = tail.Start
		if last.empty() {
			list = list[:len(list)-1]
		}
		list = slices.Insert(list, 0, Interval{Start: tail.Start - MinutesPerWeek, End: tail.End - MinutesPerWeek})
	}
	return list
}

// MinuteOfWeek is the minute of the week of t in its location, Monday
// 00:00 being 0.
func MinuteOfWeek(t time.Time) int32 {
	day := (int32(t.Weekday()) + 6) % 7 // time.Monday is 1, Sunday 0
	return day*MinutesPerDay + int32(t.Hour())*60 + int32(t.Minute())
}

// OpenAt tells whether intervals are open at the minute of the week m: an
// interval holds m, or m of the following week for an interval that
// crosses into it.
func OpenAt(intervals []Interval, m int32) bool {
	for _, i := range intervals {
		if (i.Start <= m && m < i.End) || (i.Start <= m+MinutesPerWeek && m+MinutesPerWeek < i.End) {
			return true
		}
	}
	return false
}

// OpenNow tells whether h is open at now, in the time zone of h:
// businessWorkHours.open_now, which only the server sets.
func (h *Hours) OpenNow(now time.Time) (bool, error) {
	loc, err := LoadTimezone(h.TimezoneID)
	if err != nil {
		return false, err
	}
	return OpenAt(h.Intervals, MinuteOfWeek(now.In(loc))), nil
}

// ToTL is h as businessWorkHours at now.
func (h *Hours) ToTL(now time.Time) (*mtproto.BusinessWorkHours, error) {
	open, err := h.OpenNow(now)
	if err != nil {
		return nil, err
	}
	weekly := make([]*mtproto.BusinessWeeklyOpen, 0, len(h.Intervals))
	for _, i := range h.Intervals {
		weekly = append(weekly, mtproto.MakeTLBusinessWeeklyOpen(&mtproto.BusinessWeeklyOpen{
			StartMinute: i.Start,
			EndMinute:   i.End,
		}).To_BusinessWeeklyOpen())
	}
	return mtproto.MakeTLBusinessWorkHours(&mtproto.BusinessWorkHours{
		OpenNow:    open,
		TimezoneId: h.TimezoneID,
		WeeklyOpen: weekly,
	}).To_BusinessWorkHours(), nil
}

// IntervalsFromTL are the intervals of weekly, as the client sent them.
func IntervalsFromTL(weekly []*mtproto.BusinessWeeklyOpen) []Interval {
	out := make([]Interval, 0, len(weekly))
	for _, w := range weekly {
		out = append(out, Interval{Start: w.GetStartMinute(), End: w.GetEndMinute()})
	}
	return out
}

// ErrTimezoneUnknown is the error of LoadTimezone for an id that names no
// time zone.
var ErrTimezoneUnknown = errors.New("business: unknown time zone")

var timezones sync.Map // id → *time.Location, of the ids loaded once

// LoadTimezone returns the time zone id names, an IANA name as
// help.getTimezonesList gives them. "" and "Local", which time.LoadLocation
// takes for UTC and for the zone of the host, name none here.
func LoadTimezone(id string) (*time.Location, error) {
	if v, ok := timezones.Load(id); ok {
		return v.(*time.Location), nil
	}
	if id == "" || id == "Local" {
		return nil, ErrTimezoneUnknown
	}
	loc, err := time.LoadLocation(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTimezoneUnknown, err)
	}
	timezones.Store(id, loc)
	return loc, nil
}

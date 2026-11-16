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
	"errors"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

const day = 86400

func TestValidNoActivityDays(t *testing.T) {
	for _, d := range []int32{7, 14, 21, 28} {
		if !ValidNoActivityDays(d) {
			t.Errorf("%d days refused", d)
		}
	}
	for _, d := range []int32{0, 1, 6, 8, 30, -7} {
		if ValidNoActivityDays(d) {
			t.Errorf("%d days taken", d)
		}
	}
}

func TestAwayScheduleFromTL(t *testing.T) {
	custom := func(start, end int32) *mtproto.BusinessAwayMessageSchedule {
		return mtproto.MakeTLBusinessAwayMessageScheduleCustom(&mtproto.BusinessAwayMessageSchedule{StartDate: start, EndDate: end}).To_BusinessAwayMessageSchedule()
	}
	for _, c := range []struct {
		name       string
		in         *mtproto.BusinessAwayMessageSchedule
		want       AwaySchedule
		start, end int32
		ok         bool
	}{
		{"always", mtproto.MakeTLBusinessAwayMessageScheduleAlways(nil).To_BusinessAwayMessageSchedule(), AwayAlways, 0, 0, true},
		{"outside the hours", mtproto.MakeTLBusinessAwayMessageScheduleOutsideWorkHours(nil).To_BusinessAwayMessageSchedule(), AwayOutsideWorkHours, 0, 0, true},
		{"custom", custom(100, 200), AwayCustom, 100, 200, true},
		{"custom ending as it starts", custom(100, 100), 0, 0, 0, false},
		{"custom ending before it starts", custom(200, 100), 0, 0, 0, false},
		{"none", nil, 0, 0, 0, false},
	} {
		s, start, end, ok := AwayScheduleFromTL(c.in)
		if s != c.want || start != c.start || end != c.end || ok != c.ok {
			t.Errorf("%s: %d %d %d %v, want %d %d %d %v", c.name, s, start, end, ok, c.want, c.start, c.end, c.ok)
		}
	}
}

// The settings read back as the methods answer them in userFull.
func TestAutoReplyTL(t *testing.T) {
	g := &AutoReply{Kind: KindGreeting, ShortcutID: 3, NoActivityDays: 14, Recipients: Recipients{Contacts: true, Users: []int64{9}}}
	gt := g.GreetingTL()
	if gt.GetShortcutId() != 3 || gt.GetNoActivityDays() != 14 || !gt.GetRecipients().GetContacts() || len(gt.GetRecipients().GetUsers()) != 1 {
		t.Errorf("greeting %v", gt)
	}
	a := &AutoReply{Kind: KindAway, ShortcutID: 4, OfflineOnly: true, Schedule: AwayCustom, ScheduleStart: 10, ScheduleEnd: 20,
		Recipients: Recipients{ExcludeSelected: true}}
	at := a.AwayTL()
	if at.GetShortcutId() != 4 || !at.GetOfflineOnly() || !at.GetRecipients().GetExcludeSelected() ||
		at.GetSchedule().GetPredicateName() != mtproto.Predicate_businessAwayMessageScheduleCustom ||
		at.GetSchedule().GetStartDate() != 10 || at.GetSchedule().GetEndDate() != 20 {
		t.Errorf("away %v", at)
	}
	for s, want := range map[AwaySchedule]string{
		AwayAlways:           mtproto.Predicate_businessAwayMessageScheduleAlways,
		AwayOutsideWorkHours: mtproto.Predicate_businessAwayMessageScheduleOutsideWorkHours,
	} {
		a.Schedule = s
		if got := a.AwayTL().GetSchedule().GetPredicateName(); got != want {
			t.Errorf("schedule %d: %s, want %s", s, got, want)
		}
	}
}

func TestChatHistoryIsNew(t *testing.T) {
	if !(ChatHistory{}).IsNew(1000) {
		t.Error("a chat with nothing in it is not new")
	}
	if !(ChatHistory{Any: true, First: 1000, Last: 2000}).IsNew(1000) {
		t.Error("a chat begun as the setting was turned on is not new")
	}
	if (ChatHistory{Any: true, First: 999, Last: 2000}).IsNew(1000) {
		t.Error("a chat begun before the setting is new")
	}
}

// The greeting goes on the first message, and again after the days without
// messages: 6 days of silence are too few for 7, 8 are enough.
func TestGreetingDue(t *testing.T) {
	g := &AutoReply{Kind: KindGreeting, NoActivityDays: 7}
	now := int32(100 * day)
	for _, c := range []struct {
		name string
		h    ChatHistory
		want bool
	}{
		{"an empty chat", ChatHistory{}, true},
		{"silent 6 days", ChatHistory{Any: true, First: 1, Last: now - 6*day}, false},
		{"silent 7 days less a second", ChatHistory{Any: true, First: 1, Last: now - 7*day + 1}, false},
		{"silent 7 days", ChatHistory{Any: true, First: 1, Last: now - 7*day}, true},
		{"silent 8 days", ChatHistory{Any: true, First: 1, Last: now - 8*day}, true},
		{"a message a minute ago", ChatHistory{Any: true, First: 1, Last: now - 60}, false},
	} {
		if got := g.GreetingDue(c.h, now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAwayScheduled(t *testing.T) {
	// Monday 2026-11-16 09:30 in Berlin.
	berlin, err := LoadTimezone("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	monday0930 := time.Date(2026, 11, 16, 9, 30, 0, 0, berlin)
	// Open Monday 09:00-18:00.
	hours := &Hours{TimezoneID: "Europe/Berlin", Intervals: []Interval{{Start: 540, End: 1080}}}
	for _, c := range []struct {
		name  string
		a     AutoReply
		hours *Hours
		at    time.Time
		want  bool
	}{
		{"always", AutoReply{Schedule: AwayAlways}, nil, monday0930, true},
		{"outside the hours, open", AutoReply{Schedule: AwayOutsideWorkHours}, hours, monday0930, false},
		{"outside the hours, closed", AutoReply{Schedule: AwayOutsideWorkHours}, hours, monday0930.Add(9 * time.Hour), true},
		{"outside the hours, no hours", AutoReply{Schedule: AwayOutsideWorkHours}, nil, monday0930, false},
		{"custom, before", AutoReply{Schedule: AwayCustom, ScheduleStart: int32(monday0930.Unix()) + 1, ScheduleEnd: int32(monday0930.Unix()) + 100}, nil, monday0930, false},
		{"custom, at the start", AutoReply{Schedule: AwayCustom, ScheduleStart: int32(monday0930.Unix()), ScheduleEnd: int32(monday0930.Unix()) + 100}, nil, monday0930, true},
		{"custom, at the end", AutoReply{Schedule: AwayCustom, ScheduleStart: int32(monday0930.Unix()) - 100, ScheduleEnd: int32(monday0930.Unix())}, nil, monday0930, false},
	} {
		got, err := c.a.AwayScheduled(c.hours, c.at)
		if err != nil || got != c.want {
			t.Errorf("%s: %v %v, want %v", c.name, got, err, c.want)
		}
	}
}

// offline_only: "if the account was online in the last 10 minutes" it does
// not go.
func TestOwnerAway(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	for _, c := range []struct {
		name      string
		seenUntil int64
		want      bool
	}{
		{"online", now.Unix() + 30, false},
		{"offline 9 minutes", now.Add(-9 * time.Minute).Unix(), false},
		{"offline 10 minutes", now.Add(-10 * time.Minute).Unix(), true},
		{"offline 11 minutes", now.Add(-11 * time.Minute).Unix(), true},
		{"never seen", 0, true},
	} {
		if got := OwnerAway(c.seenUntil, now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAwayRepeatDue(t *testing.T) {
	now := time.Unix(10*day, 0)
	wrote := func(v bool) func(int32) (bool, error) { return func(int32) (bool, error) { return v, nil } }
	asked := false
	ask := func(int32) (bool, error) { asked = true; return true, nil }
	for _, c := range []struct {
		name  string
		last  int32
		wrote func(int32) (bool, error)
		want  bool
	}{
		{"never sent", 0, wrote(false), true},
		{"sent an hour ago, answered", int32(now.Unix()) - 3600, wrote(true), false},
		{"sent a day ago, answered", int32(now.Unix()) - day, wrote(true), true},
		{"sent a day ago, not answered", int32(now.Unix()) - day, wrote(false), false},
	} {
		got, err := AwayRepeatDue(c.last, now, c.wrote)
		if err != nil || got != c.want {
			t.Errorf("%s: %v %v, want %v", c.name, got, err, c.want)
		}
	}
	if _, err := AwayRepeatDue(int32(now.Unix())-60, now, ask); err != nil || asked {
		t.Errorf("the chat was read for a reply sent a minute ago: %v", err)
	}
	boom := errors.New("boom")
	if _, err := AwayRepeatDue(int32(now.Unix())-2*day, now, func(int32) (bool, error) { return false, boom }); !errors.Is(err, boom) {
		t.Errorf("the error of the read is %v", err)
	}
}

// fakeState is the queue and the log of one chat.
type fakeState struct {
	queued map[AutoReplyKind]bool
	last   map[AutoReplyKind]int32
	wrote  bool
}

func (s fakeState) Queued(k AutoReplyKind) (bool, error)    { return s.queued[k], nil }
func (s fakeState) LastSent(k AutoReplyKind) (int32, error) { return s.last[k], nil }
func (s fakeState) OwnerWroteSince(int32) (bool, error)     { return s.wrote, nil }

func kinds(list []*AutoReply) []AutoReplyKind {
	out := make([]AutoReplyKind, 0, len(list))
	for _, a := range list {
		out = append(out, a.Kind)
	}
	return out
}

func TestDecideAutoReplies(t *testing.T) {
	now := time.Unix(100*day, 0)
	enabled := int32(now.Unix()) - 30*day
	greeting := &AutoReply{Kind: KindGreeting, NoActivityDays: 7, EnabledAt: enabled, Recipients: Recipients{NewChats: true, ExistingChats: true}}
	away := &AutoReply{Kind: KindAway, Schedule: AwayAlways, EnabledAt: enabled, Recipients: Recipients{ExcludeSelected: true}}
	stranger := Peer{UserID: 5}
	empty := ChatHistory{}
	recent := ChatHistory{Any: true, First: enabled - day, Last: int32(now.Unix()) - 60}
	none := fakeState{}

	for _, c := range []struct {
		name     string
		settings []*AutoReply
		in       Incoming
		st       fakeState
		want     []AutoReplyKind
	}{
		{"a new chat gets both, the greeting first", []*AutoReply{away, greeting},
			Incoming{Peer: stranger, History: empty, Now: now}, none, []AutoReplyKind{KindGreeting, KindAway}},
		{"a busy chat gets the away message", []*AutoReply{greeting, away},
			Incoming{Peer: stranger, History: recent, Now: now}, none, []AutoReplyKind{KindAway}},
		{"queued already", []*AutoReply{greeting},
			Incoming{Peer: stranger, History: empty, Now: now}, fakeState{queued: map[AutoReplyKind]bool{KindGreeting: true}}, nil},
		{"away sent an hour ago", []*AutoReply{away},
			Incoming{Peer: stranger, History: recent, Now: now}, fakeState{last: map[AutoReplyKind]int32{KindAway: int32(now.Unix()) - 3600}, wrote: true}, nil},
		{"away sent two days ago, answered since", []*AutoReply{away},
			Incoming{Peer: stranger, History: recent, Now: now}, fakeState{last: map[AutoReplyKind]int32{KindAway: int32(now.Unix()) - 2*day}, wrote: true}, []AutoReplyKind{KindAway}},
		{"offline only, the owner online", []*AutoReply{{Kind: KindAway, Schedule: AwayAlways, OfflineOnly: true, Recipients: Recipients{ExcludeSelected: true}}},
			Incoming{Peer: stranger, History: recent, SeenUntil: now.Unix() + 60, Now: now}, none, nil},
		{"offline only, the owner away", []*AutoReply{{Kind: KindAway, Schedule: AwayAlways, OfflineOnly: true, Recipients: Recipients{ExcludeSelected: true}}},
			Incoming{Peer: stranger, History: recent, SeenUntil: now.Unix() - 3600, Now: now}, none, []AutoReplyKind{KindAway}},
		// The chat began before the greeting was turned on: an existing
		// one, which "new chats" leaves out.
		{"new chats only, an existing chat", []*AutoReply{{Kind: KindGreeting, NoActivityDays: 7, EnabledAt: enabled, Recipients: Recipients{NewChats: true}}},
			Incoming{Peer: stranger, History: ChatHistory{Any: true, First: enabled - day, Last: enabled - 20*day}, Now: now}, none, nil},
		{"new chats only, a new chat", []*AutoReply{{Kind: KindGreeting, NoActivityDays: 7, EnabledAt: enabled, Recipients: Recipients{NewChats: true}}},
			Incoming{Peer: stranger, History: empty, Now: now}, none, []AutoReplyKind{KindGreeting}},
		{"contacts only, a stranger", []*AutoReply{{Kind: KindGreeting, NoActivityDays: 7, Recipients: Recipients{Contacts: true}}},
			Incoming{Peer: stranger, History: empty, Now: now}, none, nil},
		{"contacts only, a contact", []*AutoReply{{Kind: KindGreeting, NoActivityDays: 7, Recipients: Recipients{Contacts: true}}},
			Incoming{Peer: Peer{UserID: 5, Contact: true}, History: empty, Now: now}, none, []AutoReplyKind{KindGreeting}},
		{"all but the user", []*AutoReply{{Kind: KindGreeting, NoActivityDays: 7, Recipients: Recipients{ExcludeSelected: true, Users: []int64{5}}}},
			Incoming{Peer: stranger, History: empty, Now: now}, none, nil},
	} {
		got, err := DecideAutoReplies(c.settings, c.in, c.st)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if g := kinds(got); len(g) != len(c.want) || (len(g) > 0 && !equalKinds(g, c.want)) {
			t.Errorf("%s: %v, want %v", c.name, g, c.want)
		}
	}
}

func equalKinds(a, b []AutoReplyKind) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

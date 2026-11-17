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

package readmetrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

func good() Metric {
	return Metric{MsgId: 5, ViewId: 77, TimeInViewMs: 1200, ActiveTimeInViewMs: 800, HeightToViewportRatioPermille: 400, SeenRangeRatioPermille: 1000}
}

func TestValidate(t *testing.T) {
	tall := good()
	tall.HeightToViewportRatioPermille = 3500 // a post taller than the screen
	zero := Metric{MsgId: 1}
	if err := Validate([]Metric{good(), tall, zero}); err != nil {
		t.Fatalf("valid metrics: %v", err)
	}
	if err := Validate(nil); err != nil {
		t.Fatalf("no metrics: %v", err)
	}
	full := make([]Metric, MaxMetrics)
	for i := range full {
		full[i] = good()
	}
	if err := Validate(full); err != nil {
		t.Fatalf("%d metrics: %v", MaxMetrics, err)
	}
	if err := Validate(append(full, good())); !errors.Is(err, mtproto.ErrLimitInvalid) {
		t.Errorf("%d metrics: %v, want LIMIT_INVALID", MaxMetrics+1, err)
	}
	for _, tc := range []struct {
		name string
		edit func(*Metric)
		want error
	}{
		{"msg_id 0", func(m *Metric) { m.MsgId = 0 }, mtproto.ErrMsgIdInvalid},
		{"msg_id negative", func(m *Metric) { m.MsgId = -3 }, mtproto.ErrMsgIdInvalid},
		{"time in view negative", func(m *Metric) { m.TimeInViewMs = -1 }, mtproto.ErrInputRequestInvalid},
		{"active time negative", func(m *Metric) { m.ActiveTimeInViewMs = -1 }, mtproto.ErrInputRequestInvalid},
		{"height ratio negative", func(m *Metric) { m.HeightToViewportRatioPermille = -1 }, mtproto.ErrInputRequestInvalid},
		{"seen range negative", func(m *Metric) { m.SeenRangeRatioPermille = -1 }, mtproto.ErrInputRequestInvalid},
		{"seen range over the whole", func(m *Metric) { m.SeenRangeRatioPermille = 1001 }, mtproto.ErrInputRequestInvalid},
	} {
		bad := good()
		tc.edit(&bad)
		if err := Validate([]Metric{good(), bad}); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestFromInput(t *testing.T) {
	in := mtproto.MakeTLInputMessageReadMetric(&mtproto.InputMessageReadMetric{
		MsgId: 9, ViewId: -4, TimeInViewMs: 1, ActiveTimeInViewMs: 2, HeightToViewportRatioPermille: 3, SeenRangeRatioPermille: 4,
	}).To_InputMessageReadMetric()
	got := FromInput([]*mtproto.InputMessageReadMetric{in})
	want := Metric{MsgId: 9, ViewId: -4, TimeInViewMs: 1, ActiveTimeInViewMs: 2, HeightToViewportRatioPermille: 3, SeenRangeRatioPermille: 4}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("FromInput: %+v", got)
	}
}

func TestUpTo(t *testing.T) {
	ms := []Metric{{MsgId: 3}, {MsgId: 10}, {MsgId: 7}}
	if got := MaxMsgId(ms); got != 10 {
		t.Errorf("MaxMsgId: %d", got)
	}
	kept := UpTo(ms, 7)
	if len(kept) != 2 || kept[0].MsgId != 3 || kept[1].MsgId != 7 {
		t.Errorf("UpTo 7: %+v", kept)
	}
	if ms[1].MsgId != 10 {
		t.Errorf("UpTo changed its input: %+v", ms)
	}
	if got := UpTo(ms, 0); len(got) != 0 {
		t.Errorf("UpTo 0: %+v", got)
	}
}

func TestAggregate(t *testing.T) {
	rows := []metricRow{
		{MsgId: 1, UserId: 10, TimeInViewMs: 100, ActiveTimeInViewMs: 50, SeenRangePermille: 1000},
		{MsgId: 1, UserId: 10, TimeInViewMs: 300, ActiveTimeInViewMs: 10, SeenRangePermille: 200},
		{MsgId: 1, UserId: 11, TimeInViewMs: 200, ActiveTimeInViewMs: 30, SeenRangePermille: 500},
		{MsgId: 2, UserId: 12, TimeInViewMs: 40, ActiveTimeInViewMs: 40, SeenRangePermille: 900},
		{MsgId: 2, UserId: 13, TimeInViewMs: 60, ActiveTimeInViewMs: 20, SeenRangePermille: 100},
	}
	out := aggregate([]MessageStats{{MsgId: 2}, {MsgId: 1}, {MsgId: 3}}, rows)
	want := []MessageStats{
		{MsgId: 2, Views: 2, Readers: 2, TimeInViewMsSum: 100, ActiveTimeInViewMsSum: 60, TimeInViewMsMedian: 40, ActiveTimeInViewMsMedian: 20, SeenRangePermilleMedian: 100},
		{MsgId: 1, Views: 3, Readers: 2, TimeInViewMsSum: 600, ActiveTimeInViewMsSum: 90, TimeInViewMsMedian: 200, ActiveTimeInViewMsMedian: 30, SeenRangePermilleMedian: 500},
		{MsgId: 3},
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("stats %d: %+v, want %+v", i, out[i], want[i])
		}
	}
}

func TestAggregateOfChannelsOnly(t *testing.T) {
	s := &Store{}
	if _, err := s.Aggregate(context.Background(), mtproto.PEER_USER, 5, []int32{1}); !errors.Is(err, ErrNotChannel) {
		t.Errorf("a private chat: %v", err)
	}
	if _, err := s.Aggregate(context.Background(), mtproto.PEER_CHANNEL, 5, make([]int32, MaxAggregateMessages+1)); err == nil {
		t.Error("too many messages: no error")
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestAccessCache(t *testing.T) {
	ctx := context.Background()
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a := NewAccessCache()
	a.now = c.now
	chat := Chat{UserId: 1, PeerType: mtproto.PEER_CHANNEL, PeerId: 2}
	calls, top := 0, int32(10)
	resolve := func(context.Context) (int32, error) {
		calls++
		return top, nil
	}
	if got, err := a.Top(ctx, chat, 5, resolve); err != nil || got != 10 || calls != 1 {
		t.Fatalf("first: %d, %v, %d calls", got, err, calls)
	}
	// The batches of the next minute are answered from the cache.
	c.t = c.t.Add(30 * time.Second)
	if got, err := a.Top(ctx, chat, 10, resolve); err != nil || got != 10 || calls != 1 {
		t.Fatalf("cached: %d, %v, %d calls", got, err, calls)
	}
	// A message newer than the cached one is read again.
	top = 12
	if got, err := a.Top(ctx, chat, 11, resolve); err != nil || got != 12 || calls != 2 {
		t.Fatalf("a newer message: %d, %v, %d calls", got, err, calls)
	}
	// After a minute the access is read again.
	c.t = c.t.Add(AccessTTL)
	if got, err := a.Top(ctx, chat, 1, resolve); err != nil || got != 12 || calls != 3 {
		t.Fatalf("expired: %d, %v, %d calls", got, err, calls)
	}
	// Another user's access to the same chat is their own.
	if _, err := a.Top(ctx, Chat{UserId: 3, PeerType: chat.PeerType, PeerId: chat.PeerId}, 1, resolve); err != nil || calls != 4 {
		t.Fatalf("another user: %v, %d calls", err, calls)
	}
}

func TestAccessCacheKeepsNoRefusal(t *testing.T) {
	ctx := context.Background()
	a := NewAccessCache()
	chat := Chat{UserId: 1, PeerType: mtproto.PEER_CHAT, PeerId: 2}
	calls := 0
	refuse := func(context.Context) (int32, error) {
		calls++
		return 0, mtproto.ErrPeerIdInvalid
	}
	for range 2 {
		if _, err := a.Top(ctx, chat, 1, refuse); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
			t.Fatalf("refused: %v", err)
		}
	}
	if calls != 2 {
		t.Errorf("a refusal was cached: %d calls", calls)
	}
	// A user who joined is let in at once.
	if got, err := a.Top(ctx, chat, 1, func(context.Context) (int32, error) { return 4, nil }); err != nil || got != 4 {
		t.Errorf("after joining: %d, %v", got, err)
	}
}

func TestAccessCacheBounded(t *testing.T) {
	ctx := context.Background()
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a := NewAccessCache()
	a.now = c.now
	ok := func(context.Context) (int32, error) { return 1, nil }
	for i := range maxAccessEntries + 5 {
		if _, err := a.Top(ctx, Chat{UserId: int64(i), PeerType: mtproto.PEER_CHANNEL, PeerId: 1}, 1, ok); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(a.entries); n > maxAccessEntries {
		t.Errorf("%d entries, at most %d", n, maxAccessEntries)
	}
}

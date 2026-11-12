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

package audio

import (
	"reflect"
	"testing"
	"time"
)

// feed sends one packet of every sender every 20 ms for d.
func feed(s *Selector, start time.Time, d time.Duration, levels map[uint32]Level) time.Time {
	now := start
	for end := start.Add(d); now.Before(end); now = now.Add(20 * time.Millisecond) {
		for ssrc, level := range levels {
			s.Observe(ssrc, level, level < silence, now)
		}
	}
	return now
}

func TestEveryoneWhileSlotsLast(t *testing.T) {
	s := NewSelector(3, time.Second)
	now := time.Unix(1000, 0)
	for _, ssrc := range []uint32{1, 2, 3} {
		if !s.Observe(ssrc, silence, false, now) {
			t.Errorf("sender %d not forwarded with free slots", ssrc)
		}
	}
}

func TestLoudestWin(t *testing.T) {
	s := NewSelector(2, 500*time.Millisecond)
	now := feed(s, time.Unix(1000, 0), time.Second, map[uint32]Level{1: 80, 2: 20, 3: 30, 4: 100})
	if got := s.Forwarded(); !reflect.DeepEqual(got, []uint32{2, 3}) {
		t.Fatalf("forwarded %v", got)
	}
	// 4 becomes the loudest: it takes the place of the quieter one.
	now = feed(s, now, time.Second, map[uint32]Level{1: 80, 2: 20, 3: 30, 4: 5})
	if got := s.Forwarded(); !reflect.DeepEqual(got, []uint32{2, 4}) {
		t.Fatalf("forwarded %v", got)
	}
	_ = now
}

func TestHoldThroughPause(t *testing.T) {
	s := NewSelector(1, time.Second)
	now := feed(s, time.Unix(1000, 0), time.Second, map[uint32]Level{1: 20, 2: 127})
	if got := s.Forwarded(); !reflect.DeepEqual(got, []uint32{1}) {
		t.Fatalf("forwarded %v", got)
	}
	// 1 pauses: within the hold nobody quieter takes its place...
	now = feed(s, now, 700*time.Millisecond, map[uint32]Level{1: 127, 2: 60})
	if got := s.Forwarded(); !reflect.DeepEqual(got, []uint32{1}) {
		t.Fatalf("during the hold %v", got)
	}
	// ...after it the one speaking does.
	feed(s, now, time.Second, map[uint32]Level{1: 127, 2: 60})
	if got := s.Forwarded(); !reflect.DeepEqual(got, []uint32{2}) {
		t.Fatalf("after the hold %v", got)
	}
}

func TestNoFlappingBetweenEqualVoices(t *testing.T) {
	s := NewSelector(1, 300*time.Millisecond)
	now := feed(s, time.Unix(1000, 0), time.Second, map[uint32]Level{1: 30, 2: 32})
	first := s.Forwarded()
	changes := 0
	for i := 0; i < 50; i++ {
		// The two trade 2 dB back and forth.
		a, b := Level(30), Level(32)
		if i%2 == 1 {
			a, b = b, a
		}
		now = feed(s, now, 100*time.Millisecond, map[uint32]Level{1: a, 2: b})
		if got := s.Forwarded(); !reflect.DeepEqual(got, first) {
			changes++
			first = got
		}
	}
	if changes > 0 {
		t.Errorf("%d changes between voices 2 dB apart", changes)
	}
}

func TestSpeakingAndRemove(t *testing.T) {
	s := NewSelector(2, time.Second)
	start := time.Unix(1000, 0)
	feed(s, start, 200*time.Millisecond, map[uint32]Level{1: 40, 2: 127})
	got := s.Speaking(start.Add(-time.Second))
	if len(got) != 1 || got[1] != 40 {
		t.Fatalf("speaking %v", got)
	}
	if again := s.Speaking(start.Add(time.Hour)); len(again) != 0 {
		t.Fatalf("speaking after the interval %v", again)
	}
	s.Remove(1)
	if got := s.Forwarded(); !reflect.DeepEqual(got, []uint32{2}) {
		t.Fatalf("after remove %v", got)
	}
}

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

// Package audio chooses whose sound the SFU forwards: the last speakers,
// at most N at once, by the audio level senders put into every packet
// (RFC 6464, header extension 1 in tgcalls: GroupInstanceCustomImpl.cpp:1586).
// A native client mixes at most about eleven incoming channels and drops
// the quietest after a second of silence (GroupInstanceCustomImpl.cpp:4018-4045),
// so sending everyone's sound to everyone wastes the channel for nothing.
package audio

import (
	"sort"
	"sync"
	"time"
)

// Level is an RFC 6464 audio level: 0 is the loudest (0 dBov), 127 silence.
type Level = uint8

const (
	// silence is the level at which a packet does not count as sound.
	silence = 127
	// smoothing weighs a new level into the running loudness.
	smoothing = 0.3
	// activeWindow: a sender whose voice was detected within it competes
	// for a slot.
	activeWindow = 500 * time.Millisecond
	// selectedBonus (in level steps, dB) keeps a forwarded sender ahead of
	// one about as loud.
	selectedBonus = 6
)

type speaker struct {
	loudness  float64 // smoothed 127-level of speech: higher is louder
	lastVoice time.Time
	heldUntil time.Time
	selected  bool
	peak      float64 // loudest since the last Speaking call
}

// Selector keeps the senders whose sound is forwarded.
type Selector struct {
	mu       sync.Mutex
	n        int
	hold     time.Duration
	speakers map[uint32]*speaker
	lastRank time.Time
}

// NewSelector forwards at most n senders and keeps a sender for hold after
// it stops being among the loudest.
func NewSelector(n int, hold time.Duration) *Selector {
	if n <= 0 {
		n = 1
	}
	return &Selector{n: n, hold: hold, speakers: map[uint32]*speaker{}}
}

// Observe takes the level of a packet of ssrc and reports whether to
// forward the packet. voice is the V flag of the extension.
func (s *Selector) Observe(ssrc uint32, level Level, voice bool, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, ok := s.speakers[ssrc]
	if !ok {
		sp = &speaker{}
		s.speakers[ssrc] = sp
	}
	if level > silence {
		level = silence
	}
	// Loudness follows speech only: a pause keeps the loudness of the
	// words before it, so a speaker who pauses keeps its place for the
	// hold time.
	if voice {
		loud := float64(silence - level)
		sp.loudness += smoothing * (loud - sp.loudness)
		if loud > sp.peak {
			sp.peak = loud
		}
		sp.lastVoice = now
	}
	// Ranking every packet of every sender costs more than it gives: once
	// per 20 ms is finer than any packet interval of the clients (60-120 ms).
	if now.Sub(s.lastRank) >= 20*time.Millisecond || !ok {
		s.rank(now)
	}
	return sp.selected
}

// rank chooses the forwarded senders: the loudest of those speaking now or
// held from before. A forwarded sender ranks with a bonus, so that two
// voices of about the same loudness do not take turns packet by packet;
// one that falls silent keeps its place for the hold time. With no more
// senders than slots everyone is forwarded, so sound starts at once.
func (s *Selector) rank(now time.Time) {
	s.lastRank = now
	if len(s.speakers) <= s.n {
		for _, sp := range s.speakers {
			sp.selected = true
			sp.heldUntil = now.Add(s.hold)
		}
		return
	}
	type entry struct {
		ssrc  uint32
		sp    *speaker
		score float64
	}
	var list []entry
	for ssrc, sp := range s.speakers {
		active := now.Sub(sp.lastVoice) < activeWindow
		held := sp.selected && now.Before(sp.heldUntil)
		if active || held {
			score := sp.loudness
			if sp.selected {
				score += selectedBonus
			}
			list = append(list, entry{ssrc, sp, score})
		}
		sp.selected = false
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].score != list[j].score {
			return list[i].score > list[j].score
		}
		return list[i].ssrc < list[j].ssrc
	})
	for i, e := range list {
		if i == s.n {
			break
		}
		e.sp.selected = true
		if now.Sub(e.sp.lastVoice) < activeWindow {
			e.sp.heldUntil = now.Add(s.hold)
		}
	}
}

// Remove forgets a sender.
func (s *Selector) Remove(ssrc uint32) {
	s.mu.Lock()
	delete(s.speakers, ssrc)
	s.mu.Unlock()
}

// Forwarded returns the senders forwarded now, sorted.
func (s *Selector) Forwarded() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []uint32
	for ssrc, sp := range s.speakers {
		if sp.selected {
			out = append(out, ssrc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Speaking returns the senders with voice since the last call and their
// loudest level, and starts a new interval.
func (s *Selector) Speaking(since time.Time) map[uint32]Level {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[uint32]Level{}
	for ssrc, sp := range s.speakers {
		if sp.lastVoice.After(since) {
			out[ssrc] = Level(silence - int(sp.peak))
		}
		sp.peak = 0
	}
	return out
}

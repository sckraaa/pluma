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

package forward

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// LayerHeights returns the height each simulcast layer stands for, from the
// lowest. tgcalls encodes a camera in three layers scaled by 4, 2 and 1 and
// turns them on at idealHeight 180, 360 and 720; a screen in two layers
// scaled by 4 (or 2) and 1 (GroupInstanceCustomImpl.cpp:2558-2630).
func LayerHeights(layers int) []int {
	switch layers {
	case 3:
		return []int{180, 360, 720}
	case 2:
		return []int{360, 720}
	default:
		return []int{720}
	}
}

// LayerForHeight is the highest layer whose height fits maxHeight; the
// lowest layer when none fits; NoLayer when maxHeight is 0 (not wanted).
func LayerForHeight(layers, maxHeight int) int {
	if maxHeight <= 0 || layers <= 0 {
		return NoLayer
	}
	heights := LayerHeights(layers)
	best := 0
	for i, h := range heights {
		if h <= maxHeight {
			best = i
		}
	}
	return best
}

// Request is one sender's video as one receiver wants it.
type Request struct {
	// OnStage puts the video before the others when the channel is short.
	OnStage bool
	// MaxHeight is the receiver's constraint; 0 means it does not want it.
	MaxHeight int
	// Layers is how many layers the sender sends.
	Layers int
	// Bitrates is the measured bitrate of each layer, bits per second; 0
	// for a layer that is not arriving now.
	Bitrates []float64
}

// Allocate picks a layer for each request within budget (bits per second).
// Each video first gets the layer its constraint allows among those that
// arrive; while the sum is over budget, videos off stage step down first,
// from the last one, then those on stage; videos at their lowest layer are
// paused last, again off stage first.
func Allocate(reqs []Request, budget float64) []int {
	layers := make([]int, len(reqs))
	for i, r := range reqs {
		layers[i] = wantedLayer(r)
	}
	total := func() float64 {
		var sum float64
		for i, l := range layers {
			if l >= 0 {
				sum += reqs[i].Bitrates[l]
			}
		}
		return sum
	}
	// order: off stage from the last, then on stage from the last.
	order := make([]int, 0, len(reqs))
	for pass := 0; pass < 2; pass++ {
		for i := len(reqs) - 1; i >= 0; i-- {
			if reqs[i].OnStage == (pass == 1) {
				order = append(order, i)
			}
		}
	}
	for total() > budget {
		changed := false
		for _, i := range order {
			if l := lowerAvailable(reqs[i], layers[i]); l >= 0 {
				layers[i] = l
				changed = true
				break
			}
		}
		if changed {
			continue
		}
		for _, i := range order {
			if layers[i] >= 0 {
				layers[i] = NoLayer
				changed = true
				break
			}
		}
		if !changed {
			break
		}
	}
	return layers
}

// wantedLayer is the layer the constraint allows, moved down to one that
// arrives, or up to the lowest that arrives if none below does.
func wantedLayer(r Request) int {
	want := LayerForHeight(r.Layers, r.MaxHeight)
	if want == NoLayer || len(r.Bitrates) == 0 {
		return NoLayer
	}
	if want >= len(r.Bitrates) {
		want = len(r.Bitrates) - 1
	}
	for l := want; l >= 0; l-- {
		if r.Bitrates[l] > 0 {
			return l
		}
	}
	for l := want + 1; l < len(r.Bitrates); l++ {
		if r.Bitrates[l] > 0 {
			return l
		}
	}
	return NoLayer
}

// lowerAvailable is the next lower layer that arrives, NoLayer if none.
func lowerAvailable(r Request, layer int) int {
	for l := layer - 1; l >= 0; l-- {
		if r.Bitrates[l] > 0 {
			return l
		}
	}
	return NoLayer
}

// Estimator estimates a receiver's channel from the losses it reports
// (RTCP receiver reports): it grows while losses stay low and shrinks in
// proportion to them when they are high, the loss-based part of Google
// congestion control (draft-ietf-rmcat-gcc-02 §6).
type Estimator struct {
	mu       sync.Mutex
	bps      float64
	min, max float64
	last     time.Time
}

// NewEstimator starts at initial and stays within [min, max].
func NewEstimator(initial, minBps, maxBps float64) *Estimator {
	return &Estimator{bps: initial, min: minBps, max: maxBps}
}

// OnLoss takes the fraction of packets lost (0..1) since the last report.
func (e *Estimator) OnLoss(fraction float64, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.last.IsZero() && now.Sub(e.last) < 200*time.Millisecond {
		return
	}
	e.last = now
	switch {
	case fraction < 0.02:
		e.bps *= 1.08
	case fraction > 0.10:
		e.bps *= 1 - 0.5*fraction
	}
	e.bps = math.Min(math.Max(e.bps, e.min), e.max)
}

// Bitrate is the estimate in bits per second.
func (e *Estimator) Bitrate() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.bps
}

// RateMeter measures a bitrate: Add counts bytes as they come, Tick turns
// them into a smoothed rate.
type RateMeter struct {
	bytes atomic.Uint64
	last  atomic.Int64 // unix nanos of the last packet

	mu       sync.Mutex
	rate     float64
	lastTick time.Time
}

// Add counts one packet.
func (m *RateMeter) Add(n int, now time.Time) {
	m.bytes.Add(uint64(n))
	m.last.Store(now.UnixNano())
}

// Tick updates the rate; idle is how long without packets makes it 0.
func (m *RateMeter) Tick(now time.Time, idle time.Duration) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	bytes := m.bytes.Swap(0)
	if !m.lastTick.IsZero() {
		if dt := now.Sub(m.lastTick).Seconds(); dt > 0 {
			sample := float64(bytes) * 8 / dt
			if m.rate == 0 {
				m.rate = sample
			} else {
				m.rate = 0.7*m.rate + 0.3*sample
			}
		}
	}
	m.lastTick = now
	if last := m.last.Load(); last == 0 || now.Sub(time.Unix(0, last)) > idle {
		m.rate = 0
	}
	return m.rate
}

// Rate is the last rate Tick computed.
func (m *RateMeter) Rate() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rate
}

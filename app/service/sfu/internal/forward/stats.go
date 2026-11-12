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
	"sync"
	"time"

	"github.com/pion/rtcp"
)

// ReceiveStats keeps the reception statistics of one incoming stream for
// the receiver reports the SFU sends its senders (RFC 3550 §6.4.2,
// appendix A.3 and A.8). Senders use them for their loss-based rate control
// and round-trip time.
type ReceiveStats struct {
	mu        sync.Mutex
	clockRate float64

	started  bool
	baseSeq  uint32
	maxSeq   uint16
	cycles   uint32
	received uint32

	expectedPrior uint32
	receivedPrior uint32

	jitter      float64
	lastTransit float64
	haveTransit bool

	lastSR   uint32
	lastSRAt time.Time
}

// NewReceiveStats makes statistics for a stream with the RTP clock rate.
func NewReceiveStats(clockRate int) *ReceiveStats {
	return &ReceiveStats{clockRate: float64(clockRate)}
}

// Update counts a packet. Retransmitted packets are counted as received.
func (s *ReceiveStats) Update(seq uint16, timestamp uint32, arrival time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		s.started = true
		s.baseSeq = uint32(seq)
		s.maxSeq = seq
	} else if delta := int16(seq - s.maxSeq); delta > 0 {
		if seq < s.maxSeq {
			s.cycles += 1 << 16
		}
		s.maxSeq = seq
	}
	s.received++

	arrivalTS := float64(arrival.UnixNano()) / 1e9 * s.clockRate
	transit := arrivalTS - float64(timestamp)
	if s.haveTransit {
		d := transit - s.lastTransit
		if d < 0 {
			d = -d
		}
		s.jitter += (d - s.jitter) / 16
	}
	s.lastTransit = transit
	s.haveTransit = true
}

// OnSenderReport remembers the sender's report for LSR and DLSR.
func (s *ReceiveStats) OnSenderReport(ntpTime uint64, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSR = uint32(ntpTime >> 16)
	s.lastSRAt = at
}

// Report builds the reception report block for the stream; ok is false
// before any packet.
func (s *ReceiveStats) Report(ssrc uint32, now time.Time) (rtcp.ReceptionReport, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return rtcp.ReceptionReport{}, false
	}
	extMax := s.cycles + uint32(s.maxSeq)
	expected := extMax - s.baseSeq + 1
	lost := int64(expected) - int64(s.received)
	expectedInterval := expected - s.expectedPrior
	receivedInterval := s.received - s.receivedPrior
	s.expectedPrior = expected
	s.receivedPrior = s.received
	var fraction uint8
	if lostInterval := int64(expectedInterval) - int64(receivedInterval); expectedInterval > 0 && lostInterval > 0 {
		fraction = uint8((lostInterval << 8) / int64(expectedInterval))
	}
	if lost < 0 {
		lost = 0
	}
	if lost > 0x7fffff {
		lost = 0x7fffff
	}
	var dlsr uint32
	if !s.lastSRAt.IsZero() {
		dlsr = uint32(now.Sub(s.lastSRAt).Seconds() * 65536)
	}
	return rtcp.ReceptionReport{
		SSRC:               ssrc,
		FractionLost:       fraction,
		TotalLost:          uint32(lost),
		LastSequenceNumber: extMax,
		Jitter:             uint32(s.jitter),
		LastSenderReport:   s.lastSR,
		Delay:              dlsr,
	}, true
}

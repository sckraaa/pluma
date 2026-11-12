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

	"github.com/pion/rtp"
)

// PayloadTypeVP8RTX is the retransmission payload type of VP8 (apt 100), as
// tgcalls assigns it (GroupInstanceCustomImpl.cpp:299-350).
const PayloadTypeVP8RTX = 101

const (
	historySize = 1024
	// videoClockRate is the RTP clock of every video codec.
	videoClockRate = 90000
)

// NoLayer means a receiver gets nothing from a sender.
const NoLayer = -1

// record remembers how one packet of the receiver's stream is rewritten,
// so that a retransmission is rewritten the same way. A record that is not
// sent holds the place of a packet that has not come (the sender's own
// loss, or one held back while the track switched layers): a NACK of it is
// answered from the cache or asked of the sender, and the packet takes the
// place when it comes.
type record struct {
	valid     bool
	sent      bool
	inSSRC    uint32
	inSeq     uint16
	outSeq    uint16
	tsOffset  uint32
	picOffset int32
	tl0Offset int16
	layer     int8
}

// maxGap bounds the places kept for one gap; a longer jump is a new stream
// rather than loss.
const maxGap = historySize / 2

// DownTrack is the video of one sender as one receiver gets it: the packets
// of one simulcast layer at a time, rewritten into the SSRC of the lowest
// layer with continuous sequence numbers, timestamps and VP8 picture ids.
// The receiver sets its stream up from the sender's source groups and shows
// the SIM group's first SSRC only (GroupInstanceCustomImpl.cpp:1728-1790:
// SetSink(_mainVideoSsrc)), so every layer has to arrive under that SSRC.
//
// Layers switch at a key frame of the new layer only.
type DownTrack struct {
	mu sync.Mutex

	outSSRC uint32
	outRTX  uint32

	target  int
	current int

	started    bool
	segStart   uint16
	seqOffset  uint16
	tsOffset   uint32
	lastInSeq  uint16
	lastOutSeq uint16
	lastOutTS  uint32
	lastSent   time.Time
	picOffset  int
	tl0Offset  int
	lastPicID  int
	lastTL0    int

	rtxSeq  uint16
	history [historySize]record

	// The receiver's last report on the track (DownstreamLoss).
	reported   bool
	reportMax  uint32
	reportLost uint32

	packets  uint32
	octets   uint32
	switches uint64
}

// NewDownTrack makes a track that sends under outSSRC and retransmits under
// outRTX (0: retransmissions reuse outSSRC).
func NewDownTrack(outSSRC, outRTX uint32) *DownTrack {
	return &DownTrack{
		outSSRC:   outSSRC,
		outRTX:    outRTX,
		target:    NoLayer,
		current:   NoLayer,
		lastPicID: -1,
		lastTL0:   -1,
	}
}

// OutSSRC is the SSRC the receiver gets the video under.
func (d *DownTrack) OutSSRC() uint32 { return d.outSSRC }

// SetTarget chooses the layer the receiver should get, NoLayer for none. It
// returns true when the track now waits for a key frame of the target.
func (d *DownTrack) SetTarget(layer int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.target = layer
	if layer == NoLayer {
		d.current = NoLayer
		return false
	}
	return d.current != layer
}

// Layers returns the target and the layer being sent.
func (d *DownTrack) Layers() (target, current int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.target, d.current
}

// WaitsForKeyframe reports whether the track needs a key frame of its
// target to start or switch.
func (d *DownTrack) WaitsForKeyframe() (layer int, wait bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.target, d.target != NoLayer && d.current != d.target
}

// Forward rewrites a packet of the sender's layer for the receiver. It
// returns nil when the receiver does not get the packet. The packet passed
// in is not changed.
func (d *DownTrack) Forward(layer int, pkt *rtp.Packet, now time.Time) *rtp.Packet {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.target == NoLayer {
		return nil
	}
	if layer != d.current {
		if layer != d.target || !isKeyframeStart(pkt.PayloadType, pkt.Payload) {
			return nil
		}
		d.switchTo(layer, pkt, now)
	}
	if int16(pkt.SequenceNumber-d.segStart) < 0 {
		// From before the switch to this layer.
		return nil
	}
	if int16(pkt.SequenceNumber-d.lastInSeq) <= 0 {
		return d.late(pkt)
	}
	if len(pkt.Payload) == 0 {
		// Padding carries nothing for the receiver. Dropping the next
		// packet in order costs no gap: later ones move down by one.
		if pkt.SequenceNumber == d.lastInSeq+1 {
			d.seqOffset++
			d.lastInSeq = pkt.SequenceNumber
		}
		return nil
	}
	d.keepGap(pkt, layer)
	rec := d.recordOf(pkt, layer)
	rec.sent = true
	out, picID, tl0 := d.rewrite(pkt, rec)
	d.lastOutSeq = rec.outSeq
	d.lastOutTS = out.Timestamp
	d.lastInSeq = pkt.SequenceNumber
	if picID >= 0 {
		d.lastPicID = picID
	}
	if tl0 >= 0 {
		d.lastTL0 = tl0
	}
	d.started = true
	d.lastSent = now
	d.history[int(rec.outSeq)%historySize] = rec
	d.packets++
	d.octets += uint32(len(out.Payload))
	return out
}

// recordOf is how pkt goes to the receiver with the offsets in force.
func (d *DownTrack) recordOf(pkt *rtp.Packet, layer int) record {
	return record{
		valid:     true,
		inSSRC:    pkt.SSRC,
		inSeq:     pkt.SequenceNumber,
		outSeq:    pkt.SequenceNumber - d.seqOffset,
		tsOffset:  d.tsOffset,
		picOffset: int32(d.picOffset),
		tl0Offset: int16(d.tl0Offset),
		layer:     int8(layer),
	}
}

// keepGap keeps the places of the packets between the last one and pkt:
// the receiver sees the gap, NACKs it, and the SFU needs to know what to
// answer or ask the sender for.
func (d *DownTrack) keepGap(pkt *rtp.Packet, layer int) {
	if !d.started {
		return
	}
	gap := pkt.SequenceNumber - d.lastInSeq - 1
	if gap == 0 {
		return
	}
	from := d.lastInSeq + 1
	if gap > maxGap {
		from = pkt.SequenceNumber - maxGap
	}
	for seq := from; seq != pkt.SequenceNumber; seq++ {
		rec := d.recordOf(&rtp.Packet{Header: rtp.Header{SSRC: pkt.SSRC, SequenceNumber: seq}}, layer)
		d.history[int(rec.outSeq)%historySize] = rec
	}
}

// late forwards a packet older than the last one into the place kept for
// it: a retransmission the sender answered, or one that came out of order.
// A packet with no place kept (a duplicate, or one from before the track
// started) goes nowhere.
func (d *DownTrack) late(pkt *rtp.Packet) *rtp.Packet {
	back := d.lastInSeq - pkt.SequenceNumber
	if !d.started || int(back) >= historySize || len(pkt.Payload) == 0 {
		return nil
	}
	// Dropped padding moves later packets down, never an earlier one up:
	// the place is at most back below the last packet sent.
	low := pkt.SequenceNumber - d.seqOffset
	for i := 0; i <= int(back); i++ {
		rec := &d.history[int(low+uint16(i))%historySize]
		if !rec.valid || rec.outSeq != low+uint16(i) || rec.inSSRC != pkt.SSRC || rec.inSeq != pkt.SequenceNumber {
			continue
		}
		if rec.sent {
			return nil
		}
		rec.sent = true
		out, _, _ := d.rewrite(pkt, *rec)
		d.packets++
		d.octets += uint32(len(out.Payload))
		return out
	}
	return nil
}

// rewrite makes the receiver's copy of pkt by rec, and returns the picture
// id and TL0PICIDX it carries (-1 for none).
func (d *DownTrack) rewrite(pkt *rtp.Packet, rec record) (*rtp.Packet, int, int) {
	out := &rtp.Packet{Header: pkt.Header.Clone(), Payload: pkt.Payload}
	out.SSRC = d.outSSRC
	out.SequenceNumber = rec.outSeq
	out.Timestamp = pkt.Timestamp + rec.tsOffset
	picID, tl0 := -1, -1
	if pkt.PayloadType == payloadTypeVP8 {
		if desc, err := parseVP8(pkt.Payload); err == nil && (desc.pictureID >= 0 || desc.tl0PicIdx >= 0) {
			if desc.pictureID >= 0 {
				picID = (desc.pictureID + int(rec.picOffset)) % desc.pictureIDMod()
			}
			if desc.tl0PicIdx >= 0 {
				tl0 = (desc.tl0PicIdx + int(rec.tl0Offset)) % 256
			}
			out.Payload = rewriteVP8(pkt.Payload, desc, int32(picID), int16(tl0))
		}
	}
	return out, picID, tl0
}

// switchTo starts forwarding layer at pkt, which begins a key frame. The
// first packet of the new layer continues the receiver's stream: the next
// sequence number, a timestamp advanced by the wall time since the last
// packet, the next picture id.
func (d *DownTrack) switchTo(layer int, pkt *rtp.Packet, now time.Time) {
	if d.started {
		d.seqOffset = pkt.SequenceNumber - (d.lastOutSeq + 1)
		ticks := uint32(now.Sub(d.lastSent).Milliseconds() * videoClockRate / 1000)
		if ticks == 0 {
			ticks = 1
		}
		d.tsOffset = d.lastOutTS + ticks - pkt.Timestamp
		if desc, err := parseVP8(pkt.Payload); err == nil {
			if desc.pictureID >= 0 && d.lastPicID >= 0 {
				m := desc.pictureIDMod()
				d.picOffset = ((d.lastPicID+1-desc.pictureID)%m + m) % m
			}
			if desc.tl0PicIdx >= 0 && d.lastTL0 >= 0 {
				d.tl0Offset = ((d.lastTL0+1-desc.tl0PicIdx)%256 + 256) % 256
			}
		}
	}
	d.segStart = pkt.SequenceNumber
	d.lastInSeq = pkt.SequenceNumber - 1
	d.current = layer
	d.switches++
}

// Switches counts layer switches, the first start included.
func (d *DownTrack) Switches() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.switches
}

// Original finds the sender's packet behind a sequence number the receiver
// asks to retransmit: one sent, or one whose place is kept.
func (d *DownTrack) Original(outSeq uint16) (inSSRC uint32, inSeq uint16, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.history[int(outSeq)%historySize]
	if !rec.valid || rec.outSeq != outSeq {
		return 0, 0, false
	}
	return rec.inSSRC, rec.inSeq, true
}

// Retransmission rewrites a cached packet the way it goes to the receiver
// and wraps it into RTX (RFC 4588 §4). It returns nil if outSeq is not
// that packet's place.
func (d *DownTrack) Retransmission(outSeq uint16, orig *rtp.Packet) *rtp.Packet {
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := &d.history[int(outSeq)%historySize]
	if !rec.valid || rec.outSeq != outSeq || rec.inSSRC != orig.SSRC || rec.inSeq != orig.SequenceNumber {
		return nil
	}
	rec.sent = true
	out, _, _ := d.rewrite(orig, *rec)
	if d.outRTX == 0 || orig.PayloadType != payloadTypeVP8 {
		return out
	}
	payload := make([]byte, 2+len(out.Payload))
	payload[0] = byte(rec.outSeq >> 8)
	payload[1] = byte(rec.outSeq)
	copy(payload[2:], out.Payload)
	out.Payload = payload
	out.SSRC = d.outRTX
	out.PayloadType = PayloadTypeVP8RTX
	out.SequenceNumber = d.rtxSeq
	out.Padding = false
	out.PaddingSize = 0
	d.rtxSeq++
	return out
}

// DownstreamLoss turns the receiver's report on the track (its extended
// highest sequence number and cumulative loss, RFC 3550 §6.4.1) into the
// share of the packets the SFU sent since the last report that did not
// reach the receiver. The receiver counts every gap in the stream as
// loss, also those the sender's own loss leaves, which the SFU never sent
// (libwebrtc receive_statistics_impl.cc:137, 250-257 in the Android copy);
// those are not the receiver's channel. ok is false for the first report
// and when the report says nothing new.
func (d *DownTrack) DownstreamLoss(extMax, totalLost uint32) (float64, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	prevMax, prevLost, had := d.reportMax, d.reportLost, d.reported
	span := int64(extMax) - int64(prevMax)
	if had && span <= 0 {
		return 0, false
	}
	d.reported, d.reportMax, d.reportLost = true, extMax, totalLost
	if !had || span > historySize {
		return 0, false
	}
	sent := int64(0)
	for i := int64(1); i <= span; i++ {
		seq := uint16(prevMax + uint32(i))
		if rec := d.history[int(seq)%historySize]; rec.valid && rec.sent && rec.outSeq == seq {
			sent++
		}
	}
	if sent == 0 {
		return 0, false
	}
	received := span - (int64(totalLost) - int64(prevLost))
	lost := sent - received
	switch {
	case lost <= 0:
		return 0, true
	case lost >= sent:
		return 1, true
	}
	return float64(lost) / float64(sent), true
}

// SenderReportInfo is what a sender report for the receiver needs: the
// layer forwarded now, how its timestamps move, and the counts.
func (d *DownTrack) SenderReportInfo() (layer int, tsOffset uint32, packets, octets uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current, d.tsOffset, d.packets, d.octets
}

func rewriteVP8(payload []byte, desc vp8Descriptor, picID int32, tl0 int16) []byte {
	out := make([]byte, len(payload))
	copy(out, payload)
	if picID >= 0 {
		desc.setPictureID(out, int(picID))
	}
	if tl0 >= 0 {
		desc.setTL0PicIdx(out, int(tl0))
	}
	return out
}

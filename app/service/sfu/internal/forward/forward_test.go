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
	"bytes"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/pion/rtp"
)

// layerStream makes VP8 packets of one simulcast layer, one per frame, as
// libwebrtc sends them: 15-bit picture id and TL0PICIDX.
type layerStream struct {
	ssrc      uint32
	seq       uint16
	ts        uint32
	pictureID int
	tl0       uint8
}

func (s *layerStream) next(keyframe bool) *rtp.Packet {
	payload := []byte{0x90, 0xc0, 0x80 | byte(s.pictureID>>8), byte(s.pictureID), s.tl0}
	if keyframe {
		payload = append(payload, 0x10, 0x02, 0x00, 0x9d, 0x01, 0x2a)
	} else {
		payload = append(payload, 0x11, 0x02, 0x00)
	}
	pkt := &rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: payloadTypeVP8, SSRC: s.ssrc, SequenceNumber: s.seq, Timestamp: s.ts}, Payload: payload}
	s.seq++
	s.ts += 3000
	s.pictureID = (s.pictureID + 1) & 0x7fff
	if keyframe {
		s.tl0++
	}
	return pkt
}

func pictureID(t *testing.T, pkt *rtp.Packet) int {
	t.Helper()
	d, err := parseVP8(pkt.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return d.pictureID
}

func TestParseVP8(t *testing.T) {
	s := &layerStream{pictureID: 0x1234, tl0: 7}
	d, err := parseVP8(s.next(true).Payload)
	if err != nil || !d.keyframe || !d.start || d.pictureID != 0x1234 || d.pictureIDLen != 2 || d.tl0PicIdx != 7 || d.descriptorLen != 5 {
		t.Fatalf("%+v %v", d, err)
	}
	d, err = parseVP8(s.next(false).Payload)
	if err != nil || d.keyframe || !d.start {
		t.Fatalf("delta frame: %+v %v", d, err)
	}
	// A middle packet of a frame (S=0) is never a key frame start.
	if d, _ := parseVP8([]byte{0x80, 0x80, 0x05, 0x10}); d.start || d.keyframe || d.pictureID != 5 || d.pictureIDLen != 1 {
		t.Fatalf("7-bit id: %+v", d)
	}
	for _, short := range [][]byte{{}, {0x80}, {0x80, 0x80}, {0x80, 0x80, 0x80}, {0x80, 0x40}} {
		if _, err := parseVP8(short); err == nil {
			t.Errorf("% x parsed", short)
		}
	}
	if isKeyframeStart(111, s.next(true).Payload) {
		t.Error("a key frame of another payload type")
	}
}

// TestDownTrackSwitch: the receiver's stream continues without a gap
// across a layer switch, which happens at a key frame of the new layer only.
func TestDownTrackSwitch(t *testing.T) {
	low := &layerStream{ssrc: 100, seq: 65530, ts: 1000, pictureID: 32760}
	high := &layerStream{ssrc: 102, seq: 7, ts: 900000, pictureID: 5, tl0: 200}
	dt := NewDownTrack(100, 101)
	now := time.Unix(1000, 0)

	if dt.Forward(0, low.next(true), now) != nil {
		t.Fatal("forwarded without a target")
	}
	if !dt.SetTarget(0) {
		t.Fatal("no key frame wait for the first layer")
	}
	if dt.Forward(0, low.next(false), now) != nil {
		t.Fatal("started on a delta frame")
	}
	var out []*rtp.Packet
	add := func(p *rtp.Packet) {
		if p != nil {
			out = append(out, p)
		}
	}
	add(dt.Forward(0, low.next(true), now))
	for i := 0; i < 5; i++ {
		now = now.Add(33 * time.Millisecond)
		add(dt.Forward(0, low.next(false), now))
		add(dt.Forward(1, high.next(false), now))
	}
	dt.SetTarget(1)
	now = now.Add(33 * time.Millisecond)
	add(dt.Forward(1, high.next(false), now)) // not a key frame: wait
	add(dt.Forward(0, low.next(false), now))  // still the old layer
	now = now.Add(33 * time.Millisecond)
	add(dt.Forward(1, high.next(true), now)) // the switch
	add(dt.Forward(0, low.next(false), now)) // the old layer stops
	for i := 0; i < 3; i++ {
		now = now.Add(33 * time.Millisecond)
		add(dt.Forward(1, high.next(false), now))
	}

	if len(out) != 1+5+1+1+3 {
		t.Fatalf("%d packets forwarded", len(out))
	}
	for i, p := range out {
		if p.SSRC != 100 {
			t.Fatalf("packet %d under ssrc %d", i, p.SSRC)
		}
		if i == 0 {
			continue
		}
		prev := out[i-1]
		if p.SequenceNumber != prev.SequenceNumber+1 {
			t.Errorf("packet %d: sequence %d after %d", i, p.SequenceNumber, prev.SequenceNumber)
		}
		if pictureID(t, p) != (pictureID(t, prev)+1)&0x7fff {
			t.Errorf("packet %d: picture id %d after %d", i, pictureID(t, p), pictureID(t, prev))
		}
		if d := int32(p.Timestamp - prev.Timestamp); d <= 0 || d > 9000 {
			t.Errorf("packet %d: timestamp step %d", i, d)
		}
	}
	if target, current := dt.Layers(); target != 1 || current != 1 {
		t.Errorf("layers %d %d", target, current)
	}
	if dt.Switches() != 2 {
		t.Errorf("%d switches", dt.Switches())
	}

	// Pausing stops the stream; resuming waits for a key frame.
	dt.SetTarget(NoLayer)
	if dt.Forward(1, high.next(true), now) != nil {
		t.Error("forwarded while paused")
	}
	if !dt.SetTarget(1) {
		t.Error("no key frame wait after a pause")
	}
}

func TestDownTrackLateAndPadding(t *testing.T) {
	s := &layerStream{ssrc: 100, seq: 10}
	dt := NewDownTrack(100, 101)
	dt.SetTarget(0)
	now := time.Unix(1000, 0)
	first := s.next(true)
	a := dt.Forward(0, first, now)
	held := s.next(false) // arrives late
	c := dt.Forward(0, s.next(false), now)
	b := dt.Forward(0, held, now)
	if a == nil || b == nil || c == nil || b.SequenceNumber != a.SequenceNumber+1 || c.SequenceNumber != a.SequenceNumber+2 {
		t.Fatalf("reordered packets: %v %v %v", a, b, c)
	}
	// A packet from before the start of the layer is dropped.
	old := &rtp.Packet{Header: first.Header, Payload: first.Payload}
	old.SequenceNumber = first.SequenceNumber - 1
	if dt.Forward(0, old, now) != nil {
		t.Error("packet from before the switch forwarded")
	}
	// Padding in order costs no gap.
	pad := s.next(false)
	pad.Payload = nil
	if dt.Forward(0, pad, now) != nil {
		t.Error("padding forwarded")
	}
	d := dt.Forward(0, s.next(false), now)
	if d == nil || d.SequenceNumber != c.SequenceNumber+1 {
		t.Fatalf("after padding: %v", d)
	}
}

func TestRetransmission(t *testing.T) {
	s := &layerStream{ssrc: 102, seq: 500, pictureID: 99}
	cache := NewPacketCache(64)
	dt := NewDownTrack(100, 101)
	dt.SetTarget(1)
	now := time.Unix(1000, 0)
	var sent []*rtp.Packet
	for i := 0; i < 4; i++ {
		pkt := s.next(i == 0)
		cache.Put(pkt)
		sent = append(sent, dt.Forward(1, pkt, now))
	}
	lost := sent[2]
	inSSRC, inSeq, ok := dt.Original(lost.SequenceNumber)
	if !ok || inSSRC != 102 || inSeq != 502 {
		t.Fatalf("original %d %d %v", inSSRC, inSeq, ok)
	}
	orig := cache.Get(inSeq)
	if orig == nil {
		t.Fatal("not in the cache")
	}
	rtx := dt.Retransmission(lost.SequenceNumber, orig)
	if rtx.SSRC != 101 || rtx.PayloadType != PayloadTypeVP8RTX || rtx.Timestamp != lost.Timestamp {
		t.Fatalf("rtx %+v", rtx.Header)
	}
	if osn := uint16(rtx.Payload[0])<<8 | uint16(rtx.Payload[1]); osn != lost.SequenceNumber || !bytes.Equal(rtx.Payload[2:], lost.Payload) {
		t.Fatal("rtx does not carry the packet as first sent")
	}
	second := dt.Retransmission(lost.SequenceNumber, orig)
	if second.SequenceNumber != rtx.SequenceNumber+1 {
		t.Error("rtx sequence numbers do not advance")
	}
	if dt.Retransmission(lost.SequenceNumber, sent[0]) != nil {
		t.Error("retransmitted another packet under the asked number")
	}
	if _, _, ok := dt.Original(lost.SequenceNumber + 100); ok {
		t.Error("original of a number never sent")
	}
	// Without an RTX stream the packet goes again under its own SSRC.
	plain := NewDownTrack(100, 0)
	plain.SetTarget(1)
	s2 := &layerStream{ssrc: 102, seq: 1}
	p := s2.next(true)
	first := plain.Forward(1, p, now)
	again := plain.Retransmission(first.SequenceNumber, p)
	if again.SSRC != 100 || again.SequenceNumber != first.SequenceNumber {
		t.Errorf("plain retransmission %+v", again.Header)
	}
}

// TestDownTrackSenderLoss: a packet lost between the sender and the SFU
// leaves a gap whose place the track keeps, so that the receiver's NACK of
// it is known (and asked of the sender), and the packet takes its place
// when the sender sends it again.
func TestDownTrackSenderLoss(t *testing.T) {
	s := &layerStream{ssrc: 100, seq: 65534, pictureID: 40}
	dt := NewDownTrack(100, 101)
	dt.SetTarget(0)
	now := time.Unix(1000, 0)
	a := dt.Forward(0, s.next(true), now)
	lost := s.next(false) // never reaches the SFU
	c := dt.Forward(0, s.next(false), now)
	if a == nil || c == nil || c.SequenceNumber != a.SequenceNumber+2 {
		t.Fatalf("stream around the loss: %v %v", a, c)
	}
	gap := a.SequenceNumber + 1
	inSSRC, inSeq, ok := dt.Original(gap)
	if !ok || inSSRC != 100 || inSeq != lost.SequenceNumber {
		t.Fatalf("the place of the lost packet: %d %d %v", inSSRC, inSeq, ok)
	}
	// The sender sends it again: it fills the gap with the picture id and
	// timestamp it would have had.
	b := dt.Forward(0, lost, now)
	if b == nil || b.SequenceNumber != gap || pictureID(t, b) != (pictureID(t, a)+1)&0x7fff || b.Timestamp != a.Timestamp+3000 {
		t.Fatalf("the packet sent again: %+v", b)
	}
	if dt.Forward(0, lost, now) != nil {
		t.Error("a duplicate forwarded")
	}
	// The receiver lost it on its own link too: it is answered from the
	// cache like any other.
	if rtx := dt.Retransmission(gap, lost); rtx == nil || uint16(rtx.Payload[0])<<8|uint16(rtx.Payload[1]) != gap {
		t.Fatalf("rtx of the filled gap: %v", rtx)
	}
}

// TestDownTrackHeldBack: the second packet of a key frame that comes
// before the first while the track waits for it is held back; its place is
// kept and a NACK of it is answered from the cache.
func TestDownTrackHeldBack(t *testing.T) {
	s := &layerStream{ssrc: 102, seq: 300, pictureID: 7}
	dt := NewDownTrack(100, 101)
	dt.SetTarget(1)
	now := time.Unix(1000, 0)
	k1 := s.next(true)
	k2 := &rtp.Packet{Header: k1.Header.Clone(), Payload: []byte{0x80, 0xc0, 0x80, 0x07, 0x00, 0xaa}} // S=0: the rest of the frame
	k2.SequenceNumber = k1.SequenceNumber + 1
	s.seq++
	k3 := s.next(false)
	if dt.Forward(1, k2, now) != nil {
		t.Fatal("started in the middle of a frame")
	}
	a := dt.Forward(1, k1, now)
	c := dt.Forward(1, k3, now)
	if a == nil || c == nil || c.SequenceNumber != a.SequenceNumber+2 {
		t.Fatalf("frame around the held packet: %v %v", a, c)
	}
	inSSRC, inSeq, ok := dt.Original(a.SequenceNumber + 1)
	if !ok || inSSRC != 102 || inSeq != k2.SequenceNumber {
		t.Fatalf("the place of the held packet: %d %d %v", inSSRC, inSeq, ok)
	}
	rtx := dt.Retransmission(a.SequenceNumber+1, k2)
	if rtx == nil || uint16(rtx.Payload[0])<<8|uint16(rtx.Payload[1]) != a.SequenceNumber+1 || rtx.Timestamp != a.Timestamp {
		t.Fatalf("rtx of the held packet: %v", rtx)
	}
}

// TestDownstreamLoss: the loss the receiver reports counts only what the
// SFU sent; the gaps the sender's own loss left do not.
func TestDownstreamLoss(t *testing.T) {
	s := &layerStream{ssrc: 100, seq: 1000}
	dt := NewDownTrack(100, 0)
	dt.SetTarget(0)
	now := time.Unix(1000, 0)
	first := dt.Forward(0, s.next(true), now)
	base := uint32(first.SequenceNumber)
	if _, ok := dt.DownstreamLoss(base, 0); ok {
		t.Fatal("the first report gives a loss")
	}
	// 100 places: 20 lost on the sender's link, 80 sent.
	for i := 0; i < 100; i++ {
		pkt := s.next(false)
		if i%5 == 0 {
			continue
		}
		dt.Forward(0, pkt, now)
	}
	if f, ok := dt.DownstreamLoss(base+100, 20); !ok || f != 0 {
		t.Fatalf("sender's loss counted: %v %v", f, ok)
	}
	for i := 0; i < 100; i++ {
		pkt := s.next(false)
		if i%5 == 0 {
			continue
		}
		dt.Forward(0, pkt, now)
	}
	// 28 lost on the receiver's side: 8 of the 80 sent.
	if f, ok := dt.DownstreamLoss(base+200, 48); !ok || math.Abs(f-0.1) > 1e-9 {
		t.Fatalf("receiver's loss: %v %v", f, ok)
	}
	if _, ok := dt.DownstreamLoss(base+200, 48); ok {
		t.Error("a report with nothing new gives a loss")
	}
}

func TestPacketCache(t *testing.T) {
	c := NewPacketCache(3) // rounds up to 4
	for seq := uint16(0); seq < 6; seq++ {
		c.Put(&rtp.Packet{Header: rtp.Header{SequenceNumber: seq}})
	}
	if c.Get(1) != nil {
		t.Error("evicted packet returned")
	}
	if p := c.Get(5); p == nil || p.SequenceNumber != 5 {
		t.Error("recent packet lost")
	}
}

func TestLayerForHeight(t *testing.T) {
	cases := []struct{ layers, height, want int }{
		{3, 0, NoLayer}, {3, 180, 0}, {3, 300, 0}, {3, 360, 1}, {3, 720, 2}, {3, 1080, 2}, {3, 90, 0},
		{2, 180, 0}, {2, 360, 0}, {2, 720, 1},
		{1, 180, 0}, {1, 720, 0}, {0, 720, NoLayer},
	}
	for _, c := range cases {
		if got := LayerForHeight(c.layers, c.height); got != c.want {
			t.Errorf("LayerForHeight(%d, %d) = %d, want %d", c.layers, c.height, got, c.want)
		}
	}
}

func TestAllocate(t *testing.T) {
	rates := []float64{60e3, 110e3, 900e3}
	full := func(onStage bool) Request {
		return Request{OnStage: onStage, MaxHeight: 720, Layers: 3, Bitrates: rates}
	}
	// Enough for everything.
	if got := Allocate([]Request{full(true), full(false)}, 10e6); !reflect.DeepEqual(got, []int{2, 2}) {
		t.Errorf("plenty: %v", got)
	}
	// Short: the video off stage steps down first.
	if got := Allocate([]Request{full(true), full(false)}, 1.2e6); !reflect.DeepEqual(got, []int{2, 1}) {
		t.Errorf("short: %v", got)
	}
	// Shorter: the one off stage goes to its lowest, then the stage steps down.
	if got := Allocate([]Request{full(true), full(false)}, 0.5e6); !reflect.DeepEqual(got, []int{1, 0}) {
		t.Errorf("shorter: %v", got)
	}
	// Starved: the one off stage pauses first.
	if got := Allocate([]Request{full(true), full(false)}, 0.1e6); !reflect.DeepEqual(got, []int{0, NoLayer}) {
		t.Errorf("starved: %v", got)
	}
	// Constraints and layers that do not arrive.
	thumb := Request{MaxHeight: 180, Layers: 3, Bitrates: rates}
	missingTop := Request{MaxHeight: 720, Layers: 3, Bitrates: []float64{60e3, 110e3, 0}}
	onlyTop := Request{MaxHeight: 180, Layers: 3, Bitrates: []float64{0, 0, 900e3}}
	unwanted := Request{MaxHeight: 0, Layers: 3, Bitrates: rates}
	nothing := Request{MaxHeight: 720, Layers: 3, Bitrates: []float64{0, 0, 0}}
	if got := Allocate([]Request{thumb, missingTop, onlyTop, unwanted, nothing}, 10e6); !reflect.DeepEqual(got, []int{0, 1, 2, NoLayer, NoLayer}) {
		t.Errorf("constraints: %v", got)
	}
}

func TestEstimator(t *testing.T) {
	e := NewEstimator(1e6, 100e3, 5e6)
	now := time.Unix(1000, 0)
	for i := 0; i < 10; i++ {
		now = now.Add(time.Second)
		e.OnLoss(0, now)
	}
	if got := e.Bitrate(); math.Abs(got-1e6*math.Pow(1.08, 10)) > 1 {
		t.Errorf("growth %f", got)
	}
	before := e.Bitrate()
	now = now.Add(time.Second)
	e.OnLoss(0.2, now)
	if got := e.Bitrate(); math.Abs(got-before*0.9) > 1 {
		t.Errorf("after 20%% loss %f", got)
	}
	// Reports closer than 200 ms count once.
	e.OnLoss(0.5, now.Add(50*time.Millisecond))
	if got := e.Bitrate(); math.Abs(got-before*0.9) > 1 {
		t.Errorf("burst of reports %f", got)
	}
	for i := 0; i < 100; i++ {
		now = now.Add(time.Second)
		e.OnLoss(0.9, now)
	}
	if e.Bitrate() != 100e3 {
		t.Errorf("floor %f", e.Bitrate())
	}
}

func TestRateMeter(t *testing.T) {
	var m RateMeter
	now := time.Unix(1000, 0)
	m.Tick(now, time.Second)
	for i := 0; i < 10; i++ {
		m.Add(1250, now.Add(time.Duration(i)*50*time.Millisecond))
	}
	if got := m.Tick(now.Add(500*time.Millisecond), time.Second); math.Abs(got-200e3) > 1 {
		t.Errorf("rate %f", got)
	}
	if got := m.Tick(now.Add(3*time.Second), time.Second); got != 0 {
		t.Errorf("idle rate %f", got)
	}
}

func TestReceiveStats(t *testing.T) {
	s := NewReceiveStats(90000)
	now := time.Unix(1000, 0)
	if _, ok := s.Report(1, now); ok {
		t.Fatal("report before any packet")
	}
	// 65530..65539 across the wrap, 2 of them lost.
	for i := 0; i < 10; i++ {
		if i == 3 || i == 7 {
			continue
		}
		s.Update(uint16(65530+i), uint32(i*3000), now.Add(time.Duration(i)*33*time.Millisecond))
	}
	s.OnSenderReport(0x0001_2345_6789_0000, now)
	rep, ok := s.Report(7, now.Add(500*time.Millisecond))
	if !ok {
		t.Fatal("no report")
	}
	if rep.SSRC != 7 || rep.TotalLost != 2 || rep.FractionLost != 2*256/10 || rep.LastSequenceNumber != 1<<16+3 {
		t.Errorf("report %+v", rep)
	}
	if rep.LastSenderReport != 0x2345_6789 || rep.Delay != 65536/2 {
		t.Errorf("LSR %x DLSR %d", rep.LastSenderReport, rep.Delay)
	}
	// The next interval has no losses.
	s.Update(4, 30000, now.Add(time.Second))
	if rep, _ := s.Report(7, now.Add(time.Second)); rep.FractionLost != 0 || rep.TotalLost != 2 {
		t.Errorf("second report %+v", rep)
	}
}

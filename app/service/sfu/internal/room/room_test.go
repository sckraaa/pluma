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

package room

// These tests run the SFU in the process with clients of
// pkg/groupcallpeer, which speak to it as tgcalls does, over real UDP and
// TCP sockets on the loopback interface.

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"github.com/teamgram/teamgram-server/app/service/sfu/internal/transport"
	"github.com/teamgram/teamgram-server/pkg/groupcallpeer"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

const callID = 42

type recorder struct {
	mu     sync.Mutex
	events []Event
	ch     chan Event
}

func newRecorder() *recorder { return &recorder{ch: make(chan Event, 1024)} }

func (r *recorder) Publish(ev Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
	select {
	case r.ch <- ev:
	default:
	}
}

// wait returns the first event (from now on) that matches.
func (r *recorder) wait(t *testing.T, timeout time.Duration, match func(Event) bool) Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-r.ch:
			if match(ev) {
				return ev
			}
		case <-deadline:
			t.Fatal("no matching event")
		}
	}
}

func newTestManager(t *testing.T, mutate func(*Config)) (*Manager, *recorder) {
	t.Helper()
	mux, err := transport.NewMux(transport.MuxConfig{UDP: "127.0.0.1:0", TCP: "127.0.0.1:0", IncludeLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mux.Close() })
	cert, err := transport.NewCertificate()
	if err != nil {
		t.Fatal(err)
	}
	lo := netip.MustParseAddr("127.0.0.1")
	cfg := Config{
		Addresses: []tgcallsjson.Address{
			{IP: lo, Port: uint16(mux.UDPAddr().Port)},
			{IP: lo, Port: uint16(mux.TCPAddr().Port), TCP: true},
		},
		Plaintext: true,
		Transport: transport.EndpointConfig{StartTimeout: 10 * time.Second, ConnectTimeout: 10 * time.Second, Renomination: true},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	rec := newRecorder()
	m, err := NewManager(cfg, mux, cert, rec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	if _, err := m.CreateRoom(callID, false); err != nil {
		t.Fatal(err)
	}
	return m, rec
}

// joinPeer joins a new peer as participant and connects it.
func joinPeer(t *testing.T, m *Manager, participant int64, kind Kind, cfg groupcallpeer.Config) (*groupcallpeer.Peer, *JoinResult) {
	t.Helper()
	p, err := groupcallpeer.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	params, err := p.JoinParams()
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.Join(callID, participant, kind, params, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := p.Connect(ctx, res.Params); err != nil {
		t.Fatal(err)
	}
	return p, res
}

// sendAudio sends a packet every 20 ms until the test ends.
func sendAudio(t *testing.T, p *groupcallpeer.Peer, level uint8) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		tk := time.NewTicker(20 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				_ = p.SendAudio(level, true)
			}
		}
	}()
}

// awaitRTP waits for a packet that matches.
func awaitRTP(t *testing.T, p *groupcallpeer.Peer, timeout time.Duration, match func(*rtp.Packet) bool) *rtp.Packet {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case pkt := <-p.RTP():
			if match(pkt) {
				return pkt
			}
		case <-deadline:
			t.Fatal("no matching packet")
			return nil
		}
	}
}

// noRTP checks that nothing matching arrives within d.
func noRTP(t *testing.T, p *groupcallpeer.Peer, d time.Duration, match func(*rtp.Packet) bool) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case pkt := <-p.RTP():
			if match(pkt) {
				t.Fatalf("unexpected packet ssrc %d seq %d", pkt.SSRC, pkt.SequenceNumber)
			}
		case <-deadline:
			return
		}
	}
}

func drain(p *groupcallpeer.Peer) {
	for {
		select {
		case <-p.RTP():
		default:
			return
		}
	}
}

func bySSRC(ssrc uint32) func(*rtp.Packet) bool {
	return func(pkt *rtp.Packet) bool { return pkt.SSRC == ssrc }
}

func TestAudioBothWays(t *testing.T) {
	m, rec := newTestManager(t, nil)
	a, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{})
	b, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	for _, participant := range []int64{1, 2} {
		ev := rec.wait(t, 10*time.Second, func(ev Event) bool {
			return ev.Type == EventConnected && ev.Participant == participant
		})
		// tgcalls is ICE-controlled, so the SFU controls.
		if ev.Role != "controlling" || ev.Network != "udp4" {
			t.Errorf("participant %d: role %s network %s", participant, ev.Role, ev.Network)
		}
		// The client's addresses are all peer-reflexive to the SFU: its
		// nomination must not wait for better kinds that never come.
		if ev.ConnectTime > 800*time.Millisecond {
			t.Errorf("participant %d connected in %v", participant, ev.ConnectTime)
		}
	}
	sendAudio(t, a, 20)
	sendAudio(t, b, 30)
	got := awaitRTP(t, b, 5*time.Second, bySSRC(a.AudioSSRC()))
	if got.PayloadType != tgcallsjson.PayloadTypeOpus {
		t.Errorf("payload type %d", got.PayloadType)
	}
	if len(got.GetExtension(tgcallsjson.ExtTransportCC)) != 2 || got.GetExtension(tgcallsjson.ExtAudioLevel) == nil {
		t.Error("extensions not kept or stamped")
	}
	awaitRTP(t, a, 5*time.Second, bySSRC(b.AudioSSRC()))
	// Nobody gets their own sound back.
	noRTP(t, a, 300*time.Millisecond, bySSRC(a.AudioSSRC()))

	// Senders get transport-cc feedback and receiver reports.
	deadline := time.After(5 * time.Second)
	var twcc, rr bool
	for !twcc || !rr {
		select {
		case pkts := <-a.RTCP():
			for _, p := range pkts {
				switch p := p.(type) {
				case *rtcp.TransportLayerCC:
					twcc = twcc || p.MediaSSRC == a.AudioSSRC()
				case *rtcp.ReceiverReport:
					for _, rep := range p.Reports {
						rr = rr || rep.SSRC == a.AudioSSRC()
					}
				}
			}
		case <-deadline:
			t.Fatalf("twcc %v, receiver report %v", twcc, rr)
		}
	}

	srcs, err := m.Sources(callID)
	if err != nil || len(srcs) != 2 || !srcs[0].Connected || srcs[0].Sources[0] != a.AudioSSRC() {
		t.Fatalf("sources %+v %v", srcs, err)
	}
}

func TestServerMute(t *testing.T) {
	m, _ := newTestManager(t, nil)
	a, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{})
	b, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	sendAudio(t, a, 20)
	awaitRTP(t, b, 5*time.Second, bySSRC(a.AudioSSRC()))

	if err := m.SetMuted(callID, 1, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	drain(b)
	noRTP(t, b, 700*time.Millisecond, bySSRC(a.AudioSSRC()))

	if err := m.SetMuted(callID, 1, false); err != nil {
		t.Fatal(err)
	}
	awaitRTP(t, b, 5*time.Second, bySSRC(a.AudioSSRC()))
	if err := m.SetMuted(callID, 99, true); !errors.Is(err, ErrNotParticipant) {
		t.Errorf("mute of a stranger: %v", err)
	}
}

// TestLastSpeakers checks that with more senders than slots the loudest
// are forwarded.
func TestLastSpeakers(t *testing.T) {
	m, _ := newTestManager(t, func(c *Config) { c.LastN = 1; c.AudioHold = 200 * time.Millisecond })
	loud, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{})
	quiet, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	listener, _ := joinPeer(t, m, 3, KindMain, groupcallpeer.Config{})
	sendAudio(t, loud, 10)
	sendAudio(t, quiet, 90)
	awaitRTP(t, listener, 5*time.Second, bySSRC(loud.AudioSSRC()))
	time.Sleep(500 * time.Millisecond)
	drain(listener)
	noRTP(t, listener, time.Second, bySSRC(quiet.AudioSSRC()))
}

// videoSender sends three layers at 30 fps and answers key frame requests
// with a key frame of the layer asked, as libwebrtc does.
type videoSender struct {
	p       *groupcallpeer.Peer
	mu      sync.Mutex
	pending map[int]bool
	ideal   []int
	sent    map[uint16]*rtp.Packet
}

func startVideo(t *testing.T, p *groupcallpeer.Peer) *videoSender {
	t.Helper()
	v := &videoSender{p: p, pending: map[int]bool{0: true, 1: true, 2: true}, sent: map[uint16]*rtp.Packet{}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case pkts := <-p.RTCP():
				for _, pkt := range pkts {
					if pli, ok := pkt.(*rtcp.PictureLossIndication); ok {
						for i, l := range p.Layers() {
							if l.SSRC == pli.MediaSSRC {
								v.mu.Lock()
								v.pending[i] = true
								v.mu.Unlock()
							}
						}
					}
				}
			case msg := <-p.Data():
				if class, _ := tgcallsjson.ColibriClass(msg); class == tgcallsjson.ClassSenderVideoConstraints {
					var c tgcallsjson.SenderVideoConstraints
					if err := jsonUnmarshal(msg, &c); err == nil {
						v.mu.Lock()
						v.ideal = append(v.ideal, c.VideoConstraints.IdealHeight)
						v.mu.Unlock()
					}
				}
			}
		}
	}()
	go func() {
		tk := time.NewTicker(33 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				for layer := range p.Layers() {
					v.mu.Lock()
					key := v.pending[layer]
					v.pending[layer] = false
					v.mu.Unlock()
					pkt, err := p.SendVideoFrame(layer, key)
					if err == nil && layer == 0 {
						v.mu.Lock()
						v.sent[pkt.SequenceNumber] = pkt
						v.mu.Unlock()
					}
				}
			}
		}
	}()
	return v
}

func (v *videoSender) idealHeights() []int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]int(nil), v.ideal...)
}

// layerOf reads the layer a test frame came from: the filler bytes are the
// layer index (groupcallpeer.SendVideoFrame).
func layerOf(pkt *rtp.Packet) int { return int(pkt.Payload[len(pkt.Payload)-1]) }

func vp8PictureID(pkt *rtp.Packet) int {
	return int(pkt.Payload[2]&0x7f)<<8 | int(pkt.Payload[3])
}

func TestVideoLayerSwitch(t *testing.T) {
	m, rec := newTestManager(t, nil)
	sender, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{VideoLayers: 3})
	receiver, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := receiver.WaitDataChannel(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sender.WaitDataChannel(ctx); err != nil {
		t.Fatal(err)
	}
	v := startVideo(t, sender)
	main := sender.Layers()[0].SSRC

	// Nobody watches yet: nothing comes.
	noRTP(t, receiver, 700*time.Millisecond, bySSRC(main))

	if err := receiver.SendVideoConstraints(nil, map[string]tgcallsjson.VideoConstraint{
		sender.Endpoint(): {MinHeight: 180, MaxHeight: 180},
	}); err != nil {
		t.Fatal(err)
	}
	first := awaitRTP(t, receiver, 5*time.Second, bySSRC(main))
	if layerOf(first) != 0 {
		t.Fatalf("thumbnail from layer %d", layerOf(first))
	}
	rec.wait(t, 5*time.Second, func(ev Event) bool { return ev.Type == EventVideoLayers && ev.IdealHeight == 180 })

	if err := receiver.SendVideoConstraints([]string{sender.Endpoint()}, map[string]tgcallsjson.VideoConstraint{
		sender.Endpoint(): {MinHeight: 180, MaxHeight: 720},
	}); err != nil {
		t.Fatal(err)
	}
	// Every packet continues the stream: no gap in the sequence numbers
	// or picture ids, timestamps go forward, the SSRC stays the lowest
	// layer's.
	prev := first
	deadline := time.After(8 * time.Second)
	for switched := false; !switched; {
		select {
		case pkt := <-receiver.RTP():
			if pkt.SSRC != main {
				continue
			}
			if pkt.SequenceNumber != prev.SequenceNumber+1 {
				t.Fatalf("sequence %d after %d", pkt.SequenceNumber, prev.SequenceNumber)
			}
			if vp8PictureID(pkt) != (vp8PictureID(prev)+1)&0x7fff {
				t.Fatalf("picture id %d after %d", vp8PictureID(pkt), vp8PictureID(prev))
			}
			if int32(pkt.Timestamp-prev.Timestamp) <= 0 {
				t.Fatalf("timestamp %d after %d", pkt.Timestamp, prev.Timestamp)
			}
			if layerOf(pkt) == 2 {
				switched = true
			}
			prev = pkt
		case <-deadline:
			t.Fatal("no switch to the full layer")
		}
	}
	rec.wait(t, 5*time.Second, func(ev Event) bool { return ev.Type == EventVideoLayers && ev.IdealHeight == 720 })
	if h := v.idealHeights(); len(h) < 3 || h[0] != 0 || h[len(h)-1] != 720 {
		t.Errorf("SenderVideoConstraints sent %v", h)
	}

	// Back to nothing: the stream stops.
	if err := receiver.SendVideoConstraints(nil, map[string]tgcallsjson.VideoConstraint{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	drain(receiver)
	noRTP(t, receiver, 700*time.Millisecond, bySSRC(main))
}

func TestNACKAnsweredWithRTX(t *testing.T) {
	m, _ := newTestManager(t, nil)
	sender, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{VideoLayers: 3})
	receiver, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := receiver.WaitDataChannel(ctx); err != nil {
		t.Fatal(err)
	}
	startVideo(t, sender)
	layer0 := sender.Layers()[0]
	if err := receiver.SendVideoConstraints(nil, map[string]tgcallsjson.VideoConstraint{sender.Endpoint(): {MaxHeight: 180}}); err != nil {
		t.Fatal(err)
	}
	var lost *rtp.Packet
	for i := 0; i < 5; i++ {
		lost = awaitRTP(t, receiver, 5*time.Second, bySSRC(layer0.SSRC))
	}
	if err := receiver.WriteRTCP([]rtcp.Packet{&rtcp.TransportLayerNack{
		SenderSSRC: 7, MediaSSRC: layer0.SSRC, Nacks: rtcp.NackPairsFromSequenceNumbers([]uint16{lost.SequenceNumber}),
	}}); err != nil {
		t.Fatal(err)
	}
	rtx := awaitRTP(t, receiver, 5*time.Second, bySSRC(layer0.RTX))
	if rtx.PayloadType != tgcallsjson.PayloadTypeVP8RTX {
		t.Errorf("rtx payload type %d", rtx.PayloadType)
	}
	if osn := uint16(rtx.Payload[0])<<8 | uint16(rtx.Payload[1]); osn != lost.SequenceNumber {
		t.Errorf("rtx of %d, asked %d", osn, lost.SequenceNumber)
	}
	if string(rtx.Payload[2:]) != string(lost.Payload) || rtx.Timestamp != lost.Timestamp {
		t.Error("rtx payload or timestamp differs from the packet first sent")
	}
}

// videoUntil sends key frames of every layer at 30 fps until the receiver
// gets the sender's video under main, then lets the packets in flight
// arrive and returns the last one.
func videoUntil(t *testing.T, sender, receiver *groupcallpeer.Peer, main uint32) *rtp.Packet {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		for layer := range sender.Layers() {
			if _, err := sender.SendVideoFrame(layer, true); err != nil {
				t.Fatal(err)
			}
		}
		tick := time.After(33 * time.Millisecond)
	wait:
		for {
			select {
			case pkt := <-receiver.RTP():
				if pkt.SSRC == main {
					time.Sleep(200 * time.Millisecond)
					last := pkt
					for {
						select {
						case pkt := <-receiver.RTP():
							if pkt.SSRC == main {
								last = pkt
							}
						default:
							return last
						}
					}
				}
			case <-tick:
				break wait
			}
		}
	}
	t.Fatal("no video")
	return nil
}

// awaitRTCP waits for a packet the peer gets that matches.
func awaitRTCP(t *testing.T, p *groupcallpeer.Peer, timeout time.Duration, match func(rtcp.Packet) bool) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case pkts := <-p.RTCP():
			for _, pkt := range pkts {
				if match(pkt) {
					return
				}
			}
		case <-deadline:
			t.Fatal("no matching RTCP")
		}
	}
}

// TestNACKAskedOfSender: a packet lost between the sender and the SFU is
// asked of the sender when the receiver NACKs the gap it leaves, and the
// sender's retransmission fills the gap.
func TestNACKAskedOfSender(t *testing.T) {
	m, _ := newTestManager(t, nil)
	sender, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{VideoLayers: 3})
	receiver, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := receiver.WaitDataChannel(ctx); err != nil {
		t.Fatal(err)
	}
	layer0 := sender.Layers()[0]
	if err := receiver.SendVideoConstraints(nil, map[string]tgcallsjson.VideoConstraint{sender.Endpoint(): {MaxHeight: 180}}); err != nil {
		t.Fatal(err)
	}
	last := videoUntil(t, sender, receiver, layer0.SSRC)
	lost, err := sender.LoseVideoFrame(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sender.SendVideoFrame(0, false); err != nil {
		t.Fatal(err)
	}
	after := awaitRTP(t, receiver, 5*time.Second, bySSRC(layer0.SSRC))
	gap := last.SequenceNumber + 1
	if after.SequenceNumber != gap+1 {
		t.Fatalf("after the loss: sequence %d, last %d", after.SequenceNumber, last.SequenceNumber)
	}
	if err = receiver.WriteRTCP([]rtcp.Packet{&rtcp.TransportLayerNack{
		SenderSSRC: 7, MediaSSRC: layer0.SSRC, Nacks: rtcp.NackPairsFromSequenceNumbers([]uint16{gap}),
	}}); err != nil {
		t.Fatal(err)
	}
	awaitRTCP(t, sender, 5*time.Second, func(p rtcp.Packet) bool {
		nack, ok := p.(*rtcp.TransportLayerNack)
		if !ok || nack.MediaSSRC != layer0.SSRC {
			return false
		}
		for _, pair := range nack.Nacks {
			for _, seq := range pair.PacketList() {
				if seq == lost.SequenceNumber {
					return true
				}
			}
		}
		return false
	})
	if err = sender.SendRTX(0, lost); err != nil {
		t.Fatal(err)
	}
	filled := awaitRTP(t, receiver, 5*time.Second, func(p *rtp.Packet) bool { return p.SSRC == layer0.SSRC && p.SequenceNumber == gap })
	if filled.Timestamp != last.Timestamp+90000/30 || vp8PictureID(filled) != (vp8PictureID(last)+1)&0x7fff {
		t.Errorf("the gap filled with ts %d picture id %d after %d %d", filled.Timestamp, vp8PictureID(filled), last.Timestamp, vp8PictureID(last))
	}
}

// TestFIRFromReceiver: a FIR names its stream in its entries, its media
// SSRC being 0 (RFC 5104); the sender gets a key frame request.
func TestFIRFromReceiver(t *testing.T) {
	m, _ := newTestManager(t, nil)
	sender, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{VideoLayers: 3})
	receiver, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := receiver.WaitDataChannel(ctx); err != nil {
		t.Fatal(err)
	}
	layer0 := sender.Layers()[0]
	if err := receiver.SendVideoConstraints(nil, map[string]tgcallsjson.VideoConstraint{sender.Endpoint(): {MaxHeight: 180}}); err != nil {
		t.Fatal(err)
	}
	videoUntil(t, sender, receiver, layer0.SSRC)
	// Past the least time between key frame requests, with the requests of
	// the start read.
	quiet := time.After(time.Second)
	for done := false; !done; {
		select {
		case <-sender.RTCP():
		case <-quiet:
			done = true
		}
	}
	if err := receiver.WriteRTCP([]rtcp.Packet{&rtcp.FullIntraRequest{
		SenderSSRC: 7, FIR: []rtcp.FIREntry{{SSRC: layer0.SSRC, SequenceNumber: 1}},
	}}); err != nil {
		t.Fatal(err)
	}
	awaitRTCP(t, sender, 3*time.Second, func(p rtcp.Packet) bool {
		pli, ok := p.(*rtcp.PictureLossIndication)
		return ok && pli.MediaSSRC == layer0.SSRC
	})
}

// TestServerMuteStopsCamera: muted by an admin, the participant's camera
// is not forwarded and he is told to encode nothing; unmuted, it comes
// back.
func TestServerMuteStopsCamera(t *testing.T) {
	m, rec := newTestManager(t, nil)
	sender, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{VideoLayers: 3})
	receiver, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := receiver.WaitDataChannel(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sender.WaitDataChannel(ctx); err != nil {
		t.Fatal(err)
	}
	v := startVideo(t, sender)
	main := sender.Layers()[0].SSRC
	if err := receiver.SendVideoConstraints(nil, map[string]tgcallsjson.VideoConstraint{sender.Endpoint(): {MaxHeight: 180}}); err != nil {
		t.Fatal(err)
	}
	awaitRTP(t, receiver, 5*time.Second, bySSRC(main))
	rec.wait(t, 5*time.Second, func(ev Event) bool { return ev.Type == EventVideoLayers && ev.IdealHeight == 180 })

	if err := m.SetMuted(callID, 1, true); err != nil {
		t.Fatal(err)
	}
	rec.wait(t, 5*time.Second, func(ev Event) bool { return ev.Type == EventVideoLayers && ev.IdealHeight == 0 })
	time.Sleep(100 * time.Millisecond)
	drain(receiver)
	noRTP(t, receiver, 700*time.Millisecond, bySSRC(main))
	if h := v.idealHeights(); h[len(h)-1] != 0 {
		t.Errorf("SenderVideoConstraints after the mute: %v", h)
	}
	srcs, err := m.Sources(callID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range srcs {
		if c.Muted != (c.Participant == 1) {
			t.Errorf("sources: participant %d muted %v", c.Participant, c.Muted)
		}
	}

	if err := m.SetMuted(callID, 1, false); err != nil {
		t.Fatal(err)
	}
	awaitRTP(t, receiver, 5*time.Second, bySSRC(main))
}

// TestEstimateIgnoresSoundGaps: a receiver's loss on the sound (the
// selection of the last speakers stops and resumes a speaker) does not cut
// the estimate of its channel; loss of the video it was sent does.
func TestEstimateIgnoresSoundGaps(t *testing.T) {
	m, _ := newTestManager(t, nil)
	sender, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{VideoLayers: 3})
	receiver, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := receiver.WaitDataChannel(ctx); err != nil {
		t.Fatal(err)
	}
	main := sender.Layers()[0].SSRC
	if err := receiver.SendVideoConstraints(nil, map[string]tgcallsjson.VideoConstraint{sender.Endpoint(): {MaxHeight: 180}}); err != nil {
		t.Fatal(err)
	}
	last := videoUntil(t, sender, receiver, main)
	r, err := m.room(callID)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	member := r.members[memberKey{2, KindMain}]
	r.mu.Unlock()
	before := member.estimator.Bitrate()
	report := func(reports ...rtcp.ReceptionReport) {
		t.Helper()
		if err := receiver.WriteRTCP([]rtcp.Packet{&rtcp.ReceiverReport{SSRC: 7, Reports: reports}}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Half the sound lost, nothing of the video. The extended numbers
	// count one wrap, so that adding to them does not wrap.
	base := uint32(last.SequenceNumber) + 1<<16
	report(rtcp.ReceptionReport{SSRC: main, LastSequenceNumber: base},
		rtcp.ReceptionReport{SSRC: sender.AudioSSRC(), FractionLost: 128, TotalLost: 40, LastSequenceNumber: 100})
	for i := 0; i < 20; i++ {
		if _, err := sender.SendVideoFrame(0, false); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(200 * time.Millisecond)
	ext := base + 20
	report(rtcp.ReceptionReport{SSRC: main, LastSequenceNumber: ext, TotalLost: 0},
		rtcp.ReceptionReport{SSRC: sender.AudioSSRC(), FractionLost: 128, TotalLost: 80, LastSequenceNumber: 200})
	if got := member.estimator.Bitrate(); got < before {
		t.Fatalf("estimate cut by the sound's loss: %v after %v", got, before)
	}
	// Half the video lost: cut.
	for i := 0; i < 20; i++ {
		if _, err := sender.SendVideoFrame(0, false); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(200 * time.Millisecond)
	report(rtcp.ReceptionReport{SSRC: main, LastSequenceNumber: ext + 20, TotalLost: 10})
	if got := member.estimator.Bitrate(); got >= before {
		t.Fatalf("estimate not cut by the video's loss: %v after %v", got, before)
	}
}

func TestICETCPOnly(t *testing.T) {
	m, rec := newTestManager(t, nil)
	tcp := groupcallpeer.Config{Networks: []ice.NetworkType{ice.NetworkTypeTCP4}}
	a, _ := joinPeer(t, m, 1, KindMain, tcp)
	b, _ := joinPeer(t, m, 2, KindMain, tcp)
	ev := rec.wait(t, 10*time.Second, func(ev Event) bool { return ev.Type == EventConnected })
	if ev.Network != "tcp4" {
		t.Errorf("network %s", ev.Network)
	}
	if a.PairNetwork() != "tcp4" {
		t.Errorf("client network %s", a.PairNetwork())
	}
	sendAudio(t, a, 20)
	sendAudio(t, b, 20)
	awaitRTP(t, b, 5*time.Second, bySSRC(a.AudioSSRC()))
	awaitRTP(t, a, 5*time.Second, bySSRC(b.AudioSSRC()))
}

// TestPeerConnectionRole: a client that takes the controlling role (Web's
// PeerConnection) makes the SFU controlled.
func TestPeerConnectionRole(t *testing.T) {
	m, rec := newTestManager(t, nil)
	web, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{PeerConnectionRole: true})
	native, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	ev := rec.wait(t, 10*time.Second, func(ev Event) bool { return ev.Type == EventConnected && ev.Participant == 1 })
	if ev.Role != "controlled" {
		t.Errorf("role %s", ev.Role)
	}
	sendAudio(t, web, 20)
	awaitRTP(t, native, 5*time.Second, bySSRC(web.AudioSSRC()))
}

func TestJoinRules(t *testing.T) {
	m, rec := newTestManager(t, func(c *Config) { c.MaxParticipants = 2 })
	a, first := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{})
	_ = a

	// Another participant with a source of the first one.
	params := []byte(`{"ssrc":` + itoa(first.Sources[0]) + `,"ufrag":"abcd","pwd":"abcdefghijklmnopqrstuvwx","fingerprints":[{"hash":"sha-256","setup":"passive","fingerprint":"` + fakeFingerprint() + `"}]}`)
	if _, err := m.Join(callID, 2, KindMain, params, false); !errors.Is(err, ErrSSRCInUse) {
		t.Errorf("duplicate source: %v", err)
	}
	if _, err := m.Join(callID, 2, KindMain, []byte(`{"ssrc":1}`), false); !errors.Is(err, ErrBadParams) {
		t.Errorf("bad params: %v", err)
	}
	if _, err := m.Join(7, 2, KindMain, params, false); !errors.Is(err, ErrRoomNotFound) {
		t.Errorf("no room: %v", err)
	}

	// Joining again replaces the connection.
	again, res := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{})
	srcs, err := m.Sources(callID)
	if err != nil || len(srcs) != 1 || srcs[0].Sources[0] != again.AudioSSRC() {
		t.Fatalf("after rejoin: %+v %v", srcs, err)
	}
	_ = res

	joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	if _, err := m.Join(callID, 3, KindMain, mustParams(t), false); !errors.Is(err, ErrRoomFull) {
		t.Errorf("third participant: %v", err)
	}
	// A screen of a participant in the call does not count.
	joinPeer(t, m, 2, KindPresentation, groupcallpeer.Config{VideoLayers: 2})

	if err := m.Leave(callID, 2, KindPresentation); err != nil {
		t.Fatal(err)
	}
	if err := m.Leave(callID, 2, KindPresentation); !errors.Is(err, ErrNotParticipant) {
		t.Errorf("second leave: %v", err)
	}
	// A replaced or left connection is no disconnection.
	for _, ev := range rec.snapshot() {
		if ev.Type == EventDisconnected {
			t.Errorf("event %+v", ev)
		}
	}
}

// TestRoomRestart: a closed room loses its sources, as after an SFU
// restart; clients find their SSRC gone (phone.checkGroupCall) and join a
// new room.
func TestRoomRestart(t *testing.T) {
	m, _ := newTestManager(t, nil)
	joinPeer(t, m, 1, KindMain, groupcallpeer.Config{})
	if !m.CloseRoom(callID) {
		t.Fatal("room not closed")
	}
	if _, err := m.Sources(callID); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("sources after close: %v", err)
	}
	if created, err := m.CreateRoom(callID, false); err != nil || !created {
		t.Fatalf("recreate: %v %v", created, err)
	}
	a, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{})
	b, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	sendAudio(t, a, 20)
	awaitRTP(t, b, 5*time.Second, bySSRC(a.AudioSSRC()))
}

func TestClientThatNeverComes(t *testing.T) {
	m, rec := newTestManager(t, func(c *Config) { c.Transport.StartTimeout = 300 * time.Millisecond })
	if _, err := m.Join(callID, 5, KindMain, mustParams(t), false); err != nil {
		t.Fatal(err)
	}
	ev := rec.wait(t, 5*time.Second, func(ev Event) bool { return ev.Type == EventDisconnected })
	if ev.Participant != 5 || ev.Reason != "no_check" {
		t.Errorf("%+v", ev)
	}
	if srcs, _ := m.Sources(callID); len(srcs) != 0 {
		t.Errorf("sources %+v", srcs)
	}
}

func TestE2EKinds(t *testing.T) {
	m, _ := newTestManager(t, nil)
	if _, err := m.CreateRoom(9, true); !errors.Is(err, ErrE2EMismatch) {
		t.Errorf("e2e room on a plaintext node: %v", err)
	}
	if _, err := m.CreateRoom(callID, true); !errors.Is(err, ErrE2EMismatch) {
		t.Errorf("e2e reopen of a plaintext room: %v", err)
	}
}

func (r *recorder) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

func itoa(v uint32) string { return strconv.FormatUint(uint64(v), 10) }

func fakeFingerprint() string {
	return tgcallsjson.EncodeFingerprint(make([]byte, 32))
}

// mustParams returns the join params of a peer that never connects.
func mustParams(t *testing.T) []byte {
	t.Helper()
	p, err := groupcallpeer.New(groupcallpeer.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	params, err := p.JoinParams()
	if err != nil {
		t.Fatal(err)
	}
	return params
}

// TestVideoLimit: over MaxVideos, the later sender's video is not
// forwarded and the sender is told to encode nothing.
func TestVideoLimit(t *testing.T) {
	m, _ := newTestManager(t, func(c *Config) { c.MaxVideos = 1 })
	first, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{VideoLayers: 3})
	second, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{VideoLayers: 3})
	receiver, _ := joinPeer(t, m, 3, KindMain, groupcallpeer.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, p := range []*groupcallpeer.Peer{first, second, receiver} {
		if err := p.WaitDataChannel(ctx); err != nil {
			t.Fatal(err)
		}
	}
	startVideo(t, first)
	v2 := startVideo(t, second)
	if err := receiver.SendVideoConstraints(nil, map[string]tgcallsjson.VideoConstraint{
		first.Endpoint():  {MaxHeight: 360},
		second.Endpoint(): {MaxHeight: 360},
	}); err != nil {
		t.Fatal(err)
	}
	awaitRTP(t, receiver, 5*time.Second, bySSRC(first.Layers()[0].SSRC))
	noRTP(t, receiver, time.Second, bySSRC(second.Layers()[0].SSRC))
	for _, h := range v2.idealHeights() {
		if h != 0 {
			t.Fatalf("the sender over the limit was asked for %d", h)
		}
	}
	if len(v2.idealHeights()) == 0 {
		t.Fatal("the sender over the limit was not told to stop")
	}
}

// TestDataChannelRestart: a client that restarts SCTP on the same DTLS, as
// tgcalls does after an SCTP failure, gets a working data channel again.
func TestDataChannelRestart(t *testing.T) {
	m, _ := newTestManager(t, nil)
	sender, _ := joinPeer(t, m, 1, KindMain, groupcallpeer.Config{VideoLayers: 3})
	receiver, _ := joinPeer(t, m, 2, KindMain, groupcallpeer.Config{})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, p := range []*groupcallpeer.Peer{sender, receiver} {
		if err := p.WaitDataChannel(ctx); err != nil {
			t.Fatal(err)
		}
		p.RestartDataChannel()
		if err := p.WaitDataChannel(ctx); err != nil {
			t.Fatalf("after restart: %v", err)
		}
	}
	v := startVideo(t, sender)
	if err := receiver.SendVideoConstraints(nil, map[string]tgcallsjson.VideoConstraint{sender.Endpoint(): {MaxHeight: 360}}); err != nil {
		t.Fatal(err)
	}
	awaitRTP(t, receiver, 5*time.Second, bySSRC(sender.Layers()[0].SSRC))
	deadline := time.Now().Add(5 * time.Second)
	for {
		if h := v.idealHeights(); len(h) > 0 && h[len(h)-1] == 360 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("SenderVideoConstraints after the restart: %v", v.idealHeights())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

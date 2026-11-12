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

package groupcallpeer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// AudioInterval is the ptime of tgcalls (GroupInstanceCustomImpl.cpp:2716):
// one packet of 120 ms.
const AudioInterval = 120 * time.Millisecond

// StreamAudio sends a packet of speech at level every AudioInterval until
// ctx ends or the peer closes, as a client with its microphone on does.
func (p *Peer) StreamAudio(ctx context.Context, level uint8) {
	go func() {
		tk := time.NewTicker(AudioInterval)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.ctx.Done():
				return
			case <-tk.C:
				// A lost packet is what a real network does too; the
				// receiving side checks what arrives.
				_ = p.SendAudio(level, true)
			}
		}
	}()
}

// BySSRC matches the packets of one source.
func BySSRC(ssrc uint32) func(*rtp.Packet) bool {
	return func(pkt *rtp.Packet) bool { return pkt.SSRC == ssrc }
}

// AwaitRTP waits for a received packet that matches; the others are
// dropped.
func (p *Peer) AwaitRTP(ctx context.Context, match func(*rtp.Packet) bool) (*rtp.Packet, error) {
	for {
		select {
		case pkt := <-p.rtpCh:
			if match(pkt) {
				return pkt, nil
			}
		case <-ctx.Done():
			return nil, fmt.Errorf("groupcallpeer: no matching packet: %w", ctx.Err())
		}
	}
}

// ErrUnexpectedRTP: a packet came that should not have.
var ErrUnexpectedRTP = errors.New("groupcallpeer: unexpected packet")

// NoRTP drops what was received so far and fails if a packet that matches
// arrives within d.
func (p *Peer) NoRTP(d time.Duration, match func(*rtp.Packet) bool) error {
	p.Drain()
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	for {
		select {
		case pkt := <-p.rtpCh:
			if match(pkt) {
				return fmt.Errorf("%w: ssrc %d seq %d", ErrUnexpectedRTP, pkt.SSRC, pkt.SequenceNumber)
			}
		case <-deadline.C:
			return nil
		}
	}
}

// Drain drops the packets received so far.
func (p *Peer) Drain() {
	for {
		select {
		case <-p.rtpCh:
		default:
			return
		}
	}
}

// LayerOf reads the layer a frame of SendVideoFrame came from: its filler
// bytes are the layer's index.
func LayerOf(pkt *rtp.Packet) int {
	if len(pkt.Payload) == 0 {
		return -1
	}
	return int(pkt.Payload[len(pkt.Payload)-1])
}

// VideoStream sends every layer of a peer at 30 frames a second and answers
// what a sender of tgcalls answers: a key frame of the layer a PLI or FIR
// asks for, and it records the SenderVideoConstraints of the SFU
// (GroupInstanceCustomImpl.cpp:3330-3440). It reads the peer's RTCP and
// data channel, so the test does not.
type VideoStream struct {
	p       *Peer
	mu      sync.Mutex
	pending map[int]bool
	ideal   []int
}

// StreamVideo starts sending the peer's layers until ctx ends; the first
// frame of each layer is a key frame.
func (p *Peer) StreamVideo(ctx context.Context) (*VideoStream, error) {
	if len(p.layers) == 0 {
		return nil, errors.New("groupcallpeer: the peer has no video")
	}
	v := &VideoStream{p: p, pending: map[int]bool{}}
	for i := range p.layers {
		v.pending[i] = true
	}
	go v.listen(ctx)
	go v.send(ctx)
	return v, nil
}

func (v *VideoStream) listen(ctx context.Context) {
	p := v.p
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.ctx.Done():
			return
		case pkts := <-p.rtcpCh:
			for _, pkt := range pkts {
				var media []uint32
				switch r := pkt.(type) {
				case *rtcp.PictureLossIndication:
					media = append(media, r.MediaSSRC)
				case *rtcp.FullIntraRequest:
					for _, e := range r.FIR {
						media = append(media, e.SSRC)
					}
				}
				for _, ssrc := range media {
					for i, l := range p.layers {
						if l.SSRC == ssrc {
							v.mu.Lock()
							v.pending[i] = true
							v.mu.Unlock()
						}
					}
				}
			}
		case msg := <-p.dataCh:
			if class, _ := tgcallsjson.ColibriClass(msg); class != tgcallsjson.ClassSenderVideoConstraints {
				continue
			}
			var c tgcallsjson.SenderVideoConstraints
			if err := json.Unmarshal(msg, &c); err == nil {
				v.mu.Lock()
				v.ideal = append(v.ideal, c.VideoConstraints.IdealHeight)
				v.mu.Unlock()
			}
		}
	}
}

func (v *VideoStream) send(ctx context.Context) {
	p := v.p
	tk := time.NewTicker(time.Second / 30)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.ctx.Done():
			return
		case <-tk.C:
			for layer := range p.layers {
				v.mu.Lock()
				key := v.pending[layer]
				v.pending[layer] = false
				v.mu.Unlock()
				// A frame lost before the connection is up is what a
				// camera starting does too.
				_, _ = p.SendVideoFrame(layer, key)
			}
		}
	}
}

// IdealHeights are the SenderVideoConstraints received, in order.
func (v *VideoStream) IdealHeights() []int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]int(nil), v.ideal...)
}

// AwaitIdealHeight waits until the last SenderVideoConstraints received
// asks for height.
func (v *VideoStream) AwaitIdealHeight(ctx context.Context, height int) error {
	tk := time.NewTicker(50 * time.Millisecond)
	defer tk.Stop()
	for {
		if h := v.IdealHeights(); len(h) > 0 && h[len(h)-1] == height {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("groupcallpeer: SenderVideoConstraints %v, waited for %d: %w", v.IdealHeights(), height, ctx.Err())
		case <-tk.C:
		}
	}
}

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
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"

	"github.com/teamgram/teamgram-server/pkg/tde2e"
	"github.com/teamgram/teamgram-server/pkg/tde2e/peer"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// chainServer keeps the chain as the server does and hands out its blocks
// in the server form.
type chainServer struct {
	t      *testing.T
	chain  *tde2e.Chain
	blocks [][]byte
}

func (s *chainServer) last() []byte {
	if len(s.blocks) == 0 {
		return nil
	}
	return s.blocks[len(s.blocks)-1]
}

func (s *chainServer) accept(local []byte) {
	s.t.Helper()
	applied, err := s.chain.Apply(local)
	if err != nil {
		s.t.Fatalf("the server refuses a block: %v", err)
	}
	out, _ := tde2e.ToServer(applied.Bytes)
	s.blocks = append(s.blocks, out)
}

func (s *chainServer) join(userID int64, clk *fakeClock, opts ...E2EOption) *E2E {
	s.t.Helper()
	key, err := peer.GenerateKey()
	if err != nil {
		s.t.Fatal(err)
	}
	block, err := E2EJoinBlock(userID, key, s.last())
	if err != nil {
		s.t.Fatal(err)
	}
	s.accept(block)
	e, err := NewE2E(userID, key, s.last(), 0, append([]E2EOption{WithE2EClock(clk.now)}, opts...)...)
	if err != nil {
		s.t.Fatal(err)
	}
	return e
}

// poll gives e the blocks from where it asks, as getGroupCallChainBlocks.
func (s *chainServer) poll(e *E2E) error {
	next0, _ := e.Offsets()
	return e.Apply(0, s.blocks[min(int(next0), len(s.blocks)):], int32(len(s.blocks)))
}

func audioPacket(t *testing.T, from *E2E, ssrc uint32) *rtp.Packet {
	t.Helper()
	opus := make([]byte, AudioFrameSize)
	opus[0] = AudioTOC
	payload, err := from.encryptAudio(opus, 30, true)
	if err != nil {
		t.Fatalf("user %d encrypts: %v", from.userID, err)
	}
	return &rtp.Packet{Header: rtp.Header{PayloadType: tgcallsjson.PayloadTypeOpus, SSRC: ssrc}, Payload: payload}
}

// Sound and video between two participants: a frame decrypts to what was
// sent, the open bytes of VP8 stay open, a source nobody named is dropped;
// the reports count it all and start again once sent.
func TestE2EFramesAndReports(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	s := &chainServer{t: t, chain: tde2e.NewChain()}
	a := s.join(1, clk)
	b := s.join(2, clk)
	if err := s.poll(a); err != nil {
		t.Fatal(err)
	}
	a.SetSource(200, 2)
	b.SetSource(100, 1)

	pkt := audioPacket(t, a, 100)
	if len(pkt.Payload) <= AudioFrameSize+2 || bytes.Contains(pkt.Payload, make([]byte, 40)) {
		t.Fatal("the sound goes out open")
	}
	if !b.open(pkt) || len(pkt.Payload) != AudioFrameSize || pkt.Payload[0] != AudioTOC {
		t.Fatalf("Bob opens Alice's sound: %x", pkt.Payload)
	}
	stray := audioPacket(t, a, 999)
	if b.open(stray) {
		t.Fatal("a frame of an unknown source opened")
	}
	replay := audioPacket(t, a, 100)
	again := &rtp.Packet{Header: replay.Header, Payload: bytes.Clone(replay.Payload)}
	if !b.open(replay) || b.open(again) {
		t.Fatal("a frame twice")
	}

	frame := []byte{0x10, 0x02, 0x00, 0x9d, 0x01, 0x2a, 0x80, 0x02, 0x68, 0x01, 7, 7, 7, 7}
	enc, err := a.encryptVideo(frame, true)
	if err != nil || !bytes.Equal(enc[:vp8KeyPrefix], frame[:vp8KeyPrefix]) {
		t.Fatalf("a key frame: %x %v", enc, err)
	}
	video := &rtp.Packet{Header: rtp.Header{PayloadType: tgcallsjson.PayloadTypeVP8, SSRC: 200}, Payload: append([]byte{0x90, 0xc0, 0x80, 0x01, 0x05}, enc...)}
	b.SetSource(200, 1)
	if !b.open(video) || !bytes.Equal(video.Payload[5:], frame) {
		t.Fatalf("Bob opens Alice's key frame: %x", video.Payload)
	}

	clk.add(time.Second)
	r := b.Report()
	if r.Seq != 1 || r.Height != 1 || r.LastBlock == "" || r.Offset0 != 2 || r.Error != 0 || r.Encrypted != 0 ||
		len(r.Senders) != 1 || r.Senders[0].User != 1 || r.Senders[0].OK != 3 || r.Senders[0].Duplicate != 1 || r.Senders[0].LastOkMs != 1000 ||
		len(r.DecryptEpochs) != 2 {
		t.Fatalf("Bob's report: %+v", r)
	}
	if _, err := tgcallsjson.ParseE2EStatus(tgcallsjson.MarshalE2EStatus(r)); err != nil {
		t.Fatalf("the report is refused: %v", err)
	}
	ra := a.Report()
	if ra.Encrypted != 4 || len(ra.SendEpochs) != 2 || ra.SendEpochs[1] != ra.LastBlock {
		t.Fatalf("Alice's report: %+v", ra)
	}
	b.Sent(r)
	if r2 := b.Report(); r2.Seq != 2 || len(r2.Senders) != 0 || len(r2.DecryptEpochs) != 0 {
		t.Fatalf("the report after one sent: %+v", r2)
	}
}

// The rule of self-healing: a participant that stops applying blocks
// joins again 12 s after the server went past him; one whose key is wrong
// 15 s after nobody decrypts; one removed at once with its error.
func TestE2ESelfHealing(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	s := &chainServer{t: t, chain: tde2e.NewChain()}
	a := s.join(1, clk)
	b := s.join(2, clk)
	if err := s.poll(a); err != nil {
		t.Fatal(err)
	}
	b.Break(StaleChain)
	c := s.join(3, clk)
	for _, e := range []*E2E{a, b} {
		if err := s.poll(e); err != nil {
			t.Fatal(err)
		}
	}
	if next0, _ := b.Offsets(); next0 != 2 {
		t.Fatalf("the stale one polls from %d", next0)
	}
	clk.add(BehindFor - time.Second)
	if _, ok := b.NeedsRejoin(); ok {
		t.Fatal("a rejoin before the time")
	}
	clk.add(time.Second)
	if why, ok := b.NeedsRejoin(); !ok || why != RejoinBehind {
		t.Fatalf("the stale one: %q %v", why, ok)
	}
	if _, ok := a.NeedsRejoin(); ok {
		t.Fatal("a healthy one rejoins")
	}

	// Carol's key is wrong: she decrypts nobody, and after 15 s she joins
	// again; Alice decrypts the others and does not.
	clk2 := &fakeClock{t: time.Unix(5000, 0)}
	s2 := &chainServer{t: t, chain: tde2e.NewChain()}
	x := s2.join(1, clk2)
	y := s2.join(2, clk2)
	z := s2.join(3, clk2, WithWrongKey())
	for _, e := range []*E2E{x, y} {
		if err := s2.poll(e); err != nil {
			t.Fatal(err)
		}
	}
	z.SetSource(100, 1)
	z.SetSource(200, 2)
	x.SetSource(200, 2)
	x.SetSource(300, 3)
	for i := 0; i < 20; i++ {
		clk2.add(time.Second)
		if z.open(audioPacket(t, x, 100)) || z.open(audioPacket(t, y, 200)) {
			t.Fatal("the wrong key decrypts")
		}
		if !x.open(audioPacket(t, y, 200)) || x.open(audioPacket(t, z, 300)) {
			t.Fatal("Alice and the wrong key")
		}
		if i == 10 {
			if _, ok := z.NeedsRejoin(); ok {
				t.Fatal("the wrong key rejoins before 15 s")
			}
		}
	}
	if why, ok := z.NeedsRejoin(); !ok || why != RejoinUndecrypted {
		t.Fatalf("the wrong key: %q %v", why, ok)
	}
	if _, ok := x.NeedsRejoin(); ok {
		t.Fatal("one who decrypts all but the broken one rejoins")
	}
	if r := x.Report(); len(r.Senders) != 2 || r.Senders[1].User != 3 || r.Senders[1].Failed != 20 || r.Senders[1].LastOkMs != -1 {
		t.Fatalf("Alice's report of the wrong key: %+v", r.Senders)
	}

	// Removed: the block takes Carol out, her call fails at once.
	block, err := c.RemoveBlock(1)
	if err != nil {
		t.Fatal(err)
	}
	s.accept(block)
	if err := s.poll(a); err == nil {
		t.Fatal("Alice applies her removal")
	}
	if why, ok := a.NeedsRejoin(); !ok || why != RejoinError {
		t.Fatalf("the removed one: %q %v", why, ok)
	}
	if r := a.Report(); r.Error != int32(tde2e.CodeNotParticipant) || r.Height != -1 || r.LastBlock != "" {
		t.Fatalf("the report of the removed one: %+v", r)
	}
}

// No commit from a participant who never commits; the others commit.
func TestE2ENoCommit(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	s := &chainServer{t: t, chain: tde2e.NewChain()}
	a := s.join(1, clk)
	a.Break(NoCommit)
	if out, err := a.Broadcasts(); err != nil || len(out) != 0 {
		t.Fatalf("a commit of one who never commits: %d %v", len(out), err)
	}
	b := s.join(2, clk)
	if out, err := b.Broadcasts(); err != nil || len(out) != 1 {
		t.Fatalf("Bob's commit: %d %v", len(out), err)
	}
}

// At most three rejoins in five minutes, after 0, 2 and 5 s and up to a
// second more.
func TestRejoins(t *testing.T) {
	var r Rejoins
	now := time.Unix(1000, 0)
	for i, base := range rejoinDelays {
		d, ok := r.Next(now.Add(time.Duration(i) * time.Minute))
		if !ok || d < base || d > base+rejoinJitter {
			t.Fatalf("rejoin %d: %v %v", i, d, ok)
		}
	}
	if _, ok := r.Next(now.Add(3 * time.Minute)); ok {
		t.Fatal("a fourth rejoin in five minutes")
	}
	// The first left the window, two remain: the third delay.
	if d, ok := r.Next(now.Add(5*time.Minute + time.Second)); !ok || d < rejoinDelays[2] || d > rejoinDelays[2]+rejoinJitter {
		t.Fatalf("a rejoin after the first left the window: %v %v", d, ok)
	}
}

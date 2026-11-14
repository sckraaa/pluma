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

import (
	"bytes"
	"context"
	"encoding/binary"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/teamgram/teamgram-server/app/service/sfu/internal/metrics"
	"github.com/teamgram/teamgram-server/pkg/groupcallpeer"
)

// frame builds a sound frame as tde2e encrypts it (Call.cpp:360-399): no
// prefix, header_a with the hashes of the epochs, a header_b of 32 bytes
// per epoch, the packet and the size of the prefix, 0.
func frame(epochs ...byte) []byte {
	var b []byte
	b = binary.LittleEndian.AppendUint32(b, uint32(len(epochs)))
	for _, e := range epochs {
		b = append(b, bytes.Repeat([]byte{e}, epochHashSize)...)
	}
	b = append(b, make([]byte, len(epochs)*epochHashSize)...)
	b = append(b, make([]byte, 40)...)
	return binary.LittleEndian.AppendUint32(b, 0)
}

func TestFrameEpochs(t *testing.T) {
	h, err := frameEpochs(frame(0xab, 0x01))
	if err != nil {
		t.Fatal(err)
	}
	if got := epochPrefixes(h); !slices.Equal(got, []string{"abababababababab", "0101010101010101"}) {
		t.Fatalf("epochs %v", got)
	}
	short := append(append([]byte{}, frame(0xab)[:4+epochHashSize+10]...), 0, 0, 0, 0)
	prefixed := frame(0xab)
	binary.LittleEndian.PutUint32(prefixed[len(prefixed)-4:], 3)
	versioned := frame(0xab)
	versioned[1] = 1
	for name, p := range map[string][]byte{
		"plain opus":      {0x78, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		"no epoch":        frame(),
		"16 epochs":       frame(bytes.Repeat([]byte{1}, 16)...),
		"cut":             short,
		"with a prefix":   prefixed,
		"another version": versioned,
		"empty":           nil,
	} {
		if _, err := frameEpochs(p); err == nil {
			t.Errorf("%s: read as a frame", name)
		}
	}
}

func TestObserveEpochs(t *testing.T) {
	rec := newRecorder()
	r := &Room{mgr: &Manager{events: rec}, id: 7}
	m := &Member{room: r, participant: 3, kind: KindMain}
	now := time.Now()
	if !r.audioFrame(m, frame(0xab), now) {
		t.Fatal("a frame of tde2e not let through")
	}
	ev := rec.wait(t, time.Second, func(ev Event) bool { return ev.Type == EventSendEpochs })
	if ev.Participant != 3 || ev.CallID != 7 || !slices.Equal(ev.SendEpochs, []string{"abababababababab"}) {
		t.Fatalf("%+v", ev)
	}
	// The same epochs again are told only after sendEpochsEvery; new ones
	// at once.
	r.audioFrame(m, frame(0xab), now.Add(time.Second))
	r.audioFrame(m, frame(0xab, 0xcd), now.Add(2*time.Second))
	if ev = rec.wait(t, time.Second, func(Event) bool { return true }); len(ev.SendEpochs) != 2 {
		t.Fatalf("the change of epochs: %+v", ev)
	}
	r.audioFrame(m, frame(0xab, 0xcd), now.Add(2*time.Second+sendEpochsEvery))
	if ev = rec.wait(t, time.Second, func(Event) bool { return true }); len(ev.SendEpochs) != 2 {
		t.Fatalf("the epochs again: %+v", ev)
	}
	select {
	case ev := <-rec.ch:
		t.Fatalf("an extra event %+v", ev)
	default:
	}
}

// TestUnencryptedAudioDropped: the SFU forwards sound only as frames of
// tde2e. Plain Opus is dropped and counted; a packet with no payload
// (padding) carries nothing and goes on.
func TestUnencryptedAudioDropped(t *testing.T) {
	r := &Room{mgr: &Manager{events: newRecorder()}, id: 7}
	m := &Member{room: r, participant: 3, kind: KindMain}
	before := testutil.ToFloat64(metrics.E2EUnparsedAudio)
	if r.audioFrame(m, []byte{groupcallpeer.AudioTOC, 0, 0, 0, 0, 0, 0, 0, 0}, time.Now()) {
		t.Fatal("plain Opus let through")
	}
	if got := testutil.ToFloat64(metrics.E2EUnparsedAudio) - before; got != 1 {
		t.Fatalf("unparsed counted %v", got)
	}
	if !r.audioFrame(m, nil, time.Now()) {
		t.Fatal("padding dropped")
	}
	screen := &Member{room: r, participant: 3, kind: KindPresentation}
	if r.audioFrame(screen, []byte{groupcallpeer.AudioTOC, 0, 0, 0, 0, 0, 0, 0, 0}, time.Now()) {
		t.Fatal("plain sound of a screen let through")
	}
	if !r.audioFrame(screen, frame(0xab), time.Now()) {
		t.Fatal("a frame of a screen dropped")
	}

	// Through the SFU: the listener gets the frames, never the plain sound.
	mgr, _ := newTestManager(t, nil)
	a, _ := joinPeer(t, mgr, 1, KindMain, groupcallpeer.Config{})
	b, _ := joinPeer(t, mgr, 2, KindMain, groupcallpeer.Config{})
	sendAudio(t, a, 20)
	awaitRTP(t, b, 5*time.Second, bySSRC(a.AudioSSRC()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		tk := time.NewTicker(20 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				_ = a.SendAudio(10, true)
			}
		}
	}()
	plain := func(pkt *rtp.Packet) bool {
		return pkt.SSRC == a.AudioSSRC() && len(pkt.Payload) == groupcallpeer.AudioFrameSize && pkt.Payload[0] == groupcallpeer.AudioTOC
	}
	noRTP(t, b, time.Second, plain)
}

const statusReport = `{"colibriClass":"PlumaE2EStatus","v":1,"seq":1,"height":0,"lastBlock":"0123456789abcdef","offset0":1,"offset1":0,"error":0,` +
	`"sendEpochs":[],"decryptEpochs":[],"unknownEpochs":[],"encrypted":0,"encryptFailed":0,"senders":[]}`

func TestStatusReports(t *testing.T) {
	rec := newRecorder()
	r := &Room{mgr: &Manager{events: rec}, id: 7}
	m := &Member{room: r, participant: 3, kind: KindMain}
	now := time.Now()
	m.onE2EStatus([]byte(statusReport), now)
	ev := rec.wait(t, time.Second, func(ev Event) bool { return ev.Type == EventE2EStatus })
	if ev.E2EStatus == nil || ev.E2EStatus.Seq != 1 || ev.E2EStatus.LastBlock != "0123456789abcdef" {
		t.Fatalf("%+v", ev)
	}
	// Faster than one a second, of another schema, of a screen: dropped.
	m.onE2EStatus([]byte(statusReport), now.Add(100*time.Millisecond))
	m.onE2EStatus([]byte(strings.Replace(statusReport, `"v":1`, `"v":2`, 1)), now.Add(2*time.Second))
	screen := &Member{room: r, participant: 3, kind: KindPresentation}
	screen.onE2EStatus([]byte(statusReport), now)
	select {
	case ev := <-rec.ch:
		t.Fatalf("a dropped report went on: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
	m.onE2EStatus([]byte(strings.Replace(statusReport, `"seq":1`, `"seq":2`, 1)), now.Add(4*time.Second))
	if ev = rec.wait(t, time.Second, func(Event) bool { return true }); ev.E2EStatus.Seq != 2 {
		t.Fatalf("%+v", ev)
	}
}

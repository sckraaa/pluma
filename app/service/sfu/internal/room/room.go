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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"github.com/teamgram/teamgram-server/app/service/sfu/internal/audio"
	"github.com/teamgram/teamgram-server/app/service/sfu/internal/forward"
	"github.com/teamgram/teamgram-server/app/service/sfu/internal/metrics"
	"github.com/teamgram/teamgram-server/app/service/sfu/internal/transport"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// sfuSSRC is the sender SSRC of the RTCP the SFU itself writes (receiver
// reports, feedback, key frame requests). It is not 1: tgcalls keeps a
// dummy incoming channel under SSRC 1 (GroupInstanceCustomImpl.cpp:2361, 4027).
const sfuSSRC = 0x53465531

// Intervals of the room's work.
const (
	tick             = 100 * time.Millisecond
	allocateEvery    = 5  // ticks: 500 ms
	reportEvery      = 10 // ticks: 1 s
	speakingEvery    = 10 // ticks: 1 s
	layerIdle        = 1500 * time.Millisecond
	keyframeRetry    = time.Second
	audioReserve     = 40_000 // bits per second kept for each forwarded speaker
	maxNACKsPerBatch = 64
)

type memberKey struct {
	participant int64
	kind        Kind
}

// Room is one call on the SFU.
type Room struct {
	mgr      *Manager
	cfg      *Config
	id       int64
	selector *audio.Selector

	mu          sync.Mutex
	members     map[memberKey]*Member
	owners      map[uint32]*Member
	nextJoinSeq uint64
	closed      bool
	connected   atomic.Pointer[[]*Member]

	allocMu      sync.Mutex
	allocKick    chan struct{}
	lastSpeaking time.Time

	done      chan struct{}
	closeOnce sync.Once
}

func newRoom(m *Manager, id int64) *Room {
	r := &Room{
		mgr:          m,
		cfg:          &m.cfg,
		id:           id,
		selector:     audio.NewSelector(m.cfg.LastN, m.cfg.AudioHold),
		members:      map[memberKey]*Member{},
		owners:       map[uint32]*Member{},
		allocKick:    make(chan struct{}, 1),
		lastSpeaking: time.Now(),
		done:         make(chan struct{}),
	}
	r.connected.Store(&[]*Member{})
	return r
}

func newEndpointID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// validEndpointID: the id the signalling side chose is a short token of
// letters, digits, '-' and '_', as the ones made here are.
func validEndpointID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

// join connects a participant's connection. endpointID is the id the
// signalling side stored for it; "" makes one.
func (r *Room) join(participant int64, kind Kind, req *tgcallsjson.JoinRequest, muted bool, endpointID string) (*JoinResult, error) {
	layers, err := req.VideoLayers()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadParams, err)
	}
	if endpointID == "" {
		if endpointID, err = newEndpointID(); err != nil {
			return nil, err
		}
	} else if !validEndpointID(endpointID) {
		return nil, fmt.Errorf("%w: endpoint id", ErrBadParams)
	}
	fp := req.SHA256Fingerprint()
	remoteDigest, err := tgcallsjson.DecodeFingerprint(fp.Fingerprint, 32)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadParams, err)
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, ErrRoomNotFound
	}
	key := memberKey{participant, kind}
	old := r.members[key]
	sources := req.Sources()
	for _, s := range sources {
		if owner, ok := r.owners[s]; ok && owner != old {
			r.mu.Unlock()
			return nil, ErrSSRCInUse
		}
	}
	if old == nil && kind == KindMain {
		count := 0
		for k := range r.members {
			if k.kind == KindMain {
				count++
			}
		}
		if count >= r.cfg.MaxParticipants {
			r.mu.Unlock()
			return nil, ErrRoomFull
		}
	}
	m := newMember(r, participant, kind, endpointID, req.SSRC, layers, muted)
	r.nextJoinSeq++
	m.joinSeq = r.nextJoinSeq
	epCfg := r.cfg.Transport
	epCfg.RemoteUfrag = req.Ufrag
	epCfg.RemotePwd = req.Pwd
	epCfg.RemoteFingerprint = remoteDigest
	epCfg.Certificate = r.mgr.cert
	epCfg.Handler = m
	ep, err := r.mgr.mux.NewEndpoint(epCfg)
	if err != nil {
		r.mu.Unlock()
		return nil, err
	}
	m.ep = ep
	// Paired with the decrement in OnClosed, which runs for every
	// endpoint made.
	metrics.Connections.WithLabelValues(kind.String(), "connecting").Inc()
	resp, err := tgcallsjson.BuildResponse(tgcallsjson.ResponseParams{
		Ufrag:             ep.LocalUfrag(),
		Pwd:               ep.LocalPwd(),
		FingerprintSHA256: r.mgr.cert.SHA256,
		Addresses:         r.cfg.Addresses,
		Endpoint:          endpointID,
	})
	if err == nil {
		var data []byte
		if data, err = resp.Marshal(); err == nil {
			m.answer = data
		}
	}
	if err != nil {
		r.mu.Unlock()
		m.leaving.Store(true)
		ep.Close()
		return nil, err
	}
	if old != nil {
		old.leaving.Store(true)
		r.removeLocked(old)
	}
	r.members[key] = m
	for _, s := range sources {
		r.owners[s] = m
	}
	r.mu.Unlock()

	if old != nil {
		old.ep.Close()
		r.forget(old)
	}
	ep.Start()
	return &JoinResult{Params: m.answer, Endpoint: endpointID, Sources: sources}, nil
}

func (r *Room) leave(participant int64, kind Kind) error {
	r.mu.Lock()
	m := r.members[memberKey{participant, kind}]
	if m == nil {
		r.mu.Unlock()
		return ErrNotParticipant
	}
	m.leaving.Store(true)
	r.removeLocked(m)
	r.mu.Unlock()
	m.ep.Close()
	r.forget(m)
	r.kickAllocation()
	return nil
}

func (r *Room) setMuted(participant int64, muted bool) error {
	r.mu.Lock()
	m := r.members[memberKey{participant, KindMain}]
	r.mu.Unlock()
	if m == nil {
		return ErrNotParticipant
	}
	m.muted.Store(muted)
	if muted {
		r.selector.Remove(m.audioSSRC)
	}
	// The camera stops or goes again with the word.
	r.kickAllocation()
	return nil
}

func (r *Room) sources() []ParticipantSources {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ParticipantSources, 0, len(r.members))
	for k, m := range r.members {
		out = append(out, ParticipantSources{
			Participant: k.participant,
			Kind:        k.kind,
			Connected:   m.connected.Load(),
			Sources:     m.sourceList(),
			Muted:       k.kind == KindMain && m.muted.Load(),
		})
	}
	return out
}

// removeLocked takes a member out of the maps; r.mu is held.
func (r *Room) removeLocked(m *Member) {
	key := memberKey{m.participant, m.kind}
	if r.members[key] == m {
		delete(r.members, key)
	}
	for s := range m.sources {
		if r.owners[s] == m {
			delete(r.owners, s)
		}
	}
	r.rebuildConnectedLocked()
}

// forget drops what other members keep of m.
func (r *Room) forget(m *Member) {
	r.selector.Remove(m.audioSSRC)
	for _, other := range r.memberList() {
		other.removeDownTrack(m)
	}
	m.clearDownTracks()
}

func (r *Room) rebuildConnectedLocked() {
	list := make([]*Member, 0, len(r.members))
	for _, m := range r.members {
		if m.connected.Load() {
			list = append(list, m)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].joinSeq < list[j].joinSeq })
	r.connected.Store(&list)
}

func (r *Room) memberList() []*Member {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Member, 0, len(r.members))
	for _, m := range r.members {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].joinSeq < out[j].joinSeq })
	return out
}

func (r *Room) owner(ssrc uint32) *Member {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.owners[ssrc]
}

// onConnected puts a member among those media flows to.
func (r *Room) onConnected(m *Member) {
	r.mu.Lock()
	present := r.members[memberKey{m.participant, m.kind}] == m
	if present {
		r.rebuildConnectedLocked()
	}
	r.mu.Unlock()
	if !present {
		return
	}
	network := m.ep.PairNetwork()
	role := m.ep.Role().String()
	connectTime := time.Since(m.joinedAt)
	metrics.ICEPairs.WithLabelValues(network, role).Inc()
	metrics.ConnectSeconds.Observe(connectTime.Seconds())
	r.mgr.publish(Event{
		Type: EventConnected, CallID: r.id, Participant: m.participant, Kind: m.kind, Time: time.Now(),
		Network: network, Role: role, ConnectTime: connectTime,
	})
	r.kickAllocation()
}

// onClosed handles a connection that ended.
func (r *Room) onClosed(m *Member, reason error) {
	r.mu.Lock()
	r.removeLocked(m)
	r.mu.Unlock()
	r.forget(m)
	if m.leaving.Load() {
		return
	}
	cause := "closed"
	switch {
	case errors.Is(reason, transport.ErrStartTimeout):
		cause = "no_check"
	case errors.Is(reason, transport.ErrICEFailed):
		cause = "ice"
	case errors.Is(reason, transport.ErrDTLSFailed):
		cause = "dtls"
	}
	metrics.ConnectFailures.WithLabelValues(cause).Inc()
	r.mgr.publish(Event{
		Type: EventDisconnected, CallID: r.id, Participant: m.participant, Kind: m.kind, Time: time.Now(),
		Reason: cause, Source: m.audioSSRC,
	})
	r.kickAllocation()
}

func (r *Room) close() {
	r.mu.Lock()
	r.closed = true
	members := make([]*Member, 0, len(r.members))
	for _, m := range r.members {
		m.leaving.Store(true)
		members = append(members, m)
	}
	r.members = map[memberKey]*Member{}
	r.owners = map[uint32]*Member{}
	r.rebuildConnectedLocked()
	r.mu.Unlock()
	for _, m := range members {
		m.ep.Close()
		m.clearDownTracks()
	}
	r.closeOnce.Do(func() { close(r.done) })
}

func (r *Room) kickAllocation() {
	select {
	case r.allocKick <- struct{}{}:
	default:
	}
}

// run does the periodic work of the room until it closes.
func (r *Room) run() {
	t := time.NewTicker(tick)
	defer t.Stop()
	n := 0
	for {
		select {
		case <-r.done:
			return
		case <-r.allocKick:
			r.allocate(time.Now(), false)
		case now := <-t.C:
			n++
			r.sendTransportFeedback()
			if n%allocateEvery == 0 {
				r.allocate(now, true)
			}
			if n%reportEvery == 0 {
				r.sendReceiverReports(now)
			}
			if n%speakingEvery == 0 {
				r.publishSpeaking(now)
			}
		}
	}
}

// --- media ---

// forwardAudio sends a sender's sound to every other participant if it is
// among the last speakers. The sound of a participant muted by an admin is
// dropped here.
func (r *Room) forwardAudio(sender *Member, pkt *rtp.Packet, now time.Time) {
	if sender.kind == KindMain {
		r.observeEpochs(sender, pkt.Payload, now)
	}
	if sender.muted.Load() {
		metrics.MutedDropped.Inc()
		return
	}
	if sender.kind == KindMain {
		var level rtp.AudioLevelExtension
		ext := pkt.GetExtension(tgcallsjson.ExtAudioLevel)
		if ext == nil || level.Unmarshal(ext) != nil {
			level.Level, level.Voice = 127, false
		}
		if !r.selector.Observe(pkt.SSRC, level.Level, level.Voice, now) {
			return
		}
	}
	for _, rcv := range *r.connected.Load() {
		if !rcv.receives(sender) {
			continue
		}
		out := &rtp.Packet{Header: pkt.Header.Clone(), Payload: pkt.Payload}
		rcv.send(out, metrics.KindAudio, now)
	}
}

// forwardVideo sends a packet of a sender's layer to the receivers that get
// that layer now. The camera of a participant muted by an admin is dropped
// here: without the word no camera either (tdesktop turns it off itself,
// calls_group_call.cpp:2315-2318); the allocation pauses its tracks.
func (r *Room) forwardVideo(sender *Member, layer int, pkt *rtp.Packet, now time.Time) {
	if sender.muted.Load() {
		return
	}
	kind := metrics.KindCamera
	if sender.kind == KindPresentation {
		kind = metrics.KindScreen
	}
	sender.downMu.RLock()
	defer sender.downMu.RUnlock()
	for rcv, dt := range sender.downs {
		if !rcv.connected.Load() {
			continue
		}
		if out := dt.Forward(layer, pkt, now); out != nil {
			rcv.send(out, kind, now)
		}
	}
}

// requestKeyframe asks a sender for a key frame of a layer, at most once
// per KeyframeInterval.
func (r *Room) requestKeyframe(sender *Member, layer int, cause string, now time.Time) {
	if layer < 0 || layer >= len(sender.layers) {
		return
	}
	sender.pliMu.Lock()
	if now.Sub(sender.lastPLI[layer]) < r.cfg.KeyframeInterval {
		sender.pliMu.Unlock()
		return
	}
	sender.lastPLI[layer] = now
	sender.pliMu.Unlock()
	metrics.KeyframeRequests.WithLabelValues(cause).Inc()
	sender.writeRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{SenderSSRC: sfuSSRC, MediaSSRC: sender.layers[layer].SSRC}})
}

// sendTransportFeedback sends every sender the arrival times of its
// packets (transport-cc), which its bandwidth estimation runs on.
func (r *Room) sendTransportFeedback() {
	for _, m := range *r.connected.Load() {
		m.twccMu.Lock()
		pkts := m.twcc.BuildFeedbackPacket()
		m.twccMu.Unlock()
		if len(pkts) > 0 {
			m.writeRTCP(pkts)
		}
	}
}

// sendReceiverReports sends every sender the reception statistics of its
// streams.
func (r *Room) sendReceiverReports(now time.Time) {
	for _, m := range *r.connected.Load() {
		var reports []rtcp.ReceptionReport
		for _, ssrc := range m.sourceList() {
			if rep, ok := m.sources[ssrc].stats.Report(ssrc, now); ok {
				reports = append(reports, rep)
			}
		}
		if len(reports) > 0 {
			m.writeRTCP([]rtcp.Packet{&rtcp.ReceiverReport{SSRC: sfuSSRC, Reports: reports}})
		}
	}
}

func (r *Room) publishSpeaking(now time.Time) {
	levels := r.selector.Speaking(r.lastSpeaking)
	r.lastSpeaking = now
	if len(levels) == 0 {
		return
	}
	byParticipant := map[int64]uint8{}
	for ssrc, level := range levels {
		if m := r.owner(ssrc); m != nil && m.kind == KindMain {
			byParticipant[m.participant] = level
		}
	}
	if len(byParticipant) > 0 {
		r.mgr.publish(Event{Type: EventSpeaking, CallID: r.id, Time: now, Levels: byParticipant})
	}
}

// allocate chooses the layer every receiver gets of every sender and tells
// senders the largest height anyone watches. measure is set on the periodic
// runs, which update the bitrates; runs on a change (constraints, a
// connection) use the last ones.
func (r *Room) allocate(now time.Time, measure bool) {
	r.allocMu.Lock()
	defer r.allocMu.Unlock()

	connected := *r.connected.Load()
	var senders []*Member
	for _, m := range connected {
		switch {
		case len(m.layers) == 0:
		case m.muted.Load():
			// Muted by an admin: nobody gets the camera, and the sender
			// is told to encode nothing.
			m.pauseDownTracks()
			m.updateIdealHeight(0)
		default:
			senders = append(senders, m)
		}
	}
	if len(senders) > r.cfg.MaxVideos {
		// Over the limit nobody gets the video: the sender is told to
		// encode nothing.
		for _, s := range senders[r.cfg.MaxVideos:] {
			s.updateIdealHeight(0)
		}
		senders = senders[:r.cfg.MaxVideos]
	}
	rates := make(map[*Member][]float64, len(senders))
	for _, s := range senders {
		rates[s] = s.layerRates(now, measure)
	}

	var uplink float64
	for _, m := range connected {
		if measure {
			uplink += m.outMeter.Tick(now, layerIdle)
		} else {
			uplink += m.outMeter.Rate()
		}
	}
	if measure {
		metrics.Uplink.Set(uplink)
	}
	scale := 1.0
	if r.cfg.UplinkLimit > 0 && uplink > r.cfg.UplinkLimit {
		scale = r.cfg.UplinkLimit / uplink
	}

	ideal := make(map[*Member]int, len(senders))
	layerCounts := map[string]float64{}
	for _, rcv := range connected {
		if rcv.kind != KindMain {
			continue
		}
		cons := rcv.constraints()
		var reqs []forward.Request
		var from []*Member
		for _, s := range senders {
			if !rcv.receives(s) {
				continue
			}
			c, ok := cons.Constraints[s.endpointID]
			if !ok {
				c = cons.DefaultConstraints
			}
			onStage := false
			for _, e := range cons.OnStageEndpoints {
				if e == s.endpointID {
					onStage = true
					break
				}
			}
			if h := min(c.MaxHeight, tgcallsjson.HeightFull); h > ideal[s] {
				ideal[s] = h
			}
			reqs = append(reqs, forward.Request{OnStage: onStage, MaxHeight: c.MaxHeight, Layers: len(s.layers), Bitrates: rates[s]})
			from = append(from, s)
		}
		estimate := rcv.estimator.Bitrate()
		metrics.ReceiverEstimate.Observe(estimate)
		budget := estimate*scale - float64(r.cfg.LastN*audioReserve)
		chosen := forward.Allocate(reqs, budget)
		for i, s := range from {
			layer := chosen[i]
			dt := s.downTrack(rcv, layer != forward.NoLayer)
			if dt == nil {
				continue
			}
			target, _ := dt.Layers()
			if target != layer {
				metrics.LayerSwitches.Inc()
			}
			if wait := dt.SetTarget(layer); wait && target != layer {
				r.requestKeyframe(s, layer, "layer_switch", now)
			}
			if l, wait := dt.WaitsForKeyframe(); wait {
				// The first request may have been lost or ignored.
				s.pliMu.Lock()
				stale := now.Sub(s.lastPLI[l]) >= keyframeRetry
				s.pliMu.Unlock()
				if stale {
					r.requestKeyframe(s, l, "layer_switch", now)
				}
			}
			if layer == forward.NoLayer {
				layerCounts["paused"]++
			} else {
				layerCounts[strconv.Itoa(layer)]++
			}
		}
	}
	for _, l := range []string{"0", "1", "2", "paused"} {
		metrics.ForwardedLayers.WithLabelValues(l).Set(layerCounts[l])
	}
	for _, s := range senders {
		s.updateIdealHeight(ideal[s])
	}
}

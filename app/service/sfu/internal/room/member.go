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
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/interceptor/pkg/twcc"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"github.com/teamgram/teamgram-server/app/service/sfu/internal/forward"
	"github.com/teamgram/teamgram-server/app/service/sfu/internal/metrics"
	"github.com/teamgram/teamgram-server/app/service/sfu/internal/transport"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

type sourceKind int

const (
	sourceAudio sourceKind = iota
	sourceVideo
	sourceRTX
)

// source is one SSRC a member declared when joining.
type source struct {
	kind  sourceKind
	layer int
	stats *forward.ReceiveStats
	// video layers only
	meter *forward.RateMeter
	cache *forward.PacketCache
}

// Member is one connection of a participant: as a sender, its sources;
// as a receiver, what it asked to see and the estimate of its channel.
type Member struct {
	room        *Room
	participant int64
	kind        Kind
	endpointID  string
	joinSeq     uint64
	joinedAt    time.Time
	ep          *transport.Endpoint
	answer      []byte

	audioSSRC uint32
	layers    []tgcallsjson.VideoLayer
	// sources and layerSources are fixed at join.
	sources      map[uint32]*source
	layerSources []*source

	muted     atomic.Bool
	connected atomic.Bool
	leaving   atomic.Bool
	dataOpen  atomic.Bool

	twccMu sync.Mutex
	twcc   *twcc.Recorder

	// downs are the receivers of this member's video.
	downMu sync.RWMutex
	downs  map[*Member]*forward.DownTrack

	pliMu   sync.Mutex
	lastPLI []time.Time

	// idealHeight is what SenderVideoConstraints last said; only the
	// allocation touches it.
	idealHeight int
	idealSent   bool

	estimator    *forward.Estimator
	transportSeq atomic.Uint32
	outMeter     forward.RateMeter

	consMu sync.Mutex
	cons   *tgcallsjson.ReceiverVideoConstraints

	// e2e is what the connection tells of its encryption (e2e.go).
	e2e e2eState
}

func newMember(r *Room, participant int64, kind Kind, endpointID string, audioSSRC uint32, layers []tgcallsjson.VideoLayer, muted bool) *Member {
	m := &Member{
		room:        r,
		participant: participant,
		kind:        kind,
		endpointID:  endpointID,
		joinedAt:    time.Now(),
		audioSSRC:   audioSSRC,
		layers:      layers,
		sources:     map[uint32]*source{audioSSRC: {kind: sourceAudio, stats: forward.NewReceiveStats(48000)}},
		twcc:        twcc.NewRecorder(sfuSSRC),
		downs:       map[*Member]*forward.DownTrack{},
		lastPLI:     make([]time.Time, len(layers)),
		estimator:   forward.NewEstimator(r.cfg.InitialBitrate, r.cfg.MinBitrate, r.cfg.MaxBitrate),
		cons:        &tgcallsjson.ReceiverVideoConstraints{Constraints: map[string]tgcallsjson.VideoConstraint{}},
	}
	m.muted.Store(muted)
	for i, l := range layers {
		src := &source{
			kind:  sourceVideo,
			layer: i,
			stats: forward.NewReceiveStats(90000),
			meter: &forward.RateMeter{},
			cache: forward.NewPacketCache(r.cfg.PacketCache),
		}
		m.sources[l.SSRC] = src
		m.layerSources = append(m.layerSources, src)
		if l.RTX != 0 {
			m.sources[l.RTX] = &source{kind: sourceRTX, layer: i, stats: forward.NewReceiveStats(90000)}
		}
	}
	return m
}

func (m *Member) sourceList() []uint32 {
	out := make([]uint32, 0, len(m.sources))
	for s := range m.sources {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// receives reports whether m gets the media of sender: main connections of
// other participants get everyone's; a screen sharing connection only
// sends (it would play the sound into its loopback device), and nobody
// gets their own media back.
func (m *Member) receives(sender *Member) bool {
	return m.kind == KindMain && m.participant != sender.participant && m.connected.Load()
}

func (m *Member) constraints() tgcallsjson.ReceiverVideoConstraints {
	m.consMu.Lock()
	defer m.consMu.Unlock()
	return *m.cons
}

// layerRates returns the bitrate of each layer of the member's video,
// measuring it anew if measure.
func (m *Member) layerRates(now time.Time, measure bool) []float64 {
	out := make([]float64, len(m.layerSources))
	for i, src := range m.layerSources {
		if measure {
			out[i] = src.meter.Tick(now, layerIdle)
		} else {
			out[i] = src.meter.Rate()
		}
	}
	return out
}

// downTrack returns the track of m's video for rcv, making it if create.
func (m *Member) downTrack(rcv *Member, create bool) *forward.DownTrack {
	m.downMu.RLock()
	dt := m.downs[rcv]
	m.downMu.RUnlock()
	if dt != nil || !create || len(m.layers) == 0 {
		return dt
	}
	m.downMu.Lock()
	defer m.downMu.Unlock()
	if dt = m.downs[rcv]; dt == nil {
		dt = forward.NewDownTrack(m.layers[0].SSRC, m.layers[0].RTX)
		m.downs[rcv] = dt
	}
	return dt
}

// pauseDownTracks stops the member's video to every receiver; the next
// allocation that takes it starts again at a key frame.
func (m *Member) pauseDownTracks() {
	m.downMu.RLock()
	defer m.downMu.RUnlock()
	for _, dt := range m.downs {
		dt.SetTarget(forward.NoLayer)
	}
}

func (m *Member) removeDownTrack(rcv *Member) {
	m.downMu.Lock()
	delete(m.downs, rcv)
	m.downMu.Unlock()
}

func (m *Member) clearDownTracks() {
	m.downMu.Lock()
	m.downs = map[*Member]*forward.DownTrack{}
	m.downMu.Unlock()
}

// updateIdealHeight tells the member, as a sender, the largest height
// anyone watches, when it changed (SenderVideoConstraints).
func (m *Member) updateIdealHeight(h int) {
	if m.idealSent && h == m.idealHeight {
		return
	}
	if !m.dataOpen.Load() {
		return
	}
	if err := m.ep.SendData(tgcallsjson.MarshalSenderVideoConstraints(h)); err != nil {
		return
	}
	changed := !m.idealSent || h != m.idealHeight
	m.idealHeight, m.idealSent = h, true
	if changed {
		m.room.mgr.publish(Event{
			Type: EventVideoLayers, CallID: m.room.id, Participant: m.participant, Kind: m.kind,
			Time: time.Now(), IdealHeight: h,
		})
	}
}

// send stamps a packet with the member's transport-wide sequence number
// and send time, then sends it.
func (m *Member) send(pkt *rtp.Packet, media string, now time.Time) {
	seq := uint16(m.transportSeq.Add(1))
	if err := pkt.Header.SetExtension(tgcallsjson.ExtTransportCC, []byte{byte(seq >> 8), byte(seq)}); err != nil {
		return
	}
	if ast, err := rtp.NewAbsSendTimeExtension(now).Marshal(); err == nil {
		_ = pkt.Header.SetExtension(tgcallsjson.ExtAbsSendTime, ast)
	}
	if err := m.ep.WriteRTP(pkt); err != nil {
		metrics.SendErrors.Inc()
		return
	}
	size := pkt.MarshalSize()
	m.outMeter.Add(size, now)
	metrics.PacketsOut.WithLabelValues(media).Inc()
	metrics.BytesOut.WithLabelValues(media).Add(float64(size))
}

func (m *Member) writeRTCP(pkts []rtcp.Packet) {
	if err := m.ep.WriteRTCP(pkts); err != nil && m.connected.Load() {
		metrics.SendErrors.Inc()
	}
}

// --- transport.Handler ---

// OnConnected implements transport.Handler.
func (m *Member) OnConnected(*transport.Endpoint) {
	m.connected.Store(true)
	metrics.Connections.WithLabelValues(m.kind.String(), "connecting").Dec()
	metrics.Connections.WithLabelValues(m.kind.String(), "connected").Inc()
	m.room.onConnected(m)
}

// OnClosed implements transport.Handler.
func (m *Member) OnClosed(_ *transport.Endpoint, reason error) {
	if m.connected.Swap(false) {
		metrics.Connections.WithLabelValues(m.kind.String(), "connected").Dec()
	} else {
		metrics.Connections.WithLabelValues(m.kind.String(), "connecting").Dec()
	}
	m.room.onClosed(m, reason)
}

// OnDataOpen implements transport.Handler.
func (m *Member) OnDataOpen(*transport.Endpoint) {
	m.dataOpen.Store(true)
	m.room.allocMu.Lock()
	m.idealSent = false
	m.room.allocMu.Unlock()
	m.room.kickAllocation()
}

// OnData implements transport.Handler: Colibri messages of the receiver,
// and the status reports of its encryption.
func (m *Member) OnData(_ *transport.Endpoint, msg []byte) {
	class, err := tgcallsjson.ColibriClass(msg)
	if err == nil && class == tgcallsjson.ClassPlumaE2EStatus {
		m.onE2EStatus(msg, time.Now())
		return
	}
	if err != nil || class != tgcallsjson.ClassReceiverVideoConstraints {
		return
	}
	cons, err := tgcallsjson.ParseReceiverVideoConstraints(msg)
	if err != nil {
		return
	}
	m.consMu.Lock()
	m.cons = cons
	m.consMu.Unlock()
	m.room.kickAllocation()
}

// OnRTP implements transport.Handler: a packet the member sends.
func (m *Member) OnRTP(_ *transport.Endpoint, pkt *rtp.Packet, arrival time.Time) {
	src, ok := m.sources[pkt.SSRC]
	if !ok {
		metrics.UnknownSSRC.Inc()
		return
	}
	if ext := pkt.GetExtension(tgcallsjson.ExtTransportCC); len(ext) >= 2 {
		m.twccMu.Lock()
		m.twcc.Record(pkt.SSRC, uint16(ext[0])<<8|uint16(ext[1]), arrival.UnixMicro())
		m.twccMu.Unlock()
	}
	size := pkt.MarshalSize()
	src.stats.Update(pkt.SequenceNumber, pkt.Timestamp, arrival)
	switch src.kind {
	case sourceAudio:
		metrics.PacketsIn.WithLabelValues(metrics.KindAudio).Inc()
		metrics.BytesIn.WithLabelValues(metrics.KindAudio).Add(float64(size))
		m.room.forwardAudio(m, pkt, arrival)
	case sourceVideo:
		m.countVideoIn(size)
		src.meter.Add(size, arrival)
		src.cache.Put(pkt)
		m.room.forwardVideo(m, src.layer, pkt, arrival)
	case sourceRTX:
		m.countVideoIn(size)
		orig := m.unwrapRTX(src.layer, pkt)
		if orig == nil {
			return
		}
		layerSrc := m.layerSources[src.layer]
		if layerSrc.cache.Get(orig.SequenceNumber) != nil {
			// A retransmission of a packet that did arrive.
			return
		}
		layerSrc.cache.Put(orig)
		m.room.forwardVideo(m, src.layer, orig, arrival)
	}
}

func (m *Member) countVideoIn(size int) {
	kind := metrics.KindCamera
	if m.kind == KindPresentation {
		kind = metrics.KindScreen
	}
	metrics.PacketsIn.WithLabelValues(kind).Inc()
	metrics.BytesIn.WithLabelValues(kind).Add(float64(size))
}

// unwrapRTX restores the original packet from a retransmission (RFC 4588
// §4): the original sequence number leads the payload. Padding-only RTX
// packets (bandwidth probes) carry nothing.
func (m *Member) unwrapRTX(layer int, pkt *rtp.Packet) *rtp.Packet {
	if pkt.PayloadType != forward.PayloadTypeVP8RTX || len(pkt.Payload) <= 2 {
		return nil
	}
	orig := &rtp.Packet{Header: pkt.Header.Clone(), Payload: pkt.Payload[2:]}
	orig.SSRC = m.layers[layer].SSRC
	orig.SequenceNumber = uint16(pkt.Payload[0])<<8 | uint16(pkt.Payload[1])
	orig.PayloadType = tgcallsjson.PayloadTypeVP8
	orig.Padding = false
	orig.PaddingSize = 0
	return orig
}

// OnRTCP implements transport.Handler: reports and feedback of the member
// as a sender and as a receiver. The estimate of the receiver's channel
// takes the loss of the video the SFU sends it, as DownstreamLoss counts
// it: not the gaps of the sender's own loss, and not the sound, whose
// sequence numbers jump when the selection of the last speakers stops and
// resumes a speaker (libwebrtc counts a jump of up to 50 as loss,
// receive_statistics_impl.cc:93, 137 and rtp_rtcp_config.h:16 in the
// Android copy; with tgcalls' ptime of 120 ms that is a pause of up to 6 s,
// GroupInstanceCustomImpl.cpp:2716).
func (m *Member) OnRTCP(_ *transport.Endpoint, pkts []rtcp.Packet, arrival time.Time) {
	maxLoss := -1.0
	lossFrom := func(reports []rtcp.ReceptionReport) {
		for _, rep := range reports {
			sender := m.room.owner(rep.SSRC)
			if sender == nil || sender == m {
				continue
			}
			dt := sender.downTrack(m, false)
			if dt == nil || dt.OutSSRC() != rep.SSRC {
				continue
			}
			if f, ok := dt.DownstreamLoss(rep.LastSequenceNumber, rep.TotalLost); ok && f > maxLoss {
				maxLoss = f
			}
		}
	}
	for _, p := range pkts {
		switch p := p.(type) {
		case *rtcp.SenderReport:
			m.onSenderReport(p, arrival)
			lossFrom(p.Reports)
		case *rtcp.ReceiverReport:
			lossFrom(p.Reports)
		case *rtcp.TransportLayerNack:
			m.onNACK(p)
		case *rtcp.PictureLossIndication:
			m.onKeyframeRequest(p.MediaSSRC, arrival)
		case *rtcp.FullIntraRequest:
			// The media SSRC of a FIR is 0 (RFC 5104 §4.3.1.2; libwebrtc
			// rtcp_packet/fir.cc:98); the streams are in its entries.
			for _, e := range p.FIR {
				m.onKeyframeRequest(e.SSRC, arrival)
			}
		}
	}
	if maxLoss >= 0 {
		metrics.ReceiverLoss.Observe(maxLoss)
		m.estimator.OnLoss(maxLoss, arrival)
	}
}

// onSenderReport keeps the member's report for the receiver reports back
// and passes it on to receivers, translated for the video they get, so
// they can synchronize sound and picture.
func (m *Member) onSenderReport(sr *rtcp.SenderReport, arrival time.Time) {
	src, ok := m.sources[sr.SSRC]
	if !ok {
		return
	}
	src.stats.OnSenderReport(sr.NTPTime, arrival)
	switch src.kind {
	case sourceAudio:
		out := &rtcp.SenderReport{SSRC: sr.SSRC, NTPTime: sr.NTPTime, RTPTime: sr.RTPTime, PacketCount: sr.PacketCount, OctetCount: sr.OctetCount}
		for _, rcv := range *m.room.connected.Load() {
			if rcv.receives(m) {
				rcv.writeRTCP([]rtcp.Packet{out})
			}
		}
	case sourceVideo:
		m.downMu.RLock()
		defer m.downMu.RUnlock()
		for rcv, dt := range m.downs {
			layer, tsOffset, packets, octets := dt.SenderReportInfo()
			if layer != src.layer || !rcv.connected.Load() {
				continue
			}
			rcv.writeRTCP([]rtcp.Packet{&rtcp.SenderReport{
				SSRC: dt.OutSSRC(), NTPTime: sr.NTPTime, RTPTime: sr.RTPTime + tsOffset,
				PacketCount: packets, OctetCount: octets,
			}})
		}
	}
}

// onNACK answers a receiver's NACK from the cache of the sender's layer;
// what the cache lacks (the sender's own loss: the track keeps its place)
// is asked of the sender. Several receivers asking for one packet ask the
// sender several times; libwebrtc sends a packet again at most once per
// round trip (rtp_packet_history.cc:260-271 in the Android copy).
func (m *Member) onNACK(nack *rtcp.TransportLayerNack) {
	sender := m.room.owner(nack.MediaSSRC)
	if sender == nil || sender == m {
		return
	}
	dt := sender.downTrack(m, false)
	if dt == nil {
		return
	}
	upstream := map[uint32][]uint16{}
	count := 0
	now := time.Now()
	for _, pair := range nack.Nacks {
		for _, seq := range pair.PacketList() {
			if count++; count > maxNACKsPerBatch {
				break
			}
			metrics.NACKs.Inc()
			inSSRC, inSeq, ok := dt.Original(seq)
			if !ok {
				metrics.Retransmissions.WithLabelValues("missing").Inc()
				continue
			}
			src := sender.sources[inSSRC]
			if src == nil || src.cache == nil {
				continue
			}
			orig := src.cache.Get(inSeq)
			if orig == nil {
				upstream[inSSRC] = append(upstream[inSSRC], inSeq)
				metrics.Retransmissions.WithLabelValues("upstream").Inc()
				continue
			}
			if out := dt.Retransmission(seq, orig); out != nil {
				kind := metrics.KindCamera
				if sender.kind == KindPresentation {
					kind = metrics.KindScreen
				}
				m.send(out, kind, now)
				metrics.Retransmissions.WithLabelValues("rtx").Inc()
			}
		}
	}
	for ssrc, seqs := range upstream {
		sender.writeRTCP([]rtcp.Packet{&rtcp.TransportLayerNack{
			SenderSSRC: sfuSSRC, MediaSSRC: ssrc, Nacks: rtcp.NackPairsFromSequenceNumbers(seqs),
		}})
	}
}

// onKeyframeRequest passes a receiver's PLI or FIR to the sender, for the
// layer the receiver gets.
func (m *Member) onKeyframeRequest(mediaSSRC uint32, now time.Time) {
	sender := m.room.owner(mediaSSRC)
	if sender == nil || sender == m {
		return
	}
	dt := sender.downTrack(m, false)
	if dt == nil {
		return
	}
	target, current := dt.Layers()
	layer := current
	if layer == forward.NoLayer {
		layer = target
	}
	m.room.requestKeyframe(sender, layer, "receiver", now)
}

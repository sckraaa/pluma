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

// Package groupcallpeer is a group call client for tests and debugging. It
// speaks to the SFU as tgcalls does:
//
//   - join params: GroupJoinInternalPayload::serialize, sources from a random
//     31-bit audio SSRC, video layers from SSRC+1 in pairs (layer, RTX), a
//     SIM group with more than one layer and a FID group per layer
//     (tgcalls/group/GroupJoinPayloadInternal.cpp:302-344,
//     GroupInstanceCustomImpl.cpp:3699-3740);
//   - ICE: controlled, full remote agent, host candidates only, none in the
//     join params (GroupNetworkManager.cpp:398-421);
//   - DTLS: the server, setup "passive", SRTP with the AEAD-GCM profiles
//     only (GroupNetworkManager.cpp:322-326, 434; GroupInstanceCustomImpl.cpp:3768);
//   - data channel: SCTP 5000/5000, the client opens stream 0 "data"
//     (SctpDataChannelProviderInterfaceImpl.cpp:30-41, 106;
//     GroupNetworkManager.cpp:454-457);
//   - RTP: Opus 111 with header extensions 1 (audio level), 2 (abs-send-time),
//     3 (transport-cc); VP8 100 / RTX 101 with 13 (rotation)
//     (GroupInstanceCustomImpl.cpp:1586-1600, 1716-1719, 299-350).
//
// Differences from tgcalls are in the transport library only: pion connects
// to an ICE-TCP candidate actively when it says tcptype passive, which the
// SFU's answer does; libwebrtc does it without tcptype
// (webrtc p2p/base/tcp_port.cc TCPPort::CreateConnection).
//
// With PeerConnectionRole the peer takes the controlling ICE role, as a
// browser's PeerConnection does for Web.
//
// With an E2E (SetE2E) the peer is an end-to-end participant: its frames
// go out encrypted with tde2e, the frames it receives are decrypted and
// those that do not decrypt are dropped, and StartReports sends the status
// reports of the encryption (e2e.go).
package groupcallpeer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/datachannel"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/sctp"
	"github.com/pion/srtp/v3"

	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"
)

// Config of a peer.
type Config struct {
	// VideoLayers is 0 for sound only, 3 for a camera, 2 for a screen, 1
	// as in a conference.
	VideoLayers int
	// Networks the peer gathers on: UDP4 when empty. TCP4 alone makes a
	// client behind a network without UDP.
	Networks []ice.NetworkType
	// PeerConnectionRole takes the controlling ICE role, as Web does.
	PeerConnectionRole bool
	// ExcludeLoopback leaves loopback addresses out; tests on one machine
	// need them.
	ExcludeLoopback bool
}

// Peer is one connection of a client.
type Peer struct {
	cfg      Config
	agent    *ice.Agent
	ufrag    string
	pwd      string
	dtlsCert tls.Certificate
	digest   [32]byte
	req      *tgcallsjson.JoinRequest
	layers   []tgcallsjson.VideoLayer

	endpoint string

	iceConn *ice.Conn
	writeMu sync.Mutex
	srtpMu  sync.Mutex
	local   *srtp.Context
	remote  *srtp.Context

	mu       sync.Mutex
	dtlsIn   *packetConn
	dtlsConn *dtls.Conn
	assoc    *sctp.Association
	dc       *datachannel.DataChannel
	dcOpen   chan struct{}

	transportSeq atomic.Uint32
	streams      map[uint32]*outStream
	e2e          atomic.Pointer[E2E]

	rtpCh  chan *rtp.Packet
	rtcpCh chan []rtcp.Packet
	dataCh chan []byte

	closeOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
}

type outStream struct {
	seq       uint16
	timestamp uint32
	pictureID uint16
	tl0       uint8
	frames    int
}

// New prepares a peer and gathers its candidates, as tgcalls does before it
// emits the join params.
func New(cfg Config) (*Peer, error) {
	if cfg.VideoLayers < 0 || cfg.VideoLayers > tgcallsjson.MaxVideoLayers {
		return nil, fmt.Errorf("groupcallpeer: %d layers", cfg.VideoLayers)
	}
	if len(cfg.Networks) == 0 {
		cfg.Networks = []ice.NetworkType{ice.NetworkTypeUDP4}
	}
	ufrag, err := randomICE(4)
	if err != nil {
		return nil, err
	}
	pwd, err := randomICE(24)
	if err != nil {
		return nil, err
	}
	opts := []ice.AgentOption{
		ice.WithLocalCredentials(ufrag, pwd),
		ice.WithNetworkTypes(cfg.Networks),
		ice.WithCandidateTypes([]ice.CandidateType{ice.CandidateTypeHost}),
		ice.WithMulticastDNSMode(ice.MulticastDNSModeDisabled),
		ice.WithLoggerFactory(quietLogs()),
	}
	if !cfg.ExcludeLoopback {
		opts = append(opts, ice.WithIncludeLoopback())
	}
	agent, err := ice.NewAgentWithOptions(opts...)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Peer{
		cfg:     cfg,
		agent:   agent,
		ufrag:   ufrag,
		pwd:     pwd,
		streams: map[uint32]*outStream{},
		dcOpen:  make(chan struct{}),
		rtpCh:   make(chan *rtp.Packet, 4096),
		rtcpCh:  make(chan []rtcp.Packet, 1024),
		dataCh:  make(chan []byte, 256),
		ctx:     ctx,
		cancel:  cancel,
	}
	if err := agent.OnCandidate(func(ice.Candidate) {}); err != nil {
		p.Close()
		return nil, err
	}
	if err := agent.GatherCandidates(); err != nil {
		p.Close()
		return nil, err
	}
	certificate, err := selfsign.GenerateSelfSigned()
	if err != nil {
		p.Close()
		return nil, err
	}
	p.dtlsCert = certificate
	p.digest = sha256.Sum256(certificate.Certificate[0])

	audio, err := randomSSRC()
	if err != nil {
		p.Close()
		return nil, err
	}
	p.req = &tgcallsjson.JoinRequest{
		SSRC:  audio,
		Ufrag: ufrag,
		Pwd:   pwd,
		Fingerprints: []tgcallsjson.Fingerprint{{
			Hash:        "sha-256",
			Fingerprint: tgcallsjson.EncodeFingerprint(p.digest[:]),
			Setup:       tgcallsjson.SetupPassive,
		}},
	}
	if cfg.VideoLayers > 0 {
		var sim []uint32
		for i := 0; i < cfg.VideoLayers; i++ {
			l := tgcallsjson.VideoLayer{SSRC: audio + 1 + uint32(i)*2, RTX: audio + 2 + uint32(i)*2}
			p.layers = append(p.layers, l)
			sim = append(sim, l.SSRC)
		}
		if len(sim) > 1 {
			p.req.SourceGroups = append(p.req.SourceGroups, tgcallsjson.SourceGroup{Semantics: tgcallsjson.SemanticsSIM, Sources: sim})
		}
		for _, l := range p.layers {
			p.req.SourceGroups = append(p.req.SourceGroups, tgcallsjson.SourceGroup{Semantics: tgcallsjson.SemanticsFID, Sources: []uint32{l.SSRC, l.RTX}})
		}
	}
	for _, s := range p.req.Sources() {
		st := &outStream{}
		var b [7]byte
		if _, err := rand.Read(b[:]); err != nil {
			p.Close()
			return nil, err
		}
		st.seq = binary.BigEndian.Uint16(b[0:2])
		st.timestamp = binary.BigEndian.Uint32(b[2:6])
		st.pictureID = binary.BigEndian.Uint16(b[4:6]) & 0x7fff
		st.tl0 = b[6]
		p.streams[s] = st
	}
	return p, nil
}

// SetE2E makes the peer encrypt and decrypt its frames with e.
func (p *Peer) SetE2E(e *E2E) { p.e2e.Store(e) }

// E2E is the encryption of the peer, nil without one.
func (p *Peer) E2E() *E2E { return p.e2e.Load() }

// Done is closed when the peer closes.
func (p *Peer) Done() <-chan struct{} { return p.ctx.Done() }

// JoinParams returns the params of phone.joinGroupCall.
func (p *Peer) JoinParams() ([]byte, error) { return p.req.MarshalJSON() }

// AudioSSRC is the peer's sound source.
func (p *Peer) AudioSSRC() uint32 { return p.req.SSRC }

// Layers are the peer's video layers.
func (p *Peer) Layers() []tgcallsjson.VideoLayer { return p.layers }

// SourceGroups are the peer's video source groups, as other participants
// get them in groupCallParticipantVideo.source_groups.
func (p *Peer) SourceGroups() []tgcallsjson.SourceGroup { return p.req.SourceGroups }

// Endpoint is the peer's endpoint from the answer.
func (p *Peer) Endpoint() string { return p.endpoint }

// Connect connects with the SFU's answer: ICE, DTLS as the server, SRTP,
// then the data channel in the background.
func (p *Peer) Connect(ctx context.Context, answer []byte) error {
	resp, err := tgcallsjson.ParseJoinResponse(answer)
	if err != nil {
		return err
	}
	if resp.Video != nil {
		p.endpoint = resp.Video.Endpoint
	}
	if len(resp.Transport.Fingerprints) == 0 {
		return errors.New("groupcallpeer: no fingerprint in the answer")
	}
	remoteFP := resp.Transport.Fingerprints[0]
	remoteDigest, err := tgcallsjson.DecodeFingerprint(remoteFP.Fingerprint, 32)
	if err != nil || remoteFP.Hash != "sha-256" {
		return errors.New("groupcallpeer: unusable fingerprint")
	}
	for _, c := range resp.Transport.Candidates {
		line := fmt.Sprintf("candidate:%s %s %s %s %s %s typ %s", c.Foundation, c.Component, strings.ToLower(c.Protocol), c.Priority, c.IP, c.Port, c.Type)
		if c.TCPType != "" {
			line += " tcptype " + c.TCPType
		}
		cand, err := ice.UnmarshalCandidate(line)
		if err != nil {
			return fmt.Errorf("groupcallpeer: candidate: %w", err)
		}
		if !p.wants(cand.NetworkType()) {
			continue
		}
		if err := p.agent.AddRemoteCandidate(cand); err != nil {
			return err
		}
	}

	if p.cfg.PeerConnectionRole {
		p.iceConn, err = p.agent.Dial(ctx, resp.Transport.Ufrag, resp.Transport.Pwd)
	} else {
		p.iceConn, err = p.agent.Accept(ctx, resp.Transport.Ufrag, resp.Transport.Pwd)
	}
	if err != nil {
		return fmt.Errorf("groupcallpeer: ICE: %w", err)
	}
	in := newPacketConn(p.iceConn, &p.writeMu)
	p.mu.Lock()
	p.dtlsIn = in
	p.mu.Unlock()
	go p.readLoop(in)

	conn, err := dtls.ServerWithOptions(in, p.iceConn.RemoteAddr(),
		dtls.WithCertificates(p.dtlsCert),
		dtls.WithSRTPProtectionProfiles(dtls.SRTP_AEAD_AES_128_GCM, dtls.SRTP_AEAD_AES_256_GCM),
		dtls.WithExtendedMasterSecret(dtls.RequestExtendedMasterSecret),
		dtls.WithClientAuth(dtls.RequireAnyClientCert),
		dtls.WithInsecureSkipVerify(true),
		dtls.WithLoggerFactory(quietLogs()),
		dtls.WithVerifyPeerCertificate(func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("no certificate")
			}
			d := sha256.Sum256(raw[0])
			if subtle.ConstantTimeCompare(d[:], remoteDigest) != 1 {
				return errors.New("certificate does not match the fingerprint")
			}
			return nil
		}),
	)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.dtlsConn = conn
	p.mu.Unlock()
	if err := conn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("groupcallpeer: DTLS: %w", err)
	}
	if err := p.setupSRTP(conn); err != nil {
		return err
	}
	p.mu.Lock()
	opened := p.dcOpen
	p.mu.Unlock()
	go p.runDataChannel(conn, opened)
	return nil
}

func (p *Peer) wants(t ice.NetworkType) bool {
	for _, n := range p.cfg.Networks {
		if n == t {
			return true
		}
	}
	return false
}

func (p *Peer) setupSRTP(conn *dtls.Conn) error {
	profile, ok := conn.SelectedSRTPProtectionProfile()
	if !ok {
		return errors.New("groupcallpeer: no SRTP profile")
	}
	var sp srtp.ProtectionProfile
	switch profile {
	case dtls.SRTP_AEAD_AES_128_GCM:
		sp = srtp.ProtectionProfileAeadAes128Gcm
	case dtls.SRTP_AEAD_AES_256_GCM:
		sp = srtp.ProtectionProfileAeadAes256Gcm
	default:
		return fmt.Errorf("groupcallpeer: SRTP profile %d is not GCM", profile)
	}
	state, ok := conn.ConnectionState()
	if !ok {
		return errors.New("groupcallpeer: no DTLS state")
	}
	cfg := srtp.Config{Profile: sp}
	if err := cfg.ExtractSessionKeysFromDTLS(&state, false); err != nil {
		return err
	}
	local, err := srtp.CreateContext(cfg.Keys.LocalMasterKey, cfg.Keys.LocalMasterSalt, sp)
	if err != nil {
		return err
	}
	remote, err := srtp.CreateContext(cfg.Keys.RemoteMasterKey, cfg.Keys.RemoteMasterSalt, sp)
	if err != nil {
		return err
	}
	p.srtpMu.Lock()
	p.local, p.remote = local, remote
	p.srtpMu.Unlock()
	return nil
}

// runDataChannel starts SCTP and opens stream 0 "data" as tgcalls does.
func (p *Peer) runDataChannel(conn *dtls.Conn, opened chan struct{}) {
	assoc, err := sctp.ClientContext(p.ctx, sctp.WithNetConn(lend(conn)), sctp.WithLoggerFactory(quietLogs()))
	if err != nil {
		return
	}
	go func() {
		<-p.ctx.Done()
		_ = assoc.Close()
	}()
	dc, err := datachannel.Dial(assoc, 0, &datachannel.Config{
		ChannelType: datachannel.ChannelTypeReliable, Label: "data", LoggerFactory: quietLogs(),
	})
	if err != nil {
		_ = assoc.Close()
		return
	}
	p.mu.Lock()
	p.assoc, p.dc = assoc, dc
	p.mu.Unlock()
	close(opened)
	buf := make([]byte, 64<<10)
	for {
		n, isString, err := dc.ReadDataChannel(buf)
		if err != nil {
			return
		}
		if isString {
			select {
			case p.dataCh <- append([]byte(nil), buf[:n]...):
			default:
			}
		}
	}
}

// RestartDataChannel ends the SCTP association and starts a new one on the
// same DTLS connection, as tgcalls does when its SCTP transport fails
// (SctpDataChannelProviderInterfaceImpl.cpp:118-124, GroupNetworkManager.cpp:451-485).
func (p *Peer) RestartDataChannel() {
	p.mu.Lock()
	assoc, dc, conn := p.assoc, p.dc, p.dtlsConn
	p.assoc, p.dc = nil, nil
	opened := make(chan struct{})
	p.dcOpen = opened
	p.mu.Unlock()
	if dc != nil {
		_ = dc.Close()
	}
	if assoc != nil {
		_ = assoc.Close()
	}
	go p.runDataChannel(conn, opened)
}

// WaitDataChannel waits for the data channel to open.
func (p *Peer) WaitDataChannel(ctx context.Context) error {
	p.mu.Lock()
	opened := p.dcOpen
	p.mu.Unlock()
	select {
	case <-opened:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendData sends a text message on the data channel.
func (p *Peer) SendData(msg []byte) error {
	p.mu.Lock()
	dc := p.dc
	p.mu.Unlock()
	if dc == nil {
		return errors.New("groupcallpeer: data channel not open")
	}
	_, err := dc.WriteDataChannel(msg, true)
	return err
}

// SendVideoConstraints sends ReceiverVideoConstraints as tgcalls does.
func (p *Peer) SendVideoConstraints(onStage []string, constraints map[string]tgcallsjson.VideoConstraint) error {
	return p.SendData(tgcallsjson.MarshalReceiverVideoConstraints(onStage, constraints))
}

func (p *Peer) readLoop(in *packetConn) {
	buf := make([]byte, 1500)
	for {
		n, err := p.iceConn.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		switch b := buf[0]; {
		case b >= 20 && b <= 63:
			in.push(buf[:n])
		case b >= 128 && b <= 191:
			p.handleSRTP(buf[:n])
		}
	}
}

func (p *Peer) handleSRTP(data []byte) {
	p.srtpMu.Lock()
	remote := p.remote
	p.srtpMu.Unlock()
	if remote == nil || len(data) < 12 {
		return
	}
	if pt := data[1]; pt >= 192 && pt <= 223 {
		plain, err := remote.DecryptRTCP(nil, data, nil)
		if err != nil {
			return
		}
		pkts, err := rtcp.Unmarshal(plain)
		if err != nil {
			return
		}
		select {
		case p.rtcpCh <- pkts:
		default:
		}
		return
	}
	plain, err := remote.DecryptRTP(nil, data, nil)
	if err != nil {
		return
	}
	pkt := &rtp.Packet{}
	if err := pkt.Unmarshal(plain); err != nil {
		return
	}
	if e := p.e2e.Load(); e != nil && !e.open(pkt) {
		return
	}
	select {
	case p.rtpCh <- pkt:
	default:
	}
}

// RTP delivers the packets the peer receives.
func (p *Peer) RTP() <-chan *rtp.Packet { return p.rtpCh }

// RTCP delivers the RTCP the peer receives.
func (p *Peer) RTCP() <-chan []rtcp.Packet { return p.rtcpCh }

// Data delivers the text messages of the data channel.
func (p *Peer) Data() <-chan []byte { return p.dataCh }

// PairNetwork is the transport of the selected pair: udp4, tcp4...
func (p *Peer) PairNetwork() string {
	pair, err := p.agent.GetSelectedCandidatePair()
	if err != nil || pair == nil {
		return ""
	}
	return pair.Local.NetworkType().String()
}

func (p *Peer) stamp(pkt *rtp.Packet) error {
	seq := uint16(p.transportSeq.Add(1))
	if err := pkt.Header.SetExtension(tgcallsjson.ExtTransportCC, []byte{byte(seq >> 8), byte(seq)}); err != nil {
		return err
	}
	ast, err := rtp.NewAbsSendTimeExtension(time.Now()).Marshal()
	if err != nil {
		return err
	}
	return pkt.Header.SetExtension(tgcallsjson.ExtAbsSendTime, ast)
}

// The Opus frame SendAudio sends: a TOC byte of CELT fullband 20 ms and
// silence after it.
const (
	AudioTOC       = 0xfc
	AudioFrameSize = 80
)

// SendAudio sends one Opus packet of 120 ms, the ptime of tgcalls
// (GroupInstanceCustomImpl.cpp:2716), with the level (RFC 6464, 0 loudest)
// and voice flag in extension 1.
func (p *Peer) SendAudio(level uint8, voice bool) error {
	st := p.streams[p.req.SSRC]
	pkt := &rtp.Packet{Header: rtp.Header{
		Version: 2, PayloadType: tgcallsjson.PayloadTypeOpus, SSRC: p.req.SSRC,
		SequenceNumber: st.seq, Timestamp: st.timestamp,
	}}
	st.seq++
	st.timestamp += 48000 * 120 / 1000
	ext, err := rtp.AudioLevelExtension{Level: level, Voice: voice}.Marshal()
	if err != nil {
		return err
	}
	if err := pkt.Header.SetExtension(tgcallsjson.ExtAudioLevel, ext); err != nil {
		return err
	}
	if err := p.stamp(pkt); err != nil {
		return err
	}
	pkt.Payload = make([]byte, AudioFrameSize)
	pkt.Payload[0] = AudioTOC
	if e := p.e2e.Load(); e != nil {
		if pkt.Payload, err = e.encryptAudio(pkt.Payload, level, voice); err != nil {
			return err
		}
	}
	return p.WriteRTP(pkt)
}

// SendVideoFrame sends one VP8 frame of a layer as one packet, with a
// 15-bit picture id and TL0PICIDX as libwebrtc writes them for simulcast.
// It returns the packet sent.
func (p *Peer) SendVideoFrame(layer int, keyframe bool) (*rtp.Packet, error) {
	pkt, err := p.videoFrame(layer, keyframe)
	if err != nil {
		return nil, err
	}
	if err := p.stamp(pkt); err != nil {
		return nil, err
	}
	return pkt, p.WriteRTP(pkt)
}

// LoseVideoFrame makes the next frame of a layer as SendVideoFrame would,
// but does not send it: a packet lost on the way to the SFU. SendRTX sends
// it later, as a sender answers a NACK.
func (p *Peer) LoseVideoFrame(layer int) (*rtp.Packet, error) {
	return p.videoFrame(layer, false)
}

func (p *Peer) videoFrame(layer int, keyframe bool) (*rtp.Packet, error) {
	if layer < 0 || layer >= len(p.layers) {
		return nil, fmt.Errorf("groupcallpeer: no layer %d", layer)
	}
	ssrc := p.layers[layer].SSRC
	st := p.streams[ssrc]
	payload := make([]byte, 0, 200)
	// X, S, PID 0; I, L; 15-bit picture id; TL0PICIDX.
	payload = append(payload, 0x90, 0xc0, 0x80|byte(st.pictureID>>8), byte(st.pictureID), st.tl0)
	if keyframe {
		// P=0, show_frame, then the key frame start code and 640x360.
		payload = append(payload, 0x10, 0x02, 0x00, 0x9d, 0x01, 0x2a, 0x80, 0x02, 0x68, 0x01)
	} else {
		payload = append(payload, 0x11, 0x02, 0x00)
	}
	for len(payload) < 200 {
		payload = append(payload, byte(layer))
	}
	if e := p.e2e.Load(); e != nil {
		// The packetizer puts the descriptor before the encrypted frame.
		frame, err := e.encryptVideo(payload[5:], keyframe)
		if err != nil {
			return nil, err
		}
		payload = append(payload[:5:5], frame...)
	}
	pkt := &rtp.Packet{Header: rtp.Header{
		Version: 2, Marker: true, PayloadType: tgcallsjson.PayloadTypeVP8, SSRC: ssrc,
		SequenceNumber: st.seq, Timestamp: st.timestamp,
	}, Payload: payload}
	st.seq++
	st.timestamp += 90000 / 30
	st.pictureID = (st.pictureID + 1) & 0x7fff
	st.frames++
	if keyframe {
		st.tl0++
	}
	return pkt, nil
}

// SendRTX retransmits a sent packet of a layer under its RTX SSRC (RFC
// 4588), as a sender answers the SFU's NACK.
func (p *Peer) SendRTX(layer int, orig *rtp.Packet) error {
	rtx := p.layers[layer].RTX
	st := p.streams[rtx]
	payload := make([]byte, 2+len(orig.Payload))
	binary.BigEndian.PutUint16(payload, orig.SequenceNumber)
	copy(payload[2:], orig.Payload)
	pkt := &rtp.Packet{Header: rtp.Header{
		Version: 2, Marker: orig.Marker, PayloadType: tgcallsjson.PayloadTypeVP8RTX, SSRC: rtx,
		SequenceNumber: st.seq, Timestamp: orig.Timestamp,
	}, Payload: payload}
	st.seq++
	if err := p.stamp(pkt); err != nil {
		return err
	}
	return p.WriteRTP(pkt)
}

// WriteRTP encrypts and sends a packet.
func (p *Peer) WriteRTP(pkt *rtp.Packet) error {
	raw, err := pkt.Marshal()
	if err != nil {
		return err
	}
	p.srtpMu.Lock()
	defer p.srtpMu.Unlock()
	if p.local == nil {
		return errors.New("groupcallpeer: not connected")
	}
	out, err := p.local.EncryptRTP(nil, raw, nil)
	if err != nil {
		return err
	}
	return p.write(out)
}

// WriteRTCP encrypts and sends RTCP.
func (p *Peer) WriteRTCP(pkts []rtcp.Packet) error {
	raw, err := rtcp.Marshal(pkts)
	if err != nil {
		return err
	}
	p.srtpMu.Lock()
	defer p.srtpMu.Unlock()
	if p.local == nil {
		return errors.New("groupcallpeer: not connected")
	}
	out, err := p.local.EncryptRTCP(nil, raw, nil)
	if err != nil {
		return err
	}
	return p.write(out)
}

func (p *Peer) write(b []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, err := p.iceConn.Write(b)
	return err
}

// Close closes the peer.
func (p *Peer) Close() {
	p.closeOnce.Do(func() {
		p.cancel()
		p.mu.Lock()
		if p.dc != nil {
			_ = p.dc.Close()
		}
		if p.dtlsConn != nil {
			_ = p.dtlsConn.Close()
		}
		if p.dtlsIn != nil {
			_ = p.dtlsIn.Close()
		}
		p.mu.Unlock()
		_ = p.agent.Close()
	})
}

// quietLogs keeps the pion packages to errors: their debug output prints
// ICE credentials.
func quietLogs() logging.LoggerFactory {
	f := logging.NewDefaultLoggerFactory()
	f.DefaultLogLevel = logging.LogLevelError
	return f
}

func randomSSRC() (uint32, error) {
	var b [4]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			return 0, err
		}
		// 31 bits, never 0 (GroupInstanceCustomImpl.cpp:3700-3704); room
		// for six video sources above it.
		if s := binary.BigEndian.Uint32(b[:]) & 0x7fffffff; s != 0 && s < 0x7fffffff-8 {
			return s, nil
		}
	}
}

func randomICE(n int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+/"
	out := make([]byte, n)
	for i := range out {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", err
		}
		out[i] = chars[v.Int64()]
	}
	return string(out), nil
}

// lend gives the DTLS connection to one SCTP association: closing it ends
// the association's reading but keeps DTLS (see the SFU's
// transport/sctpconn.go).
func lend(conn *dtls.Conn) net.Conn {
	_ = conn.SetReadDeadline(time.Time{})
	return &lentConn{Conn: conn}
}

type lentConn struct {
	*dtls.Conn
	once   sync.Once
	closed atomic.Bool
}

func (c *lentConn) Read(b []byte) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	n, err := c.Conn.Read(b)
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	return n, err
}

func (c *lentConn) Write(b []byte) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	return c.Conn.Write(b)
}

func (c *lentConn) Close() error {
	c.once.Do(func() {
		c.closed.Store(true)
		_ = c.Conn.SetReadDeadline(time.Now())
	})
	return nil
}

// packetConn feeds DTLS records from the ICE connection to the DTLS library.
type packetConn struct {
	conn    *ice.Conn
	writeMu *sync.Mutex
	in      chan []byte
	once    sync.Once
	closed  chan struct{}
}

func newPacketConn(conn *ice.Conn, writeMu *sync.Mutex) *packetConn {
	return &packetConn{conn: conn, writeMu: writeMu, in: make(chan []byte, 64), closed: make(chan struct{})}
}

func (c *packetConn) push(b []byte) {
	select {
	case c.in <- append([]byte(nil), b...):
	default:
	}
}

func (c *packetConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case pkt := <-c.in:
		return copy(b, pkt), c.conn.RemoteAddr(), nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *packetConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(b)
}

func (c *packetConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *packetConn) LocalAddr() net.Addr              { return c.conn.LocalAddr() }
func (c *packetConn) SetDeadline(time.Time) error      { return nil }
func (c *packetConn) SetReadDeadline(time.Time) error  { return nil }
func (c *packetConn) SetWriteDeadline(time.Time) error { return nil }

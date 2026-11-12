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

package tgcallsjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
)

// RTP parameters every client uses with fixed numbers. Native clients do not
// read them from the answer: Opus is 111 (GroupInstanceCustomImpl.cpp:1604,
// 2716), the header extensions have fixed ids (:1586-1600, 1716-1719, 3157-3160)
// and the video payload types are assigned locally from 100 in the order VP8,
// VP9, H.264, each followed by its RTX (:299-350). Web builds its SDP from the
// answer (tweb sdpBuilder.ts:185-260), so the answer states the same numbers.
const (
	PayloadTypeOpus     = 111
	PayloadTypeVP8      = 100
	PayloadTypeVP8RTX   = 101
	ExtAudioLevel       = 1
	ExtAbsSendTime      = 2
	ExtTransportCC      = 3
	ExtVideoOrientation = 13
)

// URIs of the header extensions.
const (
	URIAudioLevel     = "urn:ietf:params:rtp-hdrext:ssrc-audio-level"
	URIAbsSendTime    = "http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time"
	URITransportCC    = "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"
	URIVideoRotation  = "urn:3gpp:video-orientation"
	xmlnsICEUDP       = "urn:xmpp:jingle:transports:ice-udp:1"
	candidateTypeHost = "host"
)

// Candidate is an ICE candidate of the SFU. Every field is a string:
// tgcalls rejects the whole answer when one is missing or of another type
// (GroupJoinPayloadInternal.cpp:94-172), and Web requires them as SDP tokens
// (tweb helpers/sdpSafety.ts, CANDIDATE_FIELDS).
type Candidate struct {
	Generation string `json:"generation"`
	Component  string `json:"component"`
	Protocol   string `json:"protocol"`
	Port       string `json:"port"`
	IP         string `json:"ip"`
	Foundation string `json:"foundation"`
	ID         string `json:"id"`
	Priority   string `json:"priority"`
	Type       string `json:"type"`
	Network    string `json:"network"`
	// TCPType is "passive" for ICE-TCP. tgcalls reads it but does not pass
	// it on (GroupInstanceCustomImpl.cpp:3863-3875); libwebrtc then connects
	// to a TCP candidate with a port actively (webrtc p2p/base/tcp_port.cc
	// TCPPort::CreateConnection, Android's bundled copy).
	TCPType string `json:"tcptype,omitempty"`
}

// Transport is the ICE and DTLS side of the answer.
type Transport struct {
	XMLNS        string        `json:"xmlns,omitempty"`
	RTCPMux      bool          `json:"rtcp-mux"`
	Ufrag        string        `json:"ufrag"`
	Pwd          string        `json:"pwd"`
	Fingerprints []Fingerprint `json:"fingerprints"`
	Candidates   []Candidate   `json:"candidates"`
}

// Feedback is an rtcp-fb entry of a payload type.
type Feedback struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
}

// PayloadType describes a codec. Parameters hold strings or integers.
type PayloadType struct {
	ID         int               `json:"id"`
	Name       string            `json:"name"`
	Clockrate  int               `json:"clockrate"`
	Channels   int               `json:"channels,omitempty"`
	Parameters map[string]string `json:"parameters,omitempty"`
	RTCPFbs    []Feedback        `json:"rtcp-fbs,omitempty"`
}

// HeaderExtension maps an id to a header extension URI.
type HeaderExtension struct {
	ID  int    `json:"id"`
	URI string `json:"uri"`
}

// Media describes the audio or the video of the answer.
type Media struct {
	// Endpoint is the id of this connection in Colibri messages and in
	// groupCallParticipantVideo.endpoint. Only video has it.
	Endpoint      string            `json:"endpoint,omitempty"`
	PayloadTypes  []PayloadType     `json:"payload-types"`
	HeaderExts    []HeaderExtension `json:"rtp-hdrexts"`
	ServerSources []int32           `json:"server_sources,omitempty"`
}

// JoinResponse is the params of updateGroupCallConnection. Audio and video
// are always present: Web requires video (tweb types.d.ts:73-78).
type JoinResponse struct {
	Transport Transport `json:"transport"`
	Audio     *Media    `json:"audio"`
	Video     *Media    `json:"video"`
}

// Address is a public address of the SFU where clients reach it.
type Address struct {
	IP   netip.Addr
	Port uint16
	TCP  bool
}

// ResponseParams is what an answer is built from.
type ResponseParams struct {
	Ufrag string
	Pwd   string
	// FingerprintSHA256 is the digest of the SFU's DTLS certificate.
	FingerprintSHA256 []byte
	Addresses         []Address
	Endpoint          string
}

// BuildResponse builds the answer to a join request. The SFU is the DTLS
// client, so its fingerprint carries setup "active" (ADR 0059 §4; tgcalls
// itself always takes the server role, GroupNetworkManager.cpp:434).
func BuildResponse(p ResponseParams) (*JoinResponse, error) {
	if !iceString(p.Ufrag, 4) || !iceString(p.Pwd, 22) {
		return nil, errors.New("tgcallsjson: bad ICE credentials")
	}
	if len(p.FingerprintSHA256) != 32 {
		return nil, errors.New("tgcallsjson: fingerprint must be sha-256")
	}
	if len(p.Addresses) == 0 {
		return nil, errors.New("tgcallsjson: no addresses")
	}
	if !iceString(p.Endpoint, 1) {
		return nil, errors.New("tgcallsjson: bad endpoint")
	}
	resp := &JoinResponse{
		Transport: Transport{
			XMLNS:   xmlnsICEUDP,
			RTCPMux: true,
			Ufrag:   p.Ufrag,
			Pwd:     p.Pwd,
			Fingerprints: []Fingerprint{{
				Hash:        "sha-256",
				Fingerprint: EncodeFingerprint(p.FingerprintSHA256),
				Setup:       SetupActive,
			}},
		},
		Audio: &Media{
			PayloadTypes: []PayloadType{{
				ID: PayloadTypeOpus, Name: "opus", Clockrate: 48000, Channels: 2,
				Parameters: map[string]string{"minptime": "10", "useinbandfec": "1"},
				RTCPFbs:    []Feedback{{Type: "transport-cc"}},
			}},
			HeaderExts: []HeaderExtension{
				{ID: ExtAudioLevel, URI: URIAudioLevel},
				{ID: ExtAbsSendTime, URI: URIAbsSendTime},
				{ID: ExtTransportCC, URI: URITransportCC},
			},
		},
		Video: &Media{
			Endpoint: p.Endpoint,
			PayloadTypes: []PayloadType{
				{
					ID: PayloadTypeVP8, Name: "VP8", Clockrate: 90000,
					RTCPFbs: []Feedback{
						{Type: "goog-remb"},
						{Type: "transport-cc"},
						{Type: "ccm", Subtype: "fir"},
						{Type: "nack"},
						{Type: "nack", Subtype: "pli"},
					},
				},
				{
					ID: PayloadTypeVP8RTX, Name: "rtx", Clockrate: 90000,
					Parameters: map[string]string{"apt": strconv.Itoa(PayloadTypeVP8)},
				},
			},
			HeaderExts: []HeaderExtension{
				{ID: ExtAudioLevel, URI: URIAudioLevel},
				{ID: ExtAbsSendTime, URI: URIAbsSendTime},
				{ID: ExtTransportCC, URI: URITransportCC},
				{ID: ExtVideoOrientation, URI: URIVideoRotation},
			},
		},
	}
	for i, a := range p.Addresses {
		if !a.IP.IsValid() || a.Port == 0 {
			return nil, fmt.Errorf("tgcallsjson: bad address %d", i)
		}
		c := Candidate{
			Generation: "0",
			Component:  "1",
			Protocol:   "udp",
			Port:       strconv.Itoa(int(a.Port)),
			IP:         a.IP.Unmap().String(),
			Foundation: strconv.Itoa(i + 1),
			ID:         "c" + strconv.Itoa(i+1),
			Priority:   strconv.FormatUint(uint64(candidatePriority(a)), 10),
			Type:       candidateTypeHost,
			Network:    "1",
		}
		if a.TCP {
			c.Protocol = "tcp"
			c.TCPType = "passive"
		}
		resp.Transport.Candidates = append(resp.Transport.Candidates, c)
	}
	return resp, nil
}

// candidatePriority follows RFC 8445 §5.1.2.1 for component 1: host UDP
// first, ICE-TCP after it (RFC 6544 §4.2), IPv4 before IPv6 on the same
// transport since more clients have it.
func candidatePriority(a Address) uint32 {
	typePref := uint32(126)
	if a.TCP {
		typePref = 90
	}
	localPref := uint32(65535)
	if a.IP.Unmap().Is6() {
		localPref = 65534
	}
	return typePref<<24 | localPref<<8 | (256 - 1)
}

// Marshal returns the answer as the params string of
// updateGroupCallConnection.
func (r *JoinResponse) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

// ParseJoinResponse parses an answer the way tgcalls does
// (GroupJoinPayloadInternal.cpp:347-371): transport and every candidate
// field are required. The test client uses it.
func ParseJoinResponse(data []byte) (*JoinResponse, error) {
	var raw struct {
		Transport *struct {
			Ufrag        *string                      `json:"ufrag"`
			Pwd          *string                      `json:"pwd"`
			Fingerprints []Fingerprint                `json:"fingerprints"`
			Candidates   []map[string]json.RawMessage `json:"candidates"`
		} `json:"transport"`
		Audio *Media `json:"audio"`
		Video *Media `json:"video"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	t := raw.Transport
	if t == nil || t.Ufrag == nil || t.Pwd == nil || t.Fingerprints == nil || t.Candidates == nil {
		return nil, fmt.Errorf("%w: transport incomplete", ErrMalformed)
	}
	out := &JoinResponse{
		Transport: Transport{Ufrag: *t.Ufrag, Pwd: *t.Pwd, Fingerprints: t.Fingerprints},
		Audio:     raw.Audio,
		Video:     raw.Video,
	}
	for _, f := range t.Fingerprints {
		if f.Hash == "" || f.Fingerprint == "" || f.Setup == "" {
			return nil, fmt.Errorf("%w: fingerprint incomplete", ErrMalformed)
		}
	}
	for i, c := range t.Candidates {
		var cand Candidate
		fields := map[string]*string{
			"port": &cand.Port, "protocol": &cand.Protocol, "network": &cand.Network,
			"generation": &cand.Generation, "id": &cand.ID, "component": &cand.Component,
			"foundation": &cand.Foundation, "priority": &cand.Priority, "ip": &cand.IP,
			"type": &cand.Type,
		}
		for name, dst := range fields {
			v, ok := c[name]
			if !ok || json.Unmarshal(v, dst) != nil {
				return nil, fmt.Errorf("%w: candidate %d field %s", ErrMalformed, i, name)
			}
		}
		if v, ok := c["tcptype"]; ok {
			if json.Unmarshal(v, &cand.TCPType) != nil {
				return nil, fmt.Errorf("%w: candidate %d tcptype", ErrMalformed, i)
			}
		}
		out.Transport.Candidates = append(out.Transport.Candidates, cand)
	}
	return out, nil
}

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

// Package tgcallsjson holds the JSON that group call clients and the SFU
// exchange through the server: the params of phone.joinGroupCall (and of
// phone.joinGroupCallPresentation), the params of updateGroupCallConnection
// that answer them, and the Colibri messages of the data channel.
//
// The formats follow the clients, which are the only description there is:
//
//   - request: tgcalls GroupJoinInternalPayload::serialize
//     (tgcalls/group/GroupJoinPayloadInternal.cpp:302-344) and Web
//     (tweb src/lib/calls/helpers/processMediaSection.ts:33-39);
//   - response: GroupJoinResponsePayload::parse
//     (GroupJoinPayloadInternal.cpp:41-371) and Web
//     (tweb src/lib/calls/types.d.ts:1-79);
//   - Colibri: GroupInstanceCustomImpl.cpp:3330-3440 (SenderVideoConstraints
//     in), 3511-3578 (ReceiverVideoConstraints out); Web
//     groupCallConnectionInstance.ts:672-713.
//
// tgcalls is the one pinned by the clients: 1c236c09 in tdesktop, 545f5ae5 in
// Android, c1de0882 in iOS; the files named here are byte-identical in all
// three.
package tgcallsjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Limits of a join request. The JSON comes from a client, so everything in it
// is bounded before the SFU acts on it.
const (
	// MaxRequestSize bounds the params of a join request. A real one is
	// about 300 bytes with three video layers.
	MaxRequestSize = 4096
	// MaxVideoLayers is the most simulcast layers a client sends: 3 for a
	// camera, 2 for a screen, 1 in a conference
	// (GroupInstanceCustomImpl.cpp:3706-3712).
	MaxVideoLayers = 3
	// maxSourceGroups is a SIM group and one FID group per layer.
	maxSourceGroups = 1 + MaxVideoLayers
	// maxFingerprints bounds the DTLS fingerprints of one request; clients
	// send one.
	maxFingerprints = 4
)

// Semantics of the source groups.
const (
	SemanticsSIM = "SIM"
	SemanticsFID = "FID"
)

// DTLS setup roles (RFC 4145, RFC 5763).
const (
	SetupActive  = "active"
	SetupPassive = "passive"
	SetupActpass = "actpass"
)

// Errors of ParseJoinRequest. They are wrapped with the details; callers
// test them with errors.Is.
var (
	ErrMalformed   = errors.New("tgcallsjson: malformed join params")
	ErrUnsupported = errors.New("tgcallsjson: unsupported join params")
)

// Fingerprint is a DTLS certificate fingerprint with the setup role of its
// owner.
type Fingerprint struct {
	Hash        string `json:"hash"`
	Fingerprint string `json:"fingerprint"`
	Setup       string `json:"setup"`
}

// SourceGroup is a group of SSRCs: SIM lists the simulcast layers from the
// lowest, FID pairs a layer with its retransmission (RTX) stream.
type SourceGroup struct {
	Semantics string   `json:"semantics"`
	Sources   []uint32 `json:"sources"`
}

// JoinRequest is the params of phone.joinGroupCall as the client sends them.
type JoinRequest struct {
	// SSRC is the audio source of the client. For a screen sharing
	// connection it is the source the client reserves for the screen's
	// sound; video sources go from SSRC+1 (GroupInstanceCustomImpl.cpp:3699-3712).
	SSRC         uint32
	Ufrag        string
	Pwd          string
	Fingerprints []Fingerprint
	SourceGroups []SourceGroup
}

// VideoLayer is one simulcast layer of a client's video: its media SSRC and
// the SSRC of its retransmissions (0 if the client sends none).
type VideoLayer struct {
	SSRC uint32
	RTX  uint32
}

// wireRequest is the JSON form. Sources are numbers: tgcalls writes them as
// signed 32-bit integers (GroupJoinPayloadInternal.cpp:305, 321), Web as
// unsigned ones from its SDP.
type wireRequest struct {
	SSRC         json.RawMessage `json:"ssrc"`
	Ufrag        string          `json:"ufrag"`
	Pwd          string          `json:"pwd"`
	Fingerprints []Fingerprint   `json:"fingerprints"`
	SourceGroups []struct {
		Semantics string            `json:"semantics"`
		Sources   []json.RawMessage `json:"sources"`
	} `json:"ssrc-groups"`
}

// ParseJoinRequest parses and checks the params of a join request.
//
// It requires what the SFU needs to connect: a non-zero audio SSRC, ICE
// credentials (RFC 8445 §5.3: ufrag of 4+ and password of 22+ ice-chars),
// a sha-256 fingerprint whose owner can be the DTLS server (the SFU is the
// DTLS client for every client, ADR 0059 §4), and source groups that
// describe at most three video layers.
func ParseJoinRequest(data []byte) (*JoinRequest, error) {
	if len(data) == 0 || len(data) > MaxRequestSize {
		return nil, fmt.Errorf("%w: size %d", ErrMalformed, len(data))
	}
	var w wireRequest
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if w.SSRC == nil {
		return nil, fmt.Errorf("%w: no ssrc", ErrMalformed)
	}
	ssrc, err := parseSource(w.SSRC)
	if err != nil {
		return nil, err
	}
	req := &JoinRequest{SSRC: ssrc, Ufrag: w.Ufrag, Pwd: w.Pwd}
	if !iceString(w.Ufrag, 4) {
		return nil, fmt.Errorf("%w: bad ufrag", ErrMalformed)
	}
	if !iceString(w.Pwd, 22) {
		return nil, fmt.Errorf("%w: bad pwd", ErrMalformed)
	}

	if len(w.Fingerprints) == 0 || len(w.Fingerprints) > maxFingerprints {
		return nil, fmt.Errorf("%w: %d fingerprints", ErrMalformed, len(w.Fingerprints))
	}
	for _, fp := range w.Fingerprints {
		fp.Hash = strings.ToLower(fp.Hash)
		if err := checkFingerprint(fp); err != nil {
			return nil, err
		}
		req.Fingerprints = append(req.Fingerprints, fp)
	}
	if req.SHA256Fingerprint() == nil {
		return nil, fmt.Errorf("%w: no sha-256 fingerprint", ErrUnsupported)
	}

	if len(w.SourceGroups) > maxSourceGroups {
		return nil, fmt.Errorf("%w: %d source groups", ErrMalformed, len(w.SourceGroups))
	}
	for _, g := range w.SourceGroups {
		group := SourceGroup{Semantics: g.Semantics}
		for _, n := range g.Sources {
			s, err := parseSource(n)
			if err != nil {
				return nil, err
			}
			group.Sources = append(group.Sources, s)
		}
		req.SourceGroups = append(req.SourceGroups, group)
	}
	if _, err := req.VideoLayers(); err != nil {
		return nil, err
	}
	return req, nil
}

// SHA256Fingerprint returns the sha-256 fingerprint of the request, nil if
// there is none.
func (r *JoinRequest) SHA256Fingerprint() *Fingerprint {
	for i := range r.Fingerprints {
		if r.Fingerprints[i].Hash == "sha-256" {
			return &r.Fingerprints[i]
		}
	}
	return nil
}

// HasVideo reports whether the request describes video sources.
func (r *JoinRequest) HasVideo() bool {
	return len(r.SourceGroups) > 0
}

// VideoLayers returns the video layers of the request from the lowest, or
// none for an audio-only request. tgcalls sends a SIM group only with more
// than one layer and a FID group per layer (GroupInstanceCustomImpl.cpp:3713-3740);
// a single layer is a lone FID group, or a lone SIM group of one source
// without retransmissions.
func (r *JoinRequest) VideoLayers() ([]VideoLayer, error) {
	var sim []uint32
	fid := map[uint32]uint32{}
	var fidOrder []uint32
	seen := map[uint32]bool{r.SSRC: true}
	for _, g := range r.SourceGroups {
		switch g.Semantics {
		case SemanticsSIM:
			if sim != nil {
				return nil, fmt.Errorf("%w: two SIM groups", ErrMalformed)
			}
			if len(g.Sources) == 0 || len(g.Sources) > MaxVideoLayers {
				return nil, fmt.Errorf("%w: SIM group of %d", ErrMalformed, len(g.Sources))
			}
			sim = g.Sources
		case SemanticsFID:
			if len(g.Sources) != 2 {
				return nil, fmt.Errorf("%w: FID group of %d", ErrMalformed, len(g.Sources))
			}
			if _, dup := fid[g.Sources[0]]; dup {
				return nil, fmt.Errorf("%w: two FID groups of one source", ErrMalformed)
			}
			fid[g.Sources[0]] = g.Sources[1]
			fidOrder = append(fidOrder, g.Sources[0])
		default:
			return nil, fmt.Errorf("%w: semantics %q", ErrUnsupported, g.Semantics)
		}
	}
	if sim == nil && len(fidOrder) == 0 {
		return nil, nil
	}
	if sim == nil {
		if len(fidOrder) != 1 {
			return nil, fmt.Errorf("%w: %d FID groups without SIM", ErrMalformed, len(fidOrder))
		}
		sim = fidOrder
	}
	if len(fidOrder) > len(sim) {
		return nil, fmt.Errorf("%w: FID group of a source outside SIM", ErrMalformed)
	}
	layers := make([]VideoLayer, 0, len(sim))
	for _, s := range sim {
		rtx, ok := fid[s]
		if !ok && len(fid) > 0 {
			return nil, fmt.Errorf("%w: layer without FID group", ErrMalformed)
		}
		for _, x := range []uint32{s, rtx} {
			if x == 0 {
				continue
			}
			if seen[x] {
				return nil, fmt.Errorf("%w: source repeated", ErrMalformed)
			}
			seen[x] = true
		}
		layers = append(layers, VideoLayer{SSRC: s, RTX: rtx})
	}
	return layers, nil
}

// Sources returns every SSRC of the request: audio, then the video layers
// and their retransmissions.
func (r *JoinRequest) Sources() []uint32 {
	out := []uint32{r.SSRC}
	layers, _ := r.VideoLayers()
	for _, l := range layers {
		out = append(out, l.SSRC)
		if l.RTX != 0 {
			out = append(out, l.RTX)
		}
	}
	return out
}

// MarshalJSON writes the request as tgcalls does, sources as signed 32-bit
// integers. The test client and the tests use it.
func (r *JoinRequest) MarshalJSON() ([]byte, error) {
	type group struct {
		Sources   []int32 `json:"sources"`
		Semantics string  `json:"semantics"`
	}
	w := struct {
		SSRC         int32         `json:"ssrc"`
		Ufrag        string        `json:"ufrag"`
		Pwd          string        `json:"pwd"`
		Fingerprints []Fingerprint `json:"fingerprints"`
		SourceGroups []group       `json:"ssrc-groups,omitempty"`
	}{SSRC: int32(r.SSRC), Ufrag: r.Ufrag, Pwd: r.Pwd, Fingerprints: r.Fingerprints}
	for _, g := range r.SourceGroups {
		out := group{Semantics: g.Semantics}
		for _, s := range g.Sources {
			out.Sources = append(out.Sources, int32(s))
		}
		w.SourceGroups = append(w.SourceGroups, out)
	}
	return json.Marshal(w)
}

// parseSource reads an SSRC written signed (tgcalls) or unsigned (Web). It
// must be a JSON number, as tgcalls requires (json11 is_number).
func parseSource(raw json.RawMessage) (uint32, error) {
	var n json.Number
	if len(raw) == 0 || raw[0] == '"' || json.Unmarshal(raw, &n) != nil {
		return 0, fmt.Errorf("%w: source is not a number", ErrMalformed)
	}
	v, err := n.Int64()
	if err != nil || v < math.MinInt32 || v > math.MaxUint32 {
		return 0, fmt.Errorf("%w: source %q", ErrMalformed, n.String())
	}
	s := uint32(v)
	if s == 0 {
		return 0, fmt.Errorf("%w: zero source", ErrMalformed)
	}
	return s, nil
}

// iceString checks an ICE ufrag or password: min..256 ice-chars
// (ALPHA / DIGIT / "+" / "/", RFC 8445 §5.3, RFC 8839 §5.4).
func iceString(s string, minLen int) bool {
	if len(s) < minLen || len(s) > 256 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '+' || c == '/' {
			continue
		}
		return false
	}
	return true
}

// fingerprintSizes are the digest sizes of the hash functions a DTLS
// fingerprint may use (RFC 8122 §5).
var fingerprintSizes = map[string]int{
	"sha-1": 20, "sha-224": 28, "sha-256": 32, "sha-384": 48, "sha-512": 64,
}

func checkFingerprint(fp Fingerprint) error {
	size, ok := fingerprintSizes[fp.Hash]
	if !ok {
		return fmt.Errorf("%w: fingerprint hash %q", ErrUnsupported, fp.Hash)
	}
	if _, err := DecodeFingerprint(fp.Fingerprint, size); err != nil {
		return err
	}
	switch fp.Setup {
	case SetupPassive, SetupActpass:
	case SetupActive:
		// The SFU is always the DTLS client (ADR 0059 §4); a client that
		// insists on being one cannot connect.
		return fmt.Errorf("%w: setup %q", ErrUnsupported, fp.Setup)
	default:
		return fmt.Errorf("%w: setup %q", ErrMalformed, fp.Setup)
	}
	return nil
}

// DecodeFingerprint decodes "AB:CD:…" (RFC 8122 §5, either case) of size
// bytes.
func DecodeFingerprint(s string, size int) ([]byte, error) {
	if len(s) != size*3-1 {
		return nil, fmt.Errorf("%w: fingerprint length", ErrMalformed)
	}
	out := make([]byte, size)
	for i := 0; i < size; i++ {
		if i > 0 && s[i*3-1] != ':' {
			return nil, fmt.Errorf("%w: fingerprint separator", ErrMalformed)
		}
		hi, ok1 := hexValue(s[i*3])
		lo, ok2 := hexValue(s[i*3+1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("%w: fingerprint digit", ErrMalformed)
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

// EncodeFingerprint writes a digest as "AB:CD:…", the form tgcalls produces
// (rtc::SSLFingerprint::GetRfc4572Fingerprint).
func EncodeFingerprint(digest []byte) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(digest) * 3)
	for i, c := range digest {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&0x0f])
	}
	return b.String()
}

func hexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

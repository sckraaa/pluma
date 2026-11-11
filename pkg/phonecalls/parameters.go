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

package phonecalls

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The quality of a call is set by the server: phoneCall.custom_parameters
// is a JSON object that Android (VoIPService.java:3529) and Telegram-iOS
// (OngoingCallThreadLocalContext.mm:2001-2023) hand to tgcalls as they get
// it, and that tgcalls parses into a map (InstanceV2Impl.cpp:949-956). The
// call library of the Pluma clients reads the keys below; a key that is
// absent keeps the value of the stock library, and the stock clients ignore
// keys they do not know (task 077, research audio.md and video.md).
//
// Profiles: "pluma" sends the values chosen for Pluma, "stock" sends none,
// so that the clients behave as Telegram's. Either can be changed key by key
// to compare one setting at a time.
const (
	ProfilePluma = "pluma"
	ProfileStock = "stock"
)

// parameter is one key of custom_parameters. An integer key takes a whole
// number in [min, max], or one of values when that is set; zero is never
// sent, because getCustomParameterInt of tgcalls reads an absent key as 0
// (audio.md, "phone.phoneCall custom_parameters"). A boolean key is sent
// as a JSON true or false (getCustomParameterBool reads only JSON booleans,
// NativeNetworkingImpl.cpp:33-39).
type parameter struct {
	key      string
	boolean  bool
	min, max int64
	values   []int64
	// pluma is the value of the Pluma profile.
	pluma any
}

// parameters are the keys the server sets. Units: kbit/s are 1000 bit/s.
// The stock value of each is in docs/ops/calls.md with its source.
var parameters = []parameter{
	// Opus packet time. Stock 60 ms (InstanceV2Impl.cpp:173): at about
	// 32 kbit/s a packet is three 20 ms frames and in-band FEC repairs only
	// the last of them; at 20 ms one lost packet is repaired whole and
	// packetization takes 40 ms less (audio.md, "Opus packet time" and
	// "In-band FEC").
	{key: "audio_ptime_ms", values: []int64{10, 20, 40, 60, 120}, pluma: int64(20)},
	// Opus bitrate range. Stock: fixed at 32*1024 bit/s, min = max
	// (InstanceV2Impl.cpp:2162-2165, webrtc_voice_engine.cc:1105-1126), so
	// audio does not step down on a congested link. Pluma: 16-48, below
	// the CELT-only threshold near 61.6 kbit/s where frames lose LBRR, and
	// above the FEC thresholds near 16 (audio.md, "Opus bitrate").
	{key: "audio_min_bitrate_kbps", min: 6, max: 128, pluma: int64(16)},
	{key: "audio_max_bitrate_kbps", min: 6, max: 128, pluma: int64(48)},
	// Floor of the jitter buffer (NetEq minimum delay). Stock 50 ms
	// (InstanceV2Impl.cpp:338-339); NetEq still raises its target under
	// jitter. 20 rather than 0, which is what an absent key reads as
	// (audio.md, "Jitter buffer").
	{key: "audio_jitter_min_ms", min: 1, max: 500, pluma: int64(20)},
	// RNNoise on the microphone, as in group calls
	// (GroupInstanceCustomImpl.cpp:720-731). Stock: off in 1:1, where the
	// engine's or the phone's own suppression runs (audio.md, "Noise
	// suppression").
	{key: "audio_rnnoise", boolean: true, pluma: true},
	// Camera send cap. Stock 1000*1024 bit/s (InstanceV2Impl.cpp:2162-2168);
	// WebRTC's own default for 1280x720 is 2500 kbit/s
	// (encoder_stream_factory.cc:79-96) (video.md, "Camera send cap").
	{key: "video_max_bitrate_kbps", min: 100, max: 20000, pluma: int64(2500)},
	// Screen send cap. Stock: none, WebRTC's default by frame size, 2500
	// above 960x540 (video.md, "Screen-share send cap"). 4000 for 1080p text.
	{key: "screen_max_bitrate_kbps", min: 100, max: 20000, pluma: int64(4000)},
	// Start bitrate of the camera. Stock 300 kbit/s, WebRTC's default
	// (bitrate_settings.h:41-45). Below 500000 bit/s the encoder drops the
	// first 720p frames and steps the resolution down
	// (video_stream_encoder.cc:2334-2365), so 600 (video.md, "Start bitrate").
	{key: "start_bitrate_kbps", min: 30, max: 20000, pluma: int64(600)},
	// Camera frame rate. Stock 30, but 25 on iOS (VideoCaptureInterfaceImpl.cpp
	// :129, :131 at the iOS pin); the encoder's ceiling is 60 (video.md,
	// "Camera capture resolution and fps").
	{key: "video_fps", min: 1, max: 60, pluma: int64(30)},
	// Screen frame rate and the long side of a shared screen in pixels.
	// Stock: tdesktop 1280x720 at 24 fps, halving larger screens; Android
	// about 1000 px at 30 fps; iOS 30 fps, unscaled (video.md, "Screen
	// capture path, resolution and fps per platform").
	{key: "screen_fps", min: 1, max: 60, pluma: int64(30)},
	{key: "screen_max_size_px", min: 320, max: 3840, pluma: int64(1920)},
}

func parameterOf(key string) (parameter, bool) {
	i := slices.IndexFunc(parameters, func(p parameter) bool { return p.key == key })
	if i < 0 {
		return parameter{}, false
	}
	return parameters[i], true
}

// parse reads value as the value of p.
func (p parameter) parse(value string) (any, error) {
	if p.boolean {
		switch value {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("%s: %q is not true or false", p.key, value)
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s: %q is not a whole number", p.key, value)
	}
	if p.values != nil {
		if !slices.Contains(p.values, n) {
			return nil, fmt.Errorf("%s: %d is not one of %v", p.key, n, p.values)
		}
	} else if n < p.min || n > p.max {
		return nil, fmt.Errorf("%s: %d is outside %d..%d", p.key, n, p.min, p.max)
	}
	return n, nil
}

// CustomParameters returns the JSON of phoneCall.custom_parameters for the
// setting quality: a profile, then key=value changes, comma separated, for
// example "pluma", "stock,audio_ptime_ms=20" or "audio_ptime_ms=stock". The
// profile may be left out, and is then "pluma"; the value "stock" removes
// a key, so that the clients keep their own value. "" means no parameters.
func CustomParameters(quality string) (string, error) {
	items := strings.Split(quality, ",")
	for i := range items {
		items[i] = strings.TrimSpace(items[i])
	}
	profile := ProfilePluma
	if !strings.Contains(items[0], "=") {
		if items[0] != "" {
			profile = items[0]
		}
		items = items[1:]
	}
	values := map[string]any{}
	switch profile {
	case ProfilePluma:
		for _, p := range parameters {
			values[p.key] = p.pluma
		}
	case ProfileStock:
	default:
		return "", fmt.Errorf("unknown profile %q: %q or %q", profile, ProfilePluma, ProfileStock)
	}
	seen := map[string]bool{}
	for _, item := range items {
		key, value, ok := strings.Cut(item, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok {
			return "", fmt.Errorf("%q: a change is key=value, and only the first item may name a profile", item)
		}
		p, known := parameterOf(key)
		if !known {
			return "", fmt.Errorf("unknown parameter %q", key)
		}
		if seen[key] {
			return "", fmt.Errorf("%s is set twice", key)
		}
		seen[key] = true
		if value == ProfileStock {
			delete(values, key)
			continue
		}
		v, err := p.parse(value)
		if err != nil {
			return "", err
		}
		values[key] = v
	}
	if lo, hi := values["audio_min_bitrate_kbps"], values["audio_max_bitrate_kbps"]; lo != nil && hi != nil && lo.(int64) > hi.(int64) {
		return "", fmt.Errorf("audio_min_bitrate_kbps %d is above audio_max_bitrate_kbps %d", lo, hi)
	}
	if start, hi := values["start_bitrate_kbps"], values["video_max_bitrate_kbps"]; start != nil && hi != nil && start.(int64) > hi.(int64) {
		return "", fmt.Errorf("start_bitrate_kbps %d is above video_max_bitrate_kbps %d", start, hi)
	}
	if len(values) == 0 {
		return "", nil
	}
	// A map is written with its keys sorted: the same setting gives the
	// same JSON.
	b, err := json.Marshal(values)
	return string(b), err
}

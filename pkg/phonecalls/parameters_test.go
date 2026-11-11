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
	"strconv"
	"strings"
	"testing"
)

// plumaParameters is the JSON the Pluma profile sends; the call library of
// the clients reads exactly these keys, so a change here is a change of the
// contract with them (docs/ops/calls.md).
const plumaParameters = `{"audio_jitter_min_ms":20,"audio_max_bitrate_kbps":48,"audio_min_bitrate_kbps":16,` +
	`"audio_ptime_ms":20,"audio_rnnoise":true,"screen_fps":30,"screen_max_bitrate_kbps":4000,` +
	`"screen_max_size_px":1920,"start_bitrate_kbps":600,"video_fps":30,"video_max_bitrate_kbps":2500}`

func TestCustomParameters(t *testing.T) {
	for _, tc := range []struct {
		quality, want string
	}{
		{"", plumaParameters},
		{"pluma", plumaParameters},
		{" pluma ", plumaParameters},
		{"stock", ""},
		{"stock,audio_ptime_ms=stock", ""},
		// One setting at a time against stock, or stock for one setting.
		{"stock,audio_ptime_ms=20", `{"audio_ptime_ms":20}`},
		{"stock, audio_rnnoise = false", `{"audio_rnnoise":false}`},
		{"audio_ptime_ms=60", strings.Replace(plumaParameters, `"audio_ptime_ms":20`, `"audio_ptime_ms":60`, 1)},
		{"pluma,audio_rnnoise=stock", strings.Replace(plumaParameters, `"audio_rnnoise":true,`, "", 1)},
		{"stock,audio_min_bitrate_kbps=32,audio_max_bitrate_kbps=32", `{"audio_max_bitrate_kbps":32,"audio_min_bitrate_kbps":32}`},
	} {
		got, err := CustomParameters(tc.quality)
		if err != nil || got != tc.want {
			t.Errorf("CustomParameters(%q) = %s, %v; want %s", tc.quality, got, err, tc.want)
		}
	}
}

func TestCustomParametersTypes(t *testing.T) {
	got, err := CustomParameters("")
	if err != nil {
		t.Fatal(err)
	}
	// tgcalls reads integers as JSON numbers and flags as JSON booleans
	// only (getCustomParameterBool): never strings.
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != len(parameters) {
		t.Fatalf("%d keys, want %d", len(m), len(parameters))
	}
	for _, p := range parameters {
		switch v := m[p.key].(type) {
		case bool:
			if !p.boolean {
				t.Errorf("%s: a boolean", p.key)
			}
		case float64:
			if p.boolean || v <= 0 || v != float64(int64(v)) {
				t.Errorf("%s: %v", p.key, v)
			}
			// The value of the profile passes its own check.
			if _, err := p.parse(strconv.FormatInt(int64(v), 10)); err != nil {
				t.Errorf("%s: %v", p.key, err)
			}
		default:
			t.Errorf("%s: %T", p.key, v)
		}
	}
}

func TestCustomParametersErrors(t *testing.T) {
	for _, quality := range []string{
		"telegram",
		"stock,pluma",
		"stock,audio_ptime=20",
		"audio_ptime_ms=30",
		"audio_ptime_ms=0",
		"audio_ptime_ms=twenty",
		"audio_jitter_min_ms=0",
		"audio_max_bitrate_kbps=1000",
		"audio_rnnoise=1",
		"audio_ptime_ms=20,audio_ptime_ms=60",
		"audio_min_bitrate_kbps=64",
		"start_bitrate_kbps=3000",
		"stock,audio_min_bitrate_kbps=48,audio_max_bitrate_kbps=32",
		"pluma,",
	} {
		if got, err := CustomParameters(quality); err == nil {
			t.Errorf("CustomParameters(%q) = %s, want an error", quality, got)
		}
	}
}

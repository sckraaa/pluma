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

package appconfig

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// briefProbe is pluma_probe of the brief of task 113 (section 7) for
// Russia, as the clients read it.
const briefProbe = `{"v":1,"country":"RU","gate_down":65536,"rank_down":196608,"up":20480,
	"stall_ms":1500,"deadline_ms":6000,"suspect_ms":4000,"reprobe_s":21600,
	"rank_min_interval_s":1800,"min_dwell_s":600,"switch_ratio":0.7,"switch_ratio_now":0.25,
	"edge_probe_if_direct_ms":1500,"bad_ttl_s":86400,"failed_ttl_s":300,
	"race_stagger_ms":800,"confirm_s":60}`

func mustParseProbe(t *testing.T, s ProbeSettings) *Probe {
	t.Helper()
	p, err := ParseProbe(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// probeOf is pluma_probe of c as the clients read it.
func probeOf(t *testing.T, c *AppConfig) map[string]any {
	t.Helper()
	m, ok := plainJSON(t, field(t, c, plumaProbeKey)).(map[string]any)
	if !ok {
		t.Fatal("pluma_probe is not an object")
	}
	return m
}

func plainOf(t *testing.T, text string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestProbeDefaults: without variables pluma_probe is the brief's, the
// country of the client in it, and the text the hash covers is the same
// JSON as the value.
func TestProbeDefaults(t *testing.T) {
	p := mustParseProbe(t, nil)
	base := mustParse(t, `{"a": 1}`)
	ru := p.Config(base, "RU")
	if got, want := probeOf(t, ru), plainOf(t, briefProbe); !reflect.DeepEqual(got, want) {
		t.Errorf("pluma_probe of Russia\n got %v\nwant %v", got, want)
	}
	text := string(ru.compact[strings.Index(string(ru.compact), plumaProbeKey+"=")+len(plumaProbeKey)+1:])
	if got := plainOf(t, text); !reflect.DeepEqual(got, probeOf(t, ru)) {
		t.Errorf("the hashed text %s is not the value", text)
	}
	if field(t, ru, "a") == nil {
		t.Error("the rest of the config is lost")
	}
	if p.Config(base, "RU") != ru {
		t.Error("the config of a country is made again for the same base")
	}
	var nilProbe *Probe
	if nilProbe.Config(base, "RU") != base {
		t.Error("nil Probe changed the config")
	}
}

// TestProbeByCountry: every country has its own object and hash; an
// unknown country, or anything that is not a code, is "".
func TestProbeByCountry(t *testing.T) {
	p := mustParseProbe(t, nil)
	base := mustParse(t, `{"a": 1}`)
	ru, de, none := p.Config(base, "RU"), p.Config(base, "DE"), p.Config(base, "")
	for c, cfg := range map[string]*AppConfig{"RU": ru, "DE": de, "": none} {
		if got := probeOf(t, cfg)["country"]; got != c {
			t.Errorf("the config of %q has country %v", c, got)
		}
	}
	if ru.Hash == de.Hash || ru.Hash == none.Hash || de.Hash == none.Hash || base.Hash == none.Hash {
		t.Errorf("hashes RU %d, DE %d, none %d, base %d are not all different", ru.Hash, de.Hash, none.Hash, base.Hash)
	}
	for _, bad := range []string{"ru", "RUS", "R", `"}`} {
		if got := p.Config(base, bad); got != none {
			t.Errorf("%q is not the config of an unknown country", bad)
		}
	}
	// A new base (url_auth_domains changed) makes the configs again.
	other := mustParse(t, `{"a": 2}`)
	if p.Config(other, "RU") == ru {
		t.Error("the config of a new base is the old one")
	}
}

// TestProbeVariables: every PATH_PROBE_* variable sets its field, a ratio
// as a fraction.
func TestProbeVariables(t *testing.T) {
	s := ProbeSettings{}
	want := plainOf(t, briefProbe)
	want["country"] = ""
	for i, f := range probeFields {
		v := f.min + float64(i%2) // within the range, never the default
		if f.ratio {
			v = 0.06 + float64(i)/100
		}
		if v == f.def {
			v = f.max
		}
		s[f.env] = strconv.FormatFloat(v, 'f', -1, 64)
		want[f.json] = v
	}
	// The pairs must still hold: a deadline above the stall, the ratio
	// that switches at once below the other.
	s["PATH_PROBE_STALL_MS"], want["stall_ms"] = "300", float64(300)
	s["PATH_PROBE_DEADLINE_MS"], want["deadline_ms"] = "1001", float64(1001)
	s["PATH_PROBE_SWITCH_RATIO"], want["switch_ratio"] = "0.9", 0.9
	s["PATH_PROBE_SWITCH_RATIO_NOW"], want["switch_ratio_now"] = "0.5", 0.5
	p := mustParseProbe(t, s)
	if got := probeOf(t, p.Config(mustParse(t, `{}`), "")); !reflect.DeepEqual(got, want) {
		t.Errorf("pluma_probe\n got %v\nwant %v", got, want)
	}
	if got := ProbeVariables(); len(got) != len(want)-2 {
		t.Errorf("ProbeVariables has %d names for %d fields", len(got), len(want)-2)
	}
}

func TestParseProbeRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		s    ProbeSettings
		want string
	}{
		"not a number":        {ProbeSettings{"PATH_PROBE_GATE_DOWN": "64k"}, "PATH_PROBE_GATE_DOWN"},
		"a fraction of bytes": {ProbeSettings{"PATH_PROBE_GATE_DOWN": "65536.5"}, "PATH_PROBE_GATE_DOWN"},
		"below the threshold": {ProbeSettings{"PATH_PROBE_GATE_DOWN": "16383"}, "PATH_PROBE_GATE_DOWN"},
		"above the gateway":   {ProbeSettings{"PATH_PROBE_RANK_DOWN": "524289"}, "PATH_PROBE_RANK_DOWN"},
		"upload too large":    {ProbeSettings{"PATH_PROBE_UP": "65537"}, "PATH_PROBE_UP"},
		"negative":            {ProbeSettings{"PATH_PROBE_MIN_DWELL_S": "-1"}, "PATH_PROBE_MIN_DWELL_S"},
		"ratio of one":        {ProbeSettings{"PATH_PROBE_SWITCH_RATIO": "1"}, "PATH_PROBE_SWITCH_RATIO"},
		"ratio NaN":           {ProbeSettings{"PATH_PROBE_SWITCH_RATIO": "NaN"}, "PATH_PROBE_SWITCH_RATIO"},
		"ratio not a number":  {ProbeSettings{"PATH_PROBE_SWITCH_RATIO_NOW": "a quarter"}, "PATH_PROBE_SWITCH_RATIO_NOW"},
		"deadline = stall":    {ProbeSettings{"PATH_PROBE_STALL_MS": "2000", "PATH_PROBE_DEADLINE_MS": "2000"}, "not longer than PATH_PROBE_STALL_MS"},
		"now above ratio":     {ProbeSettings{"PATH_PROBE_SWITCH_RATIO": "0.3", "PATH_PROBE_SWITCH_RATIO_NOW": "0.3"}, "not below PATH_PROBE_SWITCH_RATIO"},
		"unknown variable":    {ProbeSettings{"PATH_PROBE_GATE": "65536"}, "no variable PATH_PROBE_GATE"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseProbe(c.s)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err %v, want one naming %q", err, c.want)
			}
		})
	}
	// Spaces around a value are taken, as in the other variables of bff.
	if _, err := ParseProbe(ProbeSettings{"PATH_PROBE_GATE_DOWN": " 70000 "}); err != nil {
		t.Errorf("a value with spaces: %v", err)
	}
}

func TestProbeConcurrent(t *testing.T) {
	p := mustParseProbe(t, nil)
	base := mustParse(t, `{"a": 1}`)
	var wg sync.WaitGroup
	got := make([]*AppConfig, 32)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = p.Config(base, []string{"RU", "DE"}[i%2])
		}()
	}
	wg.Wait()
	for i := range got {
		if got[i] != got[i%2] {
			t.Fatalf("config %d is not the one of its country", i)
		}
	}
}

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
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/teamgram/proto/mtproto"
)

// plumaProbeKey is Pluma's own key of the app config with the settings of
// the path probes of the patched clients and the country of the client's
// address (task 113, ADR 0073 §7, the brief's section 7):
//
//	{"v":1,"country":"RU","gate_down":65536,"rank_down":196608,"up":20480,…}
//
// The clients key their memory of the best path by the country, so every
// country has an object, and a hash, of its own. A client without the key
// keeps the values built into it; pluma_edge v1 is not changed by it.
const plumaProbeKey = "pluma_probe"

const probeVersion = 1

// ProbeSettings is the PATH_PROBE_* variables of bff by name, as the
// config file gives them; a missing or empty one takes its default.
type ProbeSettings map[string]string

// probeField is a number of pluma_probe: its JSON name, its variable, its
// default and the range bff accepts. A ratio is a decimal fraction, every
// other field a whole number.
type probeField struct {
	json, env     string
	def, min, max float64
	ratio         bool
}

// probeFields are the numbers of pluma_probe in the order of the brief,
// with the defaults of its section 7. The ranges keep a client within what
// the gateway serves (a probe asks for 16-512 KiB and sends at most 64 KiB,
// the brief's section 2) and within what makes sense for a timer; a value
// outside them stops bff, as a wrong EDGE_ROUTES does.
var probeFields = []probeField{
	{json: "gate_down", env: "PATH_PROBE_GATE_DOWN", def: 65536, min: 16384, max: 524288},
	{json: "rank_down", env: "PATH_PROBE_RANK_DOWN", def: 196608, min: 16384, max: 524288},
	{json: "up", env: "PATH_PROBE_UP", def: 20480, min: 0, max: 65536},
	{json: "stall_ms", env: "PATH_PROBE_STALL_MS", def: 1500, min: 300, max: 10000},
	{json: "deadline_ms", env: "PATH_PROBE_DEADLINE_MS", def: 6000, min: 1000, max: 30000},
	{json: "suspect_ms", env: "PATH_PROBE_SUSPECT_MS", def: 4000, min: 1000, max: 30000},
	{json: "reprobe_s", env: "PATH_PROBE_REPROBE_S", def: 21600, min: 600, max: 604800},
	{json: "rank_min_interval_s", env: "PATH_PROBE_RANK_MIN_INTERVAL_S", def: 1800, min: 60, max: 86400},
	{json: "min_dwell_s", env: "PATH_PROBE_MIN_DWELL_S", def: 600, min: 0, max: 86400},
	{json: "switch_ratio", env: "PATH_PROBE_SWITCH_RATIO", def: 0.7, min: 0.05, max: 0.95, ratio: true},
	{json: "switch_ratio_now", env: "PATH_PROBE_SWITCH_RATIO_NOW", def: 0.25, min: 0.05, max: 0.95, ratio: true},
	{json: "edge_probe_if_direct_ms", env: "PATH_PROBE_EDGE_PROBE_IF_DIRECT_MS", def: 1500, min: 100, max: 60000},
	{json: "bad_ttl_s", env: "PATH_PROBE_BAD_TTL_S", def: 86400, min: 60, max: 604800},
	{json: "failed_ttl_s", env: "PATH_PROBE_FAILED_TTL_S", def: 300, min: 10, max: 86400},
	{json: "race_stagger_ms", env: "PATH_PROBE_RACE_STAGGER_MS", def: 800, min: 0, max: 10000},
	{json: "confirm_s", env: "PATH_PROBE_CONFIRM_S", def: 60, min: 10, max: 3600},
}

// ProbeVariables are the names of the PATH_PROBE_* variables, in the order
// of pluma_probe.
func ProbeVariables() []string {
	out := make([]string, len(probeFields))
	for i, f := range probeFields {
		out[i] = f.env
	}
	return out
}

// probeNumber is a number of pluma_probe and its text in the JSON.
type probeNumber struct {
	key   string
	value float64
	text  string
}

// Probe is pluma_probe for each country of the clients. It is safe for
// concurrent use.
type Probe struct {
	numbers []probeNumber // the fields after "country", the same for every country

	mu        sync.Mutex
	base      *AppConfig            // the config the configs below were made of
	byCountry map[string]*AppConfig // base with the pluma_probe of a country
}

// ParseProbe checks the PATH_PROBE_* variables of s and returns
// pluma_probe with them, the defaults of the brief for the empty ones. A
// value is a decimal whole number, or a decimal fraction for the two
// ratios, within its range; besides, the deadline of a probe is longer
// than its stall, and switch_ratio_now, the ratio that switches at once,
// is below switch_ratio, the one that switches after two probes. A name
// s has that is no PATH_PROBE_* variable is an error.
func ParseProbe(s ProbeSettings) (*Probe, error) {
	known := map[string]bool{}
	for _, f := range probeFields {
		known[f.env] = true
	}
	for name := range s {
		if !known[name] {
			return nil, fmt.Errorf("probe: no variable %s", name)
		}
	}
	p := &Probe{}
	got := map[string]float64{}
	for _, f := range probeFields {
		x := f.def
		if v := strings.TrimSpace(s[f.env]); v != "" {
			if f.ratio {
				var err error
				x, err = strconv.ParseFloat(v, 64)
				if err != nil || math.IsNaN(x) || x < f.min || x > f.max {
					return nil, fmt.Errorf("probe: %s %q is not a number from %g to %g", f.env, s[f.env], f.min, f.max)
				}
			} else {
				n, err := strconv.Atoi(v)
				if err != nil || float64(n) < f.min || float64(n) > f.max {
					return nil, fmt.Errorf("probe: %s %q is not a whole number from %g to %g", f.env, s[f.env], f.min, f.max)
				}
				x = float64(n)
			}
		}
		got[f.json] = x
		p.numbers = append(p.numbers, probeNumber{key: f.json, value: x, text: strconv.FormatFloat(x, 'f', -1, 64)})
	}
	if got["deadline_ms"] <= got["stall_ms"] {
		return nil, fmt.Errorf("probe: PATH_PROBE_DEADLINE_MS %g is not longer than PATH_PROBE_STALL_MS %g", got["deadline_ms"], got["stall_ms"])
	}
	if got["switch_ratio_now"] >= got["switch_ratio"] {
		return nil, fmt.Errorf("probe: PATH_PROBE_SWITCH_RATIO_NOW %g is not below PATH_PROBE_SWITCH_RATIO %g", got["switch_ratio_now"], got["switch_ratio"])
	}
	return p, nil
}

// Config returns base with the pluma_probe of country, the ISO 3166-1
// alpha-2 code of the client's address, "" when it is not known (no
// address, no answer of GeoIP, or anything but two upper-case letters).
// Each country has a hash of its own: a client that comes from another
// country takes the new value, and with it the new key of its memory of
// paths, with its next help.getAppConfig. base itself for nil Probe. The
// config of a country is made once for each new base.
func (p *Probe) Config(base *AppConfig, country string) *AppConfig {
	if p == nil {
		return base
	}
	if !validCountry(country) {
		country = ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.base != base {
		p.base, p.byCountry = base, map[string]*AppConfig{}
	}
	if c, ok := p.byCountry[country]; ok {
		return c
	}
	value, text := p.value(country)
	c := base.withValues([]keyValue{{key: plumaProbeKey, value: value, text: text}})
	p.byCountry[country] = c
	return c
}

// value is pluma_probe of country as a JSON object and as compact JSON,
// which the hash covers.
func (p *Probe) value(country string) (*mtproto.JSONValue, string) {
	fields := make([]*mtproto.JSONObjectValue, 0, 2+len(p.numbers))
	field := func(key string, v *mtproto.JSONValue) {
		fields = append(fields, mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{Key: key, Value: v}).To_JSONObjectValue())
	}
	var text strings.Builder
	field("v", jsonNumber(probeVersion))
	field("country", jsonString(country))
	// The country is two letters or nothing, which need no escaping.
	fmt.Fprintf(&text, `{"v":%d,"country":"%s"`, probeVersion, country)
	for _, n := range p.numbers {
		field(n.key, jsonNumber(n.value))
		fmt.Fprintf(&text, `,"%s":%s`, n.key, n.text)
	}
	text.WriteString("}")
	return mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: fields}).To_JSONValue(), text.String()
}

// jsonNumber is x as a JSON value.
func jsonNumber(x float64) *mtproto.JSONValue {
	return mtproto.MakeTLJsonNumber(&mtproto.JSONValue{Value_FLOAT64: x}).To_JSONValue()
}

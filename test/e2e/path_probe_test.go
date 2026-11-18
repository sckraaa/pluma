//go:build e2e

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

package e2e

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The path probes (task 113, ADR 0073 §7): pluma_probe of help.getAppConfig
// with the stand's PATH_PROBE_* and the country of the client's address,
// which the stand's GeoIP database tells (edge_origin_test.go).

// The stand's PATH_PROBE_* (run.sh, gen_edge_env): two set, the rest at
// their defaults.
const (
	stackProbeStallMs     = 1700
	stackProbeSwitchRatio = 0.65
)

// wantStackProbe is pluma_probe of the stand for a client of country ("":
// unknown): the brief's values but for the two the stand sets.
func wantStackProbe(country string) map[string]any {
	return map[string]any{
		"v": float64(1), "country": country,
		"gate_down": float64(65536), "rank_down": float64(196608), "up": float64(20480),
		"stall_ms": float64(stackProbeStallMs), "deadline_ms": float64(6000), "suspect_ms": float64(4000),
		"reprobe_s": float64(21600), "rank_min_interval_s": float64(1800), "min_dwell_s": float64(600),
		"switch_ratio": stackProbeSwitchRatio, "switch_ratio_now": 0.25,
		"edge_probe_if_direct_ms": float64(1500), "bad_ttl_s": float64(86400), "failed_ttl_s": float64(300),
		"race_stagger_ms": float64(800), "confirm_s": float64(60),
	}
}

// countryClients logs in a client of no country (the stand's own network),
// one from Germany and one from Russia, the last two through the origin of
// the Gcore edge, which hands the gateway their addresses.
func countryClients(ctx context.Context, t *testing.T, name string) (direct, germany, russia *user) {
	t.Helper()
	tlsConfig := caddyTLS(t, gcoreEdgeHost)
	phones := testPhones(t, 3)
	direct = login(ctx, t, phones[0], "Direct")
	germany = loginWith(ctx, t, phones[1], "Germany", edgeClientConfig(t, "e2e Germany "+name, func(ctx context.Context) (mtclient.Transport, error) {
		return dialGcoreEdge(ctx, tlsConfig, edgeAuth, "203.0.113.41")
	}))
	russia = loginWith(ctx, t, phones[2], "Russia", edgeClientConfig(t, "e2e Russia "+name, func(ctx context.Context) (mtclient.Transport, error) {
		return dialGcoreEdge(ctx, tlsConfig, edgeAuth, "5.8.16.41")
	}))
	return direct, germany, russia
}

// TestPathProbeAppConfigByCountry: help.getAppConfig hands every client
// pluma_probe with the stand's PATH_PROBE_* and the country of its address,
// and each country has its own hash: a client that moved from Russia to
// Germany with the config of Russia takes the new one, with the new
// country, by its next request.
func TestPathProbeAppConfigByCountry(t *testing.T) {
	needEdges(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	direct, germany, russia := countryClients(ctx, t, "probe")

	hashes := map[int32]string{}
	for country, u := range map[string]*user{"": direct, "DE": germany, "RU": russia} {
		cfg, hash, _ := appConfigHashed(ctx, t, u, 0)
		if got, want := cfg["pluma_probe"], any(wantStackProbe(country)); !reflect.DeepEqual(got, want) {
			t.Errorf("pluma_probe of the client of %q:\n got %v\nwant %v", country, got, want)
		}
		if other, ok := hashes[hash]; ok {
			t.Errorf("the configs of %q and %q have the same hash %d", country, other, hash)
		}
		hashes[hash] = country
	}
	var russiaHash int32
	for h, c := range hashes {
		if c == "RU" {
			russiaHash = h
		}
	}
	cfg, _, changed := appConfigHashed(ctx, t, germany, russiaHash)
	if !changed {
		t.Fatal("a client in Germany with the config of Russia was told it has not changed")
	}
	if got := cfg["pluma_probe"].(map[string]any)["country"]; got != "DE" {
		t.Errorf("the new config names %v", got)
	}
	if _, _, changed := appConfigHashed(ctx, t, russia, russiaHash); changed {
		t.Error("a client in Russia with its own config got it again")
	}
}

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
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/freezeproxy"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The path probe of the gateway (task 113, ADR 0073): pluma.probe on a
// fresh connection is answered with the bytes asked on every entry, and a
// path that freezes after the first 16 KB of a connection, as filtered
// networks do (task 092), is found frozen within the stall time.
//
// Every probe from the host counts against one address (ProbesPerIPMinute,
// 30 a minute): the tests here ask for few. The probes of the Edge entry
// count against the addresses in their header.

// wsURL is the URL of the WebSocket entry of the stand.
func wsURL(t *testing.T) string {
	t.Helper()
	addr := strings.TrimSpace(stackOut(t, "port", "gnetway", "11443"))
	if addr == "" {
		t.Fatal("the WebSocket port of gnetway is not published")
	}
	return "ws://" + addr + "/apiws"
}

// TestPathProbeEntries: the gate probe (64 KiB down, 20 KiB up) is answered
// in full on the TCP, WebSocket and Edge entries, after resPQ as the
// clients that start with the key exchange send it and alone; the measure
// probe of 192 KiB too. The gateway counts what it served by entry.
func TestPathProbeEntries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	served := func(entry string) float64 {
		return gatewayMetric(t, "gnetway_probe_total", `entry="`+entry+`"`, `result="served"`)
	}
	before := map[string]float64{"tcp": served("tcp"), "ws": served("ws"), "edge": served("edge")}

	check := func(name string, res *mtclient.ProbeResult, down int) {
		t.Helper()
		t.Logf("%s: %s", name, res)
		if res.Outcome != mtclient.ProbeOK || res.Received != down {
			t.Fatalf("%s: %s, want ok with %d bytes", name, res, down)
		}
		if res.RTT <= 0 || res.BPS <= 0 {
			t.Fatalf("%s: rtt %s, bps %.0f", name, res.RTT, res.BPS)
		}
	}
	check("tcp after resPQ", mtclient.ProbeTCP(ctx, gatewayAddr, mtclient.ProbeOptions{ResPQ: true}), mtclient.DefaultProbeDown)
	check("tcp measure", mtclient.ProbeTCP(ctx, gatewayAddr, mtclient.ProbeOptions{Down: 192 << 10}), 192<<10)
	check("ws", mtclient.ProbeWebSocket(ctx, wsURL(t), nil, mtclient.ProbeOptions{}), mtclient.DefaultProbeDown)
	check("edge after resPQ", mtclient.ProbeWebSocket(ctx, edgeURL(t), edgeHeader("5.8.16.80"), mtclient.ProbeOptions{ResPQ: true}), mtclient.DefaultProbeDown)

	for entry, want := range map[string]float64{"tcp": 2, "ws": 1, "edge": 1} {
		if got := served(entry) - before[entry]; got < want {
			t.Errorf("gnetway_probe_total{entry=%s,result=served} grew by %v, want %v", entry, got, want)
		}
	}
	if got := gatewayMetric(t, "gnetway_probe_bytes_total", `entry="edge"`, `direction="out"`); got < mtclient.DefaultProbeDown {
		t.Errorf("gnetway_probe_bytes_total{entry=edge,direction=out} = %v", got)
	}
}

// TestPathProbeFrozen: in front of the TCP entry and of the Edge entry, a
// proxy that passes the first 16 KB of each connection and then goes
// silent, as a filtered network does, makes the probe frozen within the
// stall time (1.5 s) of the last byte, under 2 s from the request; the
// same proxy without the limit leaves the path ok.
func TestPathProbeFrozen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	edgeAddr := strings.TrimSuffix(strings.TrimPrefix(edgeURL(t), "ws://"), "/apiws")
	for _, path := range []struct {
		name   string
		target string
		probe  func(addr string) *mtclient.ProbeResult
	}{
		{"tcp", gatewayAddr, func(addr string) *mtclient.ProbeResult {
			return mtclient.ProbeTCP(ctx, addr, mtclient.ProbeOptions{ResPQ: true})
		}},
		{"edge", edgeAddr, func(addr string) *mtclient.ProbeResult {
			return mtclient.ProbeWebSocket(ctx, "ws://"+addr+"/apiws", edgeHeader("5.8.16.81"), mtclient.ProbeOptions{})
		}},
	} {
		frozen, err := freezeproxy.Listen("127.0.0.1:0", freezeproxy.Options{Target: path.target, Down: 16 << 10})
		if err != nil {
			t.Fatal(err)
		}
		res := path.probe(frozen.Addr())
		_ = frozen.Close()
		t.Logf("%s, frozen after 16 KB: %s", path.name, res)
		if res.Outcome != mtclient.ProbeFrozen {
			t.Errorf("%s: a path frozen after 16 KB is %s, want frozen", path.name, res.Outcome)
		} else if res.Elapsed > 2*time.Second {
			t.Errorf("%s: frozen after %s, want within 2 s", path.name, res.Elapsed)
		}
		if n := frozen.Frozen(); n != 1 {
			t.Errorf("%s: %d connections froze in the proxy, want 1", path.name, n)
		}

		clean, err := freezeproxy.Listen("127.0.0.1:0", freezeproxy.Options{Target: path.target})
		if err != nil {
			t.Fatal(err)
		}
		res = path.probe(clean.Addr())
		_ = clean.Close()
		t.Logf("%s, clean: %s", path.name, res)
		if res.Outcome != mtclient.ProbeOK {
			t.Errorf("%s: the clean path is %s, want ok", path.name, res)
		}
	}
}

// TestPathProbeThroughEdges: through Caddy of the Gcore edge's origin (TLS,
// the resource's secret) and of the Russian node's edge (TLS behind the
// node's PROXY header) the probe reaches the Edge entry and is answered in
// full, counted under the edge it came through; a path to the Gcore origin
// that freezes after 16 KB, TLS handshake included, is found frozen.
func TestPathProbeThroughEdges(t *testing.T) {
	needEdges(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	served := func(via string) float64 {
		return gatewayMetric(t, "gnetway_probe_total", `entry="edge"`, `via="`+via+`"`, `result="served"`)
	}
	beforeGcore, beforeRu := served("gcore"), served("ru")

	gcoreTLS := caddyTLS(t, gcoreEdgeHost)
	origin, err := stackPort("web", "443")
	if err != nil {
		t.Fatal(err)
	}
	gcore := func(addr, clientAddr string) *mtclient.ProbeResult {
		return mtclient.ProbeWebSocketVia(ctx, "wss://"+gcoreEdgeHost+"/apiws", gcoreEdgeHeader(edgeAuth, clientAddr), dialTo(addr), gcoreTLS, mtclient.ProbeOptions{ResPQ: true})
	}
	res := gcore(origin, "5.8.16.82")
	t.Logf("gcore: %s", res)
	if res.Outcome != mtclient.ProbeOK || res.Received != mtclient.DefaultProbeDown {
		t.Errorf("through the Gcore origin: %s, want ok", res)
	}

	ruAddr, err := stackPort("web", "8443")
	if err != nil {
		t.Fatal(err)
	}
	ruDial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		conn, err := dialTo(ruAddr)(ctx, network, "")
		if err != nil {
			return nil, err
		}
		if _, err := conn.Write([]byte(proxyV1("5.8.16.83"))); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	}
	res = mtclient.ProbeWebSocketVia(ctx, "wss://"+ruEdgeHost+"/apiws", nil, ruDial, caddyTLS(t, ruEdgeHost), mtclient.ProbeOptions{ResPQ: true, Down: 192 << 10})
	t.Logf("ru: %s", res)
	if res.Outcome != mtclient.ProbeOK || res.Received != 192<<10 {
		t.Errorf("through the Russian node's edge: %s, want ok", res)
	}

	if got := served("gcore") - beforeGcore; got < 1 {
		t.Errorf("gnetway_probe_total{entry=edge,via=gcore,result=served} grew by %v", got)
	}
	if got := served("ru") - beforeRu; got < 1 {
		t.Errorf("gnetway_probe_total{entry=edge,via=ru,result=served} grew by %v", got)
	}

	frozen, err := freezeproxy.Listen("127.0.0.1:0", freezeproxy.Options{Target: origin, Down: 16 << 10})
	if err != nil {
		t.Fatal(err)
	}
	defer frozen.Close()
	res = gcore(frozen.Addr(), "5.8.16.84")
	t.Logf("gcore, frozen after 16 KB: %s", res)
	if res.Outcome != mtclient.ProbeFrozen || res.Elapsed > 2*time.Second {
		t.Errorf("through the Gcore origin frozen after 16 KB: %s, want frozen within 2 s", res)
	}
}

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

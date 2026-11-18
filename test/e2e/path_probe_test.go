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

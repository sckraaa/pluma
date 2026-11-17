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
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"github.com/gobwas/ws"
)

// The Edge entry of the gateway (task 109, ADR 0072): MTProto in a
// WebSocket that the origin proxy behind the CDN passes on with the
// client's address in X-Pluma-Client-Addr. Here the tests stand in for
// that proxy and set the headers themselves.

// edgeURL is the WebSocket URL of the Edge entry of the stand (published on
// an ephemeral loopback port by docker-compose.e2e.yaml).
func edgeURL(t *testing.T) string {
	t.Helper()
	addr := strings.TrimSpace(stackOut(t, "port", "gnetway", "11445"))
	if addr == "" {
		t.Fatal("the Edge port of gnetway is not published")
	}
	return "ws://" + addr + "/apiws"
}

// edgeHeader is what the origin proxy of Gcore adds: the client's address
// list (a forged element first, then the address Gcore saw) and the edge.
func edgeHeader(clientAddr string) http.Header {
	return http.Header{
		"X-Pluma-Client-Addr": {"198.51.100.200, " + clientAddr},
		"X-Pluma-Edge-Via":    {"gcore"},
		"User-Agent":          {"Pluma"},
	}
}

// edgeConfig is config for a client that connects to url with header.
func edgeConfig(t *testing.T, device, url string, header http.Header) mtclient.Config {
	cfg := config(t, device)
	cfg.Dial = func(ctx context.Context) (mtclient.Transport, error) {
		return mtclient.DialWebSocket(ctx, url, header)
	}
	return cfg
}

// gatewayMetric returns the sum of the samples of the gateway's metric
// name whose labels contain all of labels (as `key="value"`).
func gatewayMetric(t *testing.T, name string, labels ...string) float64 {
	t.Helper()
	out := stackOut(t, "exec", "-T", "gnetway", "bash", "-c",
		`exec 3<>/dev/tcp/127.0.0.1/20111 && printf 'GET /metrics HTTP/1.0\r\nHost: localhost\r\n\r\n' >&3 && cat <&3`)
	if !strings.HasPrefix(out, "HTTP/1.") {
		t.Fatalf("the gateway's metrics answered %.200q", out)
	}
	var sum float64
next:
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, name+"{") && !strings.HasPrefix(line, name+" ") {
			continue
		}
		for _, l := range labels {
			if !strings.Contains(line, l) {
				continue next
			}
		}
		f := strings.Fields(line)
		v, err := strconv.ParseFloat(f[len(f)-1], 64)
		if err != nil {
			t.Fatalf("metric line %q: %v", line, err)
		}
		sum += v
	}
	return sum
}

// TestEdgeGatewayMessage: a client logs in through the Edge entry, gets
// pushed messages after updates.getState and sends them; its session shows
// the address from the header (the last one of the list), not the proxy's;
// the entry counts it under its edge.
func TestEdgeGatewayMessage(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	phones := testPhones(t, 2)
	const aliceAddr = "203.0.113.71"
	alice := loginWith(ctx, t, phones[0], "Alice", edgeConfig(t, "e2e Alice edge", edgeURL(t), edgeHeader(aliceAddr)))
	bob := login(ctx, t, phones[1], "Bob")

	current, _ := authorizations(ctx, t, alice)
	if current.GetIp() != aliceAddr {
		t.Errorf("the session through the edge shows address %q, want %s", current.GetIp(), aliceAddr)
	}
	if n := gatewayMetric(t, "gnetway_entry_conns", `entry="edge"`, `via="gcore"`); n < 1 {
		t.Errorf("gnetway_entry_conns{entry=edge,via=gcore} = %v while connected", n)
	}

	getState(ctx, t, alice)
	getState(ctx, t, bob)
	sendMessageID(ctx, t, bob, peerOf(ctx, t, bob, alice), "to the edge", randomID())
	waitNewMessage(ctx, t, alice, "to the edge")
	sendMessageID(ctx, t, alice, peerOf(ctx, t, alice, bob), "from the edge", randomID())
	waitNewMessage(ctx, t, bob, "from the edge")
}

// gcoreProxy is a TCP proxy in front of the Edge entry that behaves as
// Gcore does with a WebSocket: it closes the connection after idle without
// data from the origin (30 s by default; ADR 0072, "Gcore", from
// docs.gcore.com, websockets-allow-permanent-connections-with-the-origin).
type gcoreProxy struct {
	ln   net.Listener
	idle time.Duration
	cut  atomic.Int32 // connections closed for the idle origin
	wg   sync.WaitGroup
}

func startGcoreProxy(t *testing.T, origin string, idle time.Duration) *gcoreProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &gcoreProxy{ln: ln, idle: idle}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				p.serve(client, origin)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		p.wg.Wait()
	})
	return p
}

func (p *gcoreProxy) serve(client net.Conn, origin string) {
	defer client.Close()
	o, err := net.DialTimeout("tcp", origin, 10*time.Second)
	if err != nil {
		return
	}
	defer o.Close()
	go func() {
		_, _ = io.Copy(o, client)
		_ = o.Close()
	}()
	buf := make([]byte, 64<<10)
	for {
		_ = o.SetReadDeadline(time.Now().Add(p.idle))
		n, err := o.Read(buf)
		if n > 0 {
			if _, werr := client.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				p.cut.Add(1)
			}
			return
		}
	}
}

func (p *gcoreProxy) url() string {
	return "ws://" + p.ln.Addr().String() + "/apiws"
}

// TestEdgeGatewayIdleThroughCDN: behind a proxy that drops a connection
// after 30 s without data from the origin, as Gcore does, an idle client
// of the Edge entry stays connected for 70 s, because the gateway pings it
// every 20 s, and then gets a message pushed to it.
func TestEdgeGatewayIdleThroughCDN(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	origin := strings.TrimPrefix(edgeURL(t), "ws://")
	origin = strings.TrimSuffix(origin, "/apiws")
	proxy := startGcoreProxy(t, origin, 30*time.Second)

	phones := testPhones(t, 2)
	alice := loginWith(ctx, t, phones[0], "Alice", edgeConfig(t, "e2e Alice idle edge", proxy.url(), edgeHeader("203.0.113.72")))
	bob := login(ctx, t, phones[1], "Bob")
	peer := peerOf(ctx, t, bob, alice)
	getState(ctx, t, alice)

	pings := gatewayMetric(t, "gnetway_edge_pings_total", `via="gcore"`)
	select {
	case <-time.After(70 * time.Second):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := alice.c.Err(); err != nil {
		t.Fatalf("the idle connection through the proxy stopped: %v", err)
	}
	if n := proxy.cut.Load(); n != 0 {
		t.Fatalf("the proxy cut %d idle connections", n)
	}
	if got := gatewayMetric(t, "gnetway_edge_pings_total", `via="gcore"`) - pings; got < 3 {
		t.Errorf("%v pings in 70 s, want at least 3", got)
	}

	sendMessageID(ctx, t, bob, peer, "after the idle minute", randomID())
	wctx, wcancel := context.WithTimeout(ctx, 30*time.Second)
	defer wcancel()
	waitNewMessage(wctx, t, alice, "after the idle minute")
}

// TestEdgeGatewayWithoutClientAddr: an upgrade request to the Edge entry
// without a client address in X-Pluma-Client-Addr is refused with 400 and
// counted.
func TestEdgeGatewayWithoutClientAddr(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	url := edgeURL(t)

	before := gatewayMetric(t, "gnetway_edge_no_client_addr_total", `via="gcore"`)
	for name, h := range map[string]http.Header{
		"no header": {"X-Pluma-Edge-Via": {"gcore"}, "X-Forwarded-For": {"203.0.113.73"}},
		"garbage":   {"X-Pluma-Edge-Via": {"gcore"}, "X-Pluma-Client-Addr": {"unknown"}},
	} {
		tr, err := mtclient.DialWebSocket(ctx, url, h)
		if err == nil {
			_ = tr.Close()
			t.Errorf("%s: the upgrade went through", name)
			continue
		}
		var status ws.StatusError
		if !errors.As(err, &status) || int(status) != http.StatusBadRequest {
			t.Errorf("%s: %v, want status 400", name, err)
		}
	}
	if got := gatewayMetric(t, "gnetway_edge_no_client_addr_total", `via="gcore"`) - before; got < 2 {
		t.Errorf("gnetway_edge_no_client_addr_total grew by %v, want 2", got)
	}
}

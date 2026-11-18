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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"github.com/gobwas/ws"
)

// The edges of the clients through Caddy (task 109, ADR 0072): the origin of
// the Gcore edge (teamgramd/deploy/web/edge-origin.caddy) and the Russian
// node's edge (ru-edge.caddy) of the stand's web service, in front of the
// gateway's Edge entry; and pluma_edge of help.getAppConfig by the country
// of the client's address, which the stand's GeoIP database tells
// (test/e2e/testdata/geoip: below 128.0.0.0 Russia, above it Germany). The
// tests stand in for Gcore and for the node: they set the headers of the
// resource, or write the node's PROXY v1.

var (
	// edgeOriginAddr and ruEdgeAddr are the loopback addresses of Caddy's
	// 443 and 8443 when the run started; they say the stand has the edges,
	// and the dials read the current ports (stackPort): a recreated web has
	// others.
	edgeOriginAddr = os.Getenv("PLUMA_E2E_EDGE_ORIGIN")
	ruEdgeAddr     = os.Getenv("PLUMA_E2E_RU_EDGE")
	// edgeAuth is X-Pluma-Edge-Auth of the run, which the Gcore resource
	// adds.
	edgeAuth = os.Getenv("PLUMA_E2E_EDGE_AUTH")
	// edgeWSURLs and edgeStickyS are EDGE_WS_URLS and EDGE_STICKY_S of
	// the stand's bff.
	edgeWSURLs  = os.Getenv("PLUMA_E2E_EDGE_WS_URLS")
	edgeStickyS = os.Getenv("PLUMA_E2E_EDGE_STICKY_S")
)

// The names of the edge sites on the stand (run.sh, gen_edge_env).
const (
	gcoreEdgeHost = "edge.localhost"
	ruEdgeHost    = "ru.localhost"
)

func needEdges(t *testing.T) {
	t.Helper()
	if edgeOriginAddr == "" || ruEdgeAddr == "" || edgeAuth == "" || edgeWSURLs == "" || edgeStickyS == "" {
		t.Skip("PLUMA_E2E_EDGE_* are not set (run.sh test sets them)")
	}
}

// caddyTLS is the TLS of a client of host on the stand's Caddy, which
// certifies its *.localhost names with its own CA: its root is read from
// the container, so the certificate is checked as a client checks it.
func caddyTLS(t *testing.T, host string) *tls.Config {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(stackOut(t, "exec", "-T", "web", "cat", "/data/caddy/pki/authorities/local/root.crt"))) {
		t.Fatal("no root certificate of Caddy's CA")
	}
	return &tls.Config{ServerName: host, RootCAs: pool, MinVersion: tls.VersionTLS12}
}

// dialTo is a dialer of the TCP connection to addr, whatever the URL says.
func dialTo(addr string) func(ctx context.Context, network, _ string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
}

// gcoreEdgeHeader is what the Gcore resource sends the origin: its secret,
// and the client's address appended to the X-Forwarded-For the client
// sent (a forged element first). The client's own X-Pluma-* headers are
// forged too: Caddy replaces them.
func gcoreEdgeHeader(auth, clientAddr string) http.Header {
	h := http.Header{
		"X-Forwarded-For":     {"198.51.100.200, " + clientAddr},
		"X-Pluma-Edge-Via":    {"ru"},
		"X-Pluma-Client-Addr": {"198.51.100.201"},
		"User-Agent":          {"Pluma"},
	}
	if auth != "" {
		h.Set("X-Pluma-Edge-Auth", auth)
	}
	return h
}

// dialGcoreEdge opens a WebSocket to the gateway through the origin of the
// Gcore edge, as the resource would for a client at clientAddr; tlsConfig
// is caddyTLS of gcoreEdgeHost.
func dialGcoreEdge(ctx context.Context, tlsConfig *tls.Config, auth, clientAddr string) (mtclient.Transport, error) {
	addr, err := stackPort("web", "443")
	if err != nil {
		return nil, err
	}
	return mtclient.DialWebSocketVia(ctx, "wss://"+gcoreEdgeHost+"/apiws", gcoreEdgeHeader(auth, clientAddr), dialTo(addr), tlsConfig)
}

// proxyV1 is the PROXY v1 header of a TCP connection from src, as the
// node's nginx writes it (proxy_protocol on).
func proxyV1(src string) string {
	family := "TCP4"
	dst := "10.78.0.1"
	if strings.Contains(src, ":") {
		family, dst = "TCP6", "fd00::1"
	}
	return fmt.Sprintf("PROXY %s %s %s 40000 443\r\n", family, src, dst)
}

// dialRuEdge opens a WebSocket to the gateway through the Russian node's
// edge, its TCP connection opened with the PROXY header proxy ("" for
// none), and the client's own X-Pluma-* headers forged; tlsConfig is
// caddyTLS of ruEdgeHost.
func dialRuEdge(ctx context.Context, tlsConfig *tls.Config, proxy string) (mtclient.Transport, error) {
	addr, err := stackPort("web", "8443")
	if err != nil {
		return nil, err
	}
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		conn, err := dialTo(addr)(ctx, network, "")
		if err != nil || proxy == "" {
			return conn, err
		}
		if _, err := conn.Write([]byte(proxy)); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	}
	header := http.Header{
		"X-Pluma-Client-Addr": {"198.51.100.202"},
		"X-Pluma-Edge-Via":    {"gcore"},
		"User-Agent":          {"Pluma"},
	}
	return mtclient.DialWebSocketVia(ctx, "wss://"+ruEdgeHost+"/apiws", header, dial, tlsConfig)
}

// edgeClientConfig is config for a client that connects with dial.
func edgeClientConfig(t *testing.T, device string, dial func(ctx context.Context) (mtclient.Transport, error)) mtclient.Config {
	cfg := config(t, device)
	cfg.Dial = dial
	return cfg
}

// appConfigHashed is u's help.getAppConfig with the config of hash cached
// (0: none): the config as plain JSON, its hash, and whether it changed
// (false for help.appConfigNotModified, with no config).
func appConfigHashed(ctx context.Context, t *testing.T, u *user, hash int32) (map[string]any, int32, bool) {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLHelpGetAppConfig61E3F854{
		Constructor: u.c.Constructor(mtproto.Predicate_help_getAppConfig61E3F854),
		Hash:        hash,
	})
	if err != nil {
		t.Fatalf("help.getAppConfig: %v", err)
	}
	switch r := o.(type) {
	case *mtproto.TLHelpAppConfig:
		m, ok := jsonOf(t, r.GetConfig()).(map[string]any)
		if !ok {
			t.Fatal("the app config is not an object")
		}
		return m, r.GetHash(), true
	case *mtproto.TLHelpAppConfigNotModified:
		return nil, hash, false
	}
	t.Fatalf("help.getAppConfig returned %T", o)
	return nil, 0, false
}

// stackEdges are the edges of EDGE_WS_URLS of the stand by name.
func stackEdges(t *testing.T) map[string]string {
	t.Helper()
	edges := map[string]string{}
	for item := range strings.SplitSeq(edgeWSURLs, ",") {
		name, u, ok := strings.Cut(item, "=")
		if !ok {
			t.Fatalf("EDGE_WS_URLS item %q", item)
		}
		edges[name] = u
	}
	return edges
}

// wantStackEdge is pluma_edge of the record of the stand's EDGE_ROUTES
// (run.sh, gen_edge_env) for a client from Russia, or for any other.
func wantStackEdge(t *testing.T, russia bool) map[string]any {
	t.Helper()
	edges := stackEdges(t)
	sticky, err := strconv.Atoi(edgeStickyS)
	if err != nil {
		t.Fatal(err)
	}
	if russia {
		return map[string]any{"v": float64(1), "ws": []any{edges["ru"], edges["gcore"]}, "order": []any{"edge", "direct"}, "sticky_s": float64(sticky)}
	}
	return map[string]any{"v": float64(1), "ws": []any{edges["gcore"], edges["ru"]}, "order": []any{"direct", "edge"}, "sticky_s": float64(sticky)}
}

// checkEdgeClient: u, connected through an edge from clientAddr in Russia,
// has that address in its session, gets pluma_edge of Russia, and sends
// and gets messages with other.
func checkEdgeClient(ctx context.Context, t *testing.T, u, other *user, clientAddr string) {
	t.Helper()
	checkSessionAddr(ctx, t, u, clientAddr)
	cfg, _, _ := appConfigHashed(ctx, t, u, 0)
	if got, want := cfg["pluma_edge"], any(wantStackEdge(t, true)); !reflect.DeepEqual(got, want) {
		t.Errorf("pluma_edge of a client from Russia: %v, want %v", got, want)
	}

	getState(ctx, t, u)
	getState(ctx, t, other)
	text := "to the edge " + clientAddr
	sendMessageID(ctx, t, other, peerOf(ctx, t, other, u), text, randomID())
	waitNewMessage(ctx, t, u, text)
	text = "from the edge " + clientAddr
	sendMessageID(ctx, t, u, peerOf(ctx, t, u, other), text, randomID())
	waitNewMessage(ctx, t, other, text)
}

// TestEdgeOriginGcore: through the origin of the Gcore edge a client logs
// in, its session has the address Gcore appended to X-Forwarded-For (not
// the forged one, nor Caddy's or the X-Pluma-Client-Addr the client sent),
// the gateway counts it under gcore whatever the client claimed, and it
// works as any client. A request without the resource's secret, or with a
// wrong one, is refused with 403 before it reaches the gateway.
func TestEdgeOriginGcore(t *testing.T) {
	needEdges(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	tlsConfig := caddyTLS(t, gcoreEdgeHost)

	for name, auth := range map[string]string{"no secret": "", "a wrong secret": strings.Repeat("0", len(edgeAuth))} {
		tr, err := dialGcoreEdge(ctx, tlsConfig, auth, "5.8.16.20")
		if err == nil {
			_ = tr.Close()
			t.Errorf("%s: the upgrade went through", name)
			continue
		}
		var status ws.StatusError
		if !errors.As(err, &status) || int(status) != http.StatusForbidden {
			t.Errorf("%s: %v, want status 403", name, err)
		}
	}

	const clientAddr = "5.8.16.21"
	phones := testPhones(t, 2)
	alice := loginWith(ctx, t, phones[0], "Alice", edgeClientConfig(t, "e2e Alice gcore edge", func(ctx context.Context) (mtclient.Transport, error) {
		return dialGcoreEdge(ctx, tlsConfig, edgeAuth, clientAddr)
	}))
	bob := login(ctx, t, phones[1], "Bob")
	if n := gatewayMetric(t, "gnetway_entry_conns", `entry="edge"`, `via="gcore"`); n < 1 {
		t.Errorf("gnetway_entry_conns{entry=edge,via=gcore} = %v while connected", n)
	}
	checkEdgeClient(ctx, t, alice, bob, clientAddr)
}

// TestEdgeOriginRu: through the Russian node's edge a client logs in with
// the address of the node's PROXY header, which its session has (not
// the X-Pluma-Client-Addr the client sent); the gateway counts it under ru;
// it works as any client. A connection without the PROXY header, or one
// whose header names the node's own (private) address, is cut before it
// reaches the gateway.
func TestEdgeOriginRu(t *testing.T) {
	needEdges(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	tlsConfig := caddyTLS(t, ruEdgeHost)

	for name, proxy := range map[string]string{"no PROXY": "", "the node's own address": proxyV1("10.78.0.1")} {
		dctx, dcancel := context.WithTimeout(ctx, 20*time.Second)
		tr, err := dialRuEdge(dctx, tlsConfig, proxy)
		dcancel()
		if err == nil {
			_ = tr.Close()
			t.Errorf("%s: the upgrade went through", name)
		}
	}

	const clientAddr = "5.8.16.22"
	phones := testPhones(t, 2)
	alice := loginWith(ctx, t, phones[0], "Alice", edgeClientConfig(t, "e2e Alice ru edge", func(ctx context.Context) (mtclient.Transport, error) {
		return dialRuEdge(ctx, tlsConfig, proxyV1(clientAddr))
	}))
	bob := login(ctx, t, phones[1], "Bob")
	if n := gatewayMetric(t, "gnetway_entry_conns", `entry="edge"`, `via="ru"`); n < 1 {
		t.Errorf("gnetway_entry_conns{entry=edge,via=ru} = %v while connected", n)
	}
	checkEdgeClient(ctx, t, alice, bob, clientAddr)
}

// TestEdgeAppConfigByCountry: pluma_edge of help.getAppConfig follows the
// country of the client's address. A client from Germany and one whose
// address has no country (the stand's own network) get the record *, the
// same config but for the country of pluma_probe (task 113); a client from
// Russia the record RU with a hash of its own, so a client that comes to
// Russia with the other config takes the new one, and one with the config
// of Russia is told it has not changed.
func TestEdgeAppConfigByCountry(t *testing.T) {
	needEdges(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	tlsConfig := caddyTLS(t, gcoreEdgeHost)
	phones := testPhones(t, 3)
	direct := login(ctx, t, phones[0], "Direct")
	germany := loginWith(ctx, t, phones[1], "Germany", edgeClientConfig(t, "e2e Germany gcore edge", func(ctx context.Context) (mtclient.Transport, error) {
		return dialGcoreEdge(ctx, tlsConfig, edgeAuth, "203.0.113.31")
	}))
	russia := loginWith(ctx, t, phones[2], "Russia", edgeClientConfig(t, "e2e Russia gcore edge", func(ctx context.Context) (mtclient.Transport, error) {
		return dialGcoreEdge(ctx, tlsConfig, edgeAuth, "5.8.16.23")
	}))

	directCfg, directHash, _ := appConfigHashed(ctx, t, direct, 0)
	germanyCfg, germanyHash, _ := appConfigHashed(ctx, t, germany, 0)
	russiaCfg, russiaHash, _ := appConfigHashed(ctx, t, russia, 0)
	for name, c := range map[string]map[string]any{"direct": directCfg, "Germany": germanyCfg} {
		if got, want := c["pluma_edge"], any(wantStackEdge(t, false)); !reflect.DeepEqual(got, want) {
			t.Errorf("pluma_edge of the client %s: %v, want %v", name, got, want)
		}
	}
	delete(directCfg, "pluma_probe")
	delete(germanyCfg, "pluma_probe")
	if !reflect.DeepEqual(directCfg, germanyCfg) {
		t.Errorf("the direct client and the one from Germany got different configs but for pluma_probe (hashes %d, %d)", directHash, germanyHash)
	}
	if got, want := russiaCfg["pluma_edge"], any(wantStackEdge(t, true)); !reflect.DeepEqual(got, want) {
		t.Errorf("pluma_edge of the client from Russia: %v, want %v", got, want)
	}
	if russiaHash == directHash {
		t.Errorf("the configs of Russia and of every other country have the same hash %d", russiaHash)
	}
	if _, hash, changed := appConfigHashed(ctx, t, russia, directHash); !changed || hash != russiaHash {
		t.Errorf("a client from Russia with the config of another country: changed %v, hash %d, want %d", changed, hash, russiaHash)
	}
	if _, _, changed := appConfigHashed(ctx, t, russia, russiaHash); changed {
		t.Error("a client from Russia with its own config got it again")
	}
}

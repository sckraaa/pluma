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
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The mirrors of the site and of the login on sites through the Russian
// node (task 113): Caddy's sites on :8443 behind the node's PROXY v1, as
// the edge (edge_origin_test.go). The names are the stand's (run.sh,
// gen_edge_env, and the site's PLUMA_SITE_DOMAINS).
const (
	ruSiteHost  = "rusite.localhost"
	ruOAuthHost = "ruoauth.localhost"
	siteHost    = "site.localhost"
)

// ruMirrorClient is an HTTPS client of host on Caddy's :8443, each of its
// connections opened with the PROXY header proxy, as the node's nginx
// opens them ("" for none).
func ruMirrorClient(t *testing.T, host, proxy string) *http.Client {
	t.Helper()
	addr, err := stackPort("web", "8443")
	if err != nil {
		t.Fatal(err)
	}
	return proxyClient(t, host, addr, proxy)
}

func proxyClient(t *testing.T, host, addr, proxy string) *http.Client {
	t.Helper()
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			conn, err := dialTo(addr)(ctx, network, "")
			if err != nil || proxy == "" {
				return conn, err
			}
			if _, err := conn.Write([]byte(proxy)); err != nil {
				_ = conn.Close()
				return nil, err
			}
			return conn, nil
		},
		TLSClientConfig:   caddyTLS(t, host),
		ForceAttemptHTTP2: true,
		DisableKeepAlives: true,
	}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{
		Transport: tr,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// getBody is the status and the body of GET url with c.
func getBody(t *testing.T, c *http.Client, url string, header http.Header) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

// TestRuMirrorSites: the mirror of the site through the Russian node serves
// what the site serves (the progress of the site, a file of the site's own
// content), and the mirror of the login on sites is the provider of links,
// with the issuer it has on its own name. A connection of the node without
// the PROXY header gets no answer from either.
func TestRuMirrorSites(t *testing.T) {
	needEdges(t)
	t.Parallel()

	siteAddr, err := stackPort("web", "443")
	if err != nil {
		t.Fatal(err)
	}
	site := proxyClient(t, siteHost, siteAddr, "")
	mirror := ruMirrorClient(t, ruSiteHost, proxyV1("5.8.16.42"))
	code, want := getBody(t, site, "https://"+siteHost+"/progress.json", nil)
	if code != http.StatusOK || !json.Valid(want) {
		t.Fatalf("the site's progress.json: %d %.100s", code, want)
	}
	if code, got := getBody(t, mirror, "https://"+ruSiteHost+"/progress.json", nil); code != http.StatusOK || string(got) != string(want) {
		t.Errorf("the mirror's progress.json: %d, the same as the site's: %v", code, string(got) == string(want))
	}
	// /dl/ is the releases service on both (none on the stand: Caddy's 502).
	for name, c := range map[string]struct {
		client *http.Client
		host   string
	}{"site": {site, siteHost}, "mirror": {mirror, ruSiteHost}} {
		if code, _ := getBody(t, c.client, "https://"+c.host+"/dl/releases.json", nil); code != http.StatusBadGateway {
			t.Errorf("%s: /dl/releases.json %d, want the 502 of the absent releases service", name, code)
		}
	}

	oauth := ruMirrorClient(t, ruOAuthHost, proxyV1("5.8.16.43"))
	code, body := getBody(t, oauth, "https://"+ruOAuthHost+"/.well-known/openid-configuration", nil)
	var doc struct {
		Issuer string `json:"issuer"`
	}
	if code != http.StatusOK || json.Unmarshal(body, &doc) != nil || !strings.HasPrefix(doc.Issuer, "https://") {
		t.Fatalf("the mirror's provider document: %d %.200s", code, body)
	}
	if linksAddr != "" {
		// links itself on the provider's own name answers the same.
		direct := &http.Client{Timeout: 30 * time.Second}
		req, err := http.NewRequest(http.MethodGet, "http://"+linksAddr+"/.well-known/openid-configuration", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = strings.TrimPrefix(doc.Issuer, "https://")
		resp, err := direct.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		own, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || string(own) != string(body) {
			t.Errorf("links on %s: %d, the same document: %v", req.Host, resp.StatusCode, string(own) == string(body))
		}
	}

	for _, host := range []string{ruSiteHost, ruOAuthHost} {
		c := ruMirrorClient(t, host, "")
		req, err := http.NewRequest(http.MethodGet, "https://"+host+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp, err := c.Do(req); err == nil {
			resp.Body.Close()
			t.Errorf("%s without PROXY: %d", host, resp.StatusCode)
		}
	}
}

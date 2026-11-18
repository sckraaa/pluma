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

package cdn

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCDN stands for a CDN in front of the origin: it checks the Secure
// Token of Gcore (unless off), drops the query and pulls from the origin
// with the auth header, and can misbehave the ways a check must notice.
type fakeCDN struct {
	origin http.Handler
	auth   string
	key    string // the Secure Token key; empty for path-hmac, checked by the origin

	mu   sync.Mutex
	mode string
}

func (c *fakeCDN) setMode(m string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = m
}

func (c *fakeCDN) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	mode := c.mode
	c.mu.Unlock()
	switch mode {
	case "down":
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	case "slow":
		time.Sleep(300 * time.Millisecond)
	case "redirect":
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
		return
	}
	if c.key != "" && mode != "notoken" {
		q := r.URL.Query()
		if q.Get("md5") != gcoreToken(q.Get("expires"), r.URL.Path, c.key) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	o := r.Clone(r.Context())
	o.URL.RawQuery = ""
	o.Header.Set(OriginAuthHeader, c.auth)
	if mode == "full200" {
		o.Header.Del("Range")
	}
	rec := httptest.NewRecorder()
	c.origin.ServeHTTP(rec, o)
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	body := rec.Body.Bytes()
	switch mode {
	case "badrange":
		w.Header().Set("Content-Range", "bytes 131088-135183/262144")
	case "tamper":
		if len(body) > 0 {
			body[len(body)/2] ^= 1
		}
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(body)
}

// healthEnv is a Health over a MemStore checking one delivery through a
// fakeCDN.
type healthEnv struct {
	t      *testing.T
	ctx    context.Context
	set    *Settings
	store  *MemStore
	cdn    *fakeCDN
	health *Health
	clock  *clock
}

func newHealthEnv(t *testing.T, signer string, tlsServer bool) *healthEnv {
	t.Helper()
	cdn := &fakeCDN{}
	var srv *httptest.Server
	if tlsServer {
		srv = httptest.NewTLSServer(cdn)
	} else {
		srv = httptest.NewServer(cdn)
	}
	t.Cleanup(srv.Close)
	c := testConfig(t)
	c.Deliveries[0].BaseURL = srv.URL
	c.DevAllowHTTP = "1"
	if signer == SignerPathHMAC {
		c.Deliveries[0].Signer = SignerPathHMAC
		c.Deliveries[0].Key = base64.StdEncoding.EncodeToString(randomBytes(t, PathHMACKeySize))
	} else {
		cdn.key = c.Deliveries[0].Key
	}
	set, err := c.Parse()
	if err != nil {
		t.Fatal(err)
	}
	e := &healthEnv{t: t, ctx: context.Background(), set: set, store: NewMemStore(), cdn: cdn, clock: newClock()}
	origin, err := NewOrigin(e.store, set, newMemReader(), &usageCounts{})
	if err != nil {
		t.Fatal(err)
	}
	cdn.origin, cdn.auth = origin, string(set.Origin.AuthValue)
	e.health = NewHealth(e.store, set)
	e.health.now = time.Now // the signatures are checked by the real time
	return e
}

// round checks the delivery and returns the row it wrote.
func (e *healthEnv) round() *DeliveryHealth {
	e.t.Helper()
	before := time.Now().UnixMilli()
	e.health.Round(e.ctx)
	h, err := e.store.DeliveryHealth(e.ctx, testDc)
	if err != nil || h == nil {
		e.t.Fatalf("DeliveryHealth: %v, %v", h, err)
	}
	if h.UpdatedAt < before {
		e.t.Fatalf("the row was not written by this round")
	}
	return h
}

func TestHealthHealthy(t *testing.T) {
	for _, signer := range []string{SignerGcoreMD5, SignerPathHMAC} {
		e := newHealthEnv(t, signer, false)
		h := e.round()
		if h.UnhealthyReason != "" || h.HealthyUntil != h.UpdatedAt+3*e.set.HealthEvery.Milliseconds() {
			t.Errorf("%s: %+v, want healthy for 3 checks", signer, h)
		}
	}
}

func TestHealthFailures(t *testing.T) {
	cases := []struct {
		mode, reason string
	}{
		{"down", UnhealthyStatus},
		{"redirect", UnhealthyStatus},
		{"full200", UnhealthyStatus},
		{"badrange", UnhealthyRange},
		{"tamper", UnhealthyBytes},
		{"notoken", UnhealthyTokenOff},
		{"slow", UnhealthyTimeout},
	}
	for _, c := range cases {
		e := newHealthEnv(t, SignerGcoreMD5, false)
		for _, c := range e.health.clients {
			c.Timeout = 100 * time.Millisecond
		}
		e.cdn.setMode(c.mode)
		h := e.round()
		if h.UnhealthyReason != c.reason || h.HealthyUntil != 0 {
			t.Errorf("%s: %+v, want out with %q", c.mode, h, c.reason)
		}
		// It comes back at the next check that passes.
		e.cdn.setMode("")
		if h = e.round(); h.UnhealthyReason != "" || h.HealthyUntil == 0 {
			t.Errorf("%s, then fixed: %+v", c.mode, h)
		}
	}
}

func TestHealthTLS(t *testing.T) {
	e := newHealthEnv(t, SignerGcoreMD5, true)
	if h := e.round(); h.UnhealthyReason != UnhealthyTLS {
		t.Errorf("an untrusted certificate: %+v, want tls", h)
	}
}

func TestHealthNetwork(t *testing.T) {
	e := newHealthEnv(t, SignerGcoreMD5, false)
	// Nothing listens at the address any more.
	u, _ := url.Parse(e.set.Deliveries[0].BaseURL)
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.Listener.Addr().String()
	srv.Close()
	signer, err := NewGcoreSigner("http://"+addr, e.cdn.key, e.set.URLTTL)
	if err != nil {
		t.Fatal(err)
	}
	e.set.Deliveries[0].Signer = signer
	if h := e.round(); h.UnhealthyReason != UnhealthyNetwork {
		t.Errorf("%s gone: %+v, want network", u.Host, h)
	}
}

func TestHealthStoreDown(t *testing.T) {
	e := newHealthEnv(t, SignerGcoreMD5, false)
	e.store.SetErr(context.DeadlineExceeded)
	e.health.Round(e.ctx) // logged, not fatal
	e.store.SetErr(nil)
	if h, _ := e.store.DeliveryHealth(e.ctx, testDc); h != nil {
		t.Errorf("a row written with the store down: %+v", h)
	}
}

func TestHealthStopping(t *testing.T) {
	e := newHealthEnv(t, SignerGcoreMD5, false)
	ctx, cancel := context.WithCancel(e.ctx)
	cancel()
	e.health.Round(ctx)
	if h, _ := e.store.DeliveryHealth(e.ctx, testDc); h != nil {
		t.Errorf("a check cut short by the stop was recorded: %+v", h)
	}
}

func TestBreakSignature(t *testing.T) {
	set := testSettings(t)
	now := time.Now()
	signed, _, err := set.Deliveries[0].Signer.SignURL(CanaryPath, now)
	if err != nil {
		t.Fatal(err)
	}
	broken, err := breakSignature(SignerGcoreMD5, signed)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := url.Parse(signed)
	b, _ := url.Parse(broken)
	if a.Path != b.Path || a.Query().Get("expires") != b.Query().Get("expires") {
		t.Errorf("more than the signature changed: %s", b.Path)
	}
	ma, mb := a.Query().Get("md5"), b.Query().Get("md5")
	if len(ma) != len(mb) || ma[:len(ma)-1] != mb[:len(mb)-1] || ma == mb {
		t.Errorf("md5 %q became %q, want only the last character changed", ma, mb)
	}
	// Decoded, the 128 bits differ too: the character changed in its
	// significant bits.
	da, err1 := base64.RawURLEncoding.DecodeString(ma)
	db, err2 := base64.RawURLEncoding.DecodeString(mb)
	if err1 != nil || err2 != nil || bytes.Equal(da, db) {
		t.Errorf("the decoded md5 did not change: %s, %s, %v %v", hex.EncodeToString(da), hex.EncodeToString(db), err1, err2)
	}
	if !strings.HasPrefix(b.RawQuery, "md5=") {
		t.Errorf("the order of the query changed: %s", b.RawQuery)
	}

	ps := pathHMACSettings(t)
	signed, _, _ = ps.Deliveries[0].Signer.SignURL(CanaryPath, now)
	if broken, err = breakSignature(SignerPathHMAC, signed); err != nil {
		t.Fatal(err)
	}
	pb, _ := url.Parse(broken)
	if _, _, err := ps.Deliveries[0].Signer.(*PathHMACSigner).Verify(pb.Path, now); err == nil {
		t.Error("the broken path-hmac signature verifies")
	}
	for _, s := range []string{"%zz", "https://h/c/v1", "https://h/c/v1?md5="} {
		if _, err := breakSignature(SignerGcoreMD5, s); err == nil {
			t.Errorf("broke %q", s)
		}
	}
}

// TestHealthAddr: a delivery with HealthAddr is checked at that address,
// with the name of its BaseURL as the SNI, the Host and the name of the
// certificate (delivery 202 through the tunnel to its node, task 109).
func TestHealthAddr(t *testing.T) {
	cdn := &fakeCDN{}
	var mu sync.Mutex
	var snis, hosts []string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hosts = append(hosts, r.Host)
		mu.Unlock()
		cdn.ServeHTTP(w, r)
	}))
	srv.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		snis = append(snis, hello.ServerName)
		mu.Unlock()
		return nil, nil
	}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())

	// The certificate of httptest is for example.com, which the test never
	// resolves: only HealthAddr is dialled.
	c := testConfig(t)
	c.Deliveries[0].BaseURL = "https://example.com"
	c.Deliveries[0].HealthAddr = srv.Listener.Addr().String()
	cdn.key = c.Deliveries[0].Key
	set, err := c.Parse()
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemStore()
	origin, err := NewOrigin(store, set, newMemReader(), &usageCounts{})
	if err != nil {
		t.Fatal(err)
	}
	cdn.origin, cdn.auth = origin, string(set.Origin.AuthValue)
	h := newHealth(store, set, roots)
	h.Round(context.Background())
	row, err := store.DeliveryHealth(context.Background(), testDc)
	if err != nil || row == nil || row.UnhealthyReason != "" {
		t.Fatalf("checked at HealthAddr: %+v, %v", row, err)
	}
	mu.Lock()
	if len(hosts) != 2 || hosts[0] != "example.com" || hosts[1] != "example.com" || len(snis) == 0 || snis[0] != "example.com" {
		t.Errorf("Host %v, SNI %v, want example.com", hosts, snis)
	}
	mu.Unlock()

	// The certificate is checked for the name of BaseURL: another name at
	// the same address is refused (that of httptest is for example.com and
	// *.example.com).
	c.Deliveries[0].BaseURL = "https://r1.example.org"
	if set, err = c.Parse(); err != nil {
		t.Fatal(err)
	}
	h = newHealth(store, set, roots)
	h.Round(context.Background())
	if row, _ = store.DeliveryHealth(context.Background(), testDc); row == nil || row.UnhealthyReason != UnhealthyTLS {
		t.Errorf("a certificate of another name: %+v, want tls", row)
	}
}

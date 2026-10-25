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

package pushprovider

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func init() { firstPause = time.Millisecond }

func writePEM(t *testing.T, key any) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "key.pem")
	if err = os.WriteFile(f, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

type apnsRequest struct {
	path   string
	header http.Header
	body   string
	proto  int
}

// fakeAPNs answers by the token: ok, gone, bad, auth, expired-once,
// throttle-once, busy, big.
type fakeAPNs struct {
	mu       sync.Mutex
	requests []apnsRequest
	seen     map[string]int
}

func (f *fakeAPNs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, apnsRequest{path: r.URL.Path, header: r.Header.Clone(), body: string(body), proto: r.ProtoMajor})
	token := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	f.seen[token]++
	n := f.seen[token]
	f.mu.Unlock()
	reply := func(status int, reason string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"reason": reason})
	}
	switch {
	case token == "gone":
		reply(http.StatusGone, "Unregistered")
	case token == "bad":
		reply(http.StatusBadRequest, "BadDeviceToken")
	case token == "auth":
		reply(http.StatusForbidden, "InvalidProviderToken")
	case token == "expired-once" && n == 1:
		reply(http.StatusForbidden, "ExpiredProviderToken")
	case token == "throttle-once" && n == 1:
		reply(http.StatusTooManyRequests, "TooManyRequests")
	case token == "busy":
		reply(http.StatusServiceUnavailable, "ServiceUnavailable")
	case token == "big":
		reply(http.StatusRequestEntityTooLarge, "PayloadTooLarge")
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func (f *fakeAPNs) last() apnsRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func (f *fakeAPNs) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func newTestAPNs(t *testing.T) (*APNs, *fakeAPNs, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeAPNs{seen: map[string]int{}}
	srv := httptest.NewUnstartedServer(fake)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	a, err := NewAPNs(APNsConfig{
		KeyFile: writePEM(t, key), KeyId: "KEYID12345", TeamId: "TEAMID1234", Topic: "chat.pluma.app",
		ProductionURL: srv.URL, SandboxURL: srv.URL + "/sandbox",
	}, srv.Client())
	if err != nil || a == nil {
		t.Fatalf("NewAPNs: %v", err)
	}
	return a, fake, key
}

// checkJWT verifies the provider token of a request with the public key.
func checkJWT(t *testing.T, auth string, pub *ecdsa.PublicKey) (header, claims map[string]any) {
	t.Helper()
	token, ok := strings.CutPrefix(auth, "bearer ")
	parts := strings.Split(token, ".")
	if !ok || len(parts) != 3 {
		t.Fatalf("authorization %q", auth)
	}
	for i, v := range []*map[string]any{&header, &claims} {
		b, err := base64.RawURLEncoding.DecodeString(parts[i])
		if err != nil || json.Unmarshal(b, v) != nil {
			t.Fatalf("JWT part %d: %v", i, err)
		}
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		t.Fatalf("JWT signature: %d bytes, %v", len(sig), err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("the JWT signature does not verify")
	}
	return header, claims
}

func TestAPNsSend(t *testing.T) {
	a, fake, key := newTestAPNs(t)
	ctx := context.Background()
	exp := time.Unix(1_900_000_000, 0)
	out, err := a.Send(ctx, &APNsPush{Token: "ab12", Type: APNsAlert, Body: []byte(`{"p":"x"}`), Expiration: exp})
	if out != OutcomeSent || err != nil {
		t.Fatalf("Send: %s %v", out, err)
	}
	r := fake.last()
	if r.proto != 2 || r.path != "/3/device/ab12" || r.body != `{"p":"x"}` ||
		r.header.Get("apns-topic") != "chat.pluma.app" || r.header.Get("apns-push-type") != "alert" ||
		r.header.Get("apns-priority") != "10" || r.header.Get("apns-expiration") != "1900000000" {
		t.Fatalf("request: %+v", r)
	}
	header, claims := checkJWT(t, r.header.Get("authorization"), &key.PublicKey)
	if header["alg"] != "ES256" || header["kid"] != "KEYID12345" || claims["iss"] != "TEAMID1234" || claims["iat"] == nil {
		t.Fatalf("JWT: %v %v", header, claims)
	}
	first := r.header.Get("authorization")

	// Background to the sandbox; VoIP to the .voip topic. The provider
	// token is reused within 50 minutes.
	if out, _ = a.Send(ctx, &APNsPush{Token: "cd34", Sandbox: true, Type: APNsBackground, Body: []byte(`{}`)}); out != OutcomeSent {
		t.Fatal(out)
	}
	if r = fake.last(); r.path != "/sandbox/3/device/cd34" || r.header.Get("apns-priority") != "5" || r.header.Get("apns-push-type") != "background" ||
		r.header.Get("authorization") != first {
		t.Fatalf("background request: %+v", r)
	}
	if out, _ = a.Send(ctx, &APNsPush{Token: "ef56", Type: APNsVoIP, Body: []byte(`{}`)}); out != OutcomeSent || fake.last().header.Get("apns-topic") != "chat.pluma.app.voip" {
		t.Fatalf("voip: %s %+v", out, fake.last().header)
	}
	a.now = func() time.Time { return time.Now().Add(51 * time.Minute) }
	if _, _ = a.Send(ctx, &APNsPush{Token: "ab12", Type: APNsAlert, Body: []byte(`{}`)}); fake.last().header.Get("authorization") == first {
		t.Fatal("the provider token is not renewed after 50 minutes")
	}
}

func TestAPNsOutcomes(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		token    string
		want     Outcome
		requests int
	}{
		{"gone", OutcomeTokenInvalid, 1},
		{"bad", OutcomeTokenInvalid, 1},
		{"auth", OutcomeAuthFailed, 1},
		{"expired-once", OutcomeSent, 2},
		{"throttle-once", OutcomeSent, 2},
		{"busy", OutcomeUnavailable, attempts},
		{"big", OutcomeRejected, 1},
	} {
		a, fake, _ := newTestAPNs(t)
		out, err := a.Send(ctx, &APNsPush{Token: tc.token, Type: APNsAlert, Body: []byte(`{}`)})
		if out != tc.want || fake.count() != tc.requests {
			t.Errorf("%s: %s after %d requests (%v), want %s after %d", tc.token, out, fake.count(), err, tc.want, tc.requests)
		}
		var e *APNsError
		if tc.want != OutcomeSent && !errors.As(err, &e) {
			t.Errorf("%s: error %v is not an APNsError", tc.token, err)
		}
	}
	// A renewed token after ExpiredProviderToken.
	a, fake, _ := newTestAPNs(t)
	_, _ = a.Send(ctx, &APNsPush{Token: "expired-once", Type: APNsAlert, Body: []byte(`{}`)})
	if fake.requests[0].header.Get("authorization") == fake.requests[1].header.Get("authorization") {
		t.Fatal("the same provider token after ExpiredProviderToken")
	}
}

func TestAPNsBreaker(t *testing.T) {
	a, fake, _ := newTestAPNs(t)
	ctx := context.Background()
	for i := 0; i < breakerFails; i++ {
		if out, _ := a.Send(ctx, &APNsPush{Token: "busy", Type: APNsAlert, Body: []byte(`{}`)}); out != OutcomeUnavailable {
			t.Fatal(out)
		}
	}
	n := fake.count()
	out, err := a.Send(ctx, &APNsPush{Token: "ok", Type: APNsAlert, Body: []byte(`{}`)})
	if out != OutcomeUnavailable || !errors.Is(err, errBreakerOpen) || fake.count() != n {
		t.Fatalf("open breaker: %s %v, %d requests", out, err, fake.count()-n)
	}
	a.breaker.now = func() time.Time { return time.Now().Add(breakerOpenFor + time.Second) }
	if out, _ = a.Send(ctx, &APNsPush{Token: "ok", Type: APNsAlert, Body: []byte(`{}`)}); out != OutcomeSent {
		t.Fatalf("after the pause: %s", out)
	}
	// One success closes it.
	a.breaker.now = time.Now
	if out, _ = a.Send(ctx, &APNsPush{Token: "ok", Type: APNsAlert, Body: []byte(`{}`)}); out != OutcomeSent {
		t.Fatalf("closed again: %s", out)
	}
}

func TestAPNsOff(t *testing.T) {
	if a, err := NewAPNs(APNsConfig{}, nil); a != nil || err != nil {
		t.Fatalf("without a key: %v %v", a, err)
	}
	empty := filepath.Join(t.TempDir(), "empty.p8")
	_ = os.WriteFile(empty, nil, 0o600)
	if a, err := NewAPNs(APNsConfig{KeyFile: empty}, nil); a != nil || err != nil {
		t.Fatalf("with an empty key file: %v %v", a, err)
	}
	var off *APNs
	if out, err := off.Send(context.Background(), &APNsPush{}); out != OutcomeDisabled || err != nil {
		t.Fatalf("Send when off: %s %v", out, err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if _, err := NewAPNs(APNsConfig{KeyFile: writePEM(t, key)}, nil); err == nil {
		t.Fatal("a key without KeyId, TeamId and Topic is accepted")
	}
	if _, err := NewAPNs(APNsConfig{KeyFile: filepath.Join(t.TempDir(), "missing.p8")}, nil); err == nil {
		t.Fatal("a missing key file is accepted")
	}
}

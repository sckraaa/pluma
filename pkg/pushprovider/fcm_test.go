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

package provider

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeFCM is the token endpoint and the send endpoint of FCM. It answers
// by the device token: ok, unregistered, badtoken, badpayload, mismatch,
// unauth-once, forbidden, unavailable.
type fakeFCM struct {
	t      *testing.T
	pub    *rsa.PublicKey
	aud    string
	mu     sync.Mutex
	tokens int
	sends  []fcmRequest
	seen   map[string]int
}

func (f *fakeFCM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/token":
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		claims := f.verifyAssertion(r.Form.Get("assertion"))
		if claims["iss"] != "push@pluma-test.iam.gserviceaccount.com" || claims["scope"] != fcmScope || claims["aud"] != f.aud {
			f.t.Errorf("assertion claims: %v", claims)
		}
		f.tokens++
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("at-%d", f.tokens), "expires_in": 3600})
	case "/v1/projects/pluma-test/messages:send":
		var req fcmRequest
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.sends = append(f.sends, req)
		token := req.Message.Token
		f.seen[token]++
		if r.Header.Get("authorization") != fmt.Sprintf("Bearer at-%d", f.tokens) {
			f.t.Errorf("authorization %q, want the last access token at-%d", r.Header.Get("authorization"), f.tokens)
		}
		fail := func(status int, code, message string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
				"code": status, "message": message, "status": code,
				"details": []map[string]string{{"@type": "type.googleapis.com/google.firebase.fcm.v1.FcmError", "errorCode": code}},
			}})
		}
		switch {
		case token == "unregistered":
			fail(http.StatusNotFound, "UNREGISTERED", "Requested entity was not found.")
		case token == "badtoken":
			fail(http.StatusBadRequest, "INVALID_ARGUMENT", "The registration token is not a valid FCM registration token")
		case token == "badpayload":
			fail(http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid JSON payload received.")
		case token == "mismatch":
			fail(http.StatusForbidden, "SENDER_ID_MISMATCH", "SenderId mismatch")
		case token == "unauth-once" && f.seen[token] == 1:
			fail(http.StatusUnauthorized, "UNAUTHENTICATED", "Request had invalid authentication credentials.")
		case token == "forbidden":
			fail(http.StatusForbidden, "PERMISSION_DENIED", "Permission denied.")
		case token == "unavailable":
			fail(http.StatusServiceUnavailable, "UNAVAILABLE", "The service is currently unavailable.")
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "projects/pluma-test/messages/1"})
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeFCM) verifyAssertion(jwt string) map[string]any {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		f.t.Fatalf("assertion %q", jwt)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(f.pub, crypto.SHA256, digest[:], sig); err != nil {
		f.t.Fatalf("assertion signature: %v", err)
	}
	b, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	_ = json.Unmarshal(b, &claims)
	return claims
}

func newTestFCM(t *testing.T) (*FCM, *fakeFCM) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeFCM{t: t, pub: &key.PublicKey, seen: map[string]int{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	fake.aud = srv.URL + "/token"
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	creds, _ := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "pluma-test",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email": "push@pluma-test.iam.gserviceaccount.com",
		"token_uri":    srv.URL + "/token",
	})
	file := filepath.Join(t.TempDir(), "fcm.json")
	if err = os.WriteFile(file, creds, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := NewFCM(FCMConfig{CredentialsFile: file, Endpoint: srv.URL}, srv.Client())
	if err != nil || f == nil {
		t.Fatalf("NewFCM: %v", err)
	}
	return f, fake
}

func TestFCMSend(t *testing.T) {
	f, fake := newTestFCM(t)
	ctx := context.Background()
	if f.ProjectId() != "pluma-test" {
		t.Fatalf("project %q", f.ProjectId())
	}
	for i := 0; i < 2; i++ {
		if out, err := f.Send(ctx, "ok", map[string]string{"p": "abc"}, false); out != OutcomeSent || err != nil {
			t.Fatalf("Send: %s %v", out, err)
		}
	}
	if fake.tokens != 1 {
		t.Fatalf("%d access tokens for two sends, want 1", fake.tokens)
	}
	req := fake.sends[0]
	if req.ValidateOnly || req.Message.Token != "ok" || req.Message.Data["p"] != "abc" || req.Message.Android.Priority != "HIGH" || req.Message.Android.TTL != "86400s" {
		t.Fatalf("request: %+v", req)
	}
	if out, _ := f.Send(ctx, "ok", map[string]string{"p": "abc"}, true); out != OutcomeSent || !fake.sends[2].ValidateOnly {
		t.Fatalf("validate only: %s %+v", out, fake.sends[2])
	}
}

func TestFCMOutcomes(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		token string
		want  Outcome
		sends int
	}{
		{"unregistered", OutcomeTokenInvalid, 1},
		{"badtoken", OutcomeTokenInvalid, 1},
		{"mismatch", OutcomeTokenInvalid, 1},
		{"badpayload", OutcomeRejected, 1},
		{"unauth-once", OutcomeSent, 2},
		{"forbidden", OutcomeAuthFailed, 1},
		{"unavailable", OutcomeUnavailable, attempts},
	} {
		f, fake := newTestFCM(t)
		out, err := f.Send(ctx, tc.token, map[string]string{"p": "x"}, false)
		if out != tc.want || len(fake.sends) != tc.sends {
			t.Errorf("%s: %s after %d sends (%v), want %s after %d", tc.token, out, len(fake.sends), err, tc.want, tc.sends)
		}
		if tc.token == "unauth-once" && fake.tokens != 2 {
			t.Errorf("unauth-once: %d access tokens, want a renewed one", fake.tokens)
		}
	}
}

func TestFCMOff(t *testing.T) {
	if f, err := NewFCM(FCMConfig{}, nil); f != nil || err != nil {
		t.Fatalf("without credentials: %v %v", f, err)
	}
	var off *FCM
	if out, err := off.Send(context.Background(), "t", nil, false); out != OutcomeDisabled || err != nil {
		t.Fatalf("Send when off: %s %v", out, err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(bad, []byte(`{"type":"authorized_user"}`), 0o600)
	if _, err := NewFCM(FCMConfig{CredentialsFile: bad}, nil); err == nil {
		t.Fatal("credentials that are not a service account are accepted")
	}
}

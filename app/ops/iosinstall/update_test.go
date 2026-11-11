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

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/iosupdate"
)

const testUpdateSecret = "fedcba9876543210fedcba9876543210fedcba9876543210"

// updateHarness is a harness with one registered iPhone, build 21 uploaded
// and signed, and the update from the app set up: the releases service
// names *current as the current release of the iPhone app.
func updateHarness(t *testing.T) (h *harness, token string, current *int32, known *bool) {
	t.Helper()
	h = newHarness(t)
	h.srv.cfg.updateSecret = testUpdateSecret
	current, known = new(int32), new(bool)
	*current, *known = 21, true
	h.srv.current = func() (int32, bool) { return *current, *known }
	token = tokenOf(t, h.enroll(udid1, "iPhone16,1"))
	if w := h.upload(testIPA(t), testBundles, testToken); w.Code != http.StatusNoContent {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	if err := h.signer.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	return h, token, current, known
}

func (h *harness) updateStatus() iosupdate.Status {
	h.t.Helper()
	w := h.do("GET", iosupdate.StatusPath, nil, nil)
	var out iosupdate.Status
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		h.t.Fatalf("update status: %d %s", w.Code, w.Body)
	}
	return out
}

// TestUpdateTicket: the manifest and the IPA are given for an update ticket
// of the signed, current build as they are for the device's token, the
// manifest naming the IPA with the same ticket; the status tells the bff
// that build.
func TestUpdateTicket(t *testing.T) {
	h, token, _, _ := updateHarness(t)
	secret := []byte(testUpdateSecret)
	ticket := iosupdate.Mint(secret, 21, 7001, h.now)

	if st := h.updateStatus(); st.Build != 21 || st.Manifest != "https://pluma.example/ios/adhoc/manifest" {
		t.Fatalf("status %+v", st)
	}
	w := h.do("GET", "/ios/adhoc/manifest?u="+ticket, nil, nil)
	if m := w.Body.String(); w.Code != 200 || !strings.Contains(m, "<string>chat.pluma.app</string>") ||
		!strings.Contains(m, "<string>https://pluma.example/ios/adhoc/app.ipa?u="+ticket+"</string>") || strings.Contains(m, token) {
		t.Fatalf("manifest by a ticket: %d %s", w.Code, m)
	}
	byTicket := h.do("GET", "/ios/adhoc/app.ipa?u="+ticket, nil, nil)
	byToken := h.do("GET", "/ios/adhoc/app.ipa?t="+token, nil, nil)
	if byTicket.Code != 200 || byToken.Code != 200 || byTicket.Body.Len() == 0 || byTicket.Body.String() != byToken.Body.String() {
		t.Fatalf("the IPA by a ticket: %d, %d bytes; by the token: %d, %d bytes", byTicket.Code, byTicket.Body.Len(), byToken.Code, byToken.Body.Len())
	}
	// The device's token keeps working, with its own IPA link.
	if m := h.do("GET", "/ios/adhoc/manifest?t="+token, nil, nil).Body.String(); !strings.Contains(m, "/ios/adhoc/app.ipa?t="+token+"<") {
		t.Fatalf("manifest by the token: %s", m)
	}
	// A ticket of yesterday still installs; the log names the session, not
	// the ticket.
	h.now = h.now.Add(24 * time.Hour)
	if w := h.do("GET", "/ios/adhoc/manifest?u="+ticket, nil, nil); w.Code != 200 {
		t.Fatalf("a ticket of yesterday: %d", w.Code)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, "an update from the app for 198.51.100.7, session ") || strings.Contains(logs, ticket) || strings.Contains(logs, token) {
		t.Errorf("log: %s", logs)
	}
}

// TestUpdateTicketRefusals: nothing is given for a ticket that is expired,
// of another secret, of another build, or changed; nor for a good one when
// the build is withdrawn, the releases service does not answer, a newer
// build is not signed yet, or the secret is not set. The device's token is
// not touched by any of that.
func TestUpdateTicketRefusals(t *testing.T) {
	secret := []byte(testUpdateSecret)
	refused := func(t *testing.T, h *harness, ticket string) {
		t.Helper()
		for _, p := range []string{"/ios/adhoc/manifest?u=", "/ios/adhoc/app.ipa?u="} {
			if w := h.do("GET", p+ticket, nil, nil); w.Code != http.StatusNotFound {
				t.Errorf("%s: %d", p, w.Code)
			}
		}
	}
	served := func(t *testing.T, h *harness, credential string) {
		t.Helper()
		for _, p := range []string{"/ios/adhoc/manifest?", "/ios/adhoc/app.ipa?"} {
			if w := h.do("GET", p+credential, nil, nil); w.Code != 200 {
				t.Errorf("%s%s: %d", p, credential[:2], w.Code)
			}
		}
	}

	t.Run("bad tickets", func(t *testing.T) {
		h, token, _, _ := updateHarness(t)
		good := iosupdate.Mint(secret, 21, 7001, h.now)
		served(t, h, "u="+good)
		changed := []byte(good)
		changed[3] ^= 1
		for name, ticket := range map[string]string{
			"two days old":   iosupdate.Mint(secret, 21, 7001, h.now.Add(-48*time.Hour)),
			"of tomorrow":    iosupdate.Mint(secret, 21, 7001, h.now.Add(24*time.Hour)),
			"another secret": iosupdate.Mint([]byte(strings.Repeat("x", 32)), 21, 7001, h.now),
			"another build":  iosupdate.Mint(secret, 20, 7001, h.now),
			"a newer build":  iosupdate.Mint(secret, 22, 7001, h.now),
			"changed":        string(changed),
			"empty":          "",
			"the token":      token,
		} {
			t.Run(name, func(t *testing.T) { refused(t, h, ticket) })
		}
		// A ticket does not open the device's status.
		if w := h.do("GET", "/ios/adhoc/status?u="+good, nil, nil); w.Code != http.StatusNotFound {
			t.Errorf("status by a ticket: %d", w.Code)
		}
	})

	t.Run("withdrawn release", func(t *testing.T) {
		h, token, current, _ := updateHarness(t)
		ticket := iosupdate.Mint(secret, 21, 7001, h.now)
		*current = 20
		refused(t, h, ticket)
		if st := h.updateStatus(); st.Build != 0 || st.Manifest != "" {
			t.Errorf("status %+v", st)
		}
		served(t, h, "t="+token)
		// Restored: the same ticket works again.
		*current = 21
		served(t, h, "u="+ticket)
	})

	t.Run("releases service down", func(t *testing.T) {
		h, token, _, known := updateHarness(t)
		*known = false
		refused(t, h, iosupdate.Mint(secret, 21, 7001, h.now))
		// No answer rather than "nothing": the bff keeps the one it has.
		if w := h.do("GET", iosupdate.StatusPath, nil, nil); w.Code != http.StatusServiceUnavailable {
			t.Errorf("status: %d %s", w.Code, w.Body)
		}
		served(t, h, "t="+token)
	})

	t.Run("a new build not signed yet", func(t *testing.T) {
		h, _, _, _ := updateHarness(t)
		ticket := iosupdate.Mint(secret, 21, 7001, h.now)
		if w := h.upload(testIPA(t, "Payload/Telegram.app/new.txt"), testBundles, testToken); w.Code != http.StatusNoContent {
			t.Fatalf("second upload: %d %s", w.Code, w.Body)
		}
		refused(t, h, ticket)
		if st := h.updateStatus(); st.Build != 0 {
			t.Errorf("status %+v", st)
		}
		if err := h.signer.run(context.Background()); err != nil {
			t.Fatal(err)
		}
		served(t, h, "u="+ticket)
	})

	t.Run("no secret", func(t *testing.T) {
		h, token, _, _ := updateHarness(t)
		h.srv.cfg.updateSecret = ""
		refused(t, h, iosupdate.Mint(secret, 21, 7001, h.now))
		refused(t, h, iosupdate.Mint(nil, 21, 7001, h.now))
		if st := h.updateStatus(); st.Build != 0 {
			t.Errorf("status %+v", st)
		}
		served(t, h, "t="+token)
	})

	t.Run("no build", func(t *testing.T) {
		h := newHarness(t)
		h.srv.cfg.updateSecret = testUpdateSecret
		h.srv.current = func() (int32, bool) { return 21, true }
		refused(t, h, iosupdate.Mint(secret, 21, 7001, h.now))
		if st := h.updateStatus(); st.Build != 0 {
			t.Errorf("status %+v", st)
		}
	})
}

// TestCurrentRelease: the build of the iPhone app in releases.json, asked
// again after half a minute; while the releases service fails it is asked
// again sooner and its last answer serves for five minutes, after which,
// and when there never was an answer, the release is not known; 0 when the
// app has no release.
func TestCurrentRelease(t *testing.T) {
	var requests atomic.Int32
	var body atomic.Value
	body.Store("")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		b := body.Load().(string)
		if b == "" {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(b))
	}))
	defer ts.Close()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	c := newCurrentRelease(ts.URL)
	c.now = func() time.Time { return now }
	check := func(step string, wantBuild int32, wantKnown bool, wantRequests int32) {
		t.Helper()
		if b, known := c.build(); b != wantBuild || known != wantKnown || requests.Load() != wantRequests {
			t.Fatalf("%s: build %d known %v after %d requests, want %d %v after %d", step, b, known, requests.Load(), wantBuild, wantKnown, wantRequests)
		}
	}
	check("never answered", 0, false, 1)
	body.Store(`{"platforms":{"android":{"build":9},"ios":{"version":"12.9.2","build":21}}}`)
	now = now.Add(4 * time.Second)
	check("never answered, not asked", 0, false, 1)
	now = now.Add(time.Second)
	check("first", 21, true, 2)
	body.Store(`{"platforms":{"ios":{"build":20}}}`)
	now = now.Add(29 * time.Second)
	check("kept", 21, true, 2)
	now = now.Add(time.Second)
	check("asked again", 20, true, 3)

	// Down: the last answer serves, asked every five seconds.
	body.Store("")
	now = now.Add(30 * time.Second)
	check("down", 20, true, 4)
	now = now.Add(4 * time.Second)
	check("down, not asked", 20, true, 4)
	body.Store(`not json`)
	now = now.Add(time.Second)
	check("not json", 20, true, 5)
	// The last answer was 35 seconds before this moment.
	now = now.Add(5*time.Minute - 35*time.Second)
	check("down for five minutes", 20, true, 6)
	now = now.Add(5 * time.Second)
	check("down for longer", 0, false, 7)
	body.Store(`{"platforms":{}}`)
	now = now.Add(5 * time.Second)
	check("no release", 0, true, 8)
}

func TestConfigUpdateSecret(t *testing.T) {
	dir := t.TempDir()
	tokenFile := dir + "/upload-token"
	if err := os.WriteFile(tokenFile, []byte(testToken), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"PUBLIC_URL": "https://pluma.example", "ASC_KEY_ID": "K", "ASC_ISSUER": "I", "ASC_KEY_FILE": "/k",
		"SIGN_CERTIFICATE_ID": "C", "SIGN_P12_FILE": "/p", "SIGN_P12_PASSWORD_FILE": "/pw", "UPLOAD_TOKEN_FILE": tokenFile,
	}
	get := func(k string) string { return env[k] }
	c, err := configFromEnv(get)
	if err != nil || c.updateSecret != "" || c.releasesURL != "http://releases:9097/dl/releases.json" {
		t.Fatalf("without a secret: %v, releases %q", err, c.releasesURL)
	}
	env["UPDATE_TICKET_SECRET"] = "short"
	if _, err := configFromEnv(get); err == nil || !strings.Contains(err.Error(), "UPDATE_TICKET_SECRET") {
		t.Fatalf("a short secret: %v", err)
	}
	env["UPDATE_TICKET_SECRET"] = testUpdateSecret
	env["RELEASES_JSON_URL"] = "releases"
	if _, err := configFromEnv(get); err == nil || !strings.Contains(err.Error(), "RELEASES_JSON_URL") {
		t.Fatalf("a wrong releases address: %v", err)
	}
	env["RELEASES_JSON_URL"] = "http://127.0.0.1:9097/dl/releases.json"
	if c, err := configFromEnv(get); err != nil || c.updateSecret != testUpdateSecret {
		t.Fatalf("with a secret: %v", err)
	}
}

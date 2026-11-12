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

package iosupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestChecker: the answer of the install service is kept for half a
// minute; while the service fails, the last answer serves for five minutes
// and the service is asked again every five seconds; after that, and when
// there never was an answer, the error is returned.
func TestChecker(t *testing.T) {
	var requests atomic.Int32
	var body atomic.Value
	body.Store(`{"build":60,"manifest":"https://pluma.example/ios/adhoc/manifest"}`)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		b := body.Load().(string)
		if b == "" || r.URL.Path != StatusPath {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(b))
	}))
	defer ts.Close()
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	c := NewChecker(ts.URL + StatusPath)
	c.Now = func() time.Time { return now }
	check := func(step string, wantBuild int32, wantErr bool, wantRequests int32) {
		t.Helper()
		st, err := c.Status(context.Background())
		if st.Build != wantBuild || (err != nil) != wantErr || requests.Load() != wantRequests {
			t.Fatalf("%s: build %d, error %v, %d requests; want %d, error %v, %d", step, st.Build, err, requests.Load(), wantBuild, wantErr, wantRequests)
		}
		if wantBuild != 0 && st.Manifest != "https://pluma.example/ios/adhoc/manifest" {
			t.Fatalf("%s: manifest %q", step, st.Manifest)
		}
	}
	check("first", 60, false, 1)
	now = now.Add(29 * time.Second)
	check("kept", 60, false, 1)
	body.Store(`{"build":0}`)
	now = now.Add(time.Second)
	check("nothing to offer", 0, false, 2)
	body.Store(`{"build":61,"manifest":"https://pluma.example/ios/adhoc/manifest"}`)
	now = now.Add(30 * time.Second)
	check("a new build", 61, false, 3)

	// The service goes away: the last answer serves, and the service is
	// asked every five seconds.
	body.Store("")
	now = now.Add(30 * time.Second)
	check("down", 61, false, 4)
	now = now.Add(4 * time.Second)
	check("down, not asked", 61, false, 4)
	now = now.Add(time.Second)
	check("down, asked again", 61, false, 5)
	// The last answer was 35 seconds before this moment.
	now = now.Add(5*time.Minute - 35*time.Second)
	check("down for five minutes", 61, false, 6)
	now = now.Add(5 * time.Second)
	check("down for longer", 0, true, 7)
	body.Store(`{"build":61,"manifest":"https://pluma.example/ios/adhoc/manifest"}`)
	now = now.Add(5 * time.Second)
	check("back", 61, false, 8)

	// A canceled caller still gets the answer the others will share.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	now = now.Add(30 * time.Second)
	if st, err := c.Status(ctx); err != nil || st.Build != 61 || requests.Load() != 9 {
		t.Fatalf("a canceled caller: %+v, %v, %d requests", st, err, requests.Load())
	}
}

// TestCheckerRefusesAnswers: an answer that is not the status, or names a
// manifest iOS would not take, is an error and nothing is offered.
func TestCheckerRefusesAnswers(t *testing.T) {
	for name, body := range map[string]string{
		"not json":       `<html>`,
		"http manifest":  `{"build":60,"manifest":"http://pluma.example/ios/adhoc/manifest"}`,
		"no manifest":    `{"build":60}`,
		"with a query":   `{"build":60,"manifest":"https://pluma.example/ios/adhoc/manifest?x=1"}`,
		"not an address": `{"build":60,"manifest":"manifest"}`,
	} {
		t.Run(name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer ts.Close()
			if st, err := NewChecker(ts.URL).Status(context.Background()); err == nil || st.Build != 0 {
				t.Errorf("%+v, %v", st, err)
			}
		})
	}
	// Nothing listens.
	ts := httptest.NewServer(http.NotFoundHandler())
	ts.Close()
	if st, err := NewChecker(ts.URL).Status(context.Background()); err == nil || st.Build != 0 {
		t.Errorf("no service: %+v, %v", st, err)
	}
}

func TestSettings(t *testing.T) {
	secret := strings.Repeat("a", 32)
	for _, c := range []struct {
		name    string
		s       Settings
		enabled bool
		wantErr string
	}{
		{"off", Settings{}, false, ""},
		{"off with an address", Settings{StatusURL: "http://iosinstall:9098/internal/update"}, false, ""},
		{"on", Settings{Secret: secret, StatusURL: "http://iosinstall:9098/internal/update"}, true, ""},
		{"a short secret", Settings{Secret: "short", StatusURL: "http://iosinstall:9098/internal/update"}, true, "at least 32"},
		{"no address", Settings{Secret: secret}, true, "StatusURL"},
		{"not an address", Settings{Secret: secret, StatusURL: "iosinstall:9098"}, true, "StatusURL"},
	} {
		err := c.s.Check()
		if c.s.Enabled() != c.enabled || (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: enabled %v, %v", c.name, c.s.Enabled(), err)
		}
	}
}

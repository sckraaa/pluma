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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const topic = "0123456789abcdef0123456789abcdef"

type ntfyFake struct {
	mu     sync.Mutex
	status int
	got    []http.Header
	bodies []string
	paths  []string
}

func (f *ntfyFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.got = append(f.got, r.Header.Clone())
	f.bodies = append(f.bodies, string(b))
	f.paths = append(f.paths, r.URL.Path)
	st := f.status
	f.mu.Unlock()
	if st == 0 {
		st = http.StatusOK
	}
	w.WriteHeader(st)
}

const notification = `{
  "version": "4", "status": "firing", "groupLabels": {"alertname": "DeliveryDeadEvents"},
  "alerts": [
    {"status": "firing", "labels": {"alertname": "DeliveryDeadEvents", "severity": "critical", "state": "dead"},
     "annotations": {"summary": "3 outbox events are dead", "description": "Inspect with pluma-admin outbox list."}},
    {"status": "resolved", "labels": {"alertname": "DeliveryLag", "severity": "warning"},
     "annotations": {"summary": "Lag of Inbox did not reach 0"}},
    {"status": "firing", "labels": {"alertname": "Bare", "job": "x"}}
  ]
}`

func post(t *testing.T, h http.Handler, body string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(body)))
	return rec.Code
}

func TestPushesPerAlert(t *testing.T) {
	f := &ntfyFake{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	h := newHandler(config{server: srv.URL, topic: topic, token: "tk"}, srv.Client())

	if code := post(t, h, notification); code != http.StatusNoContent {
		t.Fatalf("status %d", code)
	}
	if len(f.got) != 3 {
		t.Fatalf("%d pushes, want 3", len(f.got))
	}
	want := []struct{ title, prio, tags, body string }{
		{"DeliveryDeadEvents", "5", "rotating_light", "3 outbox events are dead\nInspect with pluma-admin outbox list."},
		{"Resolved: DeliveryLag", "2", "white_check_mark", "Lag of Inbox did not reach 0"},
		{"Bare", "4", "warning", "alertname=Bare job=x"},
	}
	for i, w := range want {
		h := f.got[i]
		if h.Get("Title") != w.title || h.Get("Priority") != w.prio || h.Get("Tags") != w.tags || f.bodies[i] != w.body {
			t.Errorf("push %d: %q %q %q %q, want %+v", i, h.Get("Title"), h.Get("Priority"), h.Get("Tags"), f.bodies[i], w)
		}
		if h.Get("Authorization") != "Bearer tk" || f.paths[i] != "/"+topic {
			t.Errorf("push %d: auth %q path %q", i, h.Get("Authorization"), f.paths[i])
		}
	}
}

func TestFailedPushAsksAlertmanagerToRetry(t *testing.T) {
	f := &ntfyFake{status: http.StatusTooManyRequests}
	srv := httptest.NewServer(f)
	defer srv.Close()
	h := newHandler(config{server: srv.URL, topic: topic}, srv.Client())
	if code := post(t, h, notification); code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", code)
	}
	if code := post(t, h, "{not json"); code != http.StatusBadRequest {
		t.Fatalf("bad payload: status %d, want 400", code)
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := configFromEnv(env(map[string]string{"NTFY_TOPIC": topic}))
	if err != nil || c.server != "https://ntfy.sh" || c.listen != ":9097" {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	for _, bad := range []string{"", "short", topic + "/x", "has space in the topic name ok"} {
		if _, err := configFromEnv(env(map[string]string{"NTFY_TOPIC": bad})); err == nil {
			t.Errorf("topic %q accepted", bad)
		}
	}
	if _, err := configFromEnv(env(map[string]string{"NTFY_TOPIC": topic, "NTFY_URL": "not a url"})); err == nil {
		t.Error("bad NTFY_URL accepted")
	}
}

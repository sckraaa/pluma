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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type config struct {
	server string // ntfy server URL
	topic  string
	token  string
	listen string
}

func configFromEnv(getenv func(string) string) (config, error) {
	c := config{
		server: strings.TrimRight(getenv("NTFY_URL"), "/"),
		topic:  getenv("NTFY_TOPIC"),
		token:  getenv("NTFY_TOKEN"),
		listen: getenv("LISTEN"),
	}
	if c.server == "" {
		c.server = "https://ntfy.sh"
	}
	if c.listen == "" {
		c.listen = ":9097"
	}
	if _, err := url.ParseRequestURI(c.server); err != nil {
		return c, fmt.Errorf("NTFY_URL: %w", err)
	}
	// A topic on a public server is readable by anyone who knows its name:
	// it must be long and random (openssl rand -hex 16).
	if len(c.topic) < 20 || strings.ContainsAny(c.topic, "/?#& ") {
		return c, errors.New("NTFY_TOPIC must be a random name of at least 20 characters")
	}
	return c, nil
}

// webhook is the part of Alertmanager's webhook payload (version 4) that a
// push needs.
type webhook struct {
	Status      string            `json:"status"`
	GroupLabels map[string]string `json:"groupLabels"`
	Alerts      []alert           `json:"alerts"`
}

type alert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

// push is one ntfy message.
type push struct {
	title    string
	body     string
	priority string // ntfy priority 1..5
	tags     string
}

// pushes turns a notification into one push per alert: a phone shows each
// separately, and a resolved alert says so.
func pushes(w webhook) []push {
	out := make([]push, 0, len(w.Alerts))
	for _, a := range w.Alerts {
		name := a.Labels["alertname"]
		sev := a.Labels["severity"]
		p := push{priority: "4", tags: "warning"}
		switch {
		case a.Status == "resolved":
			p.priority, p.tags = "2", "white_check_mark"
			p.title = "Resolved: " + name
		case sev == "critical":
			p.priority, p.tags = "5", "rotating_light"
			p.title = name
		default:
			p.title = name
		}
		var b strings.Builder
		if s := a.Annotations["summary"]; s != "" {
			b.WriteString(s)
		}
		if d := a.Annotations["description"]; d != "" && a.Status != "resolved" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(d)
		}
		if b.Len() == 0 {
			b.WriteString(labelLine(a.Labels))
		}
		p.body = b.String()
		out = append(out, p)
	}
	return out
}

// labelLine lists an alert's labels, for alerts without annotations.
func labelLine(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, " ")
}

type handler struct {
	cfg    config
	client *http.Client
}

func newHandler(cfg config, client *http.Client) http.Handler {
	h := &handler{cfg: cfg, client: client}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /alertmanager", h.alertmanager)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return mux
}

// alertmanager answers 5xx when a push fails, so Alertmanager sends the
// notification again.
func (h *handler) alertmanager(w http.ResponseWriter, r *http.Request) {
	var n webhook
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&n); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	for _, p := range pushes(n) {
		if err := h.send(r, p); err != nil {
			log.Printf("alertpush: %s: %v", p.title, err)
			http.Error(w, "push failed", http.StatusBadGateway)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) send(r *http.Request, p push) error {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.cfg.server+"/"+h.cfg.topic, strings.NewReader(p.body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", p.title)
	req.Header.Set("Priority", p.priority)
	req.Header.Set("Tags", p.tags)
	if h.cfg.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.cfg.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ntfy answered %s", resp.Status)
	}
	return nil
}

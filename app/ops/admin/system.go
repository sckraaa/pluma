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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// promClient reads what the monitoring stack already knows
// (docs/ops/monitoring.md): the port of Prometheus is published on the
// host's loopback, where the panel runs.
type promClient struct {
	base string
	http *http.Client
}

func newPromClient(base string) *promClient {
	return &promClient{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 5 * time.Second}}
}

func (p *promClient) get(ctx context.Context, path string, out any) error {
	if p.base == "" {
		return errors.New("prometheus is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base+path, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("prometheus: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// serviceState is a scrape target of Prometheus.
type serviceState struct {
	Job      string `json:"job"`
	Instance string `json:"instance"`
	Up       bool   `json:"up"`
}

// alertState is an active alert.
type alertState struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
	State    string `json:"state"`
	Summary  string `json:"summary"`
	Instance string `json:"instance,omitempty"`
}

// diskState is the root disk of the host.
type diskState struct {
	Available int64 `json:"available"`
	Size      int64 `json:"size"`
}

type promVector struct {
	Data struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func (p *promClient) query(ctx context.Context, q string) (*promVector, error) {
	var out promVector
	if err := p.get(ctx, "/api/v1/query?query="+url.QueryEscape(q), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func vectorValue(v [2]any) string {
	s, _ := v[1].(string)
	return s
}

func (p *promClient) services(ctx context.Context) ([]serviceState, error) {
	v, err := p.query(ctx, "up")
	if err != nil {
		return nil, err
	}
	out := make([]serviceState, 0, len(v.Data.Result))
	for _, r := range v.Data.Result {
		out = append(out, serviceState{Job: r.Metric["job"], Instance: r.Metric["instance"], Up: vectorValue(r.Value) == "1"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Job < out[j].Job })
	return out, nil
}

func (p *promClient) alerts(ctx context.Context) ([]alertState, error) {
	var resp struct {
		Data struct {
			Alerts []struct {
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
				State       string            `json:"state"`
			} `json:"alerts"`
		} `json:"data"`
	}
	if err := p.get(ctx, "/api/v1/alerts", &resp); err != nil {
		return nil, err
	}
	out := make([]alertState, 0, len(resp.Data.Alerts))
	for _, a := range resp.Data.Alerts {
		out = append(out, alertState{Name: a.Labels["alertname"], Severity: a.Labels["severity"], State: a.State,
			Summary: a.Annotations["summary"], Instance: a.Labels["instance"]})
	}
	return out, nil
}

func (p *promClient) disk(ctx context.Context) (*diskState, error) {
	const fs = `{fstype!~"tmpfs|overlay|squashfs|iso9660|virtiofs|nfs.*|fuse.*",mountpoint="/"}`
	avail, err := p.query(ctx, "node_filesystem_avail_bytes"+fs)
	if err != nil {
		return nil, err
	}
	size, err := p.query(ctx, "node_filesystem_size_bytes"+fs)
	if err != nil {
		return nil, err
	}
	if len(avail.Data.Result) == 0 || len(size.Data.Result) == 0 {
		return nil, nil
	}
	return &diskState{Available: parseFloatInt(vectorValue(avail.Data.Result[0].Value)), Size: parseFloatInt(vectorValue(size.Data.Result[0].Value))}, nil
}

func parseFloatInt(s string) int64 {
	var f float64
	if _, err := fmt.Sscanf(s, "%g", &f); err != nil {
		return 0
	}
	return int64(f)
}

// outboxRow is what the panel shows of a stuck event: metadata, no payload.
type outboxRow struct {
	Id        int64  `json:"id"`
	Topic     string `json:"topic"`
	Recipient int64  `json:"recipient"`
	Method    string `json:"method"`
	State     int8   `json:"state"`
	Attempts  int    `json:"attempts"`
	Replays   int    `json:"replays"`
	Error     string `json:"error"`
	CreatedAt int64  `json:"created_at"`
}

// outboxAdmin runs the actions of pkg/outbox on the database of mq_outbox
// with the session settings of the box transactions.
type outboxAdmin struct {
	db *sql.DB
}

func rowOf(info outbox.RowInfo) outboxRow {
	return outboxRow{Id: info.Id, Topic: info.Topic, Recipient: info.Recipient, Method: info.Method, State: int8(info.State),
		Attempts: info.Attempts, Replays: info.Replays, Error: info.Error, CreatedAt: info.CreatedAt.Unix()}
}

func (o *outboxAdmin) list(ctx context.Context, states ...int8) ([]outboxRow, error) {
	var out []outboxRow
	for _, st := range states {
		state := userbox.EventState(st)
		rows, err := outbox.List(ctx, o.db, outbox.ListFilter{State: &state, Limit: 200})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, rowOf(r))
		}
	}
	return out, nil
}

func (o *outboxAdmin) replay(ctx context.Context, recipient int64) (int, error) {
	return outbox.Replay(ctx, o.db, recipient)
}

func (o *outboxAdmin) discard(ctx context.Context, id int64) (outboxRow, error) {
	info, err := outbox.Discard(ctx, o.db, id)
	return rowOf(info), err
}

// handleSystem is the System page: each block fails on its own, so that a
// Prometheus that is down still leaves the events and the links.
func (s *server) handleSystem(w http.ResponseWriter, r *http.Request, a *actor) {
	out := map[string]any{"grafana": s.cfg.GrafanaUrl}
	if services, err := s.prom.services(r.Context()); err != nil {
		s.logWarn(r, "prometheus services", err)
		out["services_error"] = true
	} else {
		out["services"] = services
	}
	if alerts, err := s.prom.alerts(r.Context()); err != nil {
		s.logWarn(r, "prometheus alerts", err)
		out["alerts_error"] = true
	} else {
		out["alerts"] = alerts
	}
	rows, err := s.outbox.list(r.Context(), int8(userbox.EventDead), int8(userbox.EventParked))
	if err != nil {
		s.logWarn(r, "outbox", err)
		out["events_error"] = true
	} else {
		out["events"] = rows
	}
	writeJSON(w, out)
}

func (s *server) handleOutboxReplay(w http.ResponseWriter, r *http.Request, a *actor) {
	var body struct {
		Recipient int64 `json:"recipient"`
	}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	if body.Recipient <= 0 {
		httpError(w, http.StatusBadRequest, "bad recipient")
		return
	}
	if !s.journal(w, r, a, "outbox_replay", fmt.Sprintf("outbox_recipient:%d", body.Recipient), "") {
		return
	}
	n, err := s.outbox.replay(r.Context(), body.Recipient)
	if err != nil {
		s.fail(w, r, "outbox replay", err)
		return
	}
	writeJSON(w, map[string]any{"replayed": n})
}

func (s *server) handleOutboxDiscard(w http.ResponseWriter, r *http.Request, a *actor) {
	var body struct {
		Id int64 `json:"id"`
	}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	if body.Id <= 0 {
		httpError(w, http.StatusBadRequest, "bad id")
		return
	}
	if !s.journal(w, r, a, "outbox_discard", fmt.Sprintf("outbox:%d", body.Id), "") {
		return
	}
	row, err := s.outbox.discard(r.Context(), body.Id)
	if errors.Is(err, outbox.ErrNoRow) {
		httpError(w, http.StatusNotFound, "no such event")
		return
	}
	if err != nil {
		s.fail(w, r, "outbox discard", err)
		return
	}
	writeJSON(w, row)
}

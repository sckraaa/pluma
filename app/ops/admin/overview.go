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
	"fmt"
	"net/http"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// parsePayload reads the payload of a request that is not dangerous.
func parsePayload(payload string, out any) error {
	if payload == "" {
		return fmt.Errorf("no payload")
	}
	return json.Unmarshal([]byte(payload), out)
}

// handleOverview is the first page: numbers from the database, the problems
// from Prometheus. A part that fails does not hide the rest.
func (s *server) handleOverview(w http.ResponseWriter, r *http.Request, a *actor) {
	ctr, err := s.store.counters(r.Context())
	if err != nil {
		s.fail(w, r, "counters", err)
		return
	}
	out := map[string]any{"counters": ctr}
	if alerts, err := s.prom.alerts(r.Context()); err != nil {
		s.logWarn(r, "alerts", err)
		out["alerts_error"] = true
	} else {
		out["alerts"] = alerts
	}
	if services, err := s.prom.services(r.Context()); err != nil {
		s.logWarn(r, "services", err)
		out["services_error"] = true
	} else {
		down := 0
		for _, sv := range services {
			if !sv.Up {
				down++
			}
		}
		out["services_down"] = down
		out["services_total"] = len(services)
	}
	if d, err := s.prom.disk(r.Context()); err != nil {
		s.logWarn(r, "disk", err)
		out["disk_error"] = true
	} else {
		out["disk"] = d
	}
	if rows, err := s.outbox.list(r.Context(), int8(userbox.EventDead)); err != nil {
		s.logWarn(r, "outbox", err)
	} else {
		out["dead_events"] = len(rows)
	}
	writeJSON(w, out)
}

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
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// errReleasesOff: the releases service has no admin token configured.
var errReleasesOff = errors.New("the releases service is not configured")

// releaseFile is a file of a release.
type releaseFile struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Size int64  `json:"size"`
	Arch string `json:"arch,omitempty"`
}

// releaseRow is a build of the releases service.
type releaseRow struct {
	Version    string        `json:"version"`
	Build      int32         `json:"build"`
	NotesRu    string        `json:"notes_ru"`
	NotesEn    string        `json:"notes_en"`
	CanNotSkip bool          `json:"can_not_skip"`
	CreatedAt  int64         `json:"created_at"`
	Withdrawn  bool          `json:"withdrawn"`
	Files      []releaseFile `json:"files"`
}

// platformReleases are the builds of a platform, the newest first, with the
// one the site and the apps offer now.
type platformReleases struct {
	Platform string       `json:"platform"`
	Current  int32        `json:"current"` // the build, 0 for none
	Builds   []releaseRow `json:"builds"`
}

// releasesClient calls the admin endpoints of the releases service over the
// loopback port it publishes for the panel (docs/ops/releases.md).
type releasesClient struct {
	base  string
	token string
	http  *http.Client
}

func newReleasesClient(base, token string) *releasesClient {
	return &releasesClient{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *releasesClient) do(ctx context.Context, method, path string, out any) error {
	if c.base == "" || c.token == "" {
		return errReleasesOff
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, http.NoBody) //nolint:gosec // G704: an address of the configuration, not of a request
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req) //nolint:gosec // G704: an address of the configuration, not of a request
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return errReleasesOff
	}
	if resp.StatusCode/100 != 2 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("releases: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *releasesClient) list(ctx context.Context) ([]platformReleases, error) {
	var out struct {
		Platforms []platformReleases `json:"platforms"`
	}
	if err := c.do(ctx, http.MethodGet, "/internal/releases", &out); err != nil {
		return nil, err
	}
	return out.Platforms, nil
}

func (c *releasesClient) setWithdrawn(ctx context.Context, platform string, build int32, withdrawn bool) (*platformReleases, error) {
	action := "restore"
	if withdrawn {
		action = "withdraw"
	}
	var out platformReleases
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/internal/releases/%s/%d/%s", platform, build, action), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *server) handleReleases(w http.ResponseWriter, r *http.Request, a *actor) {
	platforms, err := s.releases.list(r.Context())
	switch {
	case errors.Is(err, errReleasesOff):
		writeJSON(w, map[string]any{"configured": false})
		return
	case err != nil:
		s.logWarn(r, "releases", err)
		httpError(w, http.StatusBadGateway, "the releases service does not answer")
		return
	}
	writeJSON(w, map[string]any{"configured": true, "platforms": platforms})
}

// handleReleaseWithdraw rolls a platform back: the build the site and the
// apps offer is withdrawn, the one before it becomes current (task 054).
func (s *server) handleReleaseWithdraw(w http.ResponseWriter, r *http.Request, a *actor) {
	s.setWithdrawn(w, r, a, true)
}

// handleReleaseRestore brings a withdrawn build back: a release CI has
// handed over and that was rolled back returns to the site; new builds
// arrive from CI (POST /dl/upload).
func (s *server) handleReleaseRestore(w http.ResponseWriter, r *http.Request, a *actor) {
	s.setWithdrawn(w, r, a, false)
}

func (s *server) setWithdrawn(w http.ResponseWriter, r *http.Request, a *actor, withdrawn bool) {
	platform := r.PathValue("platform")
	build, err := strconv.ParseInt(r.PathValue("build"), 10, 32)
	if err != nil || build <= 0 {
		httpError(w, http.StatusBadRequest, "bad build")
		return
	}
	var body struct{}
	if !s.readDangerous(w, r, a, &body) {
		return
	}
	action := "release_restore"
	if withdrawn {
		action = "release_withdraw"
	}
	if !s.journal(w, r, a, action, fmt.Sprintf("release:%s:%d", platform, build), "") {
		return
	}
	state, err := s.releases.setWithdrawn(r.Context(), platform, int32(build), withdrawn)
	switch {
	case errors.Is(err, errReleasesOff):
		httpError(w, http.StatusServiceUnavailable, "the releases service is not configured")
		return
	case err != nil:
		s.logWarn(r, action, err)
		httpError(w, http.StatusBadGateway, "the releases service refused the change")
		return
	}
	writeJSON(w, state)
}

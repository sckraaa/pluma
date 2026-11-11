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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// StatusPath is where the install service tells, inside the deployment
// only, what it gives for update tickets.
const StatusPath = "/internal/update"

// Status is the answer of StatusPath.
type Status struct {
	// Build is the build the install service gives for a ticket now: its
	// IPA is signed and it is the current release. 0 for none.
	Build int32 `json:"build"`
	// Manifest is the public https address of the manifest, to which the
	// ticket is added.
	Manifest string `json:"manifest,omitempty"`
}

const (
	statusFresh   = 30 * time.Second
	statusRetry   = 5 * time.Second
	statusStale   = 5 * time.Minute
	statusTimeout = 2 * time.Second
)

// Checker asks the install service for its Status and keeps the answer for
// half a minute, so that help.getAppUpdate offers the update from the app
// only when its manifest will be there: iOS shows a bare error for a
// manifest it cannot get.
//
// An answer is kept for five minutes while the install service does not
// answer (it restarts in seconds on a deployment): the link is the same
// again when it is back, and the app does not show its update screen anew
// for a link that went away and returned.
type Checker struct {
	URL    string           // of StatusPath
	Client *http.Client     // http.DefaultClient if nil
	Now    func() time.Time // time.Now if nil

	mu      sync.Mutex
	last    Status
	lastErr error
	asked   time.Time // the last request
	good    time.Time // the last answer
}

// NewChecker is a Checker of the status at statusURL.
func NewChecker(statusURL string) *Checker { return &Checker{URL: statusURL} }

// Status is the install service's answer, or the error of asking it.
func (c *Checker) Status(ctx context.Context) (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	wait := statusFresh
	if c.lastErr != nil {
		wait = statusRetry
	}
	if c.asked.IsZero() || now.Sub(c.asked) >= wait {
		st, err := c.fetch(ctx)
		c.asked, c.lastErr = now, err
		if err == nil {
			c.last, c.good = st, now
		}
	}
	if c.lastErr != nil && (c.good.IsZero() || now.Sub(c.good) > statusStale) {
		return Status{}, c.lastErr
	}
	return c.last, nil
}

func (c *Checker) fetch(ctx context.Context) (Status, error) {
	var st Status
	// The caller's deadline is not this request's: its answer serves the
	// callers of the next half minute too.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return st, err
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return st, fmt.Errorf("iosupdate: %s answers %d", c.URL, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&st); err != nil {
		return st, fmt.Errorf("iosupdate: the answer of %s: %w", c.URL, err)
	}
	if st.Build > 0 {
		// PUBLIC_URL of the install service is https.
		u, err := url.Parse(st.Manifest)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" {
			return Status{}, fmt.Errorf("iosupdate: the answer of %s names no https manifest", c.URL)
		}
	}
	return st, nil
}

// Settings is the bff's side of the update from the app.
type Settings struct {
	// Secret signs the tickets; the install service holds the same
	// (PLUMA_IOS_UPDATE_SECRET). Empty: the update is offered as the
	// site's install page, as before task 085.
	Secret string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// StatusURL is StatusPath of the install service inside the deployment.
	StatusURL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Enabled tells whether the update from the app is set up.
func (s Settings) Enabled() bool { return s.Secret != "" }

// Check finds what Enabled settings lack.
func (s Settings) Check() error {
	if !s.Enabled() {
		return nil
	}
	if !ValidSecret(s.Secret) {
		return fmt.Errorf("iosupdate: the secret must be at least %d characters", MinSecret)
	}
	if u, err := url.Parse(s.StatusURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("iosupdate: StatusURL must be the install service's %s", StatusPath)
	}
	return nil
}

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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/pkg/iosupdate"
)

// The update from the app (task 085). An installed Ad Hoc build is offered
// the next one by help.getAppUpdate; its link is the manifest here with an
// update ticket (pkg/iosupdate) in place of the device's token, which the
// bff does not know. The ticket gives the same re-signed IPA every
// registered iPhone gets, and only while that IPA is the current release:
//
//   - the last uploaded build is signed (state.signedBuild);
//   - the ticket is of that build, minted today or yesterday;
//   - the releases service names that build as the current release of the
//     iPhone app: a withdrawn release (docs/ops/releases.md) stops being
//     given for the tickets already out. While the releases service does
//     not answer, its last answer serves for five minutes (it restarts in
//     seconds on a deployment); after that, and when it never answered, no
//     ticket is taken: the page of the site and its device tokens work as
//     before.
//
// The bff asks updateStatus before it offers the link, so an app is not
// sent to a manifest that is not there.

// grant is what a request's credential gives.
type grant struct {
	// query is the credential as the IPA's address carries it.
	query url.Values
	// subject is the session of an update ticket, nil for a device token.
	subject []byte
}

// served checks the credential of a manifest or IPA request, the device's
// token (t) or an update ticket (u), and returns the state whose signed IPA
// it gives.
func (s *server) served(r *http.Request) (state, grant, bool) {
	q := r.URL.Query()
	if q.Get("t") != "" {
		d, st, ok := s.device(r)
		if !ok || !st.ready(d) {
			return st, grant{}, false
		}
		return st, grant{query: url.Values{"t": {d.Token}}}, true
	}
	ticket := q.Get(iosupdate.Param)
	if ticket == "" || s.cfg.updateSecret == "" {
		return state{}, grant{}, false
	}
	t, err := iosupdate.Verify([]byte(s.cfg.updateSecret), ticket, s.now())
	if err != nil {
		return state{}, grant{}, false
	}
	st := s.store.view()
	if build, ok := s.offered(&st); !ok || build != t.Build {
		return st, grant{}, false
	}
	return st, grant{query: url.Values{iosupdate.Param: {ticket}}, subject: t.Subject[:]}, true
}

// offered is the build update tickets are taken for now.
func (s *server) offered(st *state) (int32, bool) {
	build, ok := st.signedBuild()
	if !ok || s.cfg.updateSecret == "" {
		return 0, false
	}
	if current, known := s.current(); !known || current != build {
		return 0, false
	}
	return build, true
}

// updateStatus tells the bff what help.getAppUpdate may offer. It has no
// answer while the current release is not known: the bff then keeps the
// answer it has (iosupdate.Checker) instead of taking the link away from
// the apps and giving it back, which shows them the update screen anew.
func (s *server) updateStatus(w http.ResponseWriter, _ *http.Request) {
	var out iosupdate.Status
	if s.cfg.updateSecret != "" {
		if _, known := s.current(); !known {
			http.Error(w, "the current release is not known", http.StatusServiceUnavailable)
			return
		}
	}
	st := s.store.view()
	if build, ok := s.offered(&st); ok {
		out = iosupdate.Status{Build: build, Manifest: s.cfg.publicURL + "/ios/adhoc/manifest"}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

const (
	currentFresh   = 30 * time.Second
	currentRetry   = 5 * time.Second
	currentStale   = 5 * time.Minute
	currentTimeout = 3 * time.Second
)

// currentRelease reads the build of the current release of the iPhone app
// from releases.json of the releases service and keeps it for half a
// minute: a withdrawal reaches the tickets within that time. While the
// service fails it is asked every five seconds and its last answer serves
// for five minutes.
type currentRelease struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu     sync.Mutex
	asked  time.Time // the last request
	good   time.Time // the last answer
	failed bool      // the last request
	value  int32
}

func newCurrentRelease(releasesURL string) *currentRelease {
	return &currentRelease{url: releasesURL, client: &http.Client{Timeout: currentTimeout}, now: time.Now}
}

// build is the current release's build, 0 for none; false when it is not
// known.
func (c *currentRelease) build() (int32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	wait := currentFresh
	if c.failed {
		wait = currentRetry
	}
	now := c.now()
	if c.asked.IsZero() || now.Sub(c.asked) >= wait {
		v, err := c.fetch()
		c.asked, c.failed = now, err != nil
		if err == nil {
			c.value, c.good = v, now
		}
	}
	if c.failed && (c.good.IsZero() || now.Sub(c.good) > currentStale) {
		return 0, false
	}
	return c.value, true
}

// fetch: 0 with no error when the iPhone app has no release.
func (c *currentRelease) fetch() (int32, error) {
	ctx, cancel := context.WithTimeout(context.Background(), currentTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out struct {
		Platforms map[string]struct {
			Build int32 `json:"build"`
		} `json:"platforms"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return 0, err
	}
	return out.Platforms["ios"].Build, nil
}

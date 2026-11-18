//go:build e2e

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

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// The caches of the releases' /dl/ by the country of the client (tasks
// 113, 115): help.getConfig names the Russian node's for Russia and the
// CDN's for the others. The stand's PLUMA_RELEASES_FILES_URL and
// PLUMA_RELEASES_FILES_ROUTES (run.sh, gen_edge_env).
var (
	stackReleasesCdnURL = os.Getenv("PLUMA_E2E_RELEASES_CDN_URL")
	stackReleasesRuURL  = "https://" + ruFilesDomain
)

// TestReleaseSiteByCountry: help.getConfig gives a desktop app in Russia
// the update prefix on the cache of the Russian node, and the others, one
// of an unknown country among them, on the CDN in front of the site.
func TestReleaseSiteByCountry(t *testing.T) {
	needEdges(t)
	needRuFiles(t)
	if stackReleasesCdnURL == "" {
		t.Skip("PLUMA_E2E_RELEASES_CDN_URL is not set (run.sh test sets it)")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	direct, germany, russia := countryClients(ctx, t, "releases")

	for name, c := range map[string]struct {
		u    *user
		want string
	}{
		"unknown": {direct, stackReleasesCdnURL + "/dl/desktop"},
		"Germany": {germany, stackReleasesCdnURL + "/dl/desktop"},
		"Russia":  {russia, stackReleasesRuURL + "/dl/desktop"},
	} {
		o, err := c.u.c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: c.u.c.Constructor(mtproto.Predicate_help_getConfig)})
		if err != nil {
			t.Fatalf("%s: help.getConfig: %v", name, err)
		}
		conf, ok := o.(*mtproto.TLConfig)
		if !ok {
			t.Fatalf("%s: help.getConfig returned %T", name, o)
		}
		if got := conf.GetAutoupdateUrlPrefix().GetValue(); got != c.want {
			t.Errorf("%s: autoupdate_url_prefix %q, want %q", name, got, c.want)
		}
	}
}

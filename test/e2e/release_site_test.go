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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// The site of the releases by the country of the client (task 113):
// help.getConfig names the mirror of the site for Russia. The stand's
// PLUMA_RELEASES_URL and PLUMA_RELEASES_ROUTES (run.sh, gen_edge_env).
const (
	stackReleasesURL   = "https://site.localhost"
	stackReleasesRuURL = "https://rusite.localhost"
)

// TestReleaseSiteByCountry: help.getConfig gives a desktop app in Russia
// the update prefix on the mirror of the releases' site, and the others,
// one of an unknown country among them, on the site itself.
func TestReleaseSiteByCountry(t *testing.T) {
	needEdges(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	direct, germany, russia := countryClients(ctx, t, "releases")

	for name, c := range map[string]struct {
		u    *user
		want string
	}{
		"unknown": {direct, stackReleasesURL + "/dl/desktop"},
		"Germany": {germany, stackReleasesURL + "/dl/desktop"},
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

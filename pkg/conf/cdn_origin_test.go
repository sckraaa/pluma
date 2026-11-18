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

package conf

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestCdnOriginSite: the origin of the HTTPS CDN (task 107) is published by
// Caddy only under PLUMA_CDN_ORIGIN_DOMAIN. Caddy refuses a whole Caddyfile
// with a site of an empty address, so the site is a file of its own that the
// web container links into the imported directory only when the variable is
// set; it proxies bff's origin listener, which no compose file publishes on
// the host, and neither compresses nor logs.
func TestCdnOriginSite(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	caddy := read("teamgramd/deploy/web/Caddyfile")
	if !regexp.MustCompile(`(?m)^import /etc/caddy/cdn-origin\.d/\*\.caddy$`).MatchString(caddy) {
		t.Error("Caddyfile: no import of /etc/caddy/cdn-origin.d/*.caddy")
	}
	if strings.Contains(caddy, "{$PLUMA_CDN_ORIGIN_DOMAIN}") {
		t.Error("Caddyfile: a site of PLUMA_CDN_ORIGIN_DOMAIN stops Caddy when the variable is empty")
	}

	site := read("teamgramd/deploy/web/cdn-origin.caddy")
	var directives []string
	for _, line := range strings.Split(site, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			directives = append(directives, line)
		}
	}
	body := strings.Join(directives, "\n")
	if !strings.HasPrefix(body, "{$PLUMA_CDN_ORIGIN_DOMAIN} {") {
		t.Error("cdn-origin.caddy: not the site of {$PLUMA_CDN_ORIGIN_DOMAIN}")
	}
	if !strings.Contains(body, "handle {\nreverse_proxy bff:20480 {\nheader_up -Accept-Encoding\n}\n}") {
		t.Error("cdn-origin.caddy: does not proxy the rest to bff:20480 without Accept-Encoding")
	}
	// /dl/ of the releases (task 115), which the Russian node caches: for
	// reading only, and without the access value of the origin.
	if !strings.Contains(body, "handle /dl/* {\n@write not method GET HEAD\nrespond @write 405\nreverse_proxy releases:9097 {\nheader_up -X-Pluma-Origin-Auth\n}\n}") {
		t.Error("cdn-origin.caddy: /dl/ is not the releases service for GET and HEAD without X-Pluma-Origin-Auth")
	}
	for _, d := range []string{"encode", "log"} {
		if regexp.MustCompile(`(?m)^` + d + `\b`).MatchString(body) {
			t.Errorf("cdn-origin.caddy: %s directive", d)
		}
	}

	var web struct {
		Services map[string]struct {
			composeService `yaml:",inline"`
			Tmpfs          []string `yaml:"tmpfs"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(read("docker-compose.web.yaml")), &web); err != nil {
		t.Fatal(err)
	}
	w := web.Services["web"]
	if val := w.Environment["PLUMA_CDN_ORIGIN_DOMAIN"]; val != "${PLUMA_CDN_ORIGIN_DOMAIN:-}" {
		t.Errorf("web gets PLUMA_CDN_ORIGIN_DOMAIN as %q, want an optional variable", val)
	}
	if !slices.Contains(w.Volumes, "./teamgramd/deploy/web/cdn-origin.caddy:/etc/caddy/cdn-origin.caddy:ro") {
		t.Error("web: cdn-origin.caddy is not mounted at /etc/caddy/cdn-origin.caddy")
	}
	if !slices.Contains(w.Tmpfs, "/etc/caddy/cdn-origin.d") {
		t.Error("web: /etc/caddy/cdn-origin.d is not a tmpfs, so a link would outlive the variable")
	}
	script := strings.Join(w.Command, "\n")
	if !strings.Contains(script, `if [ -n "$$PLUMA_CDN_ORIGIN_DOMAIN" ]; then`) ||
		!strings.Contains(script, "ln -s /etc/caddy/cdn-origin.caddy /etc/caddy/cdn-origin.d/") ||
		!strings.Contains(script, "exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile") {
		t.Errorf("web: the command does not link the site only with a name and run Caddy: %q", script)
	}

	// The origin listener is reached through Caddy only.
	for _, f := range []string{"docker-compose.yaml", "docker-compose.web.yaml", "docker-compose.public-ip.yaml"} {
		if strings.Contains(read(f), "20480") {
			t.Errorf("%s publishes or names the origin port 20480", f)
		}
	}
}

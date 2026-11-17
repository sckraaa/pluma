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

// TestMapsPublicSite: the public map tiles of the clients' maps (task 112,
// ADR 0070) are published by Caddy only under PLUMA_MAPS_DOMAIN, from a site
// file the web container links only with that name and a key that fits;
// the site passes only paths under the key to maps-tiles, and logs nothing.
func TestMapsPublicSite(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	caddy := read("teamgramd/deploy/web/Caddyfile")
	if !regexp.MustCompile(`(?m)^import /etc/caddy/maps-public\.d/\*\.caddy$`).MatchString(caddy) {
		t.Error("Caddyfile: no import of /etc/caddy/maps-public.d/*.caddy")
	}
	if strings.Contains(caddy, "{$PLUMA_MAPS_DOMAIN}") {
		t.Error("Caddyfile: a site of PLUMA_MAPS_DOMAIN stops Caddy when the variable is empty")
	}

	var directives []string
	for _, line := range strings.Split(read("teamgramd/deploy/web/maps-public.caddy"), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			directives = append(directives, line)
		}
	}
	body := strings.Join(directives, "\n")
	if !strings.HasPrefix(body, "{$PLUMA_MAPS_DOMAIN} {\nhandle_path /{$MAPS_PUBLIC_KEY}/* {") {
		t.Error("maps-public.caddy: not the site of {$PLUMA_MAPS_DOMAIN} with everything under the key first")
	}
	proxies := regexp.MustCompile(`(?m)^reverse_proxy (\S+)`).FindAllStringSubmatch(body, -1)
	if len(proxies) == 0 {
		t.Error("maps-public.caddy: proxies nothing")
	}
	for _, p := range proxies {
		if p[1] != "maps-tiles:8080" {
			t.Errorf("maps-public.caddy: proxies %s, want only maps-tiles:8080", p[1])
		}
	}
	if !strings.Contains(body, "header_up X-Forwarded-Path /{$MAPS_PUBLIC_KEY}") {
		t.Error("maps-public.caddy: maps-tiles is not told the key's prefix of the links")
	}
	for _, d := range []string{"log", "file_server", "static"} {
		if regexp.MustCompile(`(?m)^`+d+`\b`).MatchString(body) || strings.Contains(body, "/"+d+"/") {
			t.Errorf("maps-public.caddy: %s", d)
		}
	}
	// The paths a map loads, and nothing else (tileserver-gl v5.7.0: the
	// style JSON and the sprites of src/serve_style.js, the glyphs of
	// src/serve_font.js, the TileJSON and the tiles of src/serve_data.js).
	var allowed []*regexp.Regexp
	for _, m := range regexp.MustCompile(`(?m)^path_regexp (\S+)$`).FindAllStringSubmatch(body, -1) {
		allowed = append(allowed, regexp.MustCompile(m[1]))
	}
	match := func(path string) bool {
		for _, re := range allowed[:2] {
			if re.MatchString(path) {
				return true
			}
		}
		return false
	}
	if len(allowed) != 3 {
		t.Fatalf("maps-public.caddy: %d path_regexp, want those of the JSON, the files and the preflight", len(allowed))
	}
	for _, path := range []string{
		"/styles/ru/style.json", "/styles/en-dark/style.json", "/data/protomaps.json",
		"/styles/ru/sprite.json", "/styles/ru-dark/sprite@2x.png",
		"/fonts/Noto Sans Regular/0-255.pbf", "/fonts/Noto Sans Regular,Noto Sans Medium/1024-1279.pbf",
		"/data/protomaps/15/19806/10241.pbf",
	} {
		if !match(path) {
			t.Errorf("maps-public.caddy does not pass %s", path)
		}
	}
	for _, path := range []string{
		"/styles/ru/static/37.6,55.7,15/277x195@2x.png", "/styles/ru/", "/styles/ru/wmts.xml",
		"/styles.json", "/data.json", "/index.json", "/rendered.json", "/health", "/",
		"/styles/ru/sprite@3x.png", "/data/protomaps/15/1/2.mvt", "/data/other.json", "/fonts.json",
		"/styles/../style.json", "/data/protomaps/1/2/3.pbf/x",
	} {
		if match(path) {
			t.Errorf("maps-public.caddy passes %s", path)
		}
	}

	var web struct {
		Services map[string]struct {
			composeService `yaml:",inline"`
			Tmpfs          []string  `yaml:"tmpfs"`
			Networks       yaml.Node `yaml:"networks"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(read("docker-compose.web.yaml")), &web); err != nil {
		t.Fatal(err)
	}
	w := web.Services["web"]
	for _, v := range []string{"PLUMA_MAPS_DOMAIN", "MAPS_PUBLIC_KEY"} {
		if val := w.Environment[v]; val != "${"+v+":-}" {
			t.Errorf("web gets %s as %q, want an optional variable", v, val)
		}
	}
	if !slices.Contains(w.Volumes, "./teamgramd/deploy/web/maps-public.caddy:/etc/caddy/maps-public.caddy:ro") {
		t.Error("web: maps-public.caddy is not mounted at /etc/caddy/maps-public.caddy")
	}
	if !slices.Contains(w.Tmpfs, "/etc/caddy/maps-public.d") {
		t.Error("web: /etc/caddy/maps-public.d is not a tmpfs, so a link would outlive the variable")
	}
	script := strings.Join(w.Command, "\n")
	if !strings.Contains(script, `if [ -n "$$PLUMA_MAPS_DOMAIN" ]; then`) ||
		!strings.Contains(script, `*[!A-Za-z0-9_-]*) bad=1 ;;`) ||
		!strings.Contains(script, `[ "$${#MAPS_PUBLIC_KEY}" -lt 16 ]`) ||
		!strings.Contains(script, "ln -s /etc/caddy/maps-public.caddy /etc/caddy/maps-public.d/") {
		t.Errorf("web: the command does not link the site only with a name and a key that fits: %q", script)
	}
	if nets := networkNames(w.Networks); !slices.Equal(nets, []string{"teamgram_net", "maps_public_net"}) {
		t.Errorf("web is on %v, want teamgram_net and maps_public_net", nets)
	}

	// The key is in no file of the repository: only in .env.
	for _, f := range []string{"docker-compose.maps.yaml", "docker-compose.web.yaml"} {
		if strings.Contains(read(f), "--public_url") {
			t.Errorf("%s gives tileserver-gl a public URL, which holds the key", f)
		}
	}
}

// TestMapsTilesIsolated: maps-tiles is on maps_public_net alone, an internal
// network that only it and Caddy join, with no published port, the public
// config, no CORS of its own and a log without URLs.
func TestMapsTilesIsolated(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot, "docker-compose*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("compose files: %v", err)
	}
	type service struct {
		Image       string    `yaml:"image"`
		Command     []string  `yaml:"command"`
		Environment yaml.Node `yaml:"environment"`
		Ports       []string  `yaml:"ports"`
		Networks    yaml.Node `yaml:"networks"`
	}
	found := false
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var compose struct {
			Services map[string]service `yaml:"services"`
			Networks map[string]struct {
				Internal bool `yaml:"internal"`
			} `yaml:"networks"`
		}
		if err := yaml.Unmarshal(data, &compose); err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		base := filepath.Base(f)
		if n, ok := compose.Networks["maps_public_net"]; ok && !n.Internal {
			t.Errorf("%s: maps_public_net is not internal", base)
		}
		for name, s := range compose.Services {
			nets := networkNames(s.Networks)
			if slices.Contains(nets, "maps_public_net") && name != "maps-tiles" && name != "web" {
				t.Errorf("%s: %s is on maps_public_net, which is for maps-tiles and Caddy only", base, name)
			}
			if name != "maps-tiles" {
				continue
			}
			found = true
			if !slices.Equal(nets, []string{"maps_public_net"}) || len(s.Ports) > 0 {
				t.Errorf("%s: maps-tiles is on %v with ports %v, want maps_public_net alone and no port", base, nets, s.Ports)
			}
			if s.Image != "${PLUMA_MAPS_IMAGE:-pluma-maps:local}" {
				t.Errorf("%s: maps-tiles runs %q, not the image of the maps service", base, s.Image)
			}
			cmd := strings.Join(s.Command, " ")
			if !strings.HasPrefix(cmd, "--config /app/config-public.json ") || !slices.Contains(s.Command, "--no-cors") ||
				!slices.Contains(s.Command, "--silent") || strings.Contains(cmd, ":url") || !strings.Contains(cmd, "--log_format") {
				t.Errorf("%s: maps-tiles command %q", base, cmd)
			}
			if v := envValue(s.Environment, "TILESERVER_GL_ALLOWED_HOSTS"); v != "${PLUMA_MAPS_DOMAIN:-}" {
				t.Errorf("%s: maps-tiles builds its links for %q, want the site's name", base, v)
			}
		}
	}
	if !found {
		t.Error("no maps-tiles service")
	}
}

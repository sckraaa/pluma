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
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestRuMirrorSites: the mirrors of the site and of the login on sites
// through the Russian node (task 113) are sites of their own files on the
// node's server :8443, each linked only with its name; they abort a
// request of the node without the PROXY header first, as the edge does,
// and then serve the snippets the site and the login are made of, so the
// mirrors cannot drift from them. The login hands links the provider's own
// name.
func TestRuMirrorSites(t *testing.T) {
	caddy := readRepo(t, "teamgramd/deploy/web/Caddyfile")
	if !regexp.MustCompile(`(?m)^import /etc/caddy/ru-mirror\.d/\*\.caddy$`).MatchString(caddy) {
		t.Error("Caddyfile: no import of /etc/caddy/ru-mirror.d/*.caddy")
	}
	for _, name := range []string{"{$PLUMA_RU_SITE_DOMAIN}", "{$PLUMA_RU_OAUTH_DOMAIN}"} {
		if strings.Contains(caddy, name) {
			t.Errorf("Caddyfile: a site of %s stops Caddy when the variable is empty", name)
		}
	}
	d := caddyDirectives(caddy)
	for _, want := range []string{
		"\n{$PLUMA_SITE_DOMAINS} {\nimport pluma_site\nimport pluma_site_errors\n}\n",
		"\n{$PLUMA_OAUTH_DOMAIN} {\nimport pluma_oauth\n}\n",
		"\n(pluma_site) {\n",
		"\n(pluma_site_errors) {\nhandle_errors {\n",
		"\n(pluma_oauth) {\nencode zstd gzip\nreverse_proxy links:9096 {\nheader_up Host {$PLUMA_OAUTH_DOMAIN}\n}\n",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("Caddyfile has no\n%s", want)
		}
	}
	// The snippets come before every site that imports them, the imported
	// files of the mirrors among them.
	if i, j := strings.Index(d, "\n(pluma_site) {\n"), strings.Index(d, "\n{$PLUMA_SITE_DOMAINS} {\n"); i < 0 || j < i {
		t.Error("Caddyfile: the snippet of the site is not before its site")
	}

	for file, head := range map[string]string{
		"ru-site.caddy":  "{$PLUMA_RU_SITE_DOMAIN}:8443 {\n@unwrapped remote_ip {$PLUMA_RU_EDGE_PROXY_FROM}\nroute {\nabort @unwrapped\nhandle {\nimport pluma_site\n}\n}\nimport pluma_site_errors\n}",
		"ru-oauth.caddy": "{$PLUMA_RU_OAUTH_DOMAIN}:8443 {\n@unwrapped remote_ip {$PLUMA_RU_EDGE_PROXY_FROM}\nroute {\nabort @unwrapped\nhandle {\nimport pluma_oauth\n}\n}\n}",
	} {
		if got := caddyDirectives(readRepo(t, "teamgramd/deploy/web/"+file)); got != head {
			t.Errorf("%s:\n%s\nwant\n%s", file, got, head)
		}
	}

	w, tmpfs := webService(t)
	for _, name := range []string{"PLUMA_RU_SITE_DOMAIN", "PLUMA_RU_OAUTH_DOMAIN"} {
		if got, want := w.Environment[name], "${"+name+":-}"; got != want {
			t.Errorf("web gets %s as %q, want %q", name, got, want)
		}
	}
	for _, f := range []string{"ru-site", "ru-oauth"} {
		if !slices.Contains(w.Volumes, "./teamgramd/deploy/web/"+f+".caddy:/etc/caddy/"+f+".caddy:ro") {
			t.Errorf("web: %s.caddy is not mounted at /etc/caddy/%s.caddy", f, f)
		}
	}
	if !slices.Contains(tmpfs, "/etc/caddy/ru-mirror.d") {
		t.Error("web: /etc/caddy/ru-mirror.d is not a tmpfs, so a link would outlive the variable")
	}
}

// TestRuMirrorSitesLinked runs the command of the web container as
// TestEdgeSitesLinked does: each mirror is linked only with its name.
func TestRuMirrorSitesLinked(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	w, _ := webService(t)
	const run = "exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile"
	if len(w.Command) != 3 || !strings.Contains(w.Command[2], run) {
		t.Fatalf("web: the command %q does not run Caddy", w.Command)
	}
	script := strings.ReplaceAll(strings.ReplaceAll(w.Command[2], "$$", "$"), run, "exit 0")
	for _, c := range []struct {
		name   string
		env    []string
		linked []string
	}{
		{"none", nil, nil},
		{"site", []string{"PLUMA_RU_SITE_DOMAIN=pluma.example"}, []string{"ru-site.caddy"}},
		{"oauth", []string{"PLUMA_RU_OAUTH_DOMAIN=oauth.example"}, []string{"ru-oauth.caddy"}},
		{"both", []string{"PLUMA_RU_SITE_DOMAIN=pluma.example", "PLUMA_RU_OAUTH_DOMAIN=oauth.example"}, []string{"ru-oauth.caddy", "ru-site.caddy"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, d := range []string{"cdn-origin.d", "maps-public.d", "edge-origin.d", "ru-edge.d", "ru-mirror.d"} {
				if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(sh, "-ec", strings.ReplaceAll(script, "/etc/caddy", dir))
			cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, c.env...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("the script failed: %v, %s", err, out)
			}
			entries, err := os.ReadDir(filepath.Join(dir, "ru-mirror.d"))
			if err != nil {
				t.Fatal(err)
			}
			var linked []string
			for _, e := range entries {
				target, err := os.Readlink(filepath.Join(dir, "ru-mirror.d", e.Name()))
				if err != nil || target != filepath.Join(dir, e.Name()) {
					t.Errorf("%s links to %q (%v)", e.Name(), target, err)
				}
				linked = append(linked, e.Name())
			}
			if !slices.Equal(linked, c.linked) {
				t.Errorf("linked %v, want %v", linked, c.linked)
			}
		})
	}
}

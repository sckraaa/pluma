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

	"gopkg.in/yaml.v3"
)

// readRepo is the text of the file name of the repository.
func readRepo(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// caddyDirectives is a Caddy file without its comments and blank lines, each
// line trimmed.
func caddyDirectives(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// webService is the web service of docker-compose.web.yaml.
func webService(t *testing.T) (svc composeService, tmpfs []string) {
	t.Helper()
	var web struct {
		Services map[string]struct {
			composeService `yaml:",inline"`
			Tmpfs          []string `yaml:"tmpfs"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(readRepo(t, "docker-compose.web.yaml")), &web); err != nil {
		t.Fatal(err)
	}
	w := web.Services["web"]
	return w.composeService, w.Tmpfs
}

// TestEdgeSites: the edges of the clients (task 109, ADR 0072) are two sites
// of Caddy, each a file of its own that the web container links into an
// imported directory only when its name is set (Caddy refuses a whole
// Caddyfile with a site of an empty address). The Gcore origin refuses
// every request without the resource's secret before anything else and
// hands the gateway the address Gcore saw; the Russian node's edge is on a
// server of its own that takes PROXY v1 from the node alone and hands the
// gateway the address of the PROXY header. Both go to the gateway's Edge
// entry, which no compose file publishes, and neither logs.
func TestEdgeSites(t *testing.T) {
	caddy := readRepo(t, "teamgramd/deploy/web/Caddyfile")
	for _, dir := range []string{"edge-origin.d", "ru-edge.d"} {
		if !regexp.MustCompile(`(?m)^import /etc/caddy/` + regexp.QuoteMeta(dir) + `/\*\.caddy$`).MatchString(caddy) {
			t.Errorf("Caddyfile: no import of /etc/caddy/%s/*.caddy", dir)
		}
	}
	for _, name := range []string{"{$PLUMA_EDGE_ORIGIN_DOMAIN}", "{$PLUMA_RU_EDGE_DOMAIN}"} {
		if strings.Contains(caddy, name) {
			t.Errorf("Caddyfile: a site of %s stops Caddy when the variable is empty", name)
		}
	}
	servers := regexp.MustCompile(`(?s)\nservers :8443 \{\nlistener_wrappers \{\nproxy_protocol \{\nallow \{\$PLUMA_RU_EDGE_PROXY_FROM\}\nfallback_policy reject\n\}\ntls\n\}\nprotocols h1 h2\n\}`)
	if !servers.MatchString(caddyDirectives(caddy)) {
		t.Error("Caddyfile: the server :8443 does not take PROXY v1 from PLUMA_RU_EDGE_PROXY_FROM alone, before TLS, without HTTP/3")
	}

	gcore := caddyDirectives(readRepo(t, "teamgramd/deploy/web/edge-origin.caddy"))
	if !strings.HasPrefix(gcore, "{$PLUMA_EDGE_ORIGIN_DOMAIN} {\n"+
		"@denied not header X-Pluma-Edge-Auth {$PLUMA_EDGE_ORIGIN_AUTH}\n"+
		"route {\n"+
		"respond @denied 403\n"+
		"handle /apiws {\n"+
		"reverse_proxy gnetway:11445 {\n") {
		t.Error("edge-origin.caddy: not the site of {$PLUMA_EDGE_ORIGIN_DOMAIN} that refuses a request without X-Pluma-Edge-Auth first and proxies /apiws to gnetway:11445")
	}
	for _, d := range []string{
		"header_up X-Pluma-Client-Addr {http.request.header.X-Forwarded-For}",
		"header_up X-Pluma-Edge-Via gcore",
		"header_up -X-Pluma-Edge-Auth",
	} {
		if !strings.Contains(gcore, "\n"+d+"\n") {
			t.Errorf("edge-origin.caddy: no %s", d)
		}
	}
	// Task 115: /dl/ of the releases for reading, and nothing Gcore must
	// not keep is cacheable: /apiws, redirects and errors say no-store.
	for what, block := range map[string]string{
		"/dl/ to the releases service for GET and HEAD": "handle /dl/* {\n@write not method GET HEAD\nrespond @write 405\nreverse_proxy releases:9097 {\nheader_up -X-Pluma-Edge-Auth\n}\n}",
		"/apiws no-store":               "header_up -X-Pluma-Edge-Auth\n}\nheader Cache-Control \"no-store\"\n}\nhandle /dl/* {",
		"redirects and errors no-store": "header {\nCache-Control \"no-store\"\nmatch status 301 302 303 307 308 4xx 5xx\n}",
		"Caddy's errors no-store":       "handle_errors {\nheader -Server\nheader Cache-Control \"no-store\"\n}",
	} {
		if !strings.Contains(gcore, block) {
			t.Errorf("edge-origin.caddy: not %s", what)
		}
	}

	ru := caddyDirectives(readRepo(t, "teamgramd/deploy/web/ru-edge.caddy"))
	if !strings.HasPrefix(ru, "{$PLUMA_RU_EDGE_DOMAIN}:8443 {\n"+
		"@unwrapped remote_ip {$PLUMA_RU_EDGE_PROXY_FROM}\n"+
		"route {\n"+
		"abort @unwrapped\n"+
		"handle /apiws {\n"+
		"reverse_proxy gnetway:11445 {\n"+
		"header_up X-Pluma-Client-Addr {http.request.remote.host}\n"+
		"header_up X-Pluma-Edge-Via ru\n") {
		t.Error("ru-edge.caddy: not the site of {$PLUMA_RU_EDGE_DOMAIN}:8443 that aborts a request of the node without PROXY first and proxies /apiws to gnetway:11445 with the PROXY address")
	}

	for name, site := range map[string]string{"edge-origin.caddy": gcore, "ru-edge.caddy": ru} {
		for _, d := range []string{"log", "trusted_proxies"} {
			if regexp.MustCompile(`(?m)^` + d + `\b`).MatchString(site) {
				t.Errorf("%s: %s directive", name, d)
			}
		}
		for _, d := range []string{"-Server", "-Via"} {
			if !strings.Contains(site, "\n"+d+"\n") {
				t.Errorf("%s: answers keep %s", name, strings.TrimPrefix(d, "-"))
			}
		}
	}

	w, tmpfs := webService(t)
	for name, want := range map[string]string{
		"PLUMA_EDGE_ORIGIN_DOMAIN": "${PLUMA_EDGE_ORIGIN_DOMAIN:-}",
		"PLUMA_EDGE_ORIGIN_AUTH":   "${PLUMA_EDGE_ORIGIN_AUTH:-}",
		"PLUMA_RU_EDGE_DOMAIN":     "${PLUMA_RU_EDGE_DOMAIN:-}",
		// The node's address in the tunnel of tunnel.sh --link ru.
		"PLUMA_RU_EDGE_PROXY_FROM": "10.78.0.1/32",
	} {
		if got := w.Environment[name]; got != want {
			t.Errorf("web gets %s as %q, want %q", name, got, want)
		}
	}
	if !strings.Contains(readRepo(t, "teamgramd/deploy/edge/tunnel.sh"), "PEER_ADDR=10.78.0.1\n") {
		t.Error("tunnel.sh no longer gives the node 10.78.0.1, which web takes PROXY v1 from")
	}
	for _, f := range []string{"edge-origin", "ru-edge"} {
		if !slices.Contains(w.Volumes, "./teamgramd/deploy/web/"+f+".caddy:/etc/caddy/"+f+".caddy:ro") {
			t.Errorf("web: %s.caddy is not mounted at /etc/caddy/%s.caddy", f, f)
		}
		if !slices.Contains(tmpfs, "/etc/caddy/"+f+".d") {
			t.Errorf("web: /etc/caddy/%s.d is not a tmpfs, so a link would outlive the variable", f)
		}
	}

	// The Edge entry is reached through Caddy only.
	files, err := filepath.Glob(filepath.Join(repoRoot, "docker-compose*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.Contains(readRepo(t, filepath.Base(f)), "11445") {
			t.Errorf("%s publishes or names the Edge entry 11445", filepath.Base(f))
		}
	}
}

// TestEdgeSitesLinked runs the command of the web container, with its
// directories in a temporary one and Caddy left out: each edge site is
// linked only with its name, and the Gcore origin only with a secret of 64
// hex digits (an empty one would let every request through), or Caddy does
// not start.
func TestEdgeSitesLinked(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	w, _ := webService(t)
	if len(w.Command) != 3 || w.Command[0] != "/bin/sh" || w.Command[1] != "-ec" {
		t.Fatalf("web: the command %q is not a script of /bin/sh -ec", w.Command)
	}
	const run = "exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile"
	script := w.Command[2]
	if !strings.Contains(script, run) {
		t.Fatalf("web: the script does not run Caddy: %q", script)
	}
	// Compose leaves "$" of "$$" to the shell.
	script = strings.ReplaceAll(script, "$$", "$")
	script = strings.ReplaceAll(script, run, "exit 0")

	const auth = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, c := range []struct {
		name   string
		env    []string
		linked []string // the sites linked, by directory
		fails  bool
	}{
		{"none", nil, nil, false},
		{"gcore", []string{"PLUMA_EDGE_ORIGIN_DOMAIN=edge.example", "PLUMA_EDGE_ORIGIN_AUTH=" + auth}, []string{"edge-origin.d"}, false},
		{"ru", []string{"PLUMA_RU_EDGE_DOMAIN=ru.example"}, []string{"ru-edge.d"}, false},
		{"both", []string{"PLUMA_EDGE_ORIGIN_DOMAIN=edge.example", "PLUMA_EDGE_ORIGIN_AUTH=" + auth, "PLUMA_RU_EDGE_DOMAIN=ru.example"}, []string{"edge-origin.d", "ru-edge.d"}, false},
		{"secret without a name", []string{"PLUMA_EDGE_ORIGIN_AUTH=" + auth}, nil, false},
		{"no secret", []string{"PLUMA_EDGE_ORIGIN_DOMAIN=edge.example"}, nil, true},
		{"short secret", []string{"PLUMA_EDGE_ORIGIN_DOMAIN=edge.example", "PLUMA_EDGE_ORIGIN_AUTH=" + auth[1:]}, nil, true},
		{"long secret", []string{"PLUMA_EDGE_ORIGIN_DOMAIN=edge.example", "PLUMA_EDGE_ORIGIN_AUTH=" + auth + "0"}, nil, true},
		{"upper-case secret", []string{"PLUMA_EDGE_ORIGIN_DOMAIN=edge.example", "PLUMA_EDGE_ORIGIN_AUTH=" + strings.ToUpper(auth)}, nil, true},
		{"secret with a space", []string{"PLUMA_EDGE_ORIGIN_DOMAIN=edge.example", "PLUMA_EDGE_ORIGIN_AUTH=" + auth[:63] + " "}, nil, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, d := range []string{"cdn-origin.d", "maps-public.d", "edge-origin.d", "ru-edge.d"} {
				if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(sh, "-ec", strings.ReplaceAll(script, "/etc/caddy", dir))
			cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, c.env...)
			out, err := cmd.CombinedOutput()
			if c.fails {
				if err == nil || !strings.Contains(string(out), "PLUMA_EDGE_ORIGIN_AUTH of 64 hex digits") {
					t.Errorf("the script went on: %v, %s", err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("the script failed: %v, %s", err, out)
			}
			var linked []string
			for _, d := range []string{"edge-origin.d", "ru-edge.d"} {
				entries, err := os.ReadDir(filepath.Join(dir, d))
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) > 0 {
					linked = append(linked, d)
				}
			}
			if !slices.Equal(linked, c.linked) {
				t.Errorf("linked %v, want %v", linked, c.linked)
			}
		})
	}
}

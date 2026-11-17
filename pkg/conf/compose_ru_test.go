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

// TestComposeRuTunnelPorts: the Russian node reaches home's Caddy through its
// own tunnel (task 109, docker-compose.ru.yaml), on 80, 443 and 8443 of
// home's tunnel address. The edge on 8443 has no secret of its own (the
// node's PROXY v1 carries the client's address), so the tunnel is its only
// guard: the overlay publishes on PLUMA_RU_TUNNEL_IPV4 alone, required, and
// no other compose file publishes 8443.
func TestComposeRuTunnelPorts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.ru.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var overlay struct {
		Services map[string]struct {
			Image string   `yaml:"image"`
			Ports []string `yaml:"ports"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &overlay); err != nil {
		t.Fatal(err)
	}
	if len(overlay.Services) != 1 {
		t.Fatalf("docker-compose.ru.yaml has services %v, want only web", overlay.Services)
	}
	web, ok := overlay.Services["web"]
	if !ok || web.Image != "" {
		t.Fatalf("docker-compose.ru.yaml must only add ports to web of docker-compose.web.yaml")
	}
	port := regexp.MustCompile(`^\$\{PLUMA_RU_TUNNEL_IPV4:\?[^}]+\}:(\d+):(\d+)$`)
	var got []string
	for _, p := range web.Ports {
		m := port.FindStringSubmatch(p)
		if m == nil {
			t.Errorf("docker-compose.ru.yaml publishes %q, not on the required ${PLUMA_RU_TUNNEL_IPV4:?...}", p)
			continue
		}
		if m[1] != m[2] {
			t.Errorf("docker-compose.ru.yaml maps %s to %s; the node expects the same port", m[1], m[2])
		}
		got = append(got, m[1])
	}
	if want := []string{"80", "443", "8443"}; !slices.Equal(got, want) {
		t.Errorf("docker-compose.ru.yaml publishes %v, want %v", got, want)
	}

	files, err := filepath.Glob(filepath.Join(repoRoot, "docker-compose*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	published8443 := regexp.MustCompile(`(?m)^\s+- "?([^"\n]*:)?8443:\d+`)
	for _, f := range files {
		if filepath.Base(f) == "docker-compose.ru.yaml" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := published8443.Find(data); m != nil {
			t.Errorf("%s publishes 8443 (%s): only the tunnel to the Russian node may reach it", filepath.Base(f), strings.TrimSpace(string(m)))
		}
	}
}

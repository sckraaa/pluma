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
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type composeService struct {
	Image           string            `yaml:"image"`
	Command         []string          `yaml:"command"`
	Restart         string            `yaml:"restart"`
	WorkingDir      string            `yaml:"working_dir"`
	Environment     map[string]string `yaml:"environment"`
	Ports           []string          `yaml:"ports"`
	Secrets         []any             `yaml:"secrets"`
	StopGracePeriod string            `yaml:"stop_grace_period"`
}

// TestComposeServices keeps docker-compose.yaml at one process per service
// (ADR 0012, p. 8): each service runs its own binary in exec form with its
// own config, gets exactly the variables that config needs, drains within its
// stop_grace_period, and only gnetway publishes ports and gets the RSA key.
func TestComposeServices(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]composeService `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatal(err)
	}

	configs, _ := filepath.Glob(filepath.Join(repoRoot, "teamgramd/etc2/*.yaml"))
	want := map[string]bool{}
	for _, f := range configs {
		name := strings.TrimSuffix(filepath.Base(f), ".yaml")
		if name == "httpserver" { // legacy transport, not deployed (task 008)
			continue
		}
		want[name] = true
	}
	for name := range want {
		if _, ok := compose.Services[name]; !ok {
			t.Errorf("teamgramd/etc2/%s.yaml has no compose service", name)
		}
	}

	varRe := regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)
	for name, s := range compose.Services {
		if !want[name] {
			t.Errorf("compose service %s has no config in teamgramd/etc2", name)
			continue
		}
		wantCmd := []string{"./" + name, "-f=../etc2/" + name + ".yaml"}
		if strings.Join(s.Command, " ") != strings.Join(wantCmd, " ") || s.WorkingDir != "/app/bin" {
			t.Errorf("%s: command %q in %q, want %q in /app/bin", name, s.Command, s.WorkingDir, wantCmd)
		}
		if s.Restart == "" || s.Image == "" {
			t.Errorf("%s: image %q, restart %q: both must come from the x-teamgram anchor", name, s.Image, s.Restart)
		}
		grace, err := time.ParseDuration(s.StopGracePeriod)
		if err != nil || grace < 15*time.Second {
			t.Errorf("%s: stop_grace_period %q, want at least 15s", name, s.StopGracePeriod)
		}

		cfg, err := os.ReadFile(filepath.Join(repoRoot, "teamgramd/etc2", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		needed := map[string]bool{}
		for _, m := range varRe.FindAllStringSubmatch(string(cfg), -1) {
			needed[m[1]] = true
		}
		var got []string
		for v := range s.Environment {
			got = append(got, v)
			if !needed[v] {
				t.Errorf("%s gets %s, which its config does not use", name, v)
			}
		}
		sort.Strings(got)
		for v := range needed {
			val, ok := s.Environment[v]
			if !ok || !strings.HasPrefix(val, "${"+v+":?") {
				t.Errorf("%s: its config uses ${%s}, but the service does not get it as a required variable (has %v)", name, v, got)
			}
		}

		if name == "gnetway" {
			if len(s.Ports) == 0 || len(s.Secrets) == 0 {
				t.Errorf("gnetway must publish the client ports and get the RSA key")
			}
		} else if len(s.Ports) > 0 || len(s.Secrets) > 0 {
			t.Errorf("%s publishes ports %v or gets secrets %v: only gnetway may", name, s.Ports, s.Secrets)
		}
	}
}

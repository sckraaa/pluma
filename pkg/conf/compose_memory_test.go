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
	"testing"

	"gopkg.in/yaml.v3"
)

// TestComposeMemory keeps every container of Pluma bounded (docs/ops/memory.md;
// the incident of 2026-11-02, when dfs alone took 6.9 GB of a shared 8 GB
// server): each service of each compose file has a memory limit, no swap
// beyond it, and the slice of PLUMA_CGROUP_PARENT. A service an overlay only
// amends (gnetway in docker-compose.web.yaml) keeps the base file's.
func TestComposeMemory(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot, "docker-compose*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("compose files: %v", err)
	}
	type service struct {
		Image        string `yaml:"image"`
		MemLimit     string `yaml:"mem_limit"`
		MemswapLimit string `yaml:"memswap_limit"`
		CgroupParent string `yaml:"cgroup_parent"`
	}
	n := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var compose struct {
			Services map[string]service `yaml:"services"`
		}
		if err := yaml.Unmarshal(data, &compose); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for name, s := range compose.Services {
			if s.Image == "" {
				continue // amends a service of another file
			}
			n++
			where := filepath.Base(f) + ": " + name
			if s.MemLimit == "" {
				t.Errorf("%s has no mem_limit", where)
			}
			if s.MemswapLimit != s.MemLimit {
				t.Errorf("%s: memswap_limit %q, want %q (no swap)", where, s.MemswapLimit, s.MemLimit)
			}
			if s.CgroupParent != "${PLUMA_CGROUP_PARENT:-}" {
				t.Errorf("%s: cgroup_parent %q, want ${PLUMA_CGROUP_PARENT:-}", where, s.CgroupParent)
			}
		}
	}
	if n < 30 {
		t.Errorf("%d services checked; the compose files have more", n)
	}
}

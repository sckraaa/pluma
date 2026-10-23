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
	"testing"

	"gopkg.in/yaml.v3"
)

// TestRPCTimeouts keeps the RPC deadlines of ADR 0012, p. 9: no deadline is
// 0 (a request without one can hang forever), and a client waits at least as
// long as the server it calls may take.
func TestRPCTimeouts(t *testing.T) {
	for _, dir := range []string{"teamgramd/etc", "teamgramd/etc2"} {
		t.Run(dir, func(t *testing.T) {
			files, _ := filepath.Glob(filepath.Join(repoRoot, dir, "*.yaml"))
			zero := regexp.MustCompile(`(?m)^\s*Timeout:\s*0\s*$`)
			for _, f := range files {
				if filepath.Base(f) == "httpserver.yaml" { // legacy transport, not deployed (task 008)
					continue
				}
				data, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				if zero.Match(data) {
					t.Errorf("%s has Timeout: 0", filepath.Base(f))
				}
			}

			cfg := func(service string) map[string]any {
				data, err := os.ReadFile(filepath.Join(repoRoot, dir, service+".yaml"))
				if err != nil {
					t.Fatal(err)
				}
				var m map[string]any
				if err := yaml.Unmarshal(data, &m); err != nil {
					t.Fatal(err)
				}
				return m
			}
			timeout := func(service string, path ...string) int {
				var v any = cfg(service)
				for _, k := range path {
					m, ok := v.(map[string]any)
					if !ok {
						t.Fatalf("%s: %v is not a section", service, path)
					}
					v = m[k]
				}
				ms, ok := v.(int)
				if !ok {
					t.Fatalf("%s: %v.Timeout missing", service, path)
				}
				return ms
			}

			server := func(service string) int { return timeout(service, "Timeout") }
			for _, c := range []struct {
				client string
				path   []string
				server string
			}{
				{"bff", []string{"MsgClient", "Timeout"}, "msg"},
				{"bff", []string{"BizServiceClient", "Timeout"}, "biz"},
				{"bff", []string{"MediaClient", "Timeout"}, "media"},
				{"bff", []string{"DfsClient", "Timeout"}, "dfs"},
				{"media", []string{"Dfs", "Timeout"}, "dfs"},
			} {
				if got, need := timeout(c.client, c.path...), server(c.server); got < need {
					t.Errorf("%s %v = %d ms, below the %s server deadline %d ms", c.client, c.path, got, c.server, need)
				}
			}

			// session waits for bff at least as long as bff may take.
			proxies, _ := cfg("session")["BFFProxyClients"].(map[string]any)
			clients, _ := proxies["Clients"].([]any)
			if len(clients) == 0 {
				t.Fatal("session: no BFFProxyClients.Clients")
			}
			for i, c := range clients {
				m, _ := c.(map[string]any)
				if ms, _ := m["Timeout"].(int); ms < server("bff") {
					t.Errorf("session BFFProxyClients.Clients[%d].Timeout = %d ms, below the bff server deadline %d ms", i, ms, server("bff"))
				}
			}
		})
	}
}

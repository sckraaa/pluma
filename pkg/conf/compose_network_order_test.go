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
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestComposeServicesNetworkFirst keeps teamgram_net the first network of
// every service an overlay adds networks to. Docker gives a container its
// networks in name order, and a go-zero service listening on 0.0.0.0
// registers in etcd the address of its first interface: on a network that
// sorts before teamgram_net the session dials an address it cannot reach and
// every request through that service fails (bff on maps_net, 2026-11-17).
func TestComposeServicesNetworkFirst(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot, "docker-compose*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no compose files")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var compose struct {
			Services map[string]struct {
				Networks yaml.Node `yaml:"networks"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(data, &compose); err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		for name, s := range compose.Services {
			nets := networkNames(s.Networks)
			if !slices.Contains(nets, "teamgram_net") {
				continue
			}
			for _, n := range nets {
				if n < "teamgram_net" {
					t.Errorf("%s: service %s is on %s, which sorts before teamgram_net: it would register an address the session cannot reach",
						filepath.Base(f), name, n)
				}
			}
		}
	}
}

// networkNames reads the networks of a service in either compose form: a
// list of names or a map keyed by name.
func networkNames(n yaml.Node) []string {
	var names []string
	switch n.Kind {
	case yaml.SequenceNode:
		for _, c := range n.Content {
			names = append(names, c.Value)
		}
	case yaml.MappingNode:
		for i := 0; i < len(n.Content); i += 2 {
			names = append(names, n.Content[i].Value)
		}
	}
	return names
}

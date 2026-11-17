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
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestComposeServicesRegisteredAddress keeps a service that an overlay puts
// on a second network next to teamgram_net registering its teamgram_net
// address in etcd. go-zero registers the address of the container's first
// interface unless POD_IP names one, and Docker does not keep teamgram_net
// first: bff on the maps network registered its address there, the session
// could not dial it and every request through bff failed (2026-11-17). Such a
// service sets POD_IP to a fixed ipv4_address it has on teamgram_net.
func TestComposeServicesRegisteredAddress(t *testing.T) {
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
				Environment yaml.Node `yaml:"environment"`
				Networks    yaml.Node `yaml:"networks"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(data, &compose); err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		for name, s := range compose.Services {
			nets := networkNames(s.Networks)
			if len(nets) < 2 || !slices.Contains(nets, "teamgram_net") {
				continue
			}
			addr := teamgramAddress(s.Networks)
			if addr == "" {
				t.Errorf("%s: service %s is on %v without a fixed ipv4_address on teamgram_net", filepath.Base(f), name, nets)
				continue
			}
			if got := envValue(s.Environment, "POD_IP"); got != addr {
				t.Errorf("%s: service %s has POD_IP %q, not its teamgram_net address %q", filepath.Base(f), name, got, addr)
			}
		}
	}
}

// teamgramAddress is the ipv4_address of teamgram_net in the map form of a
// service's networks, or "".
func teamgramAddress(n yaml.Node) string {
	if n.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value != "teamgram_net" {
			continue
		}
		var attach struct {
			IPv4 string `yaml:"ipv4_address"`
		}
		if err := n.Content[i+1].Decode(&attach); err != nil {
			return ""
		}
		return attach.IPv4
	}
	return ""
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

// envValue reads a variable from a service's environment in either compose
// form: a map or a list of NAME=value.
func envValue(n yaml.Node, name string) string {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == name {
				return n.Content[i+1].Value
			}
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if v, ok := strings.CutPrefix(c.Value, name+"="); ok {
				return v
			}
		}
	}
	return ""
}

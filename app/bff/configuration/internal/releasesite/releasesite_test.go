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

package releasesite

import (
	"strings"
	"testing"
)

func TestParseOff(t *testing.T) {
	s, err := Parse("", "")
	if err != nil || s != nil {
		t.Fatalf("no site: %v, %v", s, err)
	}
	if s.ByCountry() || s.URL("RU") != "" || s.Main() != "" {
		t.Error("nil Sites names a site")
	}
}

func TestParseMainOnly(t *testing.T) {
	s, err := Parse(" https://pluma.chat/ ", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.ByCountry() {
		t.Error("no mirrors, yet by country")
	}
	for _, c := range []string{"RU", "DE", ""} {
		if got := s.URL(c); got != "https://pluma.chat" {
			t.Errorf("%q: %q", c, got)
		}
	}
}

func TestParseMirrors(t *testing.T) {
	s, err := Parse("https://pluma.chat", " RU, BY = HTTPS://Pluma.Sckra.dev/ ; KZ=https://kz.example:8443")
	if err != nil {
		t.Fatal(err)
	}
	if !s.ByCountry() || s.Main() != "https://pluma.chat" {
		t.Fatalf("by country %v, main %q", s.ByCountry(), s.Main())
	}
	for c, want := range map[string]string{
		"RU": "https://pluma.sckra.dev",
		"BY": "https://pluma.sckra.dev",
		"KZ": "https://kz.example:8443",
		"DE": "https://pluma.chat",
		"":   "https://pluma.chat",
	} {
		if got := s.URL(c); got != want {
			t.Errorf("%q: %q, want %q", c, got, want)
		}
	}
	if s, err = Parse("https://pluma.chat", "RU=https://pluma.sckra.dev:443"); err != nil || s.URL("RU") != "https://pluma.sckra.dev" {
		t.Errorf("port 443: %v, %v", s.URL("RU"), err)
	}
}

func TestParseRefuses(t *testing.T) {
	for name, c := range map[string]struct{ main, routes, want string }{
		"routes without a site": {"", "RU=https://pluma.sckra.dev", "without PLUMA_RELEASES_URL"},
		"no =":                  {"https://pluma.chat", "RU:https://pluma.sckra.dev", "want <countries>=<site>"},
		"http":                  {"https://pluma.chat", "RU=http://pluma.sckra.dev", "not an https:// URL"},
		"a path":                {"https://pluma.chat", "RU=https://pluma.sckra.dev/dl", "a path"},
		"a query":               {"https://pluma.chat", "RU=https://pluma.sckra.dev?x=1", "a query"},
		"no dot":                {"https://pluma.chat", "RU=https://mirror", "not a name"},
		"lower-case country":    {"https://pluma.chat", "ru=https://pluma.sckra.dev", "not a country code"},
		"star":                  {"https://pluma.chat", "*=https://pluma.sckra.dev", "not a country code"},
		"empty country":         {"https://pluma.chat", "RU,=https://pluma.sckra.dev", "not a country code"},
		"country twice":         {"https://pluma.chat", "RU=https://a.example;RU=https://b.example", "names country RU twice"},
		"empty record":          {"https://pluma.chat", "RU=https://a.example;", "want <countries>=<site>"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(c.main, c.routes)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err %v, want one with %q", err, c.want)
			}
		})
	}
}

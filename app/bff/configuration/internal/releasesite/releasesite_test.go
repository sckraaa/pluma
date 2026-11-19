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

// TestParseFiles: the files of /dl/ come from PLUMA_RELEASES_FILES_URL,
// or the site without it, from the caches of the records for their
// countries, and from the mirror of the site for a country only
// PLUMA_RELEASES_ROUTES names (task 115).
func TestParseFiles(t *testing.T) {
	s, err := ParseFiles(nil, "", "")
	if err != nil || s != nil {
		t.Fatalf("no site: %v, %v", s, err)
	}
	site := mustParse(t, "https://pluma.chat/", "")
	if s, err = ParseFiles(site, "", ""); err != nil || s.ByCountry() || s.URL("RU") != "https://pluma.chat" {
		t.Fatalf("site only: %v, %v", s, err)
	}
	site = mustParse(t, "https://pluma.chat", "RU,BY=https://pluma.sckra.dev;KZ=https://kz.example")
	s, err = ParseFiles(site, " HTTPS://Gk8.GcoreCDN.com/ ", "RU,BY=https://r1.sckra.dev;AM=https://am.example")
	if err != nil {
		t.Fatal(err)
	}
	if !s.ByCountry() || s.Main() != "https://gk8.gcorecdn.com" {
		t.Fatalf("by country %v, main %q", s.ByCountry(), s.Main())
	}
	for c, want := range map[string]string{
		"RU": "https://r1.sckra.dev",
		"BY": "https://r1.sckra.dev",
		"AM": "https://am.example",
		"KZ": "https://kz.example",
		"DE": "https://gk8.gcorecdn.com",
		"":   "https://gk8.gcorecdn.com",
	} {
		if got := s.URL(c); got != want {
			t.Errorf("%q: %q, want %q", c, got, want)
		}
	}
	if site.URL("RU") != "https://pluma.sckra.dev" || site.Main() != "https://pluma.chat" {
		t.Error("ParseFiles changed the sites it was given")
	}
	// Without settings of their own the files follow the site and its
	// mirrors, as before task 115.
	if s, err = ParseFiles(site, "", ""); err != nil || s.URL("RU") != "https://pluma.sckra.dev" || s.URL("DE") != "https://pluma.chat" {
		t.Errorf("no files settings: %v, %v", s, err)
	}
}

func mustParse(t *testing.T, main, routes string) *Sites {
	t.Helper()
	s, err := Parse(main, routes)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseFilesRefuses(t *testing.T) {
	site := mustParse(t, "https://pluma.chat", "")
	for name, c := range map[string]struct {
		site          *Sites
		files, routes string
		want          string
	}{
		"files without a site":  {nil, "https://cdn.example", "", "without PLUMA_RELEASES_URL"},
		"routes without a site": {nil, "", "RU=https://r1.example", "without PLUMA_RELEASES_URL"},
		"http files":            {site, "http://cdn.example", "", `PLUMA_RELEASES_FILES_URL: "http://cdn.example" is not an https:// URL`},
		"files with a path":     {site, "https://cdn.example/dl", "", "a path"},
		"a bad record":          {site, "", "RU:https://r1.example", "PLUMA_RELEASES_FILES_ROUTES record"},
		"country twice":         {site, "", "RU=https://a.example;RU=https://b.example", "PLUMA_RELEASES_FILES_ROUTES names country RU twice"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseFiles(c.site, c.files, c.routes)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err %v, want one with %q", err, c.want)
			}
		})
	}
}

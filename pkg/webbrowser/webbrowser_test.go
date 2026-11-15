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

package webbrowser

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	host253 := strings.Repeat("a.", 126) + "a"
	host254 := "b" + host253
	for _, tc := range []struct {
		in, domain, url string
	}{
		{"https://example.com/page?x=1", "example.com", "https://example.com/page?x=1"},
		{"  http://Example.COM  ", "example.com", "http://Example.COM"},
		{"example.com", "example.com", "example.com"},
		{"example.com/path", "example.com", "example.com/path"},
		{"//example.com/a", "example.com", "//example.com/a"},
		{"https://user:pw@sub.example.com:8443/x", "sub.example.com", "https://user:pw@sub.example.com:8443/x"},
		{"example.com:8080", "example.com", "example.com:8080"},
		{"https://example.com./", "example.com", "https://example.com./"},
		{"https://ПРИМЕР.рф/путь", "пример.рф", "https://ПРИМЕР.рф/путь"},
		{"xn--e1afmkfd.xn--p1ai", "xn--e1afmkfd.xn--p1ai", "xn--e1afmkfd.xn--p1ai"},
		{host253, host253, host253},
	} {
		got, err := NormalizeURL(tc.in)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got.Domain != tc.domain || got.URL != tc.url {
			t.Errorf("%q: %+v, want domain %q url %q", tc.in, got, tc.domain, tc.url)
		}
	}

	for _, in := range []string{
		"",
		"   ",
		"https://",
		"https:///path",
		"https://exa mple.com",
		"https://:443/",
		"https://example.com:port/",
		host254,
		"https://" + strings.Repeat(strings.Repeat("я", 63)+".", 4), // 255 characters
		"\xff\xfe",
	} {
		if got, err := NormalizeURL(in); !errors.Is(err, ErrURLInvalid) {
			t.Errorf("%q: %+v, %v, want ErrURLInvalid", in, got, err)
		}
	}

	// MaxURLLength counts characters, as the column does; a longer
	// address is kept as its domain, never refused.
	cyr := "https://example.com/" + strings.Repeat("ж", MaxURLLength-len("https://example.com/"))
	if got, err := NormalizeURL(cyr); err != nil || got.URL != cyr {
		t.Errorf("%d characters: %+v, %v", MaxURLLength, got, err)
	}
	long := "https://Example.com/" + strings.Repeat("x", MaxURLLength)
	if got, err := NormalizeURL(long); err != nil || got != (Exception{Domain: "example.com", URL: "example.com"}) {
		t.Errorf("%d characters: %+v, %v, want the domain as the address", len(long), got, err)
	}
}

func TestHash(t *testing.T) {
	base := Settings{
		OpenExternal:       false,
		DisplayCloseButton: true,
		External:           []Exception{{"a.com", "https://a.com"}, {"b.com", "b.com"}},
		InApp:              []Exception{{"c.com", "https://c.com/x"}},
	}
	clone := func() Settings {
		s := base
		s.External = append([]Exception(nil), base.External...)
		s.InApp = append([]Exception(nil), base.InApp...)
		return s
	}
	h := Hash(base)
	if h <= 0 {
		t.Fatalf("hash %d, want positive", h)
	}
	if Hash(clone()) != h {
		t.Error("the same settings give another hash")
	}
	if Hash(Defaults()) == 0 || Hash(Settings{}) == 0 {
		t.Error("a hash is 0")
	}

	for name, change := range map[string]func(*Settings){
		"open_external":        func(s *Settings) { s.OpenExternal = true },
		"display_close_button": func(s *Settings) { s.DisplayCloseButton = false },
		"order":                func(s *Settings) { s.External[0], s.External[1] = s.External[1], s.External[0] },
		"url":                  func(s *Settings) { s.InApp[0].URL = "https://c.com/y" },
		"list":                 func(s *Settings) { s.InApp, s.External = append(s.InApp, s.External[1]), s.External[:1] },
		"removed":              func(s *Settings) { s.InApp = nil },
		"domain and url split": func(s *Settings) { s.InApp[0] = Exception{"c.comhttps://c.com/", "x"} },
	} {
		s := clone()
		change(&s)
		if Hash(s) == h {
			t.Errorf("%s: the hash did not change", name)
		}
	}
}

func TestMemStore(t *testing.T) {
	var user int64 = 1000
	testStore(t, func() Store { return NewMemStore() }, func() int64 { user++; return user })
}

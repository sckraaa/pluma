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

package urlauth

import (
	"slices"
	"testing"
	"unicode/utf8"
)

// The set has 32 different emoji of one code point each, so that tdesktop
// draws every code (boxes/url_auth_box.cpp:99-104).
func TestMatchCodeSet(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range matchCodeSet {
		if utf8.RuneCountInString(e) != 1 {
			t.Errorf("%q is %d code points", e, utf8.RuneCountInString(e))
		}
		if seen[e] {
			t.Errorf("%q twice", e)
		}
		seen[e] = true
	}
	if len(seen) != 32 {
		t.Fatalf("%d emoji", len(seen))
	}
}

// Four different codes of the set, the right one among them, and every code
// of the set comes up in both roles.
func TestNewMatchCodes(t *testing.T) {
	shown, right := map[string]bool{}, map[string]bool{}
	for range 2000 {
		codes, r, err := NewMatchCodes()
		if err != nil {
			t.Fatal(err)
		}
		if len(codes) != MatchCodeCount {
			t.Fatalf("%d codes", len(codes))
		}
		if !slices.Contains(codes, r) {
			t.Fatalf("right %q not among %q", r, codes)
		}
		uniq := map[string]bool{}
		for _, c := range codes {
			if !slices.Contains(matchCodeSet[:], c) {
				t.Fatalf("%q is not of the set", c)
			}
			uniq[c] = true
			shown[c] = true
		}
		if len(uniq) != MatchCodeCount {
			t.Fatalf("repeated codes %q", codes)
		}
		right[r] = true
	}
	if len(shown) != len(matchCodeSet) || len(right) != len(matchCodeSet) {
		t.Fatalf("shown %d, right %d of %d", len(shown), len(right), len(matchCodeSet))
	}
}

func TestUserAgent(t *testing.T) {
	for _, c := range []struct{ ua, browser, platform string }{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", "Chrome 120", "Windows"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.2210.91", "Edge 120", "Windows"},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15", "Safari 17", "macOS"},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0", "Firefox 121", "Linux"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1", "Safari 17", "iOS"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/120.0.6099.119 Mobile/15E148 Safari/604.1", "Chrome 120", "iOS"},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.6099.144 Mobile Safari/537.36", "Chrome 120", "Android"},
		{"Mozilla/5.0 (Linux; Android 13; SM-S911B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/23.0 Chrome/115.0.0.0 Mobile Safari/537.36", "Samsung Internet 23", "Android"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 YaBrowser/24.1.0.0 Safari/537.36", "Yandex Browser 24", "Windows"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 OPR/106.0.0.0", "Opera 106", "Windows"},
		{"Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", "Chrome 120", "ChromeOS"},
		{"curl/8.4.0", "", ""},
		{"", "", ""},
	} {
		b, p := UserAgent(c.ua)
		if b != c.browser || p != c.platform {
			t.Errorf("%q: %q, %q; want %q, %q", c.ua, b, p, c.browser, c.platform)
		}
	}
}

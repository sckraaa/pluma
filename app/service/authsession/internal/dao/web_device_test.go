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

package dao

import "testing"

// User agents from MDN's User-Agent header reference and its Firefox user
// agent string reference (read 2026-10-30), unless said otherwise.
const (
	safariMac     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Safari/605.1.15"
	safariIPhone  = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1"
	chromeMac     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"
	chromeAndroid = "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Mobile Safari/537.36"
	edgeWindows   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36 Edg/143.0.0.0"
	edgeAndroid   = "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36 EdgA/141.0.0.0"
	operaMac      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36 OPR/124.0.0.0 (Edition developer)"
	operaAndroid  = "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Mobile Safari/537.36 OPR/92.0.0.0"
	firefoxLinux  = "Mozilla/5.0 (X11; Linux x86_64; rv:10.0) Gecko/20100101 Firefox/10.0"
	firefoxIPhone = "Mozilla/5.0 (iPhone; CPU iPhone OS 8_3 like Mac OS X) AppleWebKit/600.1.4 (KHTML, like Gecko) FxiOS/1.0 Mobile/12F69 Safari/600.1.4"
	// Built after MDN's words: Chrome "adds CriOS/<version> on iPhone",
	// Edge "EdgiOS/<version> on iPhone", with Safari's Version/ beside it.
	chromeIPhone = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/141.0.0.0 Mobile/15E148 Safari/604.1"
	edgeIPhone   = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 EdgiOS/141.0.0.0 Mobile/15E148 Safari/604.1"
	// Android's own WebKit browser, from MDN's browser detection guide.
	androidBrowser = "Mozilla/5.0 (Linux; U; Android 4.0.3; de-ch; HTC Sensation Build/IML74K) AppleWebKit/534.30 (KHTML, like Gecko) Version/4.0 Mobile Safari/534.30"
)

func TestDeviceModel(t *testing.T) {
	cases := []struct {
		langPack, model string
		want            string
	}{
		// Telegram showed Web A in Safari 26.x as "Safari 26".
		{"weba", safariMac, "Safari 26"},
		{"webk", safariMac, "Safari 26"},
		{"webk", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.1 Safari/605.1.15", "Safari 26"},
		{"weba", safariIPhone, "Safari 26"},

		// The other browsers by the same pattern, each by its own token
		// before Chrome's and Safari's.
		{"weba", chromeMac, "Chrome 143"},
		{"webk", chromeAndroid, "Chrome 143"},
		{"webk", chromeIPhone, "Chrome 141"},
		{"weba", edgeWindows, "Edge 143"},
		{"webk", edgeAndroid, "Edge 141"},
		{"webk", edgeIPhone, "Edge 141"},
		{"weba", operaMac, "Opera 124"},
		{"webk", operaAndroid, "Opera 92"},
		{"weba", firefoxLinux, "Firefox 10"},
		{"webk", firefoxIPhone, "Firefox 1"},

		// What is not a known browser's user agent stays as it came: a
		// device named in Web K's settings, an engine without a browser,
		// Android's own browser, a browser without a version.
		{"webk", "My MacBook", "My MacBook"},
		{"webk", "", ""},
		{"weba", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15 (KHTML, like Gecko)", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15 (KHTML, like Gecko)"},
		{"webk", androidBrowser, androidBrowser},
		{"weba", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/beta Safari/537.36", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/beta Safari/537.36"},
		{"weba", " " + safariMac, " " + safariMac},

		// The other apps keep their model.
		{"android", "Samsung SM-G950F", "Samsung SM-G950F"},
		{"tdesktop", safariMac, safariMac},
		{"web", safariMac, safariMac},
		{"", safariMac, safariMac},
	}
	for _, c := range cases {
		if got := deviceModel(c.langPack, c.model); got != c.want {
			t.Errorf("deviceModel(%q, %q) = %q, want %q", c.langPack, c.model, got, c.want)
		}
	}
}

func TestSystemVersion(t *testing.T) {
	cases := []struct {
		langPack, system, model string
		want                    string
	}{
		// Web K's navigator.platform, named as Web A names it.
		{"webk", "MacIntel", safariMac, "macOS"},
		{"webk", "Macintosh", safariMac, "macOS"},
		{"webk", "Win32", edgeWindows, "Windows"},
		{"webk", "Win64", edgeWindows, "Windows"},
		{"webk", "Linux x86_64", firefoxLinux, "Linux"},
		{"webk", "iPhone", safariIPhone, "iOS"},
		{"webk", "iPad", safariIPhone, "iOS"},
		// Android by the user agent, before the Linux of the platform.
		{"webk", "Linux armv81", chromeAndroid, "Android"},
		{"webk", "Linux armv81", "My phone", "Linux"},
		// What Web A does not know stays.
		{"webk", "Unknown Platform", safariMac, "Unknown Platform"},
		{"webk", "FreeBSD amd64", firefoxLinux, "FreeBSD amd64"},
		{"webk", "", "", ""},

		// Web A names its system itself; the other apps keep theirs.
		{"weba", "macOS", safariMac, "macOS"},
		{"weba", "MacIntel", safariMac, "MacIntel"},
		{"tdesktop", "Windows 11 x64", "Desktop", "Windows 11 x64"},
		// Android's API level as Telegram shows it.
		{"android", "SDK 36", "Samsung SM-G950F", "16 (36)"},
		{"android", "SDK 35", "Google Pixel 8", "15 (35)"},
		{"android", "SDK 32", "Google Pixel 6", "12 (32)"},
		{"android", "SDK 99", "Google Pixel 20", "SDK 99"},
		{"android", "SDK x", "Google Pixel 8", "SDK x"},
		{"android", "15", "Google Pixel 8", "15"},
	}
	for _, c := range cases {
		if got := systemVersion(c.langPack, c.system, c.model); got != c.want {
			t.Errorf("systemVersion(%q, %q, %q) = %q, want %q", c.langPack, c.system, c.model, got, c.want)
		}
	}
}

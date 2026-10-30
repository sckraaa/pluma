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

func TestPlatform(t *testing.T) {
	cases := []struct {
		langPack, systemVersion string
		want                    string
	}{
		// The mobile and macOS apps: by the app, whatever the system says.
		{"android", "SDK 36", "Android"},
		{"ios", "18.1", "iOS"},
		{"macos", "macOS 15.1", "macOS"},

		// Telegram Desktop: by its Platform::SystemVersionPretty.
		{"tdesktop", "Windows 11", "Windows"},
		{"tdesktop", "Windows 10", "Windows"},
		{"tdesktop", "macOS 15.1", "macOS"},
		{"tdesktop", "OS X 10.11", "macOS"},
		{"tdesktop", "OS X", "macOS"},
		{"tdesktop", "Linux KDE Wayland glibc 2.40", "Linux"},
		// Its Ubuntu icon comes from "ubuntu" or "unity" in system_version
		// (XDG_CURRENT_DESKTOP "ubuntu:GNOME", split at ':'), which clients
		// look for before "linux".
		{"tdesktop", "Linux ubuntu GNOME Wayland glibc 2.39", "Linux"},
		{"tdesktop", "FreeBSD 14.1 KDE X11", ""},
		{"tdesktop", "", ""},

		// The web apps: the browser in device_model decides.
		{"webk", "macOS", ""},
		{"weba", "Windows", ""},
		{"", "Linux", ""},

		// An app this server does not know.
		{"android_x", "SDK 36", ""},
	}
	for _, c := range cases {
		if got := platform(c.langPack, c.systemVersion); got != c.want {
			t.Errorf("platform(%q, %q) = %q, want %q", c.langPack, c.systemVersion, got, c.want)
		}
	}
}

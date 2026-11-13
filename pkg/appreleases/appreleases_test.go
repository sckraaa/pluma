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

package appreleases

import (
	"strings"
	"testing"
)

// TestAndroidNewer follows versionBiggerOrEqual of Android
// (SharedConfig.java:811-824 at aac1efcb): only the parts both versions
// have count.
func TestAndroidNewer(t *testing.T) {
	for _, c := range []struct {
		installed, offered string
		newer              bool
	}{
		{"12.9.1.4", "12.9.1.5", true},
		{"12.9.1.5", "12.9.1.5", false},
		{"12.9.1.10", "12.9.1.9", false},
		{"12.9.1.9", "12.9.1.10", true},
		{"12.9.1.5", "12.10.0.1", true},
		{"12.9.1", "12.9.1.5", false}, // Android compares 3 parts: equal
		{"12.9.0", "12.9.1.1", true},
		{"12.9.1.x", "12.9.1.5", false},
	} {
		if got := AndroidNewer(c.installed, c.offered); got != c.newer {
			t.Errorf("AndroidNewer(%q, %q) = %v, want %v", c.installed, c.offered, got, c.newer)
		}
	}
}

func TestAndroidInstalled(t *testing.T) {
	for _, c := range []struct {
		appVersion, name string
		ok               bool
	}{
		{"12.9.1.5 (69791509)", "12.9.1.5", true},
		{"12.9.1 (69799) beta", "12.9.1", true},
		{"App version unknown", "", false},
		{"12.9.1-pluma (1)", "", false},
	} {
		name, ok := AndroidInstalled(c.appVersion)
		if name != c.name || ok != c.ok {
			t.Errorf("AndroidInstalled(%q) = %q, %v", c.appVersion, name, ok)
		}
	}
}

func TestValidVersion(t *testing.T) {
	for _, c := range []struct {
		platform, version string
		ok                bool
	}{
		{Android, "12.9.1.5", true},
		{Android, "12.9.1", false},
		{Android, "12.9.1.5-x", false},
		{IOS, "12.9.2", true},
		{IOS, "12.9.2.1", false},
		{MacOS, "7.0.9.00057", true},
		{MacOS, "7000066", false},
		{Windows, "0", false},
		{Linux, "7.0.9", false},
		{"web", "1", false},
	} {
		if got := ValidVersion(c.platform, c.version); got != c.ok {
			t.Errorf("ValidVersion(%q, %q) = %v", c.platform, c.version, got)
		}
	}
}

// TestCanBeMandatory: only the apps that take an update from
// help.getAppUpdate have mandatory releases.
func TestCanBeMandatory(t *testing.T) {
	for p, want := range map[string]bool{Android: true, IOS: true, MacOS: false, Windows: false, Linux: false, "web": false} {
		if got := CanBeMandatory(p); got != want {
			t.Errorf("CanBeMandatory(%q) = %v", p, got)
		}
	}
}

func TestNotes(t *testing.T) {
	r := &Release{NotesEn: "en", NotesRu: "ru"}
	if r.Notes("ru") != "ru" || r.Notes("en") != "en" || r.Notes("de") != "en" {
		t.Errorf("notes by language: %q %q %q", r.Notes("ru"), r.Notes("en"), r.Notes("de"))
	}
	if only := (&Release{NotesRu: "ru"}); only.Notes("en") != "ru" {
		t.Errorf("the other language when one is missing: %q", only.Notes("en"))
	}
}

func TestIOSInstalled(t *testing.T) {
	for _, c := range []struct {
		appVersion string
		build      int32
		adhoc, ok  bool
	}{
		{"12.9.2 (57) adhoc", 57, true, true},
		{"12.9.2 (57)", 57, false, true},
		{"12.9.2 (57) ", 57, false, true}, // MtProtoKit's form, an empty suffix
		{"12.9.2", 0, false, false},
		{"12.9.2 (x) adhoc", 0, false, false},
	} {
		build, adhoc, ok := IOSInstalled(c.appVersion)
		if build != c.build || adhoc != c.adhoc || ok != c.ok {
			t.Errorf("IOSInstalled(%q) = %d %v %v", c.appVersion, build, adhoc, ok)
		}
	}
}

func TestUpdaterKeys(t *testing.T) {
	for _, c := range []struct {
		platform string
		build    int32
		name     string
		want     string // keys joined, "" for not a package
	}{
		{MacOS, 57, "tmacupd57", "mac armac"},
		{MacOS, 57, "tarmacupd57", "armac"},
		{Windows, 57, "tx64upd57", "win64"},
		{Windows, 57, "tarm64upd57", "winarm"},
		{Windows, 57, "tupdate57", "win"},
		{Linux, 57, "tlinuxupd57", "linux"},
		{Linux, 57, "tlinuxupd58", ""}, // another build
		{Linux, 57, "tmacupd57", ""},   // another system
		{MacOS, 57, "Pluma-7.0.9.00057.zip", ""},
		{MacOS, 57, "tmacupd57_x", ""},
	} {
		keys, ok := UpdaterKeys(c.platform, c.build, c.name)
		if got := strings.Join(keys, " "); ok != (c.want != "") || got != c.want {
			t.Errorf("%s %d %s: %q %v", c.platform, c.build, c.name, got, ok)
		}
	}
}

func TestUpdaterLinks(t *testing.T) {
	mac := &Release{Platform: MacOS, Version: "7.0.9.00057", Build: 57, Files: []File{
		{Name: "tarmacupd57", Kind: KindUpdate},
		{Name: "tmacupd57", Kind: KindUpdate},
		{Name: "Pluma.zip", Kind: KindInstaller},
	}}
	win := &Release{Platform: Windows, Version: "7.0.9.00056", Build: 56, Files: []File{{Name: "tx64upd56", Kind: KindUpdate}}}
	got := UpdaterLinks([]*Release{mac, win})
	if len(got) != 3 || got["mac"].Name != "tmacupd57" || got["armac"].Name != "tarmacupd57" ||
		got["win64"].Release != win || got["win64"].Name != "tx64upd56" {
		t.Errorf("%+v", got)
	}
	universal := UpdaterLinks([]*Release{{Platform: MacOS, Version: "7.0.9.00057", Build: 57, Files: []File{{Name: "tmacupd57", Kind: KindUpdate}}}})
	if universal["armac"].Name != "tmacupd57" || universal["mac"].Name != "tmacupd57" {
		t.Errorf("universal %+v", universal)
	}
}

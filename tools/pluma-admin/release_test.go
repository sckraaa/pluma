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

package main

import (
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/appreleases"
)

// TestReleaseUsage: the release commands check their arguments before
// they read the config, and mandatory/optional refuse the desktop
// platforms, whose updater has no mandatory updates.
func TestReleaseUsage(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		{"release"},
		{"release", "publish"},
		{"release", "withdraw", "android"},
		{"release", "restore", "android", "x"},
		{"release", "mandatory"},
		{"release", "mandatory", "android"},
		{"release", "mandatory", "android", "12.9.1.7"},
		{"release", "mandatory", "symbian", "7"},
		{"release", "optional", "ios", "7", "extra"},
	} {
		if code, _, _ := h.run(args...); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
	for _, p := range []string{appreleases.MacOS, appreleases.Windows, appreleases.Linux} {
		for _, cmd := range []string{"mandatory", "optional"} {
			if code, _, errOut := h.run("release", cmd, p, "7"); code != 1 || !strings.Contains(errOut, "no mandatory updates") {
				t.Errorf("%s %s: exit %d %q", cmd, p, code, errOut)
			}
		}
	}
}

// TestMandatoryColumn: release list says whether the current release of
// the apps that take mandatory updates is one, "-" for the desktop app.
func TestMandatoryColumn(t *testing.T) {
	for _, c := range []struct {
		rel  appreleases.Release
		want string
	}{
		{appreleases.Release{Platform: appreleases.Android, CanNotSkip: true}, "yes"},
		{appreleases.Release{Platform: appreleases.Android}, "no"},
		{appreleases.Release{Platform: appreleases.IOS, CanNotSkip: true}, "yes"},
		{appreleases.Release{Platform: appreleases.MacOS, CanNotSkip: true}, "-"},
		{appreleases.Release{Platform: appreleases.Linux}, "-"},
	} {
		if got := mandatoryColumn(&c.rel); got != c.want {
			t.Errorf("%s can_not_skip %v: %q, want %q", c.rel.Platform, c.rel.CanNotSkip, got, c.want)
		}
	}
}

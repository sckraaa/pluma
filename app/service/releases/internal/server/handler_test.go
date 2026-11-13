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

package server

import (
	"testing"

	"github.com/teamgram/teamgram-server/pkg/appreleases"
)

// TestCanNotSkip: an upload is mandatory with can_not_skip=1 on Android
// and iOS only; a value other than 1, 0 or none is refused, not taken for
// optional.
func TestCanNotSkip(t *testing.T) {
	for _, c := range []struct {
		platform, value string
		want, ok        bool
	}{
		{appreleases.Android, "1", true, true},
		{appreleases.IOS, "1", true, true},
		{appreleases.Android, "", false, true},
		{appreleases.IOS, "0", false, true},
		{appreleases.MacOS, "0", false, true},
		{appreleases.Linux, "", false, true},
		{appreleases.MacOS, "1", false, false},
		{appreleases.Windows, "1", false, false},
		{appreleases.Android, "true", false, false},
		{appreleases.IOS, "yes", false, false},
		{appreleases.Android, "2", false, false},
	} {
		got, err := canNotSkip(c.platform, c.value)
		if got != c.want || (err == nil) != c.ok {
			t.Errorf("canNotSkip(%q, %q) = %v, %v", c.platform, c.value, got, err)
		}
	}
}

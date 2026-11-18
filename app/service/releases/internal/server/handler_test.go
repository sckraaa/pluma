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
	"net/http/httptest"
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

// TestFilesBase: the latest link asked on the site's own name sends the
// browser to the CDN in front of /dl/ (task 115); asked on any other name
// (the mirror, the CDN, the Russian node's cache) it stays on that name,
// and so does every link without a CDN.
func TestFilesBase(t *testing.T) {
	h := &Handler{PublicURL: "https://pluma.example", FilesURL: "https://cdn.example"}
	for host, want := range map[string]string{
		"pluma.example":        "https://cdn.example",
		"Pluma.Example:443":    "https://cdn.example",
		"www.pluma.example":    "",
		"pluma.mirror.example": "",
		"cdn.example":          "",
		"origin.example":       "",
		"":                     "",
	} {
		r := httptest.NewRequest("GET", "/dl/linux/latest", nil)
		r.Host = host
		if got := h.filesBase(r); got != want {
			t.Errorf("%q: %q, want %q", host, got, want)
		}
	}
	h.FilesURL = ""
	r := httptest.NewRequest("GET", "/dl/linux/latest", nil)
	r.Host = "pluma.example"
	if got := h.filesBase(r); got != "" {
		t.Errorf("no CDN: %q", got)
	}
}

func TestCheckFilesURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                            "",
		" https://Gk8.GcoreCDN.com/ ": "https://gk8.gcorecdn.com",
		"https://cdn.example:8443":    "https://cdn.example:8443",
	} {
		if got, err := checkFilesURL(in); err != nil || got != want {
			t.Errorf("%q: %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"http://cdn.example", "https://cdn.example/dl", "https://cdn.example?x=1", "cdn.example", "https://"} {
		if got, err := checkFilesURL(in); err == nil {
			t.Errorf("%q taken as %q", in, got)
		}
	}
}

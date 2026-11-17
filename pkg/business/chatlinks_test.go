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

package business

import "testing"

// The slug the clients name a link by: the whole link (tdesktop, Android,
// iOS, Web K send it to edit and delete), the app link, or the slug alone.
func TestSlugOf(t *testing.T) {
	const slug = "AbCdEfGh12345678"
	for _, c := range []struct{ in, want string }{
		{slug, slug},
		{" " + slug + " ", slug},
		{"https://pluma.chat/m/" + slug, slug},
		{"https://pluma.chat/m/" + slug + "/", slug},
		{"https://pluma.chat/m/" + slug + "?x=1", slug},
		{"pluma.chat/m/" + slug, slug},
		{"https://t.me/m/" + slug, slug},
		{"tg://message?slug=" + slug, slug},
		{"plm://message?slug=" + slug + "&x=1", slug},
		{"https://pluma.chat/m/", ""},
		{"", ""},
	} {
		if got := SlugOf(c.in); got != c.want {
			t.Errorf("SlugOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

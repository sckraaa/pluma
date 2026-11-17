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

package appconfig

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
)

// emojiesOf is gif_search_emojies of the config, which must be an array of
// strings.
func emojiesOf(t *testing.T, c *AppConfig) []string {
	t.Helper()
	v := field(t, c, gifSearchEmojiesKey)
	if v.GetPredicateName() != mtproto.Predicate_jsonArray {
		t.Fatalf("%s is %s, want an array", gifSearchEmojiesKey, v.GetPredicateName())
	}
	out := []string{}
	for _, e := range v.GetValue_VECTORJSONVALUE() {
		if e.GetPredicateName() != mtproto.Predicate_jsonString {
			t.Fatalf("%s has a %s", gifSearchEmojiesKey, e.GetPredicateName())
		}
		out = append(out, e.GetValue_STRING())
	}
	return out
}

// The emoji categories are set in place of the file's empty list, with a
// hash of their own, and the file's config is not changed.
func TestWithGifSearchEmojies(t *testing.T) {
	base := mustParse(t, `{"a": 1, "gif_search_emojies": [], "b": true}`)
	got := base.WithGifSearchEmojies([]string{"👍", "😂"})
	if e := emojiesOf(t, got); !slices.Equal(e, []string{"👍", "😂"}) {
		t.Errorf("gif_search_emojies %q", e)
	}
	if k := keyList(got); k != "a,gif_search_emojies,b" {
		t.Errorf("keys %s, want those of the file in its order", k)
	}
	if got.Hash == base.Hash || got.Hash == 0 {
		t.Errorf("hash %d, the file's %d: the clients would keep the config without the categories", got.Hash, base.Hash)
	}
	if again := base.WithGifSearchEmojies([]string{"👍", "😂"}); again.Hash != got.Hash {
		t.Errorf("hash %d, then %d: the clients would fetch the same config again", got.Hash, again.Hash)
	}
	if other := base.WithGifSearchEmojies([]string{"👍"}); other.Hash == got.Hash {
		t.Error("another list has the same hash")
	}
	if e := emojiesOf(t, base); len(e) != 0 {
		t.Errorf("the file's config has %q", e)
	}
	if r := got.Reply(base.Hash); r.GetPredicateName() != mtproto.Predicate_help_appConfig {
		t.Errorf("a client with the file's config gets %s, want help.appConfig", r.GetPredicateName())
	}
}

// A file without the key gets it at the end; the translation switches and
// the login domains of bots set over it keep the categories in the config
// and in the hash.
func TestWithGifSearchEmojiesAndTheRest(t *testing.T) {
	base := mustParse(t, `{"a": 1, "url_auth_domains": []}`)
	gifs := base.WithGifSearchEmojies([]string{"👍"})
	if k := keyList(gifs); k != "a,url_auth_domains,gif_search_emojies" {
		t.Errorf("keys %s", k)
	}
	got := gifs.WithoutTranslation().WithURLAuthDomains([]string{"one.example"})
	if e := emojiesOf(t, got); !slices.Equal(e, []string{"👍"}) {
		t.Errorf("gif_search_emojies %q", e)
	}
	wantString(t, got, "translations_manual_enabled", "disabled")
	without := base.WithoutTranslation().WithURLAuthDomains([]string{"one.example"})
	if without.Hash == got.Hash {
		t.Error("the categories are not in the hash")
	}
}

// The shipped file has no categories, the config of a server without a
// search; with one they are the registry's list, Android's default.
func TestShippedConfigGifSearch(t *testing.T) {
	c, err := Load(filepath.Join(root, "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if e := emojiesOf(t, c); len(e) != 0 {
		t.Errorf("the file has gif_search_emojies %q, want none without a search", e)
	}
	on := c.WithGifSearchEmojies(inlinebots.GifSearchEmojies)
	if e := emojiesOf(t, on); !slices.Equal(e, inlinebots.GifSearchEmojies) || len(e) != 10 {
		t.Errorf("gif_search_emojies %q", e)
	}
	if keyList(on) != keyList(c) {
		t.Error("the config with the categories has other keys than the file")
	}
}

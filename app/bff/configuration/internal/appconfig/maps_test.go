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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

const testMapsURL = "https://maps.example/Ab0-_cdefghijklmnopqrstuvwxyz1234"

// stringMap is an object of strings, as tdesktop reads tdesktop_config_map:
// a value of another type fails the test.
func stringMap(t *testing.T, v *mtproto.JSONValue) map[string]string {
	t.Helper()
	if v.GetPredicateName() != mtproto.Predicate_jsonObject {
		t.Fatalf("%s, want an object", v.GetPredicateName())
	}
	out := map[string]string{}
	for _, e := range v.GetValue_VECTORJSONOBJECTVALUE() {
		if e.GetValue().GetPredicateName() != mtproto.Predicate_jsonString {
			t.Fatalf("%s is a %s: tdesktop would drop the whole object", e.GetKey(), e.GetValue().GetPredicateName())
		}
		out[e.GetKey()] = e.GetValue().GetValue_STRING()
	}
	return out
}

// stringList is an array of strings.
func stringList(t *testing.T, v *mtproto.JSONValue) []string {
	t.Helper()
	if v.GetPredicateName() != mtproto.Predicate_jsonArray {
		t.Fatalf("%s, want an array", v.GetPredicateName())
	}
	out := []string{}
	for _, e := range v.GetValue_VECTORJSONVALUE() {
		if e.GetPredicateName() != mtproto.Predicate_jsonString {
			t.Fatalf("an element is a %s", e.GetPredicateName())
		}
		out = append(out, e.GetValue_STRING())
	}
	return out
}

// A file without the keys gets them at the end, with a hash of their own:
// the Desktop picker by the maps tokens, Android by its own keys.
func TestWithMaps(t *testing.T) {
	base := mustParse(t, `{"a": 1}`)
	got := base.WithMaps(testMapsURL, []string{"ru", "en"})
	if k := keyList(got); k != "a,tdesktop_config_map,pluma_maps_url,pluma_maps_styles" {
		t.Errorf("keys %s", k)
	}
	m := stringMap(t, field(t, got, "tdesktop_config_map"))
	if len(m) != 2 || m["maps"] != testMapsURL || m["bmaps"] != testMapsURL {
		t.Errorf("tdesktop_config_map %v, want maps and bmaps, no geocoder", m)
	}
	wantString(t, got, "pluma_maps_url", testMapsURL)
	if s := stringList(t, field(t, got, "pluma_maps_styles")); !slices.Equal(s, []string{"ru", "en"}) {
		t.Errorf("pluma_maps_styles %q", s)
	}
	if got.Hash == base.Hash || got.Hash == 0 {
		t.Errorf("hash %d, the file's %d: the clients would keep the config without maps", got.Hash, base.Hash)
	}
	if again := base.WithMaps(testMapsURL, []string{"ru", "en"}); again.Hash != got.Hash {
		t.Errorf("hash %d, then %d: the clients would fetch the same config again", got.Hash, again.Hash)
	}
	for name, other := range map[string]*AppConfig{
		"another key":    base.WithMaps("https://maps.example/Zz0-_cdefghijklmnopqrstuvwxyz1234", []string{"ru", "en"}),
		"another style":  base.WithMaps(testMapsURL, []string{"ru"}),
		"another order":  base.WithMaps(testMapsURL, []string{"en", "ru"}),
		"no translation": base.WithMaps(testMapsURL, []string{"ru", "en"}).WithoutTranslation(),
	} {
		if other.Hash == got.Hash {
			t.Errorf("%s: the same hash", name)
		}
	}
	if k := keyList(base); k != "a" {
		t.Errorf("the file's config changed: keys %s", k)
	}
	if r := got.Reply(base.Hash); r.GetPredicateName() != mtproto.Predicate_help_appConfig {
		t.Errorf("a client with the file's config gets %s, want help.appConfig", r.GetPredicateName())
	}
}

// The keys of the file stay in place; the other keys of its
// tdesktop_config_map are kept and its maps tokens replaced, without
// changing the file's config.
func TestWithMapsInPlace(t *testing.T) {
	base := mustParse(t, `{"tdesktop_config_map": {"other": "x", "maps": "old"}, "pluma_maps_url": "", "b": true, "pluma_maps_styles": []}`)
	got := base.WithMaps(testMapsURL, []string{"en"})
	if k := keyList(got); k != "tdesktop_config_map,pluma_maps_url,b,pluma_maps_styles" {
		t.Errorf("keys %s, want those of the file in its order", k)
	}
	m := stringMap(t, field(t, got, "tdesktop_config_map"))
	if len(m) != 3 || m["other"] != "x" || m["maps"] != testMapsURL || m["bmaps"] != testMapsURL {
		t.Errorf("tdesktop_config_map %v", m)
	}
	if m := stringMap(t, field(t, base, "tdesktop_config_map")); len(m) != 2 || m["maps"] != "old" {
		t.Errorf("the file's tdesktop_config_map changed: %v", m)
	}
	wantString(t, base, "pluma_maps_url", "")
	if s := stringList(t, field(t, got, "pluma_maps_styles")); !slices.Equal(s, []string{"en"}) {
		t.Errorf("pluma_maps_styles %q", s)
	}
	// A value of another type is replaced by the object.
	odd := mustParse(t, `{"tdesktop_config_map": 1}`).WithMaps(testMapsURL, []string{"en"})
	if m := stringMap(t, field(t, odd, "tdesktop_config_map")); len(m) != 2 {
		t.Errorf("tdesktop_config_map %v", m)
	}
}

// The other changes of the config at the start and later keep the maps in
// the config and in the hash.
func TestWithMapsAndTheRest(t *testing.T) {
	base := mustParse(t, `{"a": 1, "url_auth_domains": []}`)
	got := base.WithoutTranslation().WithGifSearchEmojies([]string{"👍"}).WithMaps(testMapsURL, []string{"ru"}).
		WithURLAuthDomains([]string{"one.example"})
	wantString(t, got, "pluma_maps_url", testMapsURL)
	wantString(t, got, "translations_manual_enabled", "disabled")
	without := base.WithoutTranslation().WithGifSearchEmojies([]string{"👍"}).WithURLAuthDomains([]string{"one.example"})
	if without.Hash == got.Hash {
		t.Error("the maps are not in the hash")
	}
}

// The shipped file has none of the keys, the config of a server without
// public tiles, and docs/ops/app-config.md describes each of them.
func TestShippedConfigMaps(t *testing.T) {
	c, err := Load(filepath.Join(root, "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range c.Config.GetValue_VECTORJSONOBJECTVALUE() {
		switch v.GetKey() {
		case tdesktopConfigMapKey, plumaMapsURLKey, plumaMapsStylesKey:
			t.Errorf("the file has %s: a server without MAPS_PUBLIC_URL would hand it out", v.GetKey())
		}
	}
	doc, err := os.ReadFile(filepath.Join(root, "docs", "ops", "app-config.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keysOf(c.WithMaps(testMapsURL, []string{"ru"})) {
		if !strings.Contains(string(doc), "| `"+key+"` |") {
			t.Errorf("%s is not in the table of docs/ops/app-config.md", key)
		}
	}
}

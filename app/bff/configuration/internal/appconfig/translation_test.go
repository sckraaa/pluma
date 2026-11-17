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
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func keyList(c *AppConfig) string { return strings.Join(keysOf(c), ",") }

func wantString(t *testing.T, c *AppConfig, key, want string) {
	t.Helper()
	if v := field(t, c, key); v.GetPredicateName() != mtproto.Predicate_jsonString || v.GetValue_STRING() != want {
		t.Errorf("%s %v, want %q", key, v, want)
	}
}

// The file offers the translation; a server without an engine says it is
// off, in place of the file's values, with a hash of its own.
func TestWithoutTranslation(t *testing.T) {
	base := mustParse(t, `{"a": 1, "translations_manual_enabled": "enabled", "b": true, "translations_auto_enabled": "enabled"}`)
	got := base.WithoutTranslation()
	wantString(t, got, "translations_manual_enabled", "disabled")
	wantString(t, got, "translations_auto_enabled", "disabled")
	if k := keyList(got); k != "a,translations_manual_enabled,b,translations_auto_enabled" {
		t.Errorf("keys %s, want those of the file in its order", k)
	}
	if got.Hash == base.Hash || got.Hash == 0 {
		t.Errorf("hash %d, the file's %d: the clients would keep the config that offers the translation", got.Hash, base.Hash)
	}
	if again := base.WithoutTranslation(); again.Hash != got.Hash {
		t.Errorf("hash %d, then %d: the clients would fetch the same config again", got.Hash, again.Hash)
	}
	// The file itself is not changed.
	wantString(t, base, "translations_manual_enabled", "enabled")
	wantString(t, base, "translations_auto_enabled", "enabled")
	if r := got.Reply(base.Hash); r.GetPredicateName() != mtproto.Predicate_help_appConfig {
		t.Errorf("a client with the file's config gets %s, want help.appConfig", r.GetPredicateName())
	}
}

// Android takes "enabled" without the key: a file without it still gets
// both set to "disabled".
func TestWithoutTranslationAddsKeys(t *testing.T) {
	base := mustParse(t, `{"a": 1}`)
	got := base.WithoutTranslation()
	if k := keyList(got); k != "a,translations_manual_enabled,translations_auto_enabled" {
		t.Errorf("keys %s", k)
	}
	wantString(t, got, "translations_manual_enabled", "disabled")
	wantString(t, got, "translations_auto_enabled", "disabled")
	if got.Hash == base.Hash {
		t.Error("the hash did not change")
	}
}

// The login domains of bots are set over the config without translation:
// both changes reach the clients and neither hides the other in the hash.
func TestWithoutTranslationThenDomains(t *testing.T) {
	base := mustParse(t, `{"translations_manual_enabled": "enabled", "translations_auto_enabled": "enabled", "url_auth_domains": []}`)
	off := base.WithoutTranslation()
	domains := []string{"one.example"}
	got := off.WithURLAuthDomains(domains)
	wantString(t, got, "translations_manual_enabled", "disabled")
	wantString(t, got, "translations_auto_enabled", "disabled")
	if d := field(t, got, "url_auth_domains").GetValue_VECTORJSONVALUE(); len(d) != 1 || d[0].GetValue_STRING() != "one.example" {
		t.Errorf("url_auth_domains %v", d)
	}
	for name, other := range map[string]*AppConfig{
		"the file":                base,
		"without translation":     off,
		"the file with a domain":  base.WithURLAuthDomains(domains),
		"the file with no domain": base.WithURLAuthDomains(nil),
	} {
		if other.Hash == got.Hash {
			t.Errorf("hash %d is that of %s", got.Hash, name)
		}
	}
}

// The shipped file offers the translation: a server with an engine answers
// with it as it is (ADR 0064, section 9).
func TestShippedConfigTranslation(t *testing.T) {
	c, err := Load(filepath.Join(root, "teamgramd", "bin", "app_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantString(t, c, "translations_manual_enabled", "enabled")
	wantString(t, c, "translations_auto_enabled", "enabled")
	off := c.WithoutTranslation()
	wantString(t, off, "translations_manual_enabled", "disabled")
	wantString(t, off, "translations_auto_enabled", "disabled")
	if keyList(off) != keyList(c) {
		t.Error("the config without translation has other keys than the file")
	}
}

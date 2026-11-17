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

package staticmap

import (
	"strings"
	"testing"
)

const testPublicKey = "Ab0-_cdefghijklmnopqrstuvwxyz1234"

func TestPublic(t *testing.T) {
	p, err := Config{}.Public()
	if err != nil || p.Enabled() {
		t.Fatalf("no PublicURL: %+v, %v; want no public tiles", p, err)
	}
	// The public tiles do not need the renderer, and the renderer's
	// settings do not stop them.
	p, err = Config{PublicURL: " https://maps.example/" + testPublicKey + " "}.Public()
	if err != nil {
		t.Fatal(err)
	}
	if !p.Enabled() || p.URL != "https://maps.example/"+testPublicKey || strings.Join(p.Styles, ",") != DefaultStyles {
		t.Fatalf("public %+v", p)
	}
	p, err = Config{PublicURL: "https://maps.example:8443/" + testPublicKey, Styles: "en, uk"}.Public()
	if err != nil {
		t.Fatal(err)
	}
	if p.URL != "https://maps.example:8443/"+testPublicKey || strings.Join(p.Styles, ",") != "en,uk" {
		t.Fatalf("public %+v", p)
	}
	if err := (Config{PublicURL: "https://maps.example/" + testPublicKey}).Validate(); err != nil {
		t.Errorf("Validate refused the public tiles without the renderer: %v", err)
	}

	for name, raw := range map[string]string{
		"http":           "http://maps.example/" + testPublicKey,
		"no scheme":      "maps.example/" + testPublicKey,
		"no key":         "https://maps.example",
		"root":           "https://maps.example/",
		"trailing slash": "https://maps.example/" + testPublicKey + "/",
		"two segments":   "https://maps.example/" + testPublicKey + "/styles",
		"short key":      "https://maps.example/abcdefghijklmno",
		"long key":       "https://maps.example/" + strings.Repeat("a", 129),
		"bad character":  "https://maps.example/" + testPublicKey + "+",
		"escaped":        "https://maps.example/" + testPublicKey + "%2F",
		"query":          "https://maps.example/" + testPublicKey + "?a=b",
		"empty query":    "https://maps.example/" + testPublicKey + "?",
		"fragment":       "https://maps.example/" + testPublicKey + "#x",
		"empty fragment": "https://maps.example/" + testPublicKey + "#",
		"credentials":    "https://u:p@maps.example/" + testPublicKey,
		"no host":        "https:///" + testPublicKey,
		"port only":      "https://:443/" + testPublicKey,
	} {
		c := Config{PublicURL: raw}
		_, err := c.Public()
		if err == nil {
			t.Errorf("%s: Public accepted %q", name, raw)
			continue
		}
		if strings.Contains(err.Error(), testPublicKey) {
			t.Errorf("%s: the error names the key: %v", name, err)
		}
		if c.Validate() == nil {
			t.Errorf("%s: Validate accepted %q", name, raw)
		}
	}
	if _, err := (Config{PublicURL: "https://maps.example/" + testPublicKey, Styles: "ru,ru"}).Public(); err == nil {
		t.Error("Public accepted a style listed twice")
	}
}

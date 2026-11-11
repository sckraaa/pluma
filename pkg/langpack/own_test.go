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

package langpack

import (
	"fmt"
	"testing"
)

// withOwn replaces Pluma's own strings for one test.
func withOwn(t *testing.T, data string) {
	t.Helper()
	own, err := loadOwn([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	saved := ownStrings
	ownStrings = own
	t.Cleanup(func() { ownStrings = saved })
}

func TestOwnJSONIsValid(t *testing.T) {
	if _, err := loadOwn(ownJSON); err != nil {
		t.Fatal(err)
	}
}

func TestOwnRejects(t *testing.T) {
	for _, data := range []string{
		`not json`,
		`{"IOS": {"k": {"en": "x"}}}`,
		`{"ios": {"k": {"ru": "x"}}}`,
		`{"ios": {"": {"en": "x"}}}`,
		`{"ios": {"k": {"en": "x", "RU": "y"}}}`,
		`{"ios": {"k": {"en": "x", "ru": ""}}}`,
	} {
		if _, err := loadOwn([]byte(data)); err == nil {
			t.Errorf("%s: accepted", data)
		}
	}
}

// A language gets its own text, the base language's, or the English one.
func TestOwnForFallsBack(t *testing.T) {
	withOwn(t, `{"ios": {
		"Pluma.A": {"en": "Check Telegram", "ru": "Проверьте Telegram", "pt-br": "Verifique o Telegram"},
		"Pluma.B": {"en": "Send again", "pt": "Reenviar"}
	}}`)
	for _, tt := range []struct{ lang, want, missing string }{
		{"en", "[Pluma.A=Check Telegram Pluma.B=Send again]", "[]"},
		{"ru", "[Pluma.A=Проверьте Telegram Pluma.B=Send again]", "[Pluma.B]"},
		{"pt-br", "[Pluma.A=Verifique o Telegram Pluma.B=Reenviar]", "[]"},
		{"fa", "[Pluma.A=Check Telegram Pluma.B=Send again]", "[Pluma.A Pluma.B]"},
	} {
		strs, missing := ownFor("ios", tt.lang)
		var got []string
		for _, s := range strs {
			got = append(got, s.Key+"="+s.Value)
		}
		if fmt.Sprint(got) != tt.want || fmt.Sprint(missing) != tt.missing {
			t.Errorf("%s: %v, missing %v", tt.lang, got, missing)
		}
	}
	if strs, _ := ownFor("android", "ru"); len(strs) != 0 {
		t.Fatalf("android: %v", strs)
	}
}

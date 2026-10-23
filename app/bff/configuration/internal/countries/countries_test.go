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

package countries

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func find(t *testing.T, l *List, iso2 string) *mtproto.Help_Country {
	t.Helper()
	for _, c := range l.Countries {
		if c.GetIso2() == iso2 {
			return c
		}
	}
	t.Fatalf("no %s in the list", iso2)
	return nil
}

func TestCountries(t *testing.T) {
	l := Get("en")
	if len(l.Countries) < 200 {
		t.Fatalf("%d countries, expected the whole world", len(l.Countries))
	}
	for _, tc := range []struct{ iso2, code, pattern, name string }{
		{"RU", "7", "XXX XXX XX XX", "Russia"},
		{"US", "1", "XXX XXX XXXX", "United States"},
		{"DE", "49", "XXXX XXXXXXX", "Germany"},
	} {
		c := find(t, l, tc.iso2)
		if c.GetDefaultName() != tc.name {
			t.Errorf("%s: default name %q, expected %q", tc.iso2, c.GetDefaultName(), tc.name)
		}
		codes := c.GetCountryCodes()
		if len(codes) != 1 || codes[0].GetCountryCode() != tc.code {
			t.Fatalf("%s: codes %v, expected %s", tc.iso2, codes, tc.code)
		}
		if p := codes[0].GetPatterns(); len(p) != 1 || p[0] != tc.pattern {
			t.Errorf("%s: patterns %q, expected %q", tc.iso2, p, tc.pattern)
		}
	}
}

// The list goes over the wire as help.countriesList of layer 228 and comes
// back the same.
func TestEncoding(t *testing.T) {
	l := Get("ru")
	x := mtproto.NewEncodeBuf(1 << 16)
	in := mtproto.MakeTLHelpCountriesList(&mtproto.Help_CountriesList{Countries: l.Countries, Hash: l.Hash}).To_Help_CountriesList()
	if err := in.Encode(x, 228); err != nil {
		t.Fatal(err)
	}
	d := mtproto.NewDecodeBuf(x.GetBuf())
	out, ok := d.Object().(*mtproto.TLHelpCountriesList)
	if err := d.GetError(); err != nil || !ok {
		t.Fatalf("decoded %T, error %v", out, err)
	}
	got := out.To_Help_CountriesList()
	if got.GetHash() != l.Hash || len(got.GetCountries()) != len(l.Countries) {
		t.Fatalf("decoded hash %d with %d countries, sent %d with %d", got.GetHash(), len(got.GetCountries()), l.Hash, len(l.Countries))
	}
	for _, c := range got.GetCountries() {
		if c.GetIso2() != "RU" {
			continue
		}
		cc := c.GetCountryCodes()[0]
		if c.GetName().GetValue() != "Россия" || c.GetDefaultName() != "Russia" || cc.GetCountryCode() != "7" || cc.GetPatterns()[0] != "XXX XXX XX XX" {
			t.Fatalf("decoded RU: %v", c)
		}
		return
	}
	t.Fatal("no RU after decoding")
}

// The Android client takes the last country of a shared calling code.
func TestMainRegionOfSharedCodeIsLast(t *testing.T) {
	for code, main := range map[string]string{"7": "RU", "1": "US", "44": "GB"} {
		last := ""
		for _, c := range Get("en").Countries {
			if c.GetCountryCodes()[0].GetCountryCode() == code {
				last = c.GetIso2()
			}
		}
		if last != main {
			t.Errorf("+%s: last country %s, expected %s", code, last, main)
		}
	}
}

func TestLocalizedNames(t *testing.T) {
	if n := find(t, Get("ru"), "DE").GetName().GetValue(); n != "Германия" {
		t.Errorf("Germany in Russian: %q", n)
	}
	if n := find(t, Get("pt-br"), "DE").GetName().GetValue(); n != "Alemanha" {
		t.Errorf("Germany in Brazilian Portuguese: %q", n)
	}
	// A language code that is not a language gets the English names.
	if c := find(t, Get("not a language"), "DE"); c.GetName() != nil || c.GetDefaultName() != "Germany" {
		t.Errorf("unknown language: name %v, default name %q", c.GetName(), c.GetDefaultName())
	}
}

func TestHash(t *testing.T) {
	en, ru := Get("en"), Get("ru")
	if en.Hash == 0 || ru.Hash == 0 {
		t.Fatal("a zero hash means no cached list to the clients")
	}
	if en.Hash == ru.Hash {
		t.Error("lists in different languages have the same hash")
	}
	if again := build("en"); again.Hash != en.Hash {
		t.Errorf("hash of the same list changed: %d, %d", en.Hash, again.Hash)
	}
}

func TestPattern(t *testing.T) {
	for _, tc := range []struct{ in, code, want string }{
		{"+7 912 345-67-89", "7", "XXX XXX XX XX"},
		{"+1 201-555-0123", "1", "XXX XXX XXXX"},
		{"+44 7400 123456", "44", "XXXX XXXXXX"},
		{"+49 1512 3456789", "7", ""},
	} {
		if got := pattern(tc.in, tc.code); got != tc.want {
			t.Errorf("pattern(%q, %s) = %q, expected %q", tc.in, tc.code, got, tc.want)
		}
	}
}

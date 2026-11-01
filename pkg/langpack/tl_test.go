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
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// describe prints the strings of a difference as key:kind.
func describe(strs []*mtproto.LangPackString) string {
	var out []string
	for _, s := range strs {
		kind := map[string]string{
			mtproto.Predicate_langPackString:           "plain",
			mtproto.Predicate_langPackStringPluralized: "plural",
			mtproto.Predicate_langPackStringDeleted:    "deleted",
		}[s.GetPredicateName()]
		out = append(out, s.GetKey()+":"+kind)
	}
	return strings.Join(out, " ")
}

func TestDifference(t *testing.T) {
	other := "%d chats"
	lang := &Language{LangPack: "ios", LangCode: "ru", Version: 7}
	// What Store.Strings returns: from <= 0 the live strings, otherwise the
	// rows above from, tombstones included.
	all := []String{
		{Key: "a", Value: "A", Version: 3},
		{Key: "b", Value: "B", Version: 7},
		{Key: "c", Pluralized: true, Value: "%d chats", Few: &other, Version: 5},
	}
	changed := []String{
		{Key: "b", Value: "B", Version: 7},
		{Key: "c", Pluralized: true, Value: "%d chats", Few: &other, Version: 5},
		{Key: "gone", Deleted: true, Version: 6},
	}
	for _, c := range []struct {
		rows        []String
		from        int32
		withDeleted bool
		fromVersion int32
		want        string
	}{
		{all, 0, true, 0, "a:plain b:plain c:plural"},
		{all, -1, true, 0, "a:plain b:plain c:plural"},
		{changed, 4, true, 4, "b:plain c:plural gone:deleted"},
		{changed, 4, false, 4, "b:plain c:plural"},
		{changed, 5, true, 5, "b:plain gone:deleted"},
		{nil, 7, true, 7, ""},
		{nil, 9, true, 9, ""},
	} {
		d := Difference(lang, c.rows, c.from, c.withDeleted)
		if d.GetPredicateName() != mtproto.Predicate_langPackDifference || d.GetLangCode() != "ru" || d.GetVersion() != 7 ||
			d.GetFromVersion() != c.fromVersion || d.GetStrings() == nil {
			t.Errorf("from %d: difference %s %s %d->%d", c.from, d.GetPredicateName(), d.GetLangCode(), d.GetFromVersion(), d.GetVersion())
		}
		if got := describe(d.GetStrings()); got != c.want {
			t.Errorf("from %d (deleted %v): strings %q, want %q", c.from, c.withDeleted, got, c.want)
		}
	}
}

// The strings and languages survive TL serialization in the constructors
// the clients decode.
func TestTLRoundTrip(t *testing.T) {
	zero, many := "no chats", "%d chats!"
	in := []String{
		{Key: "plain", Value: "Hello"},
		{Key: "plural", Pluralized: true, Value: "%d chats", Zero: &zero, Many: &many},
	}
	d := Difference(&Language{LangCode: "pl", Version: 3}, in, 0, true)
	x := mtproto.NewEncodeBuf(256)
	if err := mtproto.MakeTLLangPackDifference(d).Encode(x, 228); err != nil {
		t.Fatal(err)
	}
	snap, err := ParseLangPack("android", Language{LangCode: "pl", PluralCode: "pl"}, x.GetBuf())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Strings) != 2 || !sameText(&snap.Strings[0], &in[0]) || !sameText(&snap.Strings[1], &in[1]) || snap.Version != 3 {
		t.Fatalf("round trip: %+v", snap.Strings)
	}

	l := Language{LangPack: "android", LangCode: "pt-br", Name: "Portuguese (Brazil)", NativeName: "Português (Brasil)",
		PluralCode: "pt", BaseLangCode: "pt", Official: true, RTL: true, Beta: true, StringsCount: 10, TranslatedCount: 9,
		TranslationsURL: "https://translations.telegram.org/pt-br/"}
	x = mtproto.NewEncodeBuf(256)
	if err = mtproto.MakeTLLangPackLanguage(l.ToTL()).Encode(x, 228); err != nil {
		t.Fatal(err)
	}
	o := mtproto.NewDecodeBuf(x.GetBuf()).Object()
	got := LanguageFromTL("android", o.(*mtproto.TLLangPackLanguage).To_LangPackLanguage())
	if got != l {
		t.Fatalf("language round trip: %+v, want %+v", got, l)
	}
	noBase := l
	noBase.BaseLangCode = ""
	if noBase.ToTL().GetBaseLangCode() != nil {
		t.Fatal("base_lang_code set without a base language")
	}
}

func TestStringsByKeys(t *testing.T) {
	found := map[string]*String{
		"lng_settings_save": {Key: "lng_settings_save", Value: "Save"},
		"lng_cancel":        {Key: "lng_cancel", Value: "Cancel"},
		"lng_old":           {Key: "lng_old", Deleted: true},
	}
	got := StringsByKeys([]string{"lng_settings_save", "lng_cancel", "lng_ok", "lng_old"}, found)
	if d := describe(got); d != "lng_settings_save:plain lng_cancel:plain lng_ok:deleted lng_old:deleted" {
		t.Fatalf("getStrings = %s", d)
	}
	if fmt.Sprint(got[1].GetValue()) != "Cancel" {
		t.Fatalf("lng_cancel = %q", got[1].GetValue())
	}
}

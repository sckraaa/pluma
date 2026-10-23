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

// Package countries builds the answer of help.getCountriesList: every
// region libphonenumber knows, with its calling code, the pattern of its
// mobile numbers and its name in the client's language. The login screens
// of the official clients take country detection and number formatting
// from it, and an empty list leaves them with no country to sign in from.
package countries

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/nyaruka/phonenumbers"
	"github.com/teamgram/proto/mtproto"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// country is a region with what does not depend on the language.
type country struct {
	iso2    string
	code    string
	main    bool // the main region of a calling code shared by several
	pattern string
}

// List is the countries list in one language.
type List struct {
	Countries []*mtproto.Help_Country
	Hash      int32
}

var (
	base     = load()
	lists    sync.Map // language code -> *List
	english  = display.English.Regions()
	maxLists = 64 // languages kept; a client cannot grow the cache past it
	nLists   int
	nListsMu sync.Mutex
)

func load() []country {
	var out []country
	for region := range phonenumbers.GetSupportedRegions() {
		code := phonenumbers.GetCountryCodeForRegion(region)
		if code == 0 {
			continue
		}
		c := country{
			iso2: region,
			code: strconv.Itoa(code),
			main: len(phonenumbers.GetRegionCodesForCountryCode(code)) > 1 && phonenumbers.GetRegionCodeForCountryCode(code) == region,
		}
		if n := phonenumbers.GetExampleNumberForType(region, phonenumbers.MOBILE); n != nil {
			c.pattern = pattern(phonenumbers.Format(n, phonenumbers.INTERNATIONAL), c.code)
		}
		out = append(out, c)
	}
	// The Android client picks the last country of a shared calling code
	// (until the user picks another one), so the main region of the code
	// goes after the others.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.code != b.code {
			return a.code < b.code
		}
		if a.main != b.main {
			return b.main
		}
		return a.iso2 < b.iso2
	})
	return out
}

// pattern turns an internationally formatted number into a pattern of the
// national part: "+7 912 345-67-89" with code 7 gives "XXX XXX XX XX".
func pattern(international, code string) string {
	national, ok := strings.CutPrefix(international, "+"+code)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.TrimSpace(national) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteByte('X')
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Get returns the list with names in the language of langCode (a Telegram
// language code such as "ru" or "pt-br"); an unknown language gets the
// English names only.
func Get(langCode string) *List {
	key := ""
	if tag, err := language.Parse(strings.TrimSpace(langCode)); err == nil {
		key = tag.String()
	}
	if v, ok := lists.Load(key); ok {
		return v.(*List)
	}
	l := build(key)
	nListsMu.Lock()
	defer nListsMu.Unlock()
	if nLists >= maxLists {
		return l
	}
	if v, loaded := lists.LoadOrStore(key, l); loaded {
		return v.(*List)
	}
	nLists++
	return l
}

func build(lang string) *List {
	var names display.Namer
	if lang != "" {
		names = display.Regions(language.Make(lang))
	}
	h := fnv.New32a()
	l := &List{Countries: make([]*mtproto.Help_Country, 0, len(base))}
	for _, c := range base {
		region := language.MustParseRegion(c.iso2)
		defaultName := english.Name(region)
		cc := mtproto.MakeTLHelpCountryCode(&mtproto.Help_CountryCode{CountryCode: c.code}).To_Help_CountryCode()
		if c.pattern != "" {
			cc.Patterns = []string{c.pattern}
		}
		hc := mtproto.MakeTLHelpCountry(&mtproto.Help_Country{
			Iso2:         c.iso2,
			DefaultName:  defaultName,
			CountryCodes: []*mtproto.Help_CountryCode{cc},
		}).To_Help_Country()
		name := ""
		if names != nil {
			name = names.Name(region)
		}
		if name != "" {
			hc.Name = wrapperspb.String(name)
		}
		fmt.Fprintf(h, "%s;%s;%s;%s;%s\n", c.iso2, c.code, c.pattern, defaultName, name)
		l.Countries = append(l.Countries, hc)
	}
	// 0 means "no cached list" to the clients.
	l.Hash = int32(h.Sum32() & 0x7fffffff)
	if l.Hash == 0 {
		l.Hash = 1
	}
	return l
}

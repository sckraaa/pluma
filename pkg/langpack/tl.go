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

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// ToTL returns s as langPackString or langPackStringPluralized.
func (s *String) ToTL() *mtproto.LangPackString {
	if !s.Pluralized {
		return mtproto.MakeTLLangPackString(&mtproto.LangPackString{
			Key:   s.Key,
			Value: s.Value,
		}).To_LangPackString()
	}
	return mtproto.MakeTLLangPackStringPluralized(&mtproto.LangPackString{
		Key:        s.Key,
		ZeroValue:  stringValue(s.Zero),
		OneValue:   stringValue(s.One),
		TwoValue:   stringValue(s.Two),
		FewValue:   stringValue(s.Few),
		ManyValue:  stringValue(s.Many),
		OtherValue: s.Value,
	}).To_LangPackString()
}

// DeletedTL returns langPackStringDeleted of key.
func DeletedTL(key string) *mtproto.LangPackString {
	return mtproto.MakeTLLangPackStringDeleted(&mtproto.LangPackString{Key: key}).To_LangPackString()
}

// StringFromTL returns the string of a langPackString or
// langPackStringPluralized; deleted is set for langPackStringDeleted.
func StringFromTL(o *mtproto.LangPackString) (s String, deleted bool, err error) {
	switch o.GetPredicateName() {
	case mtproto.Predicate_langPackString:
		return String{Key: o.GetKey(), Value: o.GetValue()}, false, nil
	case mtproto.Predicate_langPackStringPluralized:
		return String{
			Key:        o.GetKey(),
			Pluralized: true,
			Value:      o.GetOtherValue(),
			Zero:       fromStringValue(o.GetZeroValue()),
			One:        fromStringValue(o.GetOneValue()),
			Two:        fromStringValue(o.GetTwoValue()),
			Few:        fromStringValue(o.GetFewValue()),
			Many:       fromStringValue(o.GetManyValue()),
		}, false, nil
	case mtproto.Predicate_langPackStringDeleted:
		return String{Key: o.GetKey()}, true, nil
	default:
		return String{}, false, fmt.Errorf("langpack: unexpected string constructor %q", o.GetPredicateName())
	}
}

// ToTL returns l as langPackLanguage.
func (l *Language) ToTL() *mtproto.LangPackLanguage {
	var base *wrapperspb.StringValue
	if l.BaseLangCode != "" {
		base = wrapperspb.String(l.BaseLangCode)
	}
	return mtproto.MakeTLLangPackLanguage(&mtproto.LangPackLanguage{
		Official:        l.Official,
		Rtl:             l.RTL,
		Beta:            l.Beta,
		Name:            l.Name,
		NativeName:      l.NativeName,
		LangCode:        l.LangCode,
		BaseLangCode:    base,
		PluralCode:      l.PluralCode,
		StringsCount:    l.StringsCount,
		TranslatedCount: l.TranslatedCount,
		TranslationsUrl: l.TranslationsURL,
	}).To_LangPackLanguage()
}

// LanguageFromTL returns the language of a langPackLanguage of pack.
func LanguageFromTL(pack string, o *mtproto.LangPackLanguage) Language {
	return Language{
		LangPack:        pack,
		LangCode:        CanonicalLangCode(o.GetLangCode()),
		Name:            o.GetName(),
		NativeName:      o.GetNativeName(),
		PluralCode:      o.GetPluralCode(),
		BaseLangCode:    CanonicalLangCode(o.GetBaseLangCode().GetValue()),
		Official:        o.GetOfficial(),
		RTL:             o.GetRtl(),
		Beta:            o.GetBeta(),
		StringsCount:    o.GetStringsCount(),
		TranslatedCount: o.GetTranslatedCount(),
		TranslationsURL: o.GetTranslationsUrl(),
	}
}

// Difference returns langPackDifference of lang from rows, the stored
// strings of the language with a version above from (tombstones included):
//
//   - from <= 0: the whole pack with from_version 0, what
//     langpack.getLangPack answers; Android starts the pack over on
//     from_version 0 (LocaleController.java:3099-3101), tdesktop requests
//     again when its version is below from_version (lang_cloud_manager.cpp
//     :383-395);
//   - from >= the version: no strings, from_version = from;
//   - otherwise the strings changed since from and the deleted ones as
//     langPackStringDeleted, from_version = from: iOS and Web K apply a
//     difference only when from_version equals their version
//     (ManagedLocalizationUpdatesOperations.swift:148-247,
//     langPack.ts:752-788).
//
// withDeleted false leaves the deleted strings out of a difference: Web K
// keeps langPackStringDeleted as a string and shows its key in place of the
// bundled English (langPack.ts:463-475).
func Difference(lang *Language, rows []String, from int32, withDeleted bool) *mtproto.LangPackDifference {
	d := &mtproto.LangPackDifference{
		LangCode:    lang.LangCode,
		FromVersion: from,
		Version:     lang.Version,
		Strings:     []*mtproto.LangPackString{},
	}
	switch {
	case from <= 0:
		d.FromVersion = 0
		for i := range rows {
			if !rows[i].Deleted {
				d.Strings = append(d.Strings, rows[i].ToTL())
			}
		}
	case from >= lang.Version:
	default:
		for i := range rows {
			r := &rows[i]
			switch {
			case r.Version <= from:
			case !r.Deleted:
				d.Strings = append(d.Strings, r.ToTL())
			case withDeleted:
				d.Strings = append(d.Strings, DeletedTL(r.Key))
			}
		}
	}
	return mtproto.MakeTLLangPackDifference(d).To_LangPackDifference()
}

// StringsByKeys returns the answer of langpack.getStrings: the strings of
// keys in their order, langPackStringDeleted for a key the pack lacks (as
// Telegram answers, capture of 2026-04-10).
func StringsByKeys(keys []string, found map[string]*String) []*mtproto.LangPackString {
	out := make([]*mtproto.LangPackString, 0, len(keys))
	for _, k := range keys {
		if s := found[k]; s != nil && !s.Deleted {
			out = append(out, s.ToTL())
		} else {
			out = append(out, DeletedTL(k))
		}
	}
	return out
}

func stringValue(s *string) *wrapperspb.StringValue {
	if s == nil {
		return nil
	}
	return wrapperspb.String(*s)
}

func fromStringValue(v *wrapperspb.StringValue) *string {
	if v == nil {
		return nil
	}
	s := v.GetValue()
	return &s
}

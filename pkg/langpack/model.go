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

// Package langpack stores and serves the language packs of the clients
// (task 021, ADR 0025): the strings of each platform pack (android, ios,
// tdesktop, macos, webk) in every language, with versions, as langpack.*
// answers them.
//
// The packs come from Telegram's own langpack.* methods (Fetch, run by
// pluma-admin langpack fetch) as snapshot files, one per pack and language.
// Import puts a snapshot into MySQL (tables langpack_languages and
// langpack_strings, migrate-20261101-langpacks.sql) after Rebrander has
// replaced "Telegram" with "Pluma" in the values: in one transaction per pack
// and language, the changed strings get a new version of the language and the
// vanished ones become tombstones; without changes nothing is written and the
// version stays. Difference builds the answers of langpack.getLangPack and
// langpack.getDifference from what Store reads.
package langpack

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Packs are the platform packs Pluma's clients use, as they name them in
// initConnection: Android (LocaleController/ConnectionsManager), iOS and
// macOS (MTApiEnvironment.m), Desktop (lang_instance.cpp) and Web K
// (config/app.ts).
var Packs = []string{"android", "ios", "tdesktop", "macos", "webk"}

// CanonicalPack returns the stored name of the pack a client asks for: Web K
// asks for "web" in langpack.getLangPack and langpack.getLanguages and for
// "webk" everywhere else (tweb lib/langPack.ts:177-182,
// components/sidebarLeft/tabs/language.tsx:106-108,
// appLangPackManager.ts:31-44), one pack under two names.
func CanonicalPack(pack string) string {
	pack = strings.ToLower(strings.TrimSpace(pack))
	if pack == "web" {
		return "webk"
	}
	return pack
}

// CanonicalLangCode returns a language code as stored: Telegram's codes are
// lower case with '-' ("pt-br"); Android sends them so already
// (LocaleController), other clients may differ in case.
func CanonicalLangCode(code string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(code)), "_", "-")
}

// Language is a language of a pack (langPackLanguage) and its version.
type Language struct {
	LangPack        string `json:"lang_pack"`
	LangCode        string `json:"lang_code"`
	Name            string `json:"name"`
	NativeName      string `json:"native_name"`
	PluralCode      string `json:"plural_code"`
	BaseLangCode    string `json:"base_lang_code,omitempty"` // empty: no base language
	Official        bool   `json:"official,omitempty"`
	RTL             bool   `json:"rtl,omitempty"`
	Beta            bool   `json:"beta,omitempty"`
	StringsCount    int32  `json:"strings_count"`
	TranslatedCount int32  `json:"translated_count"`
	TranslationsURL string `json:"translations_url"`
	// SortOrder is the place of the language in Telegram's
	// langpack.getLanguages answer, which langpack.getLanguages keeps.
	SortOrder int32 `json:"sort_order"`

	// Version is Pluma's version of the language (stored languages only).
	Version int32 `json:"-"`
	// UpstreamVersion is Telegram's version of the last import.
	UpstreamVersion int32 `json:"-"`
	UpdatedAt       int64 `json:"-"`
}

// String is a string of a pack: plain (langPackString) or pluralized
// (langPackStringPluralized). Value is the text of a plain string and
// other_value of a pluralized one; the other forms are nil when absent.
type String struct {
	Key        string  `json:"key"`
	Pluralized bool    `json:"pluralized,omitempty"`
	Value      string  `json:"value"`
	Zero       *string `json:"zero_value,omitempty"`
	One        *string `json:"one_value,omitempty"`
	Two        *string `json:"two_value,omitempty"`
	Few        *string `json:"few_value,omitempty"`
	Many       *string `json:"many_value,omitempty"`

	// Deleted marks a tombstone (stored strings only).
	Deleted bool `json:"-"`
	// Version is the language version that last changed the string (stored
	// strings only).
	Version int32 `json:"-"`
}

// forms returns the pointers to the optional plural forms of s.
func (s *String) forms() []**string {
	return []**string{&s.Zero, &s.One, &s.Two, &s.Few, &s.Many}
}

// sameText reports whether a and b have the same kind and texts.
func sameText(a, b *String) bool {
	if a.Pluralized != b.Pluralized || a.Value != b.Value {
		return false
	}
	af, bf := a.forms(), b.forms()
	for i := range af {
		x, y := *af[i], *bf[i]
		if (x == nil) != (y == nil) || (x != nil && *x != *y) {
			return false
		}
	}
	return true
}

// Snapshot is one pack in one language as Telegram answered
// langpack.getLangPack, with the language from langpack.getLanguages: the
// file pluma-admin langpack fetch writes and langpack import reads.
type Snapshot struct {
	LangPack string `json:"lang_pack"`
	LangCode string `json:"lang_code"`
	// Version is Telegram's version of the pack in this language.
	Version   int32    `json:"version"`
	FetchedAt int64    `json:"fetched_at"` // unix seconds
	Language  Language `json:"language"`
	Strings   []String `json:"strings"`
}

// Validate checks that s can be imported: names, the language, unique keys.
func (s *Snapshot) Validate() error {
	if s.LangPack == "" || s.LangCode == "" {
		return errors.New("langpack: snapshot without lang_pack or lang_code")
	}
	if CanonicalPack(s.LangPack) != s.LangPack || CanonicalLangCode(s.LangCode) != s.LangCode {
		return fmt.Errorf("langpack: snapshot %s/%s: names must be lower case (%s/%s)", s.LangPack, s.LangCode,
			CanonicalPack(s.LangPack), CanonicalLangCode(s.LangCode))
	}
	if len(s.LangPack) > 32 || len(s.LangCode) > 64 {
		return fmt.Errorf("langpack: snapshot %s/%s: name too long", s.LangPack, s.LangCode)
	}
	if s.Language.LangCode != "" && CanonicalLangCode(s.Language.LangCode) != s.LangCode {
		return fmt.Errorf("langpack: snapshot %s/%s describes language %q", s.LangPack, s.LangCode, s.Language.LangCode)
	}
	if s.Language.PluralCode == "" {
		// Android calls .replace on plural_code unconditionally
		// (LocaleController.java:3226-3268): it must never be empty.
		return fmt.Errorf("langpack: snapshot %s/%s: language without plural_code", s.LangPack, s.LangCode)
	}
	seen := make(map[string]bool, len(s.Strings))
	for i := range s.Strings {
		k := s.Strings[i].Key
		switch {
		case k == "":
			return fmt.Errorf("langpack: snapshot %s/%s: string %d without a key", s.LangPack, s.LangCode, i)
		case len(k) > 255:
			return fmt.Errorf("langpack: snapshot %s/%s: key %.40q... longer than 255 bytes", s.LangPack, s.LangCode, k)
		case seen[k]:
			return fmt.Errorf("langpack: snapshot %s/%s: key %q twice", s.LangPack, s.LangCode, k)
		}
		seen[k] = true
	}
	return nil
}

// SnapshotPath is where a snapshot of pack in lang lives under dir.
func SnapshotPath(dir, pack, lang string) string {
	return filepath.Join(dir, pack, lang+".json")
}

// WriteSnapshot writes s to SnapshotPath(dir, ...), replacing the file
// atomically.
func WriteSnapshot(dir string, s *Snapshot) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	path := SnapshotPath(dir, s.LangPack, s.LangCode)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".snapshot-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err = tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

// ReadSnapshot reads and validates a snapshot file.
func ReadSnapshot(path string) (*Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err = json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err = s.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// SnapshotFiles returns the snapshot files under dir (<pack>/<lang>.json),
// sorted, optionally only of pack and of lang.
func SnapshotFiles(dir, pack, lang string) ([]string, error) {
	var files []string
	packs, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, p := range packs {
		if !p.IsDir() || (pack != "" && p.Name() != pack) {
			continue
		}
		langs, err := os.ReadDir(filepath.Join(dir, p.Name()))
		if err != nil {
			return nil, err
		}
		for _, l := range langs {
			name := l.Name()
			if !l.Type().IsRegular() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
				continue
			}
			if lang != "" && strings.TrimSuffix(name, ".json") != lang {
				continue
			}
			files = append(files, filepath.Join(dir, p.Name(), name))
		}
	}
	slices.Sort(files)
	return files, nil
}

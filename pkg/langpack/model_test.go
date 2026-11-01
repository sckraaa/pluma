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
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalNames(t *testing.T) {
	for in, want := range map[string]string{"web": "webk", "WebK": "webk", "android": "android", " tdesktop ": "tdesktop", "": ""} {
		if got := CanonicalPack(in); got != want {
			t.Errorf("CanonicalPack(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"pt_BR": "pt-br", "EN": "en", "zh-hans": "zh-hans"} {
		if got := CanonicalLangCode(in); got != want {
			t.Errorf("CanonicalLangCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func testSnapshot() *Snapshot {
	one := "%d Telegram chat"
	return &Snapshot{
		LangPack: "android", LangCode: "ru", Version: 42, FetchedAt: 1,
		Language: Language{LangPack: "android", LangCode: "ru", Name: "Russian", NativeName: "Русский", PluralCode: "ru"},
		Strings: []String{
			{Key: "AppName", Value: "Telegram"},
			{Key: "Chats", Pluralized: true, Value: "%d Telegram chats", One: &one},
		},
	}
}

func TestSnapshotFiles(t *testing.T) {
	dir := t.TempDir()
	s := testSnapshot()
	path, err := WriteSnapshot(dir, s)
	if err != nil {
		t.Fatal(err)
	}
	s2 := testSnapshot()
	s2.LangPack, s2.Language.LangPack = "ios", "ios"
	if _, err = WriteSnapshot(dir, s2); err != nil {
		t.Fatal(err)
	}
	// Other files are not snapshots.
	if err = os.WriteFile(filepath.Join(dir, "README"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "ios", ".snapshot-123"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 42 || len(got.Strings) != 2 || !sameText(&got.Strings[1], &s.Strings[1]) || got.Language.NativeName != "Русский" {
		t.Fatalf("read back %+v", got)
	}
	for _, c := range []struct{ pack, lang, want string }{
		{"", "", "[android/ru.json ios/ru.json]"},
		{"ios", "", "[ios/ru.json]"},
		{"", "en", "[]"},
	} {
		files, err := SnapshotFiles(dir, c.pack, c.lang)
		if err != nil {
			t.Fatal(err)
		}
		for i := range files {
			files[i] = strings.TrimPrefix(filepath.ToSlash(files[i]), filepath.ToSlash(dir)+"/")
		}
		if fmt.Sprint(files) != c.want {
			t.Errorf("SnapshotFiles(%q, %q) = %v, want %s", c.pack, c.lang, files, c.want)
		}
	}
}

func TestSnapshotValidate(t *testing.T) {
	for name, edit := range map[string]func(*Snapshot){
		"no pack":         func(s *Snapshot) { s.LangPack = "" },
		"upper case":      func(s *Snapshot) { s.LangCode = "RU" },
		"alias pack":      func(s *Snapshot) { s.LangPack = "web" },
		"other language":  func(s *Snapshot) { s.Language.LangCode = "en" },
		"no plural_code":  func(s *Snapshot) { s.Language.PluralCode = "" },
		"empty key":       func(s *Snapshot) { s.Strings[0].Key = "" },
		"key twice":       func(s *Snapshot) { s.Strings[1].Key = s.Strings[0].Key },
		"key too long":    func(s *Snapshot) { s.Strings[0].Key = strings.Repeat("k", 256) },
		"code too long":   func(s *Snapshot) { s.LangCode = strings.Repeat("x", 65) },
		"pack name long":  func(s *Snapshot) { s.LangPack = strings.Repeat("p", 33) },
		"lang code bogus": func(s *Snapshot) { s.LangCode = "pt_br" },
	} {
		s := testSnapshot()
		edit(s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := testSnapshot().Validate(); err != nil {
		t.Fatal(err)
	}
}

// planImport counts and splits the changes of a snapshot against the stored
// strings.
func TestPlanImport(t *testing.T) {
	ns := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
	row := func(key, value string, deleted bool) importRow {
		return importRow{stringRow: stringRow{Key: key, Value: value, Deleted: deleted, Version: 3}, UpValue: value}
	}
	plural := row("p", "%d chats", false)
	plural.Pluralized, plural.One, plural.UpOne = true, ns("1 chat"), ns("1 chat")
	upstreamOnly := row("u", "Pluma", false)
	upstreamOnly.UpValue = "Pluma" // stored before a rule changed
	stored := []importRow{
		row("same", "Same", false),
		row("changed", "Old", false),
		row("gone", "Gone", false),
		row("tomb", "Tomb", true),
		row("tomb2", "Tomb2", true),
		plural,
		upstreamOnly,
	}
	oneTwo := "1 chats"
	upstream := []String{
		{Key: "same", Value: "Same"},
		{Key: "changed", Value: "New"},
		{Key: "tomb", Value: "Tomb"},
		{Key: "new", Value: "New"},
		{Key: "p", Pluralized: true, Value: "%d chats", One: &oneTwo},
		{Key: "u", Value: "Telegram"},
	}
	incoming := make([]pendingString, len(upstream))
	for i := range upstream {
		r := Rebrand("en", upstream[i])
		incoming[i] = pendingString{s: &r, upstream: &upstream[i]}
	}
	p := planImport(stored, incoming)
	if p.added != 2 || p.changed != 2 || p.deleted != 1 || p.unchanged != 2 {
		t.Fatalf("added %d changed %d deleted %d unchanged %d", p.added, p.changed, p.deleted, p.unchanged)
	}
	var write []string
	for _, w := range p.write {
		write = append(write, w.s.Key)
	}
	if fmt.Sprint(write) != "[changed tomb new p]" || fmt.Sprint(p.remove) != "[gone]" ||
		len(p.refresh) != 1 || p.refresh[0].s.Key != "u" || p.refresh[0].upstream.Value != "Telegram" {
		t.Fatalf("write %v remove %v refresh %+v", write, p.remove, p.refresh)
	}

	// Nothing changed: nothing to write.
	p = planImport(stored[:1], incoming[:1])
	if len(p.write)+len(p.remove)+len(p.refresh) != 0 || p.unchanged != 1 {
		t.Fatalf("unchanged snapshot planned %+v", p)
	}
}

// The key in the code is Telegram's production key as core.telegram.org
// publishes it (the PEM block Web K quotes in rsaKeysManager.ts).
func TestTelegramPublicKey(t *testing.T) {
	const published = `-----BEGIN RSA PUBLIC KEY-----
MIIBCgKCAQEA6LszBcC1LGzyr992NzE0ieY+BSaOW622Aa9Bd4ZHLl+TuFQ4lo4g
5nKaMBwK/BIb9xUfg0Q29/2mgIR6Zr9krM7HjuIcCzFvDtr+L0GQjae9H0pRB2OO
62cECs5HKhT5DZ98K33vmWiLowc621dQuwKWSQKjWf50XYFw42h21P2KXUGyp2y/
+aEyZ+uVgLLQbRA1dEjSDZ2iGRy12Mk5gpYc397aYp438fsJoHIgJ2lgMv5h7WY9
t6N/byY9Nw9p21Og3AoXSL2q/2IJ1WRUhebgAdGVMlV1fkuOQoEzR7EdpqtQD9Cs
5+bfo3Nhmcyvk5ftB0WkJ9z6bNZ7yxrP8wIDAQAB
-----END RSA PUBLIC KEY-----
`
	block, _ := pem.Decode([]byte(published))
	want, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if got := TelegramPublicKey(); !got.Equal(want) {
		t.Fatal("TelegramPublicKey differs from the published key")
	}
}

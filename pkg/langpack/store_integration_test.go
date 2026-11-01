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

//go:build integration

package langpack

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

const migration = "migrate-20261101-langpacks.sql"

var (
	itDB    *testinfra.MySQL
	itStore *Store
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	root, err := testinfra.RepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	db, err := testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles([]string{filepath.Join(root, testinfra.SchemaDir, migration)}))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	itDB = db
	conf := db.Config()
	itStore = NewStore(sqlx.NewMySQL(&conf))
	code := m.Run()
	_ = db.Terminate(ctx)
	os.Exit(code)
}

func snapshotOf(pack, lang string, version int32, strs ...String) *Snapshot {
	return &Snapshot{
		LangPack: pack, LangCode: lang, Version: version,
		Language: Language{LangCode: lang, Name: "Lang " + lang, NativeName: "Native " + lang, PluralCode: lang,
			StringsCount: int32(len(strs)), TranslatedCount: int32(len(strs)), TranslationsURL: "https://translations.telegram.org/" + lang + "/"},
		Strings: strs,
	}
}

func mustImport(t *testing.T, s *Snapshot) *ImportResult {
	t.Helper()
	r, err := itStore.Import(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func keysOf(strs []String) string {
	var out []string
	for _, s := range strs {
		k := s.Key
		if s.Deleted {
			k += "(deleted)"
		}
		out = append(out, fmt.Sprintf("%s@%d", k, s.Version))
	}
	return fmt.Sprint(out)
}

// A key kept as Telegram wrote it is stored unchanged and left out of the
// report; a string the rules miss in the same pack is reported.
func TestImportKeptKey(t *testing.T) {
	r := mustImport(t, snapshotOf("android", "sk", 3,
		String{Key: "lng_about_text1_api", Value: "Telegram API"},
		String{Key: "lng_intro", Value: "Vitajte v Telegrame"},
	))
	if fmt.Sprint(r.Leftovers) != "map[lng_intro:[Telegrame]]" {
		t.Fatalf("leftovers %v", r.Leftovers)
	}
	got, err := itStore.StringsByKey(context.Background(), "android", "sk", []string{"lng_about_text1_api"})
	if err != nil {
		t.Fatal(err)
	}
	if s := got["lng_about_text1_api"]; s == nil || s.Value != "Telegram API" {
		t.Fatalf("kept key %+v", s)
	}
}

// The migration is safe to run again.
func TestMigrationRunsAgain(t *testing.T) {
	root, err := testinfra.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err = itDB.ApplySQLFile(context.Background(), filepath.Join(root, testinfra.SchemaDir, migration)); err != nil {
		t.Fatal(err)
	}
}

// An import bumps the version only when a string changed; a difference from
// any older version lists what changed since, removed keys included.
func TestImportVersions(t *testing.T) {
	ctx := context.Background()
	few := "%d Telegram chaty"
	v1 := snapshotOf("ios", "cs", 100,
		String{Key: "AppName", Value: "Telegram"},
		String{Key: "Chats", Pluralized: true, Value: "%d Telegram chatů", Few: &few},
		String{Key: "Welcome", Value: "Vítejte v Telegramovi"}, // an ending Czech does not list
		String{Key: "Gone", Value: "Old"},
	)
	r := mustImport(t, v1)
	if r.OldVersion != 0 || r.Version != 100 || r.Added != 4 || r.Changes() != 4 {
		t.Fatalf("first import: %+v", r)
	}
	if fmt.Sprint(r.Leftovers) != "map[Welcome:[Telegramovi]]" {
		t.Fatalf("leftovers %v", r.Leftovers)
	}

	// The same snapshot again changes nothing.
	r = mustImport(t, v1)
	if r.Version != 100 || r.OldVersion != 100 || r.Changes() != 0 || r.Unchanged != 4 {
		t.Fatalf("repeated import: %+v", r)
	}

	// A changed, a new and a removed string: one new version.
	v2 := snapshotOf("ios", "cs", 100,
		String{Key: "AppName", Value: "Telegram"},
		String{Key: "Chats", Pluralized: true, Value: "%d Telegram chatů", Few: &few},
		String{Key: "Welcome", Value: "Vítejte v Telegram"},
		String{Key: "New", Value: "Nový"},
	)
	r = mustImport(t, v2)
	if r.OldVersion != 100 || r.Version != 101 || r.Added != 1 || r.Changed != 1 || r.Deleted != 1 || r.Unchanged != 2 {
		t.Fatalf("second import: %+v", r)
	}

	lang, err := itStore.Language(ctx, "ios", "cs")
	if err != nil {
		t.Fatal(err)
	}
	if lang.Version != 101 || lang.UpstreamVersion != 100 || lang.Name != "Lang cs" || lang.PluralCode != "cs" || lang.UpdatedAt == 0 {
		t.Fatalf("language %+v", lang)
	}
	all, err := itStore.Strings(ctx, "ios", "cs", 0)
	if err != nil {
		t.Fatal(err)
	}
	if keysOf(all) != "[AppName@100 Chats@100 New@101 Welcome@101]" {
		t.Fatalf("whole pack %s", keysOf(all))
	}
	for _, s := range all {
		switch s.Key {
		case "AppName":
			if s.Value != "Pluma" {
				t.Errorf("AppName = %q", s.Value)
			}
		case "Chats":
			if !s.Pluralized || s.Value != "%d Pluma chatů" || s.Few == nil || *s.Few != "%d Pluma chaty" || s.One != nil {
				t.Errorf("Chats = %+v", s)
			}
		case "Welcome":
			if s.Value != "Vítejte v Pluma" {
				t.Errorf("Welcome = %q", s.Value)
			}
		}
	}
	since, err := itStore.Strings(ctx, "ios", "cs", 100)
	if err != nil {
		t.Fatal(err)
	}
	if keysOf(since) != "[Gone(deleted)@101 New@101 Welcome@101]" {
		t.Fatalf("since 100: %s", keysOf(since))
	}
	d := Difference(lang, since, 100, true)
	if describe(d.GetStrings()) != "Gone:deleted New:plain Welcome:plain" || d.GetFromVersion() != 100 || d.GetVersion() != 101 {
		t.Fatalf("difference %d->%d %s", d.GetFromVersion(), d.GetVersion(), describe(d.GetStrings()))
	}

	// A removed key that comes back is added again; Telegram's newer version
	// is taken over.
	v3 := snapshotOf("ios", "cs", 500, append(v2.Strings, String{Key: "Gone", Value: "Back"})...)
	r = mustImport(t, v3)
	if r.Version != 500 || r.Added != 1 || r.Changes() != 1 {
		t.Fatalf("third import: %+v", r)
	}
	found, err := itStore.StringsByKey(ctx, "ios", "cs", []string{"Gone", "AppName", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 || found["Gone"].Value != "Back" || found["Gone"].Deleted || found["Gone"].Version != 500 {
		t.Fatalf("by key %+v", found)
	}

	// Only the description changed: the version stays.
	v4 := snapshotOf("ios", "cs", 500, v3.Strings...)
	v4.Language.TranslatedCount = 3
	if r = mustImport(t, v4); r.Version != 500 || r.Changes() != 0 {
		t.Fatalf("description-only import: %+v", r)
	}
	if lang, err = itStore.Language(ctx, "ios", "cs"); err != nil || lang.TranslatedCount != 3 || lang.Version != 500 {
		t.Fatalf("language after description-only import: %+v, %v", lang, err)
	}
}

func TestLanguages(t *testing.T) {
	ctx := context.Background()
	for i, code := range []string{"en", "ru", "pt-br"} {
		s := snapshotOf("tdesktop", code, 7, String{Key: "lng_ok", Value: "OK " + code})
		s.Language.SortOrder = int32(i)
		if code == "pt-br" {
			s.Language.BaseLangCode, s.Language.RTL = "pt", true
		}
		mustImport(t, s)
	}
	langs, err := itStore.Languages(ctx, "tdesktop")
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, l := range langs {
		codes = append(codes, l.LangCode)
	}
	if fmt.Sprint(codes) != "[en ru pt-br]" || langs[2].BaseLangCode != "pt" || !langs[2].RTL || langs[0].BaseLangCode != "" {
		t.Fatalf("languages %+v", langs)
	}
	if ok, err := itStore.PackExists(ctx, "tdesktop"); err != nil || !ok {
		t.Fatalf("PackExists(tdesktop) = %v, %v", ok, err)
	}
	if ok, err := itStore.PackExists(ctx, "nope"); err != nil || ok {
		t.Fatalf("PackExists(nope) = %v, %v", ok, err)
	}
	if langs, err = itStore.Languages(ctx, "nope"); err != nil || len(langs) != 0 {
		t.Fatalf("languages of an unknown pack: %v, %v", langs, err)
	}
	if _, err = itStore.Language(ctx, "tdesktop", "xx"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown language: %v", err)
	}
}

// A pack bigger than one batch goes in whole; concurrent imports of the same
// language follow one another.
func TestImportLargeAndConcurrent(t *testing.T) {
	ctx := context.Background()
	big := func(version int32, prefix string) *Snapshot {
		var strs []String
		for i := 0; i < 2*batchSize+7; i++ {
			strs = append(strs, String{Key: fmt.Sprintf("Key%05d", i), Value: fmt.Sprintf("%s %d", prefix, i)})
		}
		return snapshotOf("android", "uk", version, strs...)
	}
	if r := mustImport(t, big(1, "a")); r.Added != 2*batchSize+7 || r.Version != 1 {
		t.Fatalf("large import: %+v", r)
	}

	var wg sync.WaitGroup
	results := make([]*ImportResult, 2)
	errs := make([]error, 2)
	for i, prefix := range []string{"b", "c"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = itStore.Import(ctx, big(1, prefix))
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	versions := map[int32]bool{results[0].Version: true, results[1].Version: true}
	if !versions[2] || !versions[3] {
		t.Fatalf("concurrent imports got versions %d and %d, want 2 and 3", results[0].Version, results[1].Version)
	}
	for _, r := range results {
		if r.Changed != 2*batchSize+7 {
			t.Fatalf("concurrent import changed %d strings", r.Changed)
		}
	}
	all, err := itStore.Strings(ctx, "android", "uk", 0)
	if err != nil {
		t.Fatal(err)
	}
	last := results[0]
	if results[1].Version > last.Version {
		last = results[1]
	}
	if len(all) != 2*batchSize+7 || all[0].Version != last.Version {
		t.Fatalf("%d strings at version %d after the imports, want version %d", len(all), all[0].Version, last.Version)
	}
}

// Import refuses a broken snapshot without writing.
func TestImportRejects(t *testing.T) {
	s := snapshotOf("webk", "de", 1, String{Key: "A", Value: "a"}, String{Key: "A", Value: "b"})
	if _, err := itStore.Import(context.Background(), s); err == nil {
		t.Fatal("a snapshot with a key twice imported")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if ok, err := itStore.PackExists(ctx, "webk"); err != nil || ok {
		t.Fatalf("PackExists(webk) = %v, %v after a refused import", ok, err)
	}
}

// help.getConfig reports the versions of the session's language and of its
// base language; nothing for a language the server lacks.
func TestConfigVersions(t *testing.T) {
	ctx := context.Background()
	base := snapshotOf("macos", "pt", 30, String{Key: "A", Value: "a"})
	mustImport(t, base)
	child := snapshotOf("macos", "pt-br", 40, String{Key: "A", Value: "á"})
	child.Language.BaseLangCode = "pt"
	mustImport(t, child)
	orphan := snapshotOf("macos", "pt-pt", 50, String{Key: "A", Value: "à"})
	orphan.Language.BaseLangCode = "pt-xx"
	mustImport(t, orphan)

	for _, c := range []struct {
		pack, lang string
		want       string
	}{
		{"macos", "pt-br", "&{pt-br 40 30}"},
		{"macos", "pt", "&{pt 30 0}"},
		{"macos", "pt-pt", "&{pt-pt 50 0}"},
		{"macos", "xx", "<nil>"},
		{"nope", "pt", "<nil>"},
		{"", "pt", "<nil>"},
		{"macos", "", "<nil>"},
	} {
		v, err := itStore.ConfigVersions(ctx, c.pack, c.lang)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprint(v); got != c.want {
			t.Errorf("ConfigVersions(%q, %q) = %s, want %s", c.pack, c.lang, got, c.want)
		}
	}

	// Read gives the language with the strings of its version.
	l, strs, err := itStore.Read(ctx, "macos", "pt-br", 0)
	if err != nil || l.Version != 40 || len(strs) != 1 || strs[0].Value != "á" {
		t.Fatalf("Read = %+v, %+v, %v", l, strs, err)
	}
	if _, _, err = itStore.Read(ctx, "macos", "xx", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read of an unknown language: %v", err)
	}
}

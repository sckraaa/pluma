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

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

func runAdmin(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut, func(string) (allowlist, codeIssuer, error) {
		return nil, nil, errors.New("no allowlist in these tests")
	})
	return code, out.String(), errOut.String()
}

// fakeTelegram answers langpack.getLanguages and getLangPack with two
// languages of every pack.
type fakeTelegram struct {
	closed   bool
	requests []string
}

func (f *fakeTelegram) InvokeRaw(_ context.Context, req mtproto.TLObject) ([]byte, error) {
	x := mtproto.NewEncodeBuf(512)
	switch r := req.(type) {
	case *mtproto.TLLangpackGetLanguages:
		f.requests = append(f.requests, "languages "+r.LangPack)
		x.UInt(uint32(mtproto.CRC32_vector))
		x.Int(2)
		for _, code := range []string{"en", "ru"} {
			l := langpack.Language{LangCode: code, Name: code, NativeName: code, PluralCode: code}
			if err := mtproto.MakeTLLangPackLanguage(l.ToTL()).Encode(x, mtclient.DefaultLayer); err != nil {
				return nil, err
			}
		}
	case *mtproto.TLLangpackGetLangPack:
		f.requests = append(f.requests, "pack "+r.LangPack+" "+r.LangCode)
		d := langpack.Difference(&langpack.Language{LangCode: r.LangCode, Version: 77},
			[]langpack.String{{Key: "AppName", Value: "Telegram"}}, 0, true)
		if err := mtproto.MakeTLLangPackDifference(d).Encode(x, mtclient.DefaultLayer); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unexpected %T", req)
	}
	return x.GetBuf(), nil
}

func (f *fakeTelegram) Close() error {
	f.closed = true
	return nil
}

func withFakeTelegram(t *testing.T) (*fakeTelegram, *langpack.TelegramConfig) {
	tg := &fakeTelegram{}
	var dialed langpack.TelegramConfig
	old := dialTelegram
	dialTelegram = func(_ context.Context, cfg langpack.TelegramConfig) (telegramConn, error) {
		dialed = cfg
		return tg, nil
	}
	t.Cleanup(func() { dialTelegram = old })
	return tg, &dialed
}

func TestLangpackFetch(t *testing.T) {
	tg, dialed := withFakeTelegram(t)
	dir := t.TempDir()
	t.Setenv(apiIDEnv, "12345")
	code, out, errOut := runAdmin("langpack", "fetch", "-out", dir, "-pack", "android,web", "-lang", "RU", "-delay", "0")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if dialed.APIID != 12345 || dialed.Addr != langpack.TelegramAddr || dialed.DC != 2 || !tg.closed {
		t.Fatalf("dialed %+v, closed %v", *dialed, tg.closed)
	}
	if fmt.Sprint(tg.requests) != "[languages android pack android ru languages webk pack webk ru]" {
		t.Fatalf("requests %v", tg.requests)
	}
	if !strings.Contains(out, "android/ru: version 77, 1 strings") || !strings.Contains(out, "2 snapshot(s)") {
		t.Fatalf("output:\n%s", out)
	}
	files, err := langpack.SnapshotFiles(dir, "", "")
	if err != nil || len(files) != 2 || files[0] != filepath.Join(dir, "android", "ru.json") {
		t.Fatalf("files %v, %v", files, err)
	}
	snap, err := langpack.ReadSnapshot(files[1])
	if err != nil || snap.LangPack != "webk" || snap.Version != 77 || snap.Strings[0].Value != "Telegram" {
		t.Fatalf("snapshot %+v, %v", snap, err)
	}

	// The flag wins over the environment; without either, no connection.
	if code, _, errOut = runAdmin("langpack", "fetch", "-out", dir, "-api-id", "7", "-pack", "ios", "-lang", "en", "-delay", "0"); code != 0 || dialed.APIID != 7 {
		t.Fatalf("-api-id 7: exit %d (%s), dialed %d", code, errOut, dialed.APIID)
	}
	t.Setenv(apiIDEnv, "")
	*dialed = langpack.TelegramConfig{}
	if code, _, errOut = runAdmin("langpack", "fetch", "-out", dir); code != 1 || !strings.Contains(errOut, apiIDEnv) || dialed.APIID != 0 {
		t.Fatalf("no api_id: exit %d %q", code, errOut)
	}
	t.Setenv(apiIDEnv, "twelve")
	if code, _, errOut = runAdmin("langpack", "fetch", "-out", dir); code != 1 || !strings.Contains(errOut, "not a number") {
		t.Fatalf("bad api_id: exit %d %q", code, errOut)
	}
}

// memLangpackStore records imports.
type memLangpackStore struct {
	imported []string
	langs    []langpack.Language
}

func (m *memLangpackStore) Import(_ context.Context, s *langpack.Snapshot) (*langpack.ImportResult, error) {
	m.imported = append(m.imported, s.LangPack+"/"+s.LangCode)
	r := &langpack.ImportResult{LangPack: s.LangPack, LangCode: s.LangCode, Version: 5, Added: len(s.Strings)}
	if s.LangCode == "cs" {
		r.Leftovers = map[string][]string{"Welcome": {"Telegramu"}, "Intro": {"Telegramu", "Telegramem"}}
	}
	return r, nil
}

func (m *memLangpackStore) Languages(_ context.Context, pack string) ([]langpack.Language, error) {
	var out []langpack.Language
	for _, l := range m.langs {
		if pack == "" || l.LangPack == pack {
			out = append(out, l)
		}
	}
	return out, nil
}

func withMemStore(t *testing.T) *memLangpackStore {
	m := &memLangpackStore{}
	old := openLangpackStore
	openLangpackStore = func(string) (langpackStore, error) { return m, nil }
	t.Cleanup(func() { openLangpackStore = old })
	return m
}

func TestLangpackImport(t *testing.T) {
	store := withMemStore(t)
	dir := t.TempDir()
	for _, pl := range [][2]string{{"android", "cs"}, {"android", "ru"}, {"ios", "ru"}} {
		s := &langpack.Snapshot{LangPack: pl[0], LangCode: pl[1], Version: 1,
			Language: langpack.Language{LangCode: pl[1], PluralCode: pl[1]},
			Strings:  []langpack.String{{Key: "AppName", Value: "Telegram"}}}
		if _, err := langpack.WriteSnapshot(dir, s); err != nil {
			t.Fatal(err)
		}
	}
	code, out, errOut := runAdmin("langpack", "import", "-pack", "android", "-keys", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if fmt.Sprint(store.imported) != "[android/cs android/ru]" {
		t.Fatalf("imported %v", store.imported)
	}
	for _, want := range []string{
		"android/cs: new, version 5: 1 strings",
		"  2 string(s) still name Telegram: Telegramu ×2, Telegramem ×1",
		"    Intro: Telegramu, Telegramem",
		"    Welcome: Telegramu",
		"2 of 2 language(s) changed.",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if code, _, errOut = runAdmin("langpack", "import", "-lang", "de", dir); code != 1 || !strings.Contains(errOut, "no snapshot files") {
		t.Fatalf("no matching files: exit %d %q", code, errOut)
	}
}

func TestPrintImport(t *testing.T) {
	for _, c := range []struct {
		r    langpack.ImportResult
		want string
	}{
		{langpack.ImportResult{LangPack: "ios", LangCode: "ru", OldVersion: 3, Version: 3, Unchanged: 10},
			"ios/ru: no changes, version 3 (10 strings)\n"},
		{langpack.ImportResult{LangPack: "ios", LangCode: "ru", OldVersion: 3, Version: 4, Added: 1, Changed: 2, Deleted: 3, Unchanged: 4},
			"ios/ru: version 3 -> 4: 1 added, 2 changed, 3 deleted, 4 unchanged\n"},
	} {
		var b bytes.Buffer
		printImport(&b, &c.r, false)
		if b.String() != c.want {
			t.Errorf("printImport = %q, want %q", b.String(), c.want)
		}
	}
}

func TestLangpackList(t *testing.T) {
	store := withMemStore(t)
	store.langs = []langpack.Language{
		{LangPack: "android", LangCode: "ru", NativeName: "Русский", Version: 12, UpstreamVersion: 900, StringsCount: 10, TranslatedCount: 9, UpdatedAt: 1_800_000_000},
		{LangPack: "ios", LangCode: "en", NativeName: "English", Version: 1},
	}
	code, out, errOut := runAdmin("langpack", "list", "-pack", "android")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "android  ru    Русский  12       900               9/10     2027-01-15 08:00 UTC") || !strings.Contains(out, "1 language(s).") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestLangpackUsage(t *testing.T) {
	withMemStore(t)
	withFakeTelegram(t)
	for _, args := range [][]string{
		{"langpack"},
		{"langpack", "update"},
		{"langpack", "fetch"},
		{"langpack", "fetch", "-out", "x", "extra"},
		{"langpack", "fetch", "-out", "x", "-api-id", "-1"},
		{"langpack", "fetch", "-out", "x", "-api-id", "1", "-pack", ","},
		{"langpack", "import"},
		{"langpack", "import", "a", "b"},
		{"langpack", "list", "extra"},
	} {
		if code, _, _ := runAdmin(args...); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
}

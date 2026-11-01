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
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The fixtures in testdata/telegram are Telegram's answers to the Desktop
// pack (langpack.getLanguages, getLangPack and getStrings of 2026-04-10),
// cut down to a few languages and strings: the objects as a TL library
// printed them. The tests serialize them back to TL, as Telegram sent them.

type capturedLanguage struct {
	Official        bool    `json:"official"`
	RTL             bool    `json:"rtl"`
	Beta            bool    `json:"beta"`
	Name            string  `json:"name"`
	NativeName      string  `json:"nativeName"`
	LangCode        string  `json:"langCode"`
	BaseLangCode    *string `json:"baseLangCode"`
	PluralCode      string  `json:"pluralCode"`
	StringsCount    int32   `json:"stringsCount"`
	TranslatedCount int32   `json:"translatedCount"`
	TranslationsURL string  `json:"translationsUrl"`
}

type capturedString struct {
	ClassName  string  `json:"className"`
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	ZeroValue  *string `json:"zeroValue"`
	OneValue   *string `json:"oneValue"`
	TwoValue   *string `json:"twoValue"`
	FewValue   *string `json:"fewValue"`
	ManyValue  *string `json:"manyValue"`
	OtherValue string  `json:"otherValue"`
}

func (c *capturedString) tl() *mtproto.LangPackString {
	sv := func(s *string) *wrapperspb.StringValue {
		if s == nil {
			return nil
		}
		return wrapperspb.String(*s)
	}
	switch c.ClassName {
	case "LangPackString":
		return mtproto.MakeTLLangPackString(&mtproto.LangPackString{Key: c.Key, Value: c.Value}).To_LangPackString()
	case "LangPackStringPluralized":
		return mtproto.MakeTLLangPackStringPluralized(&mtproto.LangPackString{
			Key: c.Key, ZeroValue: sv(c.ZeroValue), OneValue: sv(c.OneValue), TwoValue: sv(c.TwoValue),
			FewValue: sv(c.FewValue), ManyValue: sv(c.ManyValue), OtherValue: c.OtherValue,
		}).To_LangPackString()
	default:
		return DeletedTL(c.Key)
	}
}

func readFixture(t *testing.T, name string, response any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "telegram", name))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Response json.RawMessage `json:"response"`
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(f.Response, response); err != nil {
		t.Fatal(err)
	}
}

func encodeTL(t *testing.T, o mtproto.TLObject) []byte {
	t.Helper()
	x := mtproto.NewEncodeBuf(1024)
	if err := o.Encode(x, mtclient.DefaultLayer); err != nil {
		t.Fatal(err)
	}
	return x.GetBuf()
}

func encodeVector(t *testing.T, objs []mtproto.TLObject) []byte {
	t.Helper()
	b := binary.LittleEndian.AppendUint32(nil, uint32(mtproto.CRC32_vector))
	b = binary.LittleEndian.AppendUint32(b, uint32(len(objs)))
	for _, o := range objs {
		b = append(b, encodeTL(t, o)...)
	}
	return b
}

// telegramLanguages is the getLanguages fixture as Telegram serialized it.
func telegramLanguages(t *testing.T) []byte {
	var langs []capturedLanguage
	readFixture(t, "getLanguages.tdesktop.json", &langs)
	var objs []mtproto.TLObject
	for _, l := range langs {
		var base *wrapperspb.StringValue
		if l.BaseLangCode != nil {
			base = wrapperspb.String(*l.BaseLangCode)
		}
		objs = append(objs, mtproto.MakeTLLangPackLanguage(&mtproto.LangPackLanguage{
			Official: l.Official, Rtl: l.RTL, Beta: l.Beta, Name: l.Name, NativeName: l.NativeName,
			LangCode: l.LangCode, BaseLangCode: base, PluralCode: l.PluralCode, StringsCount: l.StringsCount,
			TranslatedCount: l.TranslatedCount, TranslationsUrl: l.TranslationsURL,
		}))
	}
	return encodeVector(t, objs)
}

// telegramLangPack is the getLangPack fixture as Telegram serialized it.
func telegramLangPack(t *testing.T) []byte {
	var d struct {
		LangCode    string           `json:"langCode"`
		FromVersion int32            `json:"fromVersion"`
		Version     int32            `json:"version"`
		Strings     []capturedString `json:"strings"`
	}
	readFixture(t, "getLangPack.tdesktop.en.json", &d)
	var strs []*mtproto.LangPackString
	for i := range d.Strings {
		strs = append(strs, d.Strings[i].tl())
	}
	return encodeTL(t, mtproto.MakeTLLangPackDifference(&mtproto.LangPackDifference{
		LangCode: d.LangCode, FromVersion: d.FromVersion, Version: d.Version, Strings: strs,
	}))
}

// fakeTelegram answers langpack.getLanguages and getLangPack of tdesktop
// with the fixtures, after flood waits if asked.
type fakeTelegram struct {
	t          *testing.T
	floods     int // FLOOD_WAIT_3 answers before the real one
	floodWait  int
	requests   []string
	languages  []byte
	langPackEn []byte
}

func (f *fakeTelegram) InvokeRaw(_ context.Context, req mtproto.TLObject) ([]byte, error) {
	switch r := req.(type) {
	case *mtproto.TLLangpackGetLanguages:
		f.requests = append(f.requests, "getLanguages "+r.LangPack)
		if r.Constructor != mtproto.CRC32_langpack_getLanguages_42c6978f || r.LangPack != "tdesktop" {
			return nil, &mtclient.RPCError{Code: 400, Message: "LANG_PACK_INVALID"}
		}
		return f.languages, nil
	case *mtproto.TLLangpackGetLangPack:
		f.requests = append(f.requests, "getLangPack "+r.LangPack+" "+r.LangCode)
		if f.floods > 0 {
			f.floods--
			return nil, &mtclient.RPCError{Code: 420, Message: fmt.Sprintf("FLOOD_WAIT_%d", f.floodWait)}
		}
		if r.Constructor != mtproto.CRC32_langpack_getLangPack_f2f2330a || r.LangPack != "tdesktop" || r.LangCode != "en" {
			return nil, &mtclient.RPCError{Code: 400, Message: "LANG_CODE_NOT_SUPPORTED"}
		}
		return f.langPackEn, nil
	}
	f.t.Fatalf("unexpected request %T", req)
	return nil, nil
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	return &fakeTelegram{t: t, languages: telegramLanguages(t), langPackEn: telegramLangPack(t)}
}

func TestParseLanguages(t *testing.T) {
	langs, err := ParseLanguages("tdesktop", telegramLanguages(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(langs) != 5 {
		t.Fatalf("%d languages, want 5", len(langs))
	}
	want := Language{
		LangPack: "tdesktop", LangCode: "en", Name: "English", NativeName: "English", PluralCode: "en",
		Official: true, StringsCount: 7496, TranslatedCount: 7496,
		TranslationsURL: "https://translations.telegram.org/en/", SortOrder: 0,
	}
	if langs[0] != want {
		t.Fatalf("en = %+v, want %+v", langs[0], want)
	}
	var ptbr *Language
	for i := range langs {
		if langs[i].SortOrder != int32(i) {
			t.Errorf("%s: sort order %d, want %d", langs[i].LangCode, langs[i].SortOrder, i)
		}
		if langs[i].LangCode == "pt-br" {
			ptbr = &langs[i]
		}
	}
	if ptbr == nil || ptbr.PluralCode != "pt" || ptbr.NativeName != "Português (Brasil)" || ptbr.BaseLangCode != "" {
		t.Fatalf("pt-br = %+v", ptbr)
	}
}

func TestParseLangPack(t *testing.T) {
	en := Language{LangPack: "tdesktop", LangCode: "en", PluralCode: "en"}
	snap, err := ParseLangPack("tdesktop", en, telegramLangPack(t))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Version != 5313812 || snap.LangPack != "tdesktop" || snap.LangCode != "en" || len(snap.Strings) != 10 {
		t.Fatalf("snapshot %s/%s v%d with %d strings", snap.LangPack, snap.LangCode, snap.Version, len(snap.Strings))
	}
	byKey := map[string]String{}
	for _, s := range snap.Strings {
		byKey[s.Key] = s
	}
	if s := byKey["lng_cancel"]; s.Pluralized || s.Value != "Cancel" {
		t.Errorf("lng_cancel = %+v", s)
	}
	p := byKey["lng_accounts_limit1"]
	if !p.Pluralized || p.Value != "You have reached the limit of **{count}** connected accounts." ||
		p.One == nil || *p.One != "You have reached the limit of **{count}** connected account." ||
		p.Zero != nil || p.Two != nil || p.Few != nil || p.Many != nil {
		t.Errorf("lng_accounts_limit1 = %+v", p)
	}

	// Another language's answer, or a difference, is not taken for the pack.
	if _, err = ParseLangPack("tdesktop", Language{LangCode: "ru", PluralCode: "ru"}, telegramLangPack(t)); err == nil {
		t.Error("an answer for en accepted as ru")
	}
	diff := encodeTL(t, mtproto.MakeTLLangPackDifference(&mtproto.LangPackDifference{LangCode: "en", FromVersion: 5, Version: 6}))
	if _, err = ParseLangPack("tdesktop", en, diff); err == nil {
		t.Error("a difference accepted as the whole pack")
	}
	if _, err = ParseLanguages("tdesktop", diff); err == nil {
		t.Error("a difference accepted as a list of languages")
	}
}

func TestFetchPack(t *testing.T) {
	tg := newFakeTelegram(t)
	tg.floods, tg.floodWait = 2, 3
	var slept []time.Duration
	f := &Fetcher{
		Invoker:      tg,
		Delay:        time.Second,
		MaxFloodWait: 10 * time.Second,
		now:          func() time.Time { return time.Unix(1_800_000_000, 0) },
		sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	}
	dir := t.TempDir()
	var saved []string
	err := f.FetchPack(context.Background(), "tdesktop", []string{"EN"}, func(s *Snapshot) error {
		path, err := WriteSnapshot(dir, s)
		saved = append(saved, path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(tg.requests) != "[getLanguages tdesktop getLangPack tdesktop en getLangPack tdesktop en getLangPack tdesktop en]" {
		t.Fatalf("requests %v", tg.requests)
	}
	if fmt.Sprint(slept) != "[1s 3s 3s]" {
		t.Fatalf("waits %v, want the delay and two flood waits", slept)
	}
	if len(saved) != 1 || saved[0] != filepath.Join(dir, "tdesktop", "en.json") {
		t.Fatalf("saved %v", saved)
	}
	snap, err := ReadSnapshot(saved[0])
	if err != nil {
		t.Fatal(err)
	}
	if snap.FetchedAt != 1_800_000_000 || snap.Version != 5313812 || snap.Language.Name != "English" || len(snap.Strings) != 10 {
		t.Fatalf("snapshot %+v", snap.Language)
	}

	// A language Telegram does not have fails before any pack is read.
	tg.requests = nil
	if err = f.FetchPack(context.Background(), "tdesktop", []string{"xx"}, func(*Snapshot) error { return nil }); err == nil {
		t.Fatal("a missing language fetched")
	}
	// A flood wait above the limit fails the fetch.
	tg.floods, tg.floodWait = 1, 3600
	err = f.FetchPack(context.Background(), "tdesktop", []string{"en"}, func(*Snapshot) error { return nil })
	if !mtclient.IsRPCError(err, "FLOOD_WAIT_3600") {
		t.Fatalf("long flood wait: %v", err)
	}
}

// Every language of the pack is fetched when none is named; a language
// whose pack Telegram refuses fails the fetch with the error.
func TestFetchPackAllLanguages(t *testing.T) {
	tg := newFakeTelegram(t)
	f := &Fetcher{Invoker: tg, sleep: func(context.Context, time.Duration) error { return nil }}
	var got []string
	err := f.FetchPack(context.Background(), "tdesktop", nil, func(s *Snapshot) error {
		got = append(got, s.LangCode)
		return nil
	})
	if fmt.Sprint(got) != "[en]" || !mtclient.IsRPCError(err, "LANG_CODE_NOT_SUPPORTED") {
		t.Fatalf("got %v, %v; want en, then the refusal of the second language", got, err)
	}
	if _, err = f.Languages(context.Background(), "nope"); !mtclient.IsRPCError(err, "LANG_PACK_INVALID") {
		t.Fatalf("unknown pack: %v", err)
	}
}

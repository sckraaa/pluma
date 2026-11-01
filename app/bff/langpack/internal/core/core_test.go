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

package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/langpack/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/langpack/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/langpack"

	"github.com/zeromicro/go-zero/core/logx"
)

// memStore is langpack.Store in memory: languages by pack/lang, their
// strings with versions and tombstones.
type memStore struct {
	langs   map[string]*langpack.Language
	strs    map[string][]langpack.String
	reads   int
	failErr error
}

func key(pack, lang string) string { return pack + "/" + lang }

func (m *memStore) PackExists(_ context.Context, pack string) (bool, error) {
	for _, l := range m.langs {
		if l.LangPack == pack {
			return true, nil
		}
	}
	return false, nil
}

func (m *memStore) Languages(_ context.Context, pack string) ([]langpack.Language, error) {
	if m.failErr != nil {
		return nil, m.failErr
	}
	var out []langpack.Language
	for _, l := range m.langs {
		if l.LangPack == pack {
			out = append(out, *l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SortOrder < out[j].SortOrder })
	return out, nil
}

func (m *memStore) Language(_ context.Context, pack, lang string) (*langpack.Language, error) {
	if l := m.langs[key(pack, lang)]; l != nil {
		c := *l
		return &c, nil
	}
	return nil, langpack.ErrNotFound
}

func (m *memStore) Read(ctx context.Context, pack, lang string, from int32) (*langpack.Language, []langpack.String, error) {
	m.reads++
	l, err := m.Language(ctx, pack, lang)
	if err != nil {
		return nil, nil, err
	}
	var out []langpack.String
	for _, s := range m.strs[key(pack, lang)] {
		if (from <= 0 && !s.Deleted) || (from > 0 && s.Version > from) {
			out = append(out, s)
		}
	}
	return l, out, nil
}

func (m *memStore) StringsByKey(_ context.Context, pack, lang string, keys []string) (map[string]*langpack.String, error) {
	found := map[string]*langpack.String{}
	for _, s := range m.strs[key(pack, lang)] {
		for _, k := range keys {
			if s.Key == k {
				c := s
				found[k] = &c
			}
		}
	}
	return found, nil
}

func newStore() *memStore {
	m := &memStore{langs: map[string]*langpack.Language{}, strs: map[string][]langpack.String{}}
	for i, pack := range []string{"android", "webk"} {
		for j, code := range []string{"en", "ru"} {
			m.langs[key(pack, code)] = &langpack.Language{LangPack: pack, LangCode: code, Name: code, NativeName: code,
				PluralCode: code, SortOrder: int32(j), Version: 10 + int32(i)}
			m.strs[key(pack, code)] = []langpack.String{
				{Key: "AppName", Value: "Pluma", Version: 5},
				{Key: "Chats", Value: "Chats " + code, Version: 9},
				{Key: "Gone", Deleted: true, Version: 8},
			}
		}
	}
	return m
}

func newCore(store *memStore, md *metadata.RpcMetadata) *LangpackCore {
	ctx := context.Background()
	return &LangpackCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{Dao: dao.NewWithStore(store)},
		Logger: logx.WithContext(ctx),
		MD:     md,
	}
}

func describe(strs []*mtproto.LangPackString) string {
	var out []string
	for _, s := range strs {
		out = append(out, s.GetKey()+":"+strings.TrimPrefix(s.GetPredicateName(), "langPackString"))
	}
	return strings.Join(out, " ")
}

func isError(err, want error) bool {
	return err != nil && err.Error() == want.Error()
}

// An empty lang_pack is the pack of the session's initConnection; "web" is
// Web K's "webk".
func TestPackResolution(t *testing.T) {
	store := newStore()
	android := newCore(store, &metadata.RpcMetadata{Langpack: "android", LangCode: "ru"})
	d, err := android.LangpackGetLangPack(&mtproto.TLLangpackGetLangPack{
		Constructor: mtproto.CRC32_langpack_getLangPack_9ab5c58e, LangCode: "ru"})
	if err != nil {
		t.Fatal(err)
	}
	if d.GetLangCode() != "ru" || d.GetFromVersion() != 0 || d.GetVersion() != 10 || describe(d.GetStrings()) != "AppName: Chats:" {
		t.Fatalf("getLangPack(\"\", ru): %s %d->%d %s", d.GetLangCode(), d.GetFromVersion(), d.GetVersion(), describe(d.GetStrings()))
	}

	webk := newCore(store, &metadata.RpcMetadata{Langpack: "webk"})
	d, err = webk.LangpackGetLangPack(&mtproto.TLLangpackGetLangPack{LangPack: "web", LangCode: "EN"})
	if err != nil || d.GetVersion() != 11 || d.GetLangCode() != "en" {
		t.Fatalf("getLangPack(web, EN): %v, %v", d, err)
	}
	langs, err := webk.LangpackGetLanguages(&mtproto.TLLangpackGetLanguages{LangPack: "web"})
	if err != nil || len(langs.GetDatas()) != 2 || langs.GetDatas()[0].GetLangCode() != "en" || langs.GetDatas()[1].GetPluralCode() != "ru" {
		t.Fatalf("getLanguages(web): %v, %v", langs, err)
	}
	// The constructor without lang_pack: the session's pack.
	langs, err = android.LangpackGetLanguages(&mtproto.TLLangpackGetLanguages{Constructor: mtproto.CRC32_langpack_getLanguages_800fd57d})
	if err != nil || len(langs.GetDatas()) != 2 {
		t.Fatalf("getLanguages(): %v, %v", langs, err)
	}
	// Explicit lang_pack wins over the session's.
	l, err := android.LangpackGetLanguage(&mtproto.TLLangpackGetLanguage{LangPack: "webk", LangCode: "ru"})
	if err != nil || l.GetLangCode() != "ru" || l.GetNativeName() != "ru" {
		t.Fatalf("getLanguage(webk, ru): %v, %v", l, err)
	}
}

func TestUnknownPacksAndLanguages(t *testing.T) {
	store := newStore()
	none := newCore(store, &metadata.RpcMetadata{})
	ios := newCore(store, &metadata.RpcMetadata{Langpack: "ios"})
	android := newCore(store, &metadata.RpcMetadata{Langpack: "android"})

	for name, err := range map[string]error{
		"getLangPack no pack":      second(none.LangpackGetLangPack(&mtproto.TLLangpackGetLangPack{LangCode: "en"})),
		"getLangPack unknown pack": second(ios.LangpackGetLangPack(&mtproto.TLLangpackGetLangPack{LangCode: "en"})),
		"getDifference unknown":    second(ios.LangpackGetDifference(&mtproto.TLLangpackGetDifference{LangCode: "en", FromVersion: 3})),
		"getLanguage unknown pack": second(ios.LangpackGetLanguage(&mtproto.TLLangpackGetLanguage{LangCode: "en"})),
	} {
		if !isError(err, mtproto.ErrLangPackInvalid) {
			t.Errorf("%s: %v, want LANG_PACK_INVALID", name, err)
		}
	}
	for name, err := range map[string]error{
		"getLangPack":   second(android.LangpackGetLangPack(&mtproto.TLLangpackGetLangPack{LangCode: "xx"})),
		"getDifference": second(android.LangpackGetDifference(&mtproto.TLLangpackGetDifference{LangCode: "xx"})),
		"getLanguage":   second(android.LangpackGetLanguage(&mtproto.TLLangpackGetLanguage{LangCode: "xx"})),
	} {
		if !isError(err, mtproto.ErrLangCodeNotSupported) {
			t.Errorf("%s of an unknown language: %v, want LANG_CODE_NOT_SUPPORTED", name, err)
		}
	}

	// getLanguages and getStrings answer with empty vectors.
	if v, err := ios.LangpackGetLanguages(&mtproto.TLLangpackGetLanguages{}); err != nil || v.GetDatas() == nil || len(v.GetDatas()) != 0 {
		t.Errorf("getLanguages of an unknown pack: %v, %v", v, err)
	}
	if v, err := none.LangpackGetLanguages(&mtproto.TLLangpackGetLanguages{}); err != nil || len(v.GetDatas()) != 0 {
		t.Errorf("getLanguages without a pack: %v, %v", v, err)
	}
	for _, c := range []*LangpackCore{none, ios} {
		if v, err := c.LangpackGetStrings(&mtproto.TLLangpackGetStrings{LangCode: "en", Keys: []string{"AppName"}}); err != nil ||
			v.GetDatas() == nil || len(v.GetDatas()) != 0 {
			t.Errorf("getStrings of an unknown pack: %v, %v", v, err)
		}
	}
	if v, err := android.LangpackGetStrings(&mtproto.TLLangpackGetStrings{LangCode: "xx", Keys: []string{"AppName"}}); err != nil || len(v.GetDatas()) != 0 {
		t.Errorf("getStrings of an unknown language: %v, %v", v, err)
	}

	// A store failure is not taken for an unknown pack.
	store.failErr = errors.New("db down")
	if _, err := android.LangpackGetLanguages(&mtproto.TLLangpackGetLanguages{}); err == nil {
		t.Error("getLanguages hid a store failure")
	}
}

func second[T any](_ T, err error) error { return err }

func TestGetDifference(t *testing.T) {
	store := newStore()
	android := newCore(store, &metadata.RpcMetadata{Langpack: "android"})
	webk := newCore(store, &metadata.RpcMetadata{Langpack: "webk"})
	for _, c := range []struct {
		core        *LangpackCore
		from        int32
		fromVersion int32
		want        string
	}{
		{android, 0, 0, "AppName: Chats:"},
		{android, 7, 7, "Chats: Gone:Deleted"},
		{android, 8, 8, "Chats:"},
		{android, 10, 10, ""},
		{android, 99, 99, ""},
		{webk, 7, 7, "Chats:"}, // no langPackStringDeleted for Web K
	} {
		d, err := c.core.LangpackGetDifference(&mtproto.TLLangpackGetDifference{LangCode: "en", FromVersion: c.from})
		if err != nil {
			t.Fatal(err)
		}
		if d.GetFromVersion() != c.fromVersion || describe(d.GetStrings()) != c.want || d.GetLangCode() != "en" || d.GetStrings() == nil {
			t.Errorf("%s from %d: %d->%d %q, want from %d %q", c.core.MD.Langpack, c.from, d.GetFromVersion(), d.GetVersion(),
				describe(d.GetStrings()), c.fromVersion, c.want)
		}
	}
}

func TestGetStrings(t *testing.T) {
	android := newCore(newStore(), &metadata.RpcMetadata{Langpack: "android"})
	v, err := android.LangpackGetStrings(&mtproto.TLLangpackGetStrings{
		Constructor: mtproto.CRC32_langpack_getStrings_2e1ee318, LangCode: "ru", Keys: []string{"Chats", "Unknown", "Gone"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(v.GetDatas()); got != "Chats: Unknown:Deleted Gone:Deleted" || v.GetDatas()[0].GetValue() != "Chats ru" {
		t.Fatalf("getStrings = %s", got)
	}
}

// The whole pack is built once per version and rebuilt after an import.
func TestFullPackCache(t *testing.T) {
	store := newStore()
	android := newCore(store, &metadata.RpcMetadata{Langpack: "android"})
	get := func() *mtproto.LangPackDifference {
		d, err := android.LangpackGetLangPack(&mtproto.TLLangpackGetLangPack{LangCode: "en"})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	first := get()
	if d := get(); d != first || store.reads != 1 {
		t.Fatalf("second getLangPack read the store again (%d reads)", store.reads)
	}
	if d, _ := android.LangpackGetDifference(&mtproto.TLLangpackGetDifference{LangCode: "en"}); d != first || store.reads != 1 {
		t.Fatalf("getDifference from 0 read the store again (%d reads)", store.reads)
	}

	l := store.langs[key("android", "en")]
	l.Version = 12
	store.strs[key("android", "en")] = append(store.strs[key("android", "en")], langpack.String{Key: "New", Value: "New", Version: 12})
	d := get()
	if store.reads != 2 || d.GetVersion() != 12 || describe(d.GetStrings()) != "AppName: Chats: New:" {
		t.Fatalf("after an import: %d reads, v%d %s", store.reads, d.GetVersion(), describe(d.GetStrings()))
	}
}

func TestPackCacheBound(t *testing.T) {
	c := dao.NewPackCache(2)
	build := func(v int32) func() (*mtproto.LangPackDifference, int32, error) {
		return func() (*mtproto.LangPackDifference, int32, error) {
			return langpack.Difference(&langpack.Language{Version: v}, nil, 0, true), v, nil
		}
	}
	for i, k := range []string{"a", "b", "c"} {
		if _, err := c.Build("android", k, build(int32(i+1))); err != nil {
			t.Fatal(err)
		}
	}
	if c.Len() != 2 || c.Get("android", "a", 1) != nil || c.Get("android", "c", 3) == nil || c.Get("android", "c", 2) != nil {
		t.Fatalf("cache of %d after three builds", c.Len())
	}
	// An older build does not replace a newer one.
	if _, err := c.Build("android", "c", build(1)); err != nil {
		t.Fatal(err)
	}
	if c.Get("android", "c", 3) == nil {
		t.Fatal("an older pack replaced a newer one")
	}
	if _, err := c.Build("android", "d", func() (*mtproto.LangPackDifference, int32, error) {
		return nil, 0, fmt.Errorf("db down")
	}); err == nil || c.Len() != 2 {
		t.Fatal("a failed build was cached")
	}
}

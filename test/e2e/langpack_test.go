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

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/langpack"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// e2ePack is the pack of this test: a name no client uses, so the packs
// the stand's clients get stay as they are. A stand keeps it between runs;
// the test compares versions only with each other.
const e2ePack = "e2e"

// importLangPacks imports snapshots with pluma-admin langpack import in the
// bff container.
func importLangPacks(t *testing.T, snaps ...*langpack.Snapshot) string {
	t.Helper()
	dir := t.TempDir()
	for _, s := range snaps {
		if _, err := langpack.WriteSnapshot(dir, s); err != nil {
			t.Fatal(err)
		}
	}
	dest := fmt.Sprintf("/tmp/langpack-e2e-%d", uint64(randomID()))
	stack(t, "cp", dir, "bff:"+dest)
	defer stack(t, "exec", "-T", "bff", "rm", "-rf", dest)
	return admin(t, "langpack", "import", dest)
}

func e2eSnapshot(lang string, strs ...langpack.String) *langpack.Snapshot {
	return &langpack.Snapshot{
		LangPack: e2ePack, LangCode: lang, Version: 1,
		Language: langpack.Language{LangCode: lang, Name: "Lang " + lang, NativeName: "Native " + lang, PluralCode: lang,
			StringsCount: int32(len(strs)), TranslatedCount: int32(len(strs)), TranslationsURL: "https://translations.telegram.org/" + lang + "/"},
		Strings: strs,
	}
}

// describeStrings prints strings as key=value, deleted ones as key:deleted,
// sorted by key.
func describeStrings(strs []*mtproto.LangPackString) string {
	var out []string
	for _, s := range strs {
		switch s.GetPredicateName() {
		case mtproto.Predicate_langPackStringDeleted:
			out = append(out, s.GetKey()+":deleted")
		case mtproto.Predicate_langPackStringPluralized:
			out = append(out, fmt.Sprintf("%s=%s|%s", s.GetKey(), s.GetOneValue().GetValue(), s.GetOtherValue()))
		default:
			out = append(out, s.GetKey()+"="+s.GetValue())
		}
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func langPackDifference(ctx context.Context, t *testing.T, c *mtclient.Client, req mtproto.TLObject) *mtproto.LangPackDifference {
	t.Helper()
	o, err := c.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("%T: %v", req, err)
	}
	d, ok := o.(*mtproto.TLLangPackDifference)
	if !ok {
		t.Fatalf("%T: got %T", req, o)
	}
	return d.To_LangPackDifference()
}

func invokeVector(ctx context.Context, t *testing.T, c *mtclient.Client, req mtproto.TLObject) []mtproto.TLObject {
	t.Helper()
	raw, err := c.InvokeRaw(ctx, req)
	if err != nil {
		t.Fatalf("%T: %v", req, err)
	}
	objs, err := mtclient.DecodeVector(raw)
	if err != nil {
		t.Fatalf("%T: %v", req, err)
	}
	return objs
}

// Language packs (task 021): the languages of a pack, the whole pack and
// its differences by version, getStrings, and the version help.getConfig
// reports, without logging in, the way the clients ask (Android: empty
// lang_pack and the constructors without it).
func TestLangPacks(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	one := "%d Telegram chat"
	v1 := []langpack.String{
		{Key: "AppName", Value: "Telegram"},
		{Key: "Chats", Pluralized: true, Value: "%d Telegram chats", One: &one},
		{Key: "Cancel", Value: "Cancel"},
		{Key: "Old", Value: "Old"},
	}
	out := importLangPacks(t, e2eSnapshot("en", v1...), e2eSnapshot("ru", langpack.String{Key: "AppName", Value: "Telegram"}))
	if !strings.Contains(out, "e2e/en: ") || !strings.Contains(out, "e2e/ru: ") {
		t.Fatalf("import printed:\n%s", out)
	}

	cfg := config(t, "e2e langpack")
	cfg.LangPack, cfg.LangCode = e2ePack, "en"
	c, err := mtclient.DialNewKey(ctx, cfg, mtclient.KeyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	closeChecked(t, "langpack", c)
	conf, err := c.InitConnection(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// getLanguages with the pack named and with the session's.
	for _, req := range []*mtproto.TLLangpackGetLanguages{
		{Constructor: mtproto.CRC32_langpack_getLanguages_42c6978f, LangPack: e2ePack},
		{Constructor: mtproto.CRC32_langpack_getLanguages_800fd57d},
	} {
		var codes []string
		for _, o := range invokeVector(ctx, t, c, req) {
			l := o.(*mtproto.TLLangPackLanguage).To_LangPackLanguage()
			codes = append(codes, l.GetLangCode())
			if l.GetLangCode() == "ru" && (l.GetNativeName() != "Native ru" || l.GetPluralCode() != "ru" || l.GetStringsCount() != 1) {
				t.Errorf("ru = %v", l)
			}
		}
		if fmt.Sprint(codes) != "[en ru]" {
			t.Errorf("getLanguages(%q): %v", req.LangPack, codes)
		}
	}
	// An unknown pack has no languages, and no error.
	if objs := invokeVector(ctx, t, c, &mtproto.TLLangpackGetLanguages{
		Constructor: mtproto.CRC32_langpack_getLanguages_42c6978f, LangPack: "no-such-pack"}); len(objs) != 0 {
		t.Errorf("getLanguages of an unknown pack: %d languages", len(objs))
	}

	// The whole pack, with lang_pack from initConnection (Android's
	// constructor has none), "Telegram" replaced.
	full := langPackDifference(ctx, t, c, &mtproto.TLLangpackGetLangPack{
		Constructor: mtproto.CRC32_langpack_getLangPack_9ab5c58e, LangCode: "en"})
	v := full.GetVersion()
	if full.GetLangCode() != "en" || full.GetFromVersion() != 0 || v <= 0 ||
		describeStrings(full.GetStrings()) != "AppName=Pluma Cancel=Cancel Chats=%d Pluma chat|%d Pluma chats Old=Old" {
		t.Fatalf("getLangPack: %s %d->%d %s", full.GetLangCode(), full.GetFromVersion(), v, describeStrings(full.GetStrings()))
	}

	// help.getConfig reports that version for the session's language.
	if conf.GetLangPackVersion().GetValue() != v || conf.GetSuggestedLangCode().GetValue() != "en" ||
		conf.GetBaseLangPackVersion() == nil || conf.GetBaseLangPackVersion().GetValue() != 0 {
		t.Errorf("config: suggested %v, lang_pack_version %v, base %v; want en, %d, 0",
			conf.GetSuggestedLangCode(), conf.GetLangPackVersion(), conf.GetBaseLangPackVersion(), v)
	}

	// getDifference from 0 is the whole pack, from the current version
	// nothing; both with Android's empty lang_pack.
	diff := func(from int32) *mtproto.LangPackDifference {
		return langPackDifference(ctx, t, c, &mtproto.TLLangpackGetDifference{
			Constructor: mtproto.CRC32_langpack_getDifference, LangCode: "en", FromVersion: from})
	}
	if d := diff(0); d.GetFromVersion() != 0 || d.GetVersion() != v || describeStrings(d.GetStrings()) != describeStrings(full.GetStrings()) {
		t.Errorf("getDifference(0): %d->%d %s", d.GetFromVersion(), d.GetVersion(), describeStrings(d.GetStrings()))
	}
	if d := diff(v); d.GetFromVersion() != v || d.GetVersion() != v || len(d.GetStrings()) != 0 {
		t.Errorf("getDifference(%d): %d->%d %s", v, d.GetFromVersion(), d.GetVersion(), describeStrings(d.GetStrings()))
	}

	// getStrings: an unknown key comes back deleted.
	var strs []*mtproto.LangPackString
	for _, o := range invokeVector(ctx, t, c, &mtproto.TLLangpackGetStrings{
		Constructor: mtproto.CRC32_langpack_getStrings_efea3803, LangPack: e2ePack, LangCode: "en", Keys: []string{"Cancel", "NoSuchKey"}}) {
		s, ok := o.(*mtproto.TLLangPackString)
		if ok {
			strs = append(strs, s.To_LangPackString())
			continue
		}
		d, ok := o.(*mtproto.TLLangPackStringDeleted)
		if !ok {
			t.Fatalf("getStrings element %T", o)
		}
		strs = append(strs, d.To_LangPackString())
	}
	if got := describeStrings(strs); got != "Cancel=Cancel NoSuchKey:deleted" {
		t.Errorf("getStrings = %s", got)
	}

	// An unknown language of the pack.
	if _, err = c.Invoke(ctx, &mtproto.TLLangpackGetLangPack{
		Constructor: mtproto.CRC32_langpack_getLangPack_f2f2330a, LangPack: e2ePack, LangCode: "xx"}); !mtclient.IsRPCError(err, "LANG_CODE_NOT_SUPPORTED") {
		t.Errorf("getLangPack of an unknown language: %v", err)
	}

	// A new import: one string changed, one removed. The difference from the
	// old version has exactly that; getConfig reports the new version.
	v2 := []langpack.String{v1[0], v1[1], {Key: "Cancel", Value: "Dismiss"}}
	importLangPacks(t, e2eSnapshot("en", v2...))
	d := diff(v)
	if d.GetFromVersion() != v || d.GetVersion() <= v || describeStrings(d.GetStrings()) != "Cancel=Dismiss Old:deleted" {
		t.Fatalf("getDifference(%d) after the import: %d->%d %s", v, d.GetFromVersion(), d.GetVersion(), describeStrings(d.GetStrings()))
	}
	conf2, err := c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig})
	if err != nil {
		t.Fatal(err)
	}
	if got := conf2.(*mtproto.TLConfig).GetLangPackVersion().GetValue(); got != d.GetVersion() {
		t.Errorf("config lang_pack_version %d after the import, want %d", got, d.GetVersion())
	}

}

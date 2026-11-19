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
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/langpack"
)

// The language of the device's system and the updates of a pack (task 114):
// an app in English on a Russian system is offered Russian by
// help.getConfig, and after an import that changes its language its session
// gets updateLangPackTooLong and takes the change with getDifference.
func TestLangPackSystemLanguageAndUpdates(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// A pack of this test alone: TestLangPacks imports its own in parallel.
	const pack = "e2e-updates"
	snapshot := func(lang string, strs ...langpack.String) *langpack.Snapshot {
		s := e2eSnapshot(lang, strs...)
		s.LangPack = pack
		return s
	}
	importLangPacks(t, snapshot("en", langpack.String{Key: "Hello", Value: "Hello"}),
		snapshot("ru", langpack.String{Key: "Hello", Value: "Привет"}))

	cfg := config(t, "e2e Lena")
	cfg.LangPack, cfg.LangCode, cfg.SystemLangCode = pack, "en", "ru-RU"
	u := loginWith(ctx, t, testPhones(t, 1)[0], "Lena", cfg)

	full := langPackDifference(ctx, t, u.c, &mtproto.TLLangpackGetLangPack{
		Constructor: mtproto.CRC32_langpack_getLangPack_9ab5c58e, LangCode: "en"})
	v := full.GetVersion()
	o, err := u.c.Invoke(ctx, &mtproto.TLHelpGetConfig{Constructor: mtproto.CRC32_help_getConfig})
	if err != nil {
		t.Fatal(err)
	}
	conf := o.(*mtproto.TLConfig)
	if conf.GetSuggestedLangCode().GetValue() != "ru" || conf.GetLangPackVersion().GetValue() != v {
		t.Fatalf("config: suggested %v, lang_pack_version %v; want ru, %d (the version of en)",
			conf.GetSuggestedLangCode(), conf.GetLangPackVersion(), v)
	}

	// The session hears of the new version of its language.
	getState(ctx, t, u)
	out := importLangPacks(t, snapshot("en", langpack.String{Key: "Hello", Value: "Hello again"}))
	if !strings.Contains(out, "told of the new versions of 1 language(s)") {
		t.Fatalf("import printed:\n%s", out)
	}
	waitUpdate(ctx, t, u, mtproto.Predicate_updateLangPackTooLong, func(x *mtproto.Update) bool {
		return x.GetLangCode() == "en"
	})
	d := langPackDifference(ctx, t, u.c, &mtproto.TLLangpackGetDifference{
		Constructor: mtproto.CRC32_langpack_getDifference, LangCode: "en", FromVersion: v})
	if d.GetVersion() <= v || describeStrings(d.GetStrings()) != "Hello=Hello again" {
		t.Fatalf("getDifference(%d): %d->%d %s", v, d.GetFromVersion(), d.GetVersion(), describeStrings(d.GetStrings()))
	}
}

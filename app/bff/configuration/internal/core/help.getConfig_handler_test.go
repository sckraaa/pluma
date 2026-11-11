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
	"path/filepath"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/langpack"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/proto"
)

// fakeLangpacks answers ConfigVersions with a fixed result and records what
// it was asked.
type fakeLangpacks struct {
	v          *langpack.ConfigVersions
	err        error
	pack, lang string
}

func (f *fakeLangpacks) ConfigVersions(_ context.Context, pack, lang string) (*langpack.ConfigVersions, error) {
	f.pack, f.lang = pack, lang
	return f.v, f.err
}

// newGetConfigCore is a core over the deployed config.json and store, for a
// session that sent lang_pack and lang_code in initConnection.
func newGetConfigCore(t *testing.T, store dao.LangpackVersions, pack, lang string) *ConfigurationCore {
	t.Helper()
	cfg, err := svc.LoadClientConfig(filepath.Join("..", "..", "..", "..", "..", "teamgramd", "bin", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	return &ConfigurationCore{
		ctx:    ctx,
		svcCtx: &svc.ServiceContext{ClientConfig: cfg, Dao: &dao.Dao{Langpacks: store}},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{Langpack: pack, LangCode: lang},
	}
}

// flags.2 of config: its three fields are written together or not at all
// (the encoder writes each one that is set, and a partial set breaks the
// layout the clients read).
func langPackFlag(t *testing.T, c *mtproto.Config) (set bool, code string, version, base int32) {
	t.Helper()
	s, v, b := c.GetSuggestedLangCode(), c.GetLangPackVersion(), c.GetBaseLangPackVersion()
	if (s == nil) != (v == nil) || (v == nil) != (b == nil) {
		t.Fatalf("flags.2 partly set: suggested_lang_code %v, lang_pack_version %v, base_lang_pack_version %v", s, v, b)
	}
	if s == nil {
		return false, "", 0, 0
	}
	return true, s.GetValue(), v.GetValue(), b.GetValue()
}

func TestHelpGetConfigLangPack(t *testing.T) {
	for _, c := range []struct {
		name       string
		store      *fakeLangpacks
		pack, lang string
		wantSet    bool
		wantCode   string
		wantV      int32
		wantBase   int32
	}{
		{name: "no pack stored", store: &fakeLangpacks{}, pack: "android", lang: "ru"},
		{name: "store error", store: &fakeLangpacks{err: errors.New("mysql is down")}, pack: "android", lang: "ru"},
		{
			name:  "pack stored",
			store: &fakeLangpacks{v: &langpack.ConfigVersions{SuggestedLangCode: "pt-br", LangPackVersion: 41, BaseLangPackVersion: 30}},
			pack:  "web", lang: "pt_BR",
			wantSet: true, wantCode: "pt-br", wantV: 41, wantBase: 30,
		},
		{
			name:  "pack stored, no base language",
			store: &fakeLangpacks{v: &langpack.ConfigVersions{SuggestedLangCode: "ru", LangPackVersion: 7}},
			pack:  "ios", lang: "ru",
			wantSet: true, wantCode: "ru", wantV: 7,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			core := newGetConfigCore(t, c.store, c.pack, c.lang)
			before := proto.Clone(core.svcCtx.ClientConfig)
			got, err := core.HelpGetConfig(&mtproto.TLHelpGetConfig{})
			if err != nil {
				t.Fatalf("HelpGetConfig: %v", err)
			}
			// The rest of config is served either way.
			if got.GetDcTxtDomainName() != core.svcCtx.ClientConfig.GetDcTxtDomainName() || got.GetChatSizeMax() == 0 || got.GetExpires() <= got.GetDate() {
				t.Fatalf("config not served: dc_txt_domain_name %q, chat_size_max %d, date %d, expires %d",
					got.GetDcTxtDomainName(), got.GetChatSizeMax(), got.GetDate(), got.GetExpires())
			}
			// The clients size supergroups by it and offer a broadcast
			// group 1000 under it: it is the limit biz channel keeps.
			if got.GetMegagroupSizeMax() != channelpb.MembersMax {
				t.Fatalf("megagroup_size_max %d, the members of a supergroup go up to %d", got.GetMegagroupSizeMax(), channelpb.MembersMax)
			}
			set, code, v, base := langPackFlag(t, got)
			if set != c.wantSet || code != c.wantCode || v != c.wantV || base != c.wantBase {
				t.Fatalf("flags.2 = %v %q %d %d, want %v %q %d %d", set, code, v, base, c.wantSet, c.wantCode, c.wantV, c.wantBase)
			}
			// The store is asked for the session's pack and language as stored.
			wantPack, wantLang := langpack.CanonicalPack(c.pack), langpack.CanonicalLangCode(c.lang)
			if c.store.pack != wantPack || c.store.lang != wantLang {
				t.Fatalf("asked for %s/%s, want %s/%s", c.store.pack, c.store.lang, wantPack, wantLang)
			}
			// The answer is a copy: config.json stays without flags.2.
			if !proto.Equal(before, core.svcCtx.ClientConfig) {
				t.Fatal("the answer changed the base config")
			}
		})
	}
}

// TestHelpGetConfigAutoupdatePrefix: with the site of the releases set,
// help.getConfig tells the desktop app where its updates are (task 054);
// without it, nothing.
func TestHelpGetConfigAutoupdatePrefix(t *testing.T) {
	c := newGetConfigCore(t, &fakeLangpacks{}, "", "en")
	got, err := c.HelpGetConfig(&mtproto.TLHelpGetConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetAutoupdateUrlPrefix() != nil {
		t.Errorf("without a site: %v", got.GetAutoupdateUrlPrefix())
	}
	c.svcCtx.Config.ReleasesURL = "https://pluma.example"
	if got, err = c.HelpGetConfig(&mtproto.TLHelpGetConfig{}); err != nil {
		t.Fatal(err)
	}
	if v := got.GetAutoupdateUrlPrefix().GetValue(); v != "https://pluma.example/dl/desktop" {
		t.Errorf("autoupdate_url_prefix %q", v)
	}
}

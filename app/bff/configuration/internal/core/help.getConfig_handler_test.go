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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/cdn"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
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

// TestHelpGetConfigInlineBots: the GIF and picture search bots are always
// named, by the usernames of the built-in bots (ADR 0062, p. 2). Android has
// no picture search bot without img_search_username
// (MessagesController.java:5691). The server has no places bot (task 099):
// venue_search_username keeps its upstream value.
func TestHelpGetConfigInlineBots(t *testing.T) {
	c := newGetConfigCore(t, &fakeLangpacks{}, "android", "en")
	got, err := c.HelpGetConfig(&mtproto.TLHelpGetConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for field, f := range map[string]struct {
		v    interface{ GetValue() string }
		want string
	}{
		"gif_search_username":   {got.GetGifSearchUsername(), inlinebots.Gif.Username},
		"img_search_username":   {got.GetImgSearchUsername(), inlinebots.Pic.Username},
		"venue_search_username": {got.GetVenueSearchUsername(), "foursquare"},
	} {
		if f.v.GetValue() != f.want {
			t.Errorf("%s = %v, want %q", field, f.v, f.want)
		}
	}
}

// TestHelpGetConfigWebFileDC: clients send upload.getWebFile to webfile_dc_id
// (Android FileLoadOperation.java:411, iOS RemoteStorageConfiguration.swift:6-16,
// tdesktop file_download_mtproto.cpp:64, Web K apiFileManager.ts:381) and
// know only the DC of the server; tgnet holds a request to an unknown DC
// unanswered (ConnectionsManager.cpp:2584-2590). So it is this_dc (ADR
// 0062, p. 5).
func TestHelpGetConfigWebFileDC(t *testing.T) {
	c := newGetConfigCore(t, &fakeLangpacks{}, "android", "en")
	got, err := c.HelpGetConfig(&mtproto.TLHelpGetConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetWebfileDcId() != got.GetThisDc() || got.GetThisDc() != 1 {
		t.Errorf("webfile_dc_id %d, this_dc %d", got.GetWebfileDcId(), got.GetThisDc())
	}
}

// TestHelpGetConfigDcOptions: dc_options carries DC 1 and the CDN DC of the
// configuration as the clients read them off the wire (task 102, ADR 0065
// p. 7), and nothing without a configuration. The flags word is what
// tdesktop (mtproto_dc_options.cpp:677-697) and Android
// (ConnectionsManager.cpp:3450-3482) choose the endpoints by.
func TestHelpGetConfigDcOptions(t *testing.T) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Dc1Addresses: "203.0.113.10:10443,203.0.113.10:5222",
		Cdn: cdn.Config{
			NodeControl:          "10.77.0.1:20510",
			CertFile:             "client.pem",
			KeyFile:              "client-key.pem",
			CAFile:               "server-ca.pem",
			DcAddress:            "203.0.113.10:8443",
			DcSecret:             "dd" + hex.EncodeToString(raw),
			MonthlyEgressLimitGB: "1000",
		},
	}
	options, err := cfg.DcOptions()
	if err != nil {
		t.Fatal(err)
	}

	c := newGetConfigCore(t, &fakeLangpacks{}, "android", "en")
	got, err := c.HelpGetConfig(&mtproto.TLHelpGetConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.GetDcOptions()); n != 0 {
		t.Fatalf("without a configuration: %d dc_options", n)
	}

	c.svcCtx.DcOptions = options
	if got, err = c.HelpGetConfig(&mtproto.TLHelpGetConfig{}); err != nil {
		t.Fatal(err)
	}
	const (
		flagTcpoOnly = 1 << 2
		flagCdn      = 1 << 3
		flagStatic   = 1 << 4
		flagSecret   = 1 << 10
	)
	want := []struct {
		id    int32
		ip    string
		port  int32
		flags uint32
	}{
		{1, "203.0.113.10", 10443, flagStatic},
		{1, "203.0.113.10", 5222, flagStatic},
		{cdn.DefaultDcId, "203.0.113.10", 8443, flagCdn | flagTcpoOnly | flagSecret},
	}
	if len(got.GetDcOptions()) != len(want) {
		t.Fatalf("%d dc_options, want %d", len(got.GetDcOptions()), len(want))
	}
	for i, w := range want {
		x := mtproto.NewEncodeBuf(64)
		if err := mtproto.MakeTLDcOption(got.GetDcOptions()[i]).Encode(x, 233); err != nil {
			t.Fatalf("dc_options[%d]: %v", i, err)
		}
		b := x.GetBuf()
		if flags := binary.LittleEndian.Uint32(b[4:8]); flags != w.flags {
			t.Errorf("dc_options[%d]: flags %#x, want %#x", i, flags, w.flags)
		}
		d := mtproto.NewDecodeBuf(b)
		o, ok := d.Object().(*mtproto.TLDcOption)
		if !ok || d.GetError() != nil {
			t.Fatalf("dc_options[%d] does not decode: %v", i, d.GetError())
		}
		if o.GetId() != w.id || o.GetIpAddress() != w.ip || o.GetPort() != w.port {
			t.Errorf("dc_options[%d]: DC %d at %s:%d, want DC %d at %s:%d", i, o.GetId(), o.GetIpAddress(), o.GetPort(), w.id, w.ip, w.port)
		}
		// The CDN DC's secret as tdesktop (DcOptions::ValidateSecret,
		// mtproto_dc_options.cpp:134-140) and Android (Connection.cpp:463)
		// take a padded-transport one: 0xdd and 16 bytes.
		if w.flags&flagSecret != 0 && !bytes.Equal(o.GetSecret(), append([]byte{0xdd}, raw...)) {
			t.Errorf("dc_options[%d]: secret %x", i, o.GetSecret())
		}
	}
	// config.json stays without dc_options: they are not written into the
	// base config.
	if n := len(c.svcCtx.ClientConfig.GetDcOptions()); n != 0 {
		t.Errorf("the base config got %d dc_options", n)
	}
}

// TestDeployedConfigHasNoDcOptions: the DCs come only from the
// configuration of bff; the service refuses a config.json that has some.
func TestDeployedConfigHasNoDcOptions(t *testing.T) {
	c := newGetConfigCore(t, &fakeLangpacks{}, "android", "en")
	if n := len(c.svcCtx.ClientConfig.GetDcOptions()); n != 0 {
		t.Fatalf("teamgramd/bin/config.json has %d dc_options", n)
	}
}

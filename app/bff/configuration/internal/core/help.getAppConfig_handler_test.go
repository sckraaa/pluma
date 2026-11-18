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
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/appconfig"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/pkg/ipcountry"

	"github.com/zeromicro/go-zero/core/logx"
)

// fakeCountries answers authsession.getIpCountry from a map and counts the
// lookups; an address it does not know fails.
type fakeCountries struct {
	mu      sync.Mutex
	country map[string]string
	asked   []string
}

func (f *fakeCountries) AuthsessionGetIpCountry(_ context.Context, in *authsession.TLAuthsessionGetIpCountry) (*mtproto.String, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, in.GetIp())
	c, ok := f.country[in.GetIp()]
	if !ok {
		return nil, errors.New("authsession is down")
	}
	return mtproto.MakeTLString(&mtproto.String{V: c}).To_String(), nil
}

func newAppConfigCore(t *testing.T, edges *appconfig.Edges, countries *fakeCountries, addr string) *ConfigurationCore {
	t.Helper()
	base, err := appconfig.Parse([]byte(`{"a": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	noDomains := func(context.Context) ([]string, error) { return nil, nil }
	ctx := context.Background()
	return &ConfigurationCore{
		ctx: ctx,
		svcCtx: &svc.ServiceContext{
			AppConfig: appconfig.NewLive(base, noDomains, time.Minute),
			Edges:     edges,
			Countries: ipcountry.New(countries),
		},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{ClientAddr: addr},
	}
}

// edgeWS is the first edge of pluma_edge of the answer, "" without the key.
func edgeWS(t *testing.T, cfg *mtproto.JSONValue) string {
	t.Helper()
	for _, kv := range cfg.GetValue_VECTORJSONOBJECTVALUE() {
		if kv.GetKey() != "pluma_edge" {
			continue
		}
		for _, f := range kv.GetValue().GetValue_VECTORJSONOBJECTVALUE() {
			if f.GetKey() == "ws" {
				return f.GetValue().GetValue_VECTORJSONVALUE()[0].GetValue_STRING()
			}
		}
		t.Fatal("pluma_edge without ws")
	}
	return ""
}

// help.getAppConfig hands each client pluma_edge of the country of its
// address: a client from Russia the record RU, the others, and a client
// whose country authsession could not tell, the record *. Both
// constructors answer the same.
func TestHelpGetAppConfigEdgeByCountry(t *testing.T) {
	edges, err := appconfig.ParseEdges(
		"gcore=wss://e2x.gcorecdn.com/apiws,ru=wss://ru.pluma.chat/apiws",
		"RU:edge,direct:ru,gcore;*:direct,edge:gcore,ru", "")
	if err != nil {
		t.Fatal(err)
	}
	countries := &fakeCountries{country: map[string]string{"5.8.16.10": "RU", "203.0.113.5": "DE"}}

	ru := newAppConfigCore(t, edges, countries, "5.8.16.10")
	r, err := ru.HelpGetAppConfig61E3F854(&mtproto.TLHelpGetAppConfig61E3F854{})
	if err != nil {
		t.Fatal(err)
	}
	if ws := edgeWS(t, r.GetConfig()); ws != "wss://ru.pluma.chat/apiws" {
		t.Errorf("a client from Russia gets %q first", ws)
	}
	old, err := ru.HelpGetAppConfig98914110(&mtproto.TLHelpGetAppConfig98914110{})
	if err != nil {
		t.Fatal(err)
	}
	if ws := edgeWS(t, old); ws != "wss://ru.pluma.chat/apiws" {
		t.Errorf("the older constructor gives a client from Russia %q first", ws)
	}

	for _, addr := range []string{"203.0.113.5", "198.51.100.1", ""} {
		c := newAppConfigCore(t, edges, countries, addr)
		other, err := c.HelpGetAppConfig61E3F854(&mtproto.TLHelpGetAppConfig61E3F854{Hash: r.GetHash()})
		if err != nil {
			t.Fatal(err)
		}
		if other.GetPredicateName() != mtproto.Predicate_help_appConfig || other.GetHash() == r.GetHash() {
			t.Errorf("%q with the config of Russia gets %s, hash %d", addr, other.GetPredicateName(), other.GetHash())
		}
		if ws := edgeWS(t, other.GetConfig()); ws != "wss://e2x.gcorecdn.com/apiws" {
			t.Errorf("%q gets %q first", addr, ws)
		}
	}
}

// With a single record * the country is never looked up, and without edges
// the config has no pluma_edge.
func TestHelpGetAppConfigEdgeNoLookup(t *testing.T) {
	edges, err := appconfig.ParseEdges("gcore=wss://e2x.gcorecdn.com/apiws", "", "")
	if err != nil {
		t.Fatal(err)
	}
	countries := &fakeCountries{country: map[string]string{"5.8.16.10": "RU"}}
	r, err := newAppConfigCore(t, edges, countries, "5.8.16.10").HelpGetAppConfig61E3F854(&mtproto.TLHelpGetAppConfig61E3F854{})
	if err != nil {
		t.Fatal(err)
	}
	if ws := edgeWS(t, r.GetConfig()); ws != "wss://e2x.gcorecdn.com/apiws" {
		t.Errorf("pluma_edge starts with %q", ws)
	}
	r, err = newAppConfigCore(t, nil, countries, "5.8.16.10").HelpGetAppConfig61E3F854(&mtproto.TLHelpGetAppConfig61E3F854{})
	if err != nil {
		t.Fatal(err)
	}
	if ws := edgeWS(t, r.GetConfig()); ws != "" {
		t.Errorf("no edges, yet pluma_edge with %q", ws)
	}
	if len(countries.asked) != 0 {
		t.Errorf("looked up %v without records of countries", countries.asked)
	}
}

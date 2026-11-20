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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/groupcalls/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/pkg/ipcountry"
)

// countryStub answers authsession.getIpCountry from a map; an address not
// in it fails the lookup.
type countryStub map[string]string

func (s countryStub) AuthsessionGetIpCountry(_ context.Context, in *authsession.TLAuthsessionGetIpCountry) (*mtproto.String, error) {
	c, ok := s[in.GetIp()]
	if !ok {
		return nil, errors.New("authsession is down")
	}
	return mtproto.MakeTLString(&mtproto.String{V: c}).To_String(), nil
}

// The routes of a join (task 116): by the country of the client's
// address, "*" for every other, unknown or failed one; without records by
// country, no lookup.
func TestJoinRoutesByCountry(t *testing.T) {
	routes, err := config.ParseRoutes("RU,BY:ru;*:direct")
	if err != nil {
		t.Fatal(err)
	}
	stub := countryStub{"5.8.16.41": "RU", "203.0.113.41": "DE", "10.0.0.1": ""}
	svcCtx := &svc.ServiceContext{Routes: routes, Countries: ipcountry.New(stub)}
	as := func(addr string) []string {
		c := &GroupCallsCore{ctx: context.Background(), svcCtx: svcCtx, MD: &metadata.RpcMetadata{ClientAddr: addr}}
		return c.routes()
	}
	for addr, want := range map[string][]string{
		"5.8.16.41":    {"ru"},
		"203.0.113.41": {"direct"},
		"10.0.0.1":     {"direct"},
		"198.51.100.9": {"direct"},
		"":             {"direct"},
	} {
		if got := as(addr); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", addr, got, want)
		}
	}

	none := &GroupCallsCore{ctx: context.Background(), svcCtx: &svc.ServiceContext{}, MD: &metadata.RpcMetadata{ClientAddr: "5.8.16.41"}}
	if got := none.routes(); got != nil {
		t.Errorf("without GROUPCALLS_ROUTES: %v", got)
	}
	all, err := config.ParseRoutes("*:ru,direct")
	if err != nil {
		t.Fatal(err)
	}
	everyone := &GroupCallsCore{ctx: context.Background(), svcCtx: &svc.ServiceContext{Routes: all}, MD: &metadata.RpcMetadata{ClientAddr: "5.8.16.41"}}
	if got := everyone.routes(); !slices.Equal(got, []string{"ru", "direct"}) {
		t.Errorf("one record for everyone: %v", got)
	}
}

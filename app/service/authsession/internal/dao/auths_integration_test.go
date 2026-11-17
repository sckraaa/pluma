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

package dao

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/jsonx"

	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/pkg/cdn"
)

// The params and the proxy of initConnection reach the readers of the
// client of a key (authsession.getAuthStateData): bff reads pluma_cdn
// (task 107) and the time zone of help.getPromoData from them. Each new
// initConnection that is stored replaces the cached client.
func TestInitConnectionParamsRead(t *testing.T) {
	ctx := context.Background()
	d := newIntegrationDao(t, itFullDB)
	id := storeKey(t, d, mtproto.AuthKeyTypePerm, 0)

	params := func(cdnVersion float64) string {
		t.Helper()
		kv := []*mtproto.JSONObjectValue{
			mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{
				Key: "tz_offset", Value: mtproto.MakeTLJsonNumber(&mtproto.JSONValue{Value_FLOAT64: 10800}).To_JSONValue(),
			}).To_JSONObjectValue(),
		}
		if cdnVersion > 0 {
			kv = append(kv, mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{
				Key: cdn.PlumaCdnParam, Value: mtproto.MakeTLJsonNumber(&mtproto.JSONValue{Value_FLOAT64: cdnVersion}).To_JSONValue(),
			}).To_JSONObjectValue())
		}
		// As the session stores them (sess.MainAuthWrapper).
		s, err := jsonx.MarshalToString(mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: kv}).To_JSONValue())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	set := func(params string) {
		t.Helper()
		if err := d.SetInitConnection(ctx, &authsession.TLAuthsessionSetInitConnection{
			AuthKeyId: id, ApiId: 1, DeviceModel: "it", SystemVersion: "go", AppVersion: "1.0",
			SystemLangCode: "en", LangCode: "en", Proxy: `{"address":"proxy.example","port":443}`, Params: params,
		}); err != nil {
			t.Fatalf("SetInitConnection: %v", err)
		}
	}
	read := func() *authsession.ClientSession {
		t.Helper()
		c, err := d.GetCacheAuthData(ctx, id)
		if err != nil {
			t.Fatalf("GetCacheAuthData: %v", err)
		}
		return c.GetClient()
	}
	sameJSON := func(name, got, want string) {
		t.Helper()
		var g, w any
		if err := json.Unmarshal([]byte(got), &g); err != nil {
			t.Fatalf("%s %q: %v", name, got, err)
		}
		if err := json.Unmarshal([]byte(want), &w); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}

	first := params(1)
	set(first)
	c := read()
	sameJSON("params", c.GetParams(), first)
	sameJSON("proxy", c.GetProxy(), `{"address":"proxy.example","port":443}`)
	if v := cdn.PlumaCdnVersion(c.GetParams()); v != 1 {
		t.Errorf("pluma_cdn of the stored params: %d, want 1", v)
	}
	if c = read(); cdn.PlumaCdnVersion(c.GetParams()) != 1 {
		t.Error("the cached client lost pluma_cdn")
	}

	set(params(0))
	if v := cdn.PlumaCdnVersion(read().GetParams()); v != 0 {
		t.Errorf("pluma_cdn after an initConnection without it: %d, want 0", v)
	}
	set("")
	if p := read().GetParams(); p != "null" {
		t.Errorf("params of an initConnection without them: %q, want null", p)
	}
}

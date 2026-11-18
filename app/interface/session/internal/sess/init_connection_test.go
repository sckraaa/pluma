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

package sess

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/interface/session/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/cdn"
)

// The first initConnection of a key keeps the client's language and its
// pack apart: bff gets both in the metadata (langpack.*, help.getConfig of
// task 021) and authsession stores both.
func TestFirstInitConnectionKeepsLangCode(t *testing.T) {
	const authKeyID = int64(0x1a2b_0000_0000_0005)
	client := &fakeAuthsessionClient{}
	manager := NewMainAuthWrapperManager(&dao.Dao{AuthsessionClient: client})
	w := newMainAuthWrapper(authKeyID, 0, mtproto.AuthStateNew, nil, 0, manager)

	w.onUpdateInitConnection(context.Background(), testClientIp, &mtproto.TLInitConnection{
		ApiId:          4,
		DeviceModel:    "Pixel 8",
		SystemVersion:  "SDK 35",
		AppVersion:     "12.9.1",
		SystemLangCode: "ru-RU",
		LangPack:       "android",
		LangCode:       "ru",
	})

	if got := w.LangPack(); got != "android" {
		t.Errorf("LangPack() = %q, want android", got)
	}
	if got := w.LangCode(); got != "ru" {
		t.Errorf("LangCode() = %q, want ru", got)
	}
	in := client.setInitConnectionIn
	if client.setInitConnectionCalls != 1 || in == nil {
		t.Fatalf("authsession.setInitConnection called %d times", client.setInitConnectionCalls)
	}
	if in.LangPack != "android" || in.LangCode != "ru" || in.SystemLangCode != "ru-RU" {
		t.Errorf("stored lang_pack %q, lang_code %q, system_lang_code %q", in.LangPack, in.LangCode, in.SystemLangCode)
	}
}

// initParams are initConnection params with tz_offset and, unless
// plumaCdn is 0, pluma_cdn (task 107).
func initParams(tzOffset, plumaCdn float64) *mtproto.JSONValue {
	number := func(v float64) *mtproto.JSONValue {
		return mtproto.MakeTLJsonNumber(&mtproto.JSONValue{Value_FLOAT64: v}).To_JSONValue()
	}
	fields := []*mtproto.JSONObjectValue{
		mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{Key: "tz_offset", Value: number(tzOffset)}).To_JSONObjectValue(),
	}
	if plumaCdn != 0 {
		fields = append(fields, mtproto.MakeTLJsonObjectValue(&mtproto.JSONObjectValue{Key: "pluma_cdn", Value: number(plumaCdn)}).To_JSONObjectValue())
	}
	return mtproto.MakeTLJsonObject(&mtproto.JSONValue{Value_VECTORJSONOBJECTVALUE: fields}).To_JSONValue()
}

// A key that repeats initConnection with pluma_cdn added or dropped has its
// params stored again, since bff redirects to the HTTPS CDN by them (task
// 107); a change of tz_offset alone is not stored.
func TestInitConnectionStoresPlumaCdnChange(t *testing.T) {
	const authKeyID = int64(0x1a2b_0000_0000_0006)
	client := &fakeAuthsessionClient{}
	manager := NewMainAuthWrapperManager(&dao.Dao{AuthsessionClient: client})
	w := newMainAuthWrapper(authKeyID, 0, mtproto.AuthStateNew, nil, 0, manager)
	initConn := func(params *mtproto.JSONValue) {
		w.onUpdateInitConnection(context.Background(), testClientIp, &mtproto.TLInitConnection{
			ApiId:          4,
			DeviceModel:    "Pixel 8",
			SystemVersion:  "SDK 35",
			AppVersion:     "13.2.1",
			SystemLangCode: "ru-RU",
			LangPack:       "android",
			LangCode:       "ru",
			Params:         params,
		})
	}

	initConn(initParams(3600, 0))
	if client.setInitConnectionCalls != 1 {
		t.Fatalf("first initConnection: %d stores", client.setInitConnectionCalls)
	}

	initConn(initParams(7200, 0))
	if client.setInitConnectionCalls != 1 {
		t.Fatalf("tz_offset changed: %d stores, want none more", client.setInitConnectionCalls)
	}

	initConn(initParams(7200, 1))
	if client.setInitConnectionCalls != 2 {
		t.Fatalf("pluma_cdn added: %d stores, want 2", client.setInitConnectionCalls)
	}
	if got := cdn.PlumaCdnVersion(client.setInitConnectionIn.Params); got != 1 {
		t.Fatalf("stored params have pluma_cdn %d, want 1", got)
	}

	initConn(initParams(3600, 1))
	if client.setInitConnectionCalls != 2 {
		t.Fatalf("tz_offset changed with pluma_cdn kept: %d stores, want 2", client.setInitConnectionCalls)
	}

	initConn(initParams(3600, 0))
	if client.setInitConnectionCalls != 3 {
		t.Fatalf("pluma_cdn dropped: %d stores, want 3", client.setInitConnectionCalls)
	}
	if got := cdn.PlumaCdnVersion(client.setInitConnectionIn.Params); got != 0 {
		t.Fatalf("stored params have pluma_cdn %d, want 0", got)
	}
}

// The requests of a key carry its client's language and the language of the
// device's system, for help.getConfig's suggested_lang_code (task 114).
func TestRpcMetadataCarriesLanguages(t *testing.T) {
	w, sess, _ := newTestBotWrapper(t)
	w.onUpdateInitConnection(context.Background(), testClientIp, &mtproto.TLInitConnection{
		ApiId:          4,
		DeviceModel:    "iPhone",
		SystemVersion:  "iOS 18",
		AppVersion:     "13.2",
		SystemLangCode: "ru-RU",
		LangPack:       "ios",
		LangCode:       "en",
	})
	md, err := w.rpcMetadata(testRequest(sess, ""))
	if err != nil {
		t.Fatal(err)
	}
	if md.Langpack != "ios" || md.LangCode != "en" || md.SystemLangCode != "ru-RU" {
		t.Errorf("metadata lang_pack %q, lang_code %q, system_lang_code %q", md.Langpack, md.LangCode, md.SystemLangCode)
	}
}

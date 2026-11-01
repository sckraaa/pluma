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

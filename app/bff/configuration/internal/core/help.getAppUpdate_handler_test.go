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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/iosupdate"

	"github.com/zeromicro/go-zero/core/logx"
)

// TestIOSUpdateURL: an Ad Hoc build is sent to the itms-services link of
// the build only when the update from the app is set up and the install
// service gives exactly that build; to the site's install page otherwise.
// The link is the same on every request of a session, so the app does not
// take it for a new update, and is another session's own.
func TestIOSUpdateURL(t *testing.T) {
	const (
		secret = "0123456789abcdef0123456789abcdef0123456789abcdef"
		page   = "https://pluma.example/iphone/install"
		prefix = "itms-services://?action=download-manifest&url="
	)
	var answer atomic.Value
	answer.Store(iosupdate.Status{Build: 60, Manifest: "https://pluma.example/ios/adhoc/manifest"})
	install := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		st, ok := answer.Load().(iosupdate.Status)
		if !ok || st.Build < 0 {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(st)
	}))
	defer install.Close()

	core := func(withSecret bool, authKey int64) *ConfigurationCore {
		s := &svc.ServiceContext{}
		s.Config.ReleasesURL = "https://pluma.example"
		if withSecret {
			s.Config.IOSUpdate = iosupdate.Settings{Secret: secret, StatusURL: install.URL}
			// A checker of its own: each core asks the install service.
			s.IOSUpdate = iosupdate.NewChecker(install.URL)
		}
		ctx := context.Background()
		return &ConfigurationCore{ctx: ctx, svcCtx: s, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{PermAuthKeyId: authKey}}
	}

	if got := core(false, 1).iosUpdateURL(60); got != page {
		t.Errorf("no secret: %q", got)
	}

	link := core(true, 1).iosUpdateURL(60)
	if !strings.HasPrefix(link, prefix) {
		t.Fatalf("signed: not the install link")
	}
	manifest, err := url.QueryUnescape(strings.TrimPrefix(link, prefix))
	ticket, found := strings.CutPrefix(manifest, "https://pluma.example/ios/adhoc/manifest?u=")
	if err != nil || !found {
		t.Fatalf("the manifest's address: %v, of the install service %v", err, found)
	}
	tk, err := iosupdate.Verify([]byte(secret), ticket, time.Now())
	if err != nil || tk.Build != 60 {
		t.Fatalf("the ticket: %+v, %v", tk, err)
	}
	// The same link again, unless midnight UTC came between the two.
	again := core(true, 1).iosUpdateURL(60)
	if again != link && iosupdate.Mint([]byte(secret), 60, 1, time.Now()) != ticket {
		again, link = core(true, 1).iosUpdateURL(60), again
	}
	if again != link {
		t.Error("the link of a session changes between requests")
	}
	if other := core(true, 2).iosUpdateURL(60); other == link || !strings.HasPrefix(other, prefix) {
		t.Error("another session has the same link, or none")
	}

	// The install service has another build, none, or does not answer.
	for name, c := range map[string]struct {
		st    iosupdate.Status
		build int32
	}{
		"an older build signed": {iosupdate.Status{Build: 59, Manifest: "https://pluma.example/ios/adhoc/manifest"}, 60},
		"a newer build signed":  {iosupdate.Status{Build: 61, Manifest: "https://pluma.example/ios/adhoc/manifest"}, 60},
		"nothing to give":       {iosupdate.Status{}, 60},
		"down":                  {iosupdate.Status{Build: -1}, 60},
	} {
		answer.Store(c.st)
		if got := core(true, 1).iosUpdateURL(c.build); got != page {
			t.Errorf("%s: not the install page", name)
		}
	}
}

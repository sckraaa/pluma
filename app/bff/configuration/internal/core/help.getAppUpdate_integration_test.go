//go:build integration

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
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/svc"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/appreleases"
	"github.com/teamgram/teamgram-server/pkg/iosupdate"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/logx"
)

var itDB *sqlx.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	conf := db.Config()
	itDB = sqlx.NewMySQL(&conf)
	code := m.Run()
	_ = db.Terminate(ctx)
	os.Exit(code)
}

// auths answers the authorization of the session: its app.
type auths struct {
	authsession_client.AuthsessionClient
	apiId      int32
	appVersion string
}

func (a *auths) AuthsessionGetAuthorization(context.Context, *authsession.TLAuthsessionGetAuthorization) (*mtproto.Authorization, error) {
	return mtproto.MakeTLAuthorization(&mtproto.Authorization{ApiId: a.apiId, AppVersion: a.appVersion}).To_Authorization(), nil
}

// medias has the APK.
type medias struct{ media_client.MediaClient }

func (medias) MediaGetDocument(_ context.Context, in *mediapb.TLMediaGetDocument) (*mtproto.Document, error) {
	return mtproto.MakeTLDocument(&mtproto.Document{Id: in.Id, MimeType: "application/vnd.android.package-archive"}).To_Document(), nil
}

func appUpdateCore(apiId int32, appVersion, lang string) *ConfigurationCore {
	d := dao.NewWithDB(itDB)
	d.AuthsessionClient = &auths{apiId: apiId, appVersion: appVersion}
	d.Media = medias{}
	s := &svc.ServiceContext{Dao: d}
	s.Config.ReleasesURL = "https://pluma.example"
	ctx := context.Background()
	return &ConfigurationCore{ctx: ctx, svcCtx: s, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{PermAuthKeyId: 1, LangCode: lang}}
}

// TestHelpGetAppUpdate: an Android build older than the current release
// gets it with the APK and the notes of its language, a build as new gets
// nothing, nor one Android would not take for older (3 parts); an iOS build
// from the site gets the install page of the latest build, named with its
// number (the version is the same across builds), a TestFlight one nothing;
// Desktop nothing.
func TestHelpGetAppUpdate(t *testing.T) {
	ctx := context.Background()
	for _, r := range []*appreleases.Release{
		{Platform: appreleases.Android, Version: "12.9.1.7", Build: 7, DocumentID: 77, NotesEn: "New", NotesRu: "Новое", Files: []appreleases.File{}, CreatedAt: time.Now().Unix()},
		{Platform: appreleases.IOS, Version: "12.9.2", Build: 58, NotesEn: "older", Files: []appreleases.File{}, CreatedAt: time.Now().Unix()},
		{Platform: appreleases.IOS, Version: "12.9.2", Build: 60, NotesEn: "iOS", Files: []appreleases.File{}, CreatedAt: time.Now().Unix()},
	} {
		if err := appreleases.Insert(ctx, itDB, r); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name       string
		apiId      int32
		appVersion string
		lang       string
		want       string // version offered, "" for none
		text, url  string
	}{
		{"android older", apiIdAndroid, "12.9.1.6 (6979069)", "ru", "12.9.1.7", "Новое", ""},
		{"android current", apiIdAndroid, "12.9.1.7 (6979079)", "en", "", "", ""},
		{"android of 3 parts", apiIdAndroid, "12.9.1 (69799)", "en", "", "", ""},
		{"ios from the site", apiIdIOS, "12.9.2 (55) adhoc", "en", "12.9.2 (60)", "iOS", "https://pluma.example/iphone/install"},
		{"ios from TestFlight", apiIdIOS, "12.9.2 (55) ", "en", "", "", ""},
		{"ios current", apiIdIOS, "12.9.2 (60) adhoc", "en", "", "", ""},
		{"desktop", 10003, "7.0.9 x64", "en", "", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := appUpdateCore(c.apiId, c.appVersion, c.lang).HelpGetAppUpdate(&mtproto.TLHelpGetAppUpdate{})
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" {
				if got.GetPredicateName() != mtproto.Predicate_help_noAppUpdate {
					t.Errorf("%s %s", got.GetPredicateName(), got.GetVersion())
				}
				return
			}
			if got.GetPredicateName() != mtproto.Predicate_help_appUpdate || got.GetVersion() != c.want || got.GetText() != c.text ||
				got.GetUrl().GetValue() != c.url {
				t.Errorf("%s version %q text %q url %q", got.GetPredicateName(), got.GetVersion(), got.GetText(), got.GetUrl().GetValue())
			}
			if c.apiId == apiIdAndroid && got.GetDocument().GetId() != 77 {
				t.Errorf("document %v", got.GetDocument())
			}
		})
	}
}

// TestHelpGetAppUpdateFromTheApp: with the update from the app set up
// (task 085), an iOS build from the site gets the itms-services link of the
// current build when the install service has it signed, the install page
// while it has not; a TestFlight build and one that is current get nothing
// as before.
func TestHelpGetAppUpdateFromTheApp(t *testing.T) {
	ctx := context.Background()
	if err := appreleases.Insert(ctx, itDB, &appreleases.Release{Platform: appreleases.IOS, Version: "12.9.2", Build: 70, NotesEn: "iOS 70",
		Files: []appreleases.File{}, CreatedAt: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}
	const secret = "0123456789abcdef0123456789abcdef0123456789abcdef"
	var signed atomic.Int32
	install := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(iosupdate.Status{Build: signed.Load(), Manifest: "https://pluma.example/ios/adhoc/manifest"})
	}))
	defer install.Close()
	ask := func(appVersion string) *mtproto.Help_AppUpdate {
		t.Helper()
		c := appUpdateCore(apiIdIOS, appVersion, "en")
		c.svcCtx.Config.IOSUpdate = iosupdate.Settings{Secret: secret, StatusURL: install.URL}
		// A checker of its own: each request asks the install service.
		c.svcCtx.IOSUpdate = iosupdate.NewChecker(install.URL)
		got, err := c.HelpGetAppUpdate(&mtproto.TLHelpGetAppUpdate{})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	// The install service has an older build signed: the page.
	signed.Store(60)
	if got := ask("12.9.2 (55) adhoc"); got.GetVersion() != "12.9.2 (70)" || got.GetUrl().GetValue() != "https://pluma.example/iphone/install" {
		t.Errorf("not signed yet: %q %q", got.GetVersion(), got.GetUrl().GetValue())
	}
	// Signed: the link, with a ticket of build 70.
	signed.Store(70)
	got := ask("12.9.2 (55) adhoc")
	const prefix = "itms-services://?action=download-manifest&url="
	link := got.GetUrl().GetValue()
	if got.GetPredicateName() != mtproto.Predicate_help_appUpdate || got.GetVersion() != "12.9.2 (70)" || got.GetText() != "iOS 70" || !strings.HasPrefix(link, prefix) {
		t.Fatalf("signed: %s %q, a link %v", got.GetPredicateName(), got.GetVersion(), strings.HasPrefix(link, prefix))
	}
	manifest, err := url.QueryUnescape(strings.TrimPrefix(link, prefix))
	ticket, found := strings.CutPrefix(manifest, "https://pluma.example/ios/adhoc/manifest?u=")
	if err != nil || !found {
		t.Fatalf("the manifest's address: %v, of the install service %v", err, found)
	}
	if tk, err := iosupdate.Verify([]byte(secret), ticket, time.Now()); err != nil || tk.Build != 70 {
		t.Fatalf("the ticket: %+v, %v", tk, err)
	}
	for _, v := range []string{"12.9.2 (55) ", "12.9.2 (70) adhoc", "12.9.2 (71) adhoc"} {
		if got := ask(v); got.GetPredicateName() != mtproto.Predicate_help_noAppUpdate {
			t.Errorf("%q: %s %q", v, got.GetPredicateName(), got.GetUrl().GetValue())
		}
	}
}

// TestHelpGetAppUpdateMandatory: a release the operator makes mandatory
// (pluma-admin release mandatory, task 095) is offered with can_not_skip
// to Android and iOS, and without it once it is optional again; setting
// the state a release has already finds it, a build the platform does not
// have is not found.
func TestHelpGetAppUpdateMandatory(t *testing.T) {
	ctx := context.Background()
	for _, r := range []*appreleases.Release{
		{Platform: appreleases.Android, Version: "12.9.1.9", Build: 9, DocumentID: 99, NotesEn: "Android 9", Files: []appreleases.File{}, CreatedAt: time.Now().Unix()},
		{Platform: appreleases.IOS, Version: "12.9.2", Build: 80, NotesEn: "iOS 80", Files: []appreleases.File{}, CreatedAt: time.Now().Unix()},
	} {
		if err := appreleases.Insert(ctx, itDB, r); err != nil {
			t.Fatal(err)
		}
	}
	apps := []struct {
		platform   string
		build      int32
		apiId      int32
		appVersion string
		version    string
	}{
		{appreleases.Android, 9, apiIdAndroid, "12.9.1.6 (6979069)", "12.9.1.9"},
		{appreleases.IOS, 80, apiIdIOS, "12.9.2 (55) adhoc", "12.9.2 (80)"},
	}
	ask := func(apiId int32, appVersion, version string) bool {
		t.Helper()
		got, err := appUpdateCore(apiId, appVersion, "en").HelpGetAppUpdate(&mtproto.TLHelpGetAppUpdate{})
		if err != nil {
			t.Fatal(err)
		}
		if got.GetPredicateName() != mtproto.Predicate_help_appUpdate || got.GetVersion() != version {
			t.Fatalf("%s %q, want help.appUpdate %q", got.GetPredicateName(), got.GetVersion(), version)
		}
		return got.GetCanNotSkip()
	}
	for _, a := range apps {
		if ask(a.apiId, a.appVersion, a.version) {
			t.Errorf("%s: can_not_skip before the operator set it", a.platform)
		}
		for _, mandatory := range []bool{true, true, false, false} {
			found, err := appreleases.SetCanNotSkip(ctx, itDB, a.platform, a.build, mandatory)
			if err != nil || !found {
				t.Fatalf("%s build %d mandatory %v: found %v, %v", a.platform, a.build, mandatory, found, err)
			}
			if got := ask(a.apiId, a.appVersion, a.version); got != mandatory {
				t.Errorf("%s mandatory %v: can_not_skip %v", a.platform, mandatory, got)
			}
		}
	}
	if found, err := appreleases.SetCanNotSkip(ctx, itDB, appreleases.Android, 12345, true); err != nil || found {
		t.Errorf("a build android does not have: found %v, %v", found, err)
	}
}

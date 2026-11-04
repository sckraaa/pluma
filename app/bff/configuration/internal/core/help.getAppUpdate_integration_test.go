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
	"fmt"
	"os"
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
// from the site gets the install page, a TestFlight one nothing; Desktop
// nothing.
func TestHelpGetAppUpdate(t *testing.T) {
	ctx := context.Background()
	for _, r := range []*appreleases.Release{
		{Platform: appreleases.Android, Version: "12.9.1.7", Build: 7, DocumentID: 77, NotesEn: "New", NotesRu: "Новое", Files: []appreleases.File{}, CreatedAt: time.Now().Unix()},
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
		{"ios from the site", apiIdIOS, "12.9.2 (55) adhoc", "en", "12.9.2", "iOS", "https://pluma.example/iphone/install"},
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

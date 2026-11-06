// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/appreleases"
)

// The api_id of each client app of Pluma (their config/app.env).
const (
	apiIdAndroid = 10001
	apiIdIOS     = 10002
)

// HelpGetAppUpdate
// help.getAppUpdate#522d5a7d source:string = help.AppUpdate;
//
// The update of the account's app, if a newer release is out (task 054,
// ADR 0035). The app is the authorization's: api_id tells the platform,
// app_version the installed build.
//
//   - Android: help.appUpdate with the APK as the document when the current
//     release is newer by Android's own rule (appreleases.AndroidNewer);
//     Android downloads it and opens the system installer
//     (UpdateAppAlertDialog of the standalone app at aac1efcb).
//   - iOS: help.appUpdate with the install page as url for a build from the
//     site (" adhoc") older than the current one. iOS shows the update
//     screen on every answer and compares nothing (AppUpdate.swift:8-35 at
//     6ad963e5), so the server alone decides. TestFlight updates its own
//     builds: they get nothing.
//   - Desktop updates itself over HTTP (autoupdate_url_prefix of
//     help.getConfig), Web from the server: nothing.
//
// A failure to read is no update, logged: the method never fails the
// client, which asks again later.
func (c *ConfigurationCore) HelpGetAppUpdate(in *mtproto.TLHelpGetAppUpdate) (*mtproto.Help_AppUpdate, error) {
	none := mtproto.MakeTLHelpNoAppUpdate(nil).To_Help_AppUpdate()
	if c.svcCtx.Config.ReleasesURL == "" {
		return none, nil
	}
	auth, err := c.svcCtx.Dao.AuthsessionGetAuthorization(c.ctx, &authsession.TLAuthsessionGetAuthorization{AuthKeyId: c.MD.PermAuthKeyId})
	if err != nil {
		c.Logger.Errorf("help.getAppUpdate - the authorization: %v", err)
		return none, nil
	}
	switch auth.GetApiId() {
	case apiIdAndroid:
		installed, ok := appreleases.AndroidInstalled(auth.GetAppVersion())
		if !ok {
			return none, nil
		}
		rel := c.currentRelease(appreleases.Android)
		if rel == nil || rel.DocumentID == 0 || !appreleases.AndroidNewer(installed, rel.Version) {
			return none, nil
		}
		doc, err := c.svcCtx.Dao.Media.MediaGetDocument(c.ctx, &mediapb.TLMediaGetDocument{Id: rel.DocumentID})
		if err != nil || doc.GetPredicateName() != mtproto.Predicate_document {
			c.Logger.Errorf("help.getAppUpdate - the APK of %s: %v", rel.Version, err)
			return none, nil
		}
		return mtproto.MakeTLHelpAppUpdate(&mtproto.Help_AppUpdate{
			CanNotSkip: rel.CanNotSkip,
			Id:         rel.Build,
			Version:    rel.Version,
			Text:       rel.Notes(c.MD.LangCode),
			Entities:   []*mtproto.MessageEntity{},
			Document:   doc,
		}).To_Help_AppUpdate(), nil
	case apiIdIOS:
		build, adhoc, ok := appreleases.IOSInstalled(auth.GetAppVersion())
		if !ok || !adhoc {
			return none, nil
		}
		rel := c.currentRelease(appreleases.IOS)
		if rel == nil || rel.Build <= build {
			return none, nil
		}
		// The version is Telegram's and the same across Pluma's builds; the
		// update screen shows it as "Pluma %@" (Update.AppVersion,
		// UpdateInfoController.swift:87 at 6ad963e5), so it names the build
		// too, as the app's own app_version does.
		return mtproto.MakeTLHelpAppUpdate(&mtproto.Help_AppUpdate{
			CanNotSkip: rel.CanNotSkip,
			Id:         rel.Build,
			Version:    fmt.Sprintf("%s (%d)", rel.Version, rel.Build),
			Text:       rel.Notes(c.MD.LangCode),
			Entities:   []*mtproto.MessageEntity{},
			Url:        mtproto.MakeFlagsString(c.svcCtx.Config.ReleasesURL + "/iphone/install"),
		}).To_Help_AppUpdate(), nil
	}
	return none, nil
}

// currentRelease is the platform's current release, nil for none or on a
// failure, which is logged.
func (c *ConfigurationCore) currentRelease(platform string) *appreleases.Release {
	rel, err := appreleases.Current(c.ctx, c.svcCtx.Dao.DB(), platform)
	if err != nil {
		c.Logger.Errorf("help.getAppUpdate - the release of %s: %v", platform, err)
		return nil
	}
	return rel
}

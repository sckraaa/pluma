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

// Package newlogin tells the other sessions of an account about a login
// that has just completed (task 019), as Telegram does: they get
// updateNewAuthorization, which official clients show as a banner asking
// whether the login is the user's (https://core.telegram.org/api/auth#confirming-login),
// and the New login message of the service notifications account. The new
// session gets neither.
package newlogin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/authorization/servicetext"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Authsession is what newlogin needs of the authsession service.
type Authsession interface {
	AuthsessionGetAuthorizations(ctx context.Context, in *authsession.TLAuthsessionGetAuthorizations) (*mtproto.Account_Authorizations, error)
	AuthsessionGetRecentLangCode(ctx context.Context, in *authsession.TLAuthsessionGetRecentLangCode) (*mtproto.String, error)
}

// User is what newlogin needs of biz user.
type User interface {
	UserNewAuthorization(ctx context.Context, in *user.TLUserNewAuthorization) (*mtproto.Bool, error)
}

// Sync is what newlogin needs of sync.
type Sync interface {
	SyncUpdatesNotMe(ctx context.Context, in *sync.TLSyncUpdatesNotMe) (*mtproto.Void, error)
}

// Notifier tells the other sessions of an account about its new logins.
type Notifier struct {
	Authsession Authsession
	User        User
	Sync        Sync
}

// Login is a login that has just completed: the key is bound to the user.
type Login struct {
	User          *mtproto.ImmutableUser
	PermAuthKeyId int64 // of the new session
	Hash          int64 // of its authorization (authsession.bindAuthKeyUser)
	// LangCode is the lang_code of the session that confirmed the login
	// (auth.acceptLoginToken), empty for a login with a code.
	LangCode    string
	HasPassword bool // the account has a cloud password
	Date        time.Time
}

// Notify sends l's other sessions updateNewAuthorization, with seq so that
// sessions offline now get it later (ADR 0022), and the New login message,
// to the sessions online now: a service notification is not journaled
// (ADR 0020). An account without other sessions is told nothing.
//
// The message is in one language for all of them: the language of the
// session that confirmed the login, or for a login with a code of the
// session that connected last; Telegram did the same on 2026-10-29.
func (n Notifier) Notify(ctx context.Context, l Login) error {
	list, err := n.Authsession.AuthsessionGetAuthorizations(ctx, &authsession.TLAuthsessionGetAuthorizations{
		UserId:           l.User.Id(),
		ExcludeAuthKeyId: l.PermAuthKeyId,
	})
	if err != nil {
		return fmt.Errorf("authorizations of user %d: %w", l.User.Id(), err)
	}
	var (
		current *mtproto.Authorization
		others  int
	)
	for _, a := range list.GetAuthorizations() {
		switch {
		case a == nil:
		case a.GetCurrent():
			current = a
		default:
			others++
		}
	}
	if current == nil {
		return fmt.Errorf("user %d: no authorization on the new key", l.User.Id())
	}
	if others == 0 {
		return nil
	}

	langCode := l.LangCode
	if langCode == "" {
		recent, err := n.Authsession.AuthsessionGetRecentLangCode(ctx, &authsession.TLAuthsessionGetRecentLangCode{
			UserId:          l.User.Id(),
			ExceptAuthKeyId: l.PermAuthKeyId,
		})
		if err != nil {
			return fmt.Errorf("language of user %d: %w", l.User.Id(), err)
		}
		langCode = recent.GetV()
	}

	// The banner first: it is what guards the account, and the only one of
	// the two that reaches the sessions offline now. A failure of one does
	// not keep the other back.
	date := int32(l.Date.Unix())
	var errs []error
	if _, err = n.User.UserNewAuthorization(ctx, &user.TLUserNewAuthorization{
		UserId:          l.User.Id(),
		ExceptAuthKeyId: l.PermAuthKeyId,
		Update: mtproto.MakeTLUpdateNewAuthorization(&mtproto.Update{
			Unconfirmed:    current.GetUnconfirmed(),
			Hash:           l.Hash,
			Date_FLAGINT32: wrapperspb.Int32(date),
			Device:         wrapperspb.String(current.GetDeviceModel()),
			Location:       wrapperspb.String(current.GetCountry()),
		}).To_Update(),
	}); err != nil {
		errs = append(errs, fmt.Errorf("updateNewAuthorization of user %d: %w", l.User.Id(), err))
	}

	text, entities := servicetext.NewLogin{
		Name:        mtproto.GetUserName(l.User.ToSelfUser()),
		Date:        l.Date,
		Device:      Device(current),
		Location:    current.GetCountry(),
		HasPassword: l.HasPassword,
	}.Text(servicetext.LangOf(langCode))
	if _, err = n.Sync.SyncUpdatesNotMe(ctx, &sync.TLSyncUpdatesNotMe{
		UserId:        l.User.Id(),
		PermAuthKeyId: l.PermAuthKeyId,
		Updates: mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateServiceNotification(&mtproto.Update{
			InboxDate:      mtproto.MakeFlagsInt32(date),
			Type:           fmt.Sprintf("auth%d_%d", l.PermAuthKeyId, date),
			Message_STRING: text,
			Media:          mtproto.MakeTLMessageMediaEmpty(nil).To_MessageMedia(),
			Entities:       entities,
		}).To_Update()),
	}); err != nil {
		errs = append(errs, fmt.Errorf("new login message of user %d: %w", l.User.Id(), err))
	}
	return errors.Join(errs...)
}

// Reviewed tells every session of userId that the login of authorization
// hash needs no review: it was confirmed, or terminated. It is
// updateNewAuthorization without unconfirmed, on which official clients drop
// the banner of that login (tdesktop Authorizations::apply, Android
// UnconfirmedAuthController.processUpdate, iOS
// AccountStateManagementUtils).
func (n Notifier) Reviewed(ctx context.Context, userId, hash int64) error {
	_, err := n.User.UserNewAuthorization(ctx, &user.TLUserNewAuthorization{
		UserId: userId,
		Update: mtproto.MakeTLUpdateNewAuthorization(&mtproto.Update{
			Hash: hash,
		}).To_Update(),
	})
	return err
}

// Device is the device of a session in the New login message: app,
// version, model, platform and system, those that are known, as Telegram
// writes "Telegram Web, 12.0.48 A, Safari 26, macOS" and "Telegram Android,
// 12.1.1, Samsung Galaxy Note8, Android, 13 (33)" (docs/tasks/019).
//
// A platform that the system already names is left out. initConnection has
// no platform: for Telegram Desktop authsession derives it from the system
// version itself (platform() in authsession's dao), and that system version
// is the only place where Telegram Desktop names its system: "Windows 11
// x64", "macOS 15.1", "OS X 10.11", "Linux KDE Wayland glibc 2.40" (lib_base
// 82d182a2 base_info_win.cpp:188-235, base_info_mac.mm:169-180,
// base_info_linux.cpp:139-184). Repeated, it would add nothing: "Windows,
// Windows 11 x64".
func Device(a *mtproto.Authorization) string {
	platform, system := strings.TrimSpace(a.GetPlatform()), a.GetSystemVersion()
	if namesPlatform(system, platform) {
		platform = ""
	}
	var parts []string
	for _, p := range []string{a.GetAppName(), a.GetAppVersion(), a.GetDeviceModel(), platform, system} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

// namesPlatform reports whether the system version names the platform,
// whatever the case, by the rule authsession derives a Telegram Desktop
// platform with: the platform's own name, or "OS X" for macOS, as Telegram
// Desktop calls macOS before 10.12 (lib_base 82d182a2
// base_info_mac.mm:174-177).
func namesPlatform(system, platform string) bool {
	system, platform = strings.ToLower(system), strings.ToLower(platform)
	return strings.Contains(system, platform) ||
		platform == "macos" && strings.HasPrefix(system, "os x")
}

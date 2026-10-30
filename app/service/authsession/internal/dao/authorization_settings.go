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

package dao

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/teamgram/teamgram-server/pkg/env2"
)

// The settings of an authorization (task 019): whether a new login is
// confirmed, and whether the session takes new secret chats and calls
// (account.changeAuthorizationSettings).

// AuthorizationChange is a change of the settings of one authorization; a
// nil field stays as it is.
type AuthorizationChange struct {
	Confirm                   bool
	EncryptedRequestsDisabled *bool
	CallRequestsDisabled      *bool
}

func (c AuthorizationChange) empty() bool {
	return !c.Confirm && c.EncryptedRequestsDisabled == nil && c.CallRequestsDisabled == nil
}

// otherAuthorizations counts the sessions of userId on keys other than
// authKeyId.
func (d *Dao) otherAuthorizations(ctx context.Context, userId, authKeyId int64) (int, error) {
	var n int
	err := d.DB.QueryRow(ctx, &n,
		"SELECT COUNT(*) FROM auth_users WHERE user_id = ? AND auth_key_id <> ? AND deleted = 0",
		userId, authKeyId)
	return n, err
}

// ChangeAuthorization changes the settings of userId's authorization hash
// and returns its key, 0 when the user has no such authorization.
func (d *Dao) ChangeAuthorization(ctx context.Context, userId, hash int64, c AuthorizationChange) (int64, error) {
	var authKeyId int64
	err := d.DB.QueryRow(ctx, &authKeyId,
		"SELECT auth_key_id FROM auth_users WHERE user_id = ? AND hash = ? AND deleted = 0",
		userId, hash)
	if errors.Is(err, sqlx.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	if c.empty() {
		return authKeyId, nil
	}

	var (
		sets []string
		args []interface{}
	)
	if c.Confirm {
		sets = append(sets, "unconfirmed = 0")
	}
	if c.EncryptedRequestsDisabled != nil {
		sets = append(sets, "encrypted_requests_disabled = ?")
		args = append(args, *c.EncryptedRequestsDisabled)
	}
	if c.CallRequestsDisabled != nil {
		sets = append(sets, "call_requests_disabled = ?")
		args = append(args, *c.CallRequestsDisabled)
	}
	args = append(args, userId, hash)
	_, _, err = d.CachedConn.Exec(
		ctx,
		func(ctx context.Context, conn *sqlx.DB) (int64, int64, error) {
			r, err := conn.Exec(ctx,
				"UPDATE auth_users SET "+strings.Join(sets, ", ")+" WHERE user_id = ? AND hash = ? AND deleted = 0",
				args...)
			if err != nil {
				return 0, 0, err
			}
			n, err := r.RowsAffected()
			return 0, n, err
		},
		genAuthDataCacheKey(authKeyId))
	if err != nil {
		return 0, err
	}
	return authKeyId, nil
}

// RecentLangCode returns the lang_code of initConnection of the session of
// userId that connected last, apart from the key exceptAuthKeyId; empty
// when the user has no other session.
func (d *Dao) RecentLangCode(ctx context.Context, userId, exceptAuthKeyId int64) (string, error) {
	var langCode string
	err := d.DB.QueryRow(ctx, &langCode,
		"SELECT a.lang_code FROM auth_users u JOIN auths a ON a.auth_key_id = u.auth_key_id AND a.deleted = 0"+
			" WHERE u.user_id = ? AND u.auth_key_id <> ? AND u.deleted = 0 ORDER BY a.date_active DESC LIMIT 1",
		userId, exceptAuthKeyId)
	if errors.Is(err, sqlx.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return langCode, err
}

// Unconfirmed tells whether the authorization still waits for a
// confirmation at now.
func (c *CacheAuthData) Unconfirmed(now int64) bool {
	if c == nil || c.BindUser == nil {
		return false
	}
	return authsession.IsUnconfirmed(c.BindUser.Unconfirmed, c.BindUser.DateCreated, now)
}

func (c *CacheAuthData) EncryptedRequestsDisabled() bool {
	return c != nil && c.BindUser != nil && c.BindUser.EncryptedRequestsDisabled
}

func (c *CacheAuthData) CallRequestsDisabled() bool {
	return c != nil && c.BindUser != nil && c.BindUser.CallRequestsDisabled
}

// appName is the name of a session's app as Telegram shows it in
// account.getAuthorizations and in the New login message ("Telegram Web",
// "Telegram Android"), with this app's name (task 019). The lang_pack of
// initConnection tells the app; the web app also by the " A" or " Z" of its
// version. An unknown app keeps its lang_pack.
func appName(langPack, appVersion string) string {
	switch langPack {
	case "android":
		return env2.MyAppName + " Android"
	case "ios":
		return env2.MyAppName + " iOS"
	case "tdesktop":
		return env2.MyAppName + " Desktop"
	case "macos":
		return env2.MyAppName + " macOS"
	case "weba", "webk", "web":
		return env2.MyAppName + " Web"
	case "":
		if strings.HasSuffix(appVersion, " A") || strings.HasSuffix(appVersion, " Z") {
			return env2.MyAppName + " Web"
		}
	}
	return langPack
}

// platform is the platform of a session's device in account.getAuthorizations
// (authorization.platform). initConnection has no such field
// (mtproto.TLInitConnection), so the server derives it from the app
// (lang_pack) and, for Telegram Desktop, from system_version. Official
// clients choose the icon of a session by it, and by system_version when it
// is empty, where the Android app's "SDK 36" (DrKLO/Telegram aac1efcb
// tgnet/ConnectionsManager.java:229) matches nothing: Android drew the pink
// globe of an unknown browser for it. The values are the words the clients
// look for, after a browser named in device_model:
//   - Android: platform, or system_version when platform is empty, with
//     "ios", "windows", "macos" or "android", else an app_name with
//     "desktop" (DrKLO/Telegram aac1efcb ui/Cells/SessionCell.java:319-403);
//   - iOS: platform with "android", "ios" or "macos", platform or
//     system_version with "macos", "ubuntu", "linux" or "windows"
//     (Telegram-iOS 6ad963e5 submodules/SettingsUI/Sources/Privacy and
//     Security/Recent Sessions/ItemListRecentSessionItem.swift:118-169);
//   - Desktop (tdesktop v7.0.9 a1e89e1f
//     settings/sections/settings_active_sessions.cpp:167-235): by api_id
//     first, before the browser (172-177, 209-216): those of Telegram's own
//     Android, macOS and web apps give their icons, those of Telegram
//     Desktop platform or system_version with "windows", "macos", "ubuntu"
//     or "unity", or "linux" (193-207), else Linux; other api_ids, after a
//     browser or an iPhone or iPad in device_model (219-224), the same words
//     (227-228), then "android" or "ios" (229-232), else Other (234).
//
// It leaves out the Desktop branches that do not depend on it: a
// "chromebook" in device_model is Other (217-218) and the api_ids of
// Telegram's iOS apps are an iPhone (176, 225-226), both before the platform
// is looked at; and it never says "ubuntu" or "unity" (198-202), which
// Telegram Desktop on Linux puts in system_version itself, as part of
// XDG_CURRENT_DESKTOP, where the clients find it.
//
// Telegram Desktop tells its system only in system_version, as
// Platform::SystemVersionPretty (main/main_account.cpp:422): "Windows 11
// x64", "macOS 15.1", "OS X 10.11" (before macOS 10.12), "OS X", "Linux
// ubuntu GNOME Wayland glibc 2.39" (lib_base 82d182a2
// base_info_win.cpp:188-235, base_info_mac.mm:169-180,
// base_info_linux.cpp:36-43 and 139-184). A web app is told by the browser
// in its device_model and has no platform.
func platform(langPack, systemVersion string) string {
	switch langPack {
	case "android":
		return "Android"
	case "ios":
		return "iOS"
	case "macos":
		return "macOS"
	case "tdesktop":
		system := strings.ToLower(systemVersion)
		switch {
		case strings.Contains(system, "windows"):
			return "Windows"
		case strings.Contains(system, "macos"), strings.HasPrefix(system, "os x"):
			return "macOS"
		case strings.Contains(system, "linux"):
			return "Linux"
		}
	}
	return ""
}

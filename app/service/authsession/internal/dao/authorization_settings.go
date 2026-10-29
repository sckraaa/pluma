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

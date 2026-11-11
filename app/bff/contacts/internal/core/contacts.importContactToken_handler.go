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
	"time"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// ContactsImportContactToken
// contacts.importContactToken#13005788 token:string = User;
//
// "Obtain user info from a temporary profile link": the user of a token
// that has not expired, as the account sees it, so the client can open the
// profile and offer to add the contact (iOS UrlHandling.swift:1230-1241
// opens the profile, tdesktop window_session_controller_contact.cpp:28-41
// the peer). Nothing is added to the contacts here. A token that is
// unknown, has expired or names a deleted account is IMPORT_TOKEN_INVALID
// (core.telegram.org/method/contacts.importContactToken), which the
// clients show as an invalid or expired link.
func (c *ContactsCore) ContactsImportContactToken(in *mtproto.TLContactsImportContactToken) (*mtproto.User, error) {
	userId, err := c.svcCtx.Dao.Tokens.Import(c.ctx, in.GetToken(), time.Now().Unix())
	if err != nil {
		c.Logger.Errorf("contacts.importContactToken - error: %v", err)
		return nil, err
	}
	if userId == 0 {
		err = mtproto.ErrImportTokenInvalid
		c.Logger.Errorf("contacts.importContactToken - error: %v", err)
		return nil, err
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      []int64{c.MD.UserId, userId},
		Privacy: true,
		HasTo:   true,
		To:      []int64{c.MD.UserId},
	})
	if err != nil {
		c.Logger.Errorf("contacts.importContactToken - error: %v", err)
		return nil, err
	}
	if owner, ok := users.GetImmutableUser(userId); !ok || owner.Deleted() {
		err = mtproto.ErrImportTokenInvalid
		c.Logger.Errorf("contacts.importContactToken - error: %v", err)
		return nil, err
	}
	// The account's own token gives the account itself.
	user, err := users.GetUnsafeUser(c.MD.UserId, userId)
	if err != nil {
		c.Logger.Errorf("contacts.importContactToken - error: %v", err)
		return nil, mtproto.ErrImportTokenInvalid
	}
	return user, nil
}

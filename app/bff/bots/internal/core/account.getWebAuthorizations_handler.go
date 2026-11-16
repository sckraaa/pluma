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
	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// AccountGetWebAuthorizations
// account.getWebAuthorizations#182e6d6f = account.WebAuthorizations;
//
// The sites the user logged in on through bots, the last active first:
// "Connected websites" of the sessions screen in every client (Android
// ui/SessionsActivity.java:724, iOS RecentWebSessions.swift:24, tdesktop
// api/api_websites.cpp:51, Web K appSeamlessLoginManager.ts:56), with the
// bots as the user sees them.
func (c *BotsCore) AccountGetWebAuthorizations(in *mtproto.TLAccountGetWebAuthorizations) (*mtproto.Account_WebAuthorizations, error) {
	rows, err := c.svcCtx.Bots.WebAuthorizations(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("account.getWebAuthorizations - error: %v", err)
		return nil, err
	}
	sessions := make([]*mtproto.WebAuthorization, 0, len(rows))
	bots := make([]int64, 0, len(rows))
	seen := make(map[int64]bool, len(rows))
	for _, w := range rows {
		sessions = append(sessions, mtproto.MakeTLWebAuthorization(&mtproto.WebAuthorization{
			Hash:        w.Hash,
			BotId:       w.BotId,
			Domain:      w.Domain,
			Browser:     w.Browser,
			Platform:    w.Platform,
			DateCreated: w.DateCreated,
			DateActive:  w.DateActive,
			Ip:          w.IP,
			Region:      w.Region,
		}).To_WebAuthorization())
		if !seen[w.BotId] {
			seen[w.BotId] = true
			bots = append(bots, w.BotId)
		}
	}
	users := []*mtproto.User{}
	if len(bots) > 0 {
		all, err := c.svcCtx.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: append([]int64{c.MD.UserId}, bots...)})
		if err != nil {
			c.Logger.Errorf("account.getWebAuthorizations - error: %v", err)
			return nil, err
		}
		users = all.GetUserListByIdList(c.MD.UserId, bots...)
	}
	return mtproto.MakeTLAccountWebAuthorizations(&mtproto.Account_WebAuthorizations{
		Authorizations: sessions,
		Users:          users,
	}).To_Account_WebAuthorizations(), nil
}

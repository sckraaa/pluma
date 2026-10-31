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
	"github.com/teamgram/teamgram-server/app/bff/configuration/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// promoDataTTL is how long a client keeps the answer: Telegram's expires is
// 15 minutes ahead (help.getPromoData of a Telegram account, 2026-10-31).
const promoDataTTL = 15 * time.Minute

// HelpGetPromoData
// help.getPromoData#c0977421 = help.PromoData;
//
// Pluma has no proxy sponsor or PSA channel: the answer carries only the
// suggestions of the account (core.telegram.org/api/config,
// "Suggestions"). Telegram suggests BIRTHDAY_SETUP to an account without a
// birthday (observed on a Telegram account, 2026-10-31).
func (c *ConfigurationCore) HelpGetPromoData(in *mtproto.TLHelpGetPromoData) (*mtproto.Help_PromoData, error) {
	now := time.Now()
	dismissed, err := c.svcCtx.Dao.DismissedSuggestions(c.ctx, c.MD.UserId, now, c.clientZone())
	if err != nil {
		c.Logger.Errorf("help.getPromoData - error: %v", err)
		return nil, err
	}
	me, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		c.Logger.Errorf("help.getPromoData - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLHelpPromoData(&mtproto.Help_PromoData{
		Expires:              int32(now.Add(promoDataTTL).Unix()),
		PendingSuggestions:   dao.PendingSuggestions(me.Birthday() != nil, dismissed),
		DismissedSuggestions: dismissed,
		Chats:                []*mtproto.Chat{},
		Users:                []*mtproto.User{},
	}).To_Help_PromoData(), nil
}

// clientZone is the time zone of the client that asks, by the params of its
// initConnection (dao.ClientZone). Without them it is UTC: a failed lookup
// only moves the day of a dismissed birthday banner, so it does not fail
// the answer.
func (c *ConfigurationCore) clientZone() *time.Location {
	data, err := c.svcCtx.Dao.AuthsessionClient.AuthsessionGetAuthStateData(c.ctx, &authsession.TLAuthsessionGetAuthStateData{
		AuthKeyId: c.MD.PermAuthKeyId,
	})
	if err != nil {
		c.Logger.Errorf("help.getPromoData - the client's time zone, UTC used: %v", err)
		return time.UTC
	}
	return dao.ClientZone(data.GetClient().GetParams())
}

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
	"github.com/teamgram/teamgram-server/pkg/botmarkup"
)

// botMarkup is the markup a message of c's user carries: a bot's, checked
// and with its input buttons turned into received ones (task 070), and none
// from a user account: the keyboards are for bots
// (core.telegram.org/api/bots/buttons: "Bots can attach a ReplyMarkup").
func (c *MessagesCore) botMarkup(markup *mtproto.ReplyMarkup) (*mtproto.ReplyMarkup, error) {
	if markup == nil {
		return nil, nil
	}
	me, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		return nil, err
	}
	if !me.IsBot() {
		return nil, nil
	}
	return botmarkup.Normalize(markup, func(in *mtproto.InputUser) int64 {
		u := mtproto.FromInputUser(c.MD.UserId, in)
		if !u.IsUser() {
			return 0
		}
		if _, err := c.svcCtx.Dao.UserClient.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: u.PeerId}); err != nil {
			return 0
		}
		return u.PeerId
	})
}

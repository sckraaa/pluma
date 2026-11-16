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
	"github.com/teamgram/teamgram-server/pkg/qtsqueue"
)

// AccountGetBotBusinessConnection
// account.getBotBusinessConnection#76a86270 connection_id:string = Updates;
//
// A bot asks for a connection it does not know, as it may when an update
// names one (core.telegram.org/api/bots/connected-business-bots: "Bots may
// also invoke account.getBotBusinessConnection, passing the connection_id
// ... to fetch the info of a business connection"). The answer is
// updateBotBusinessConnect of the connection with the current qts of the
// caller's authorization, which queues nothing and does not move the qts:
// the connection is the answer, not news, and the queue keeps no second
// copy of it. Which qts Telegram puts there is not published (not
// verified). A connection of another bot, or none, is
// BUSINESS_CONNECTION_INVALID; a user asks for no connection of its own
// either.
func (c *BusinessCore) AccountGetBotBusinessConnection(in *mtproto.TLAccountGetBotBusinessConnection) (*mtproto.Updates, error) {
	const method = "account.getBotBusinessConnection"
	conn, err := c.svcCtx.Dao.Store.ConnectionByID(c.ctx, in.GetConnectionId())
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	if conn == nil || conn.BotID != c.MD.UserId {
		err = mtproto.ErrBusinessConnectionInvalid
		c.Logger.Errorf("%s - user %d: %v", method, c.MD.UserId, err)
		return nil, err
	}
	qts, err := qtsqueue.New(c.svcCtx.Dao.BoxDB).Current(c.ctx, qtsqueue.Device{UserId: c.MD.UserId, AuthKey: c.MD.PermAuthKeyId})
	if err != nil {
		c.Logger.Errorf("%s - qts of user %d: %v", method, c.MD.UserId, err)
		return nil, err
	}
	users, err := c.usersFor(method, []int64{conn.UserID})
	if err != nil {
		return nil, err
	}
	u := conn.ConnectUpdate(false)
	u.Qts = qts
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{u},
		Users:   users,
		Chats:   []*mtproto.Chat{},
		Date:    int32(time.Now().Unix()),
	}).To_Updates(), nil
}

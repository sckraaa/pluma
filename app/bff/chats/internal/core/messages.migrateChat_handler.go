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
	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// MessagesMigrateChat
// messages.migrateChat#a2875319 chat_id:long = Updates;
//
// The creator turns the basic group into a supergroup (task 047, msg
// migrateChat). The clients call it themselves before what a basic group
// cannot do: a public link, the history for new members, admin rights or
// ranks, restrictions and the rest (ADR 0034, p. 10).
func (c *ChatsCore) MessagesMigrateChat(in *mtproto.TLMessagesMigrateChat) (*mtproto.Updates, error) {
	r, err := c.svcCtx.Dao.MsgClient.MsgMigrateChat(c.ctx, &msgpb.TLMsgMigrateChat{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		ChatId:    in.ChatId,
	})
	if err != nil {
		c.Logger.Errorf("messages.migrateChat - error: %v", err)
		return nil, err
	}
	return r, nil
}

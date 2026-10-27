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
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// MessagesEditChatAdmin
// messages.editChatAdmin#a85bd1c2 chat_id:long user_id:InputUser is_admin:Bool = Bool;
//
// biz chat gives every member, this account's devices included, the members
// with seq in the transaction of the change (ADR 0021).
func (c *ChatsCore) MessagesEditChatAdmin(in *mtproto.TLMessagesEditChatAdmin) (*mtproto.Bool, error) {
	var (
		adminUser = mtproto.FromInputUser(c.MD.UserId, in.UserId)
	)

	if adminUser.PeerType != mtproto.PEER_USER ||
		adminUser.PeerId == c.MD.UserId {
		err := mtproto.ErrUserIdInvalid
		c.Logger.Errorf("messages.editChatAdmin - invalid user_id, err: %v", err)
		return nil, err
	}

	if _, err := c.svcCtx.Dao.ChatClient.Client().ChatEditChatAdmin(c.ctx, &chatpb.TLChatEditChatAdmin{
		ChatId:          in.ChatId,
		OperatorId:      c.MD.UserId,
		EditChatAdminId: adminUser.PeerId,
		IsAdmin:         in.IsAdmin,
	}); err != nil {
		c.Logger.Errorf("messages.editChatAdmin - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}

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
	"github.com/teamgram/teamgram-server/pkg/business"
)

// MessagesDeleteMessages
// messages.deleteMessages#e58e95d2 flags:# revoke:flags.0?true id:Vector<int> = messages.AffectedMessages;
//
// A connected business bot deletes, for its account, the messages it sent
// and those the account received in the chats it serves, as its rights
// allow (task 105, business.Store.CheckDelete); revoke is as the
// account's. The answer is the account's: the bot learns how many
// messages went (pts_count); what Telegram answers the bot here is not
// published.
func (c *MessagesCore) MessagesDeleteMessages(in *mtproto.TLMessagesDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	if _, err := c.businessAct(business.MethodDeleteMessages, nil); err != nil {
		c.Logger.Errorf("messages.deleteMessages - business connection: %v", err)
		return nil, err
	}
	if err := c.businessDeletable(in.Id); err != nil {
		c.Logger.Errorf("messages.deleteMessages - business connection: %v", err)
		return nil, err
	}
	// TODO(@benqi): Check message service.
	affectedMessages, err := c.svcCtx.Dao.MsgClient.MsgDeleteMessages(c.ctx, &msgpb.TLMsgDeleteMessages{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  mtproto.PEER_EMPTY,
		PeerId:    0,
		Revoke:    in.Revoke,
		Id:        in.Id,
	})

	if err != nil {
		c.Logger.Errorf("messages.deleteMessages - error: %v", err)
		return nil, err
	}

	return affectedMessages, nil
}

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

// MessagesReadMessageContents
// messages.readMessageContents#36a73f77 id:Vector<int> = messages.AffectedMessages;
//
// msg reads the messages' flags in the box transaction, clears those set,
// and pushes updateReadMessagesContents to the user's other devices.
func (c *MessagesCore) MessagesReadMessageContents(in *mtproto.TLMessagesReadMessageContents) (*mtproto.Messages_AffectedMessages, error) {
	contents := make([]*msgpb.ContentMessage, 0, len(in.Id))
	for _, id := range in.Id {
		contents = append(contents, &msgpb.ContentMessage{Id: id})
	}
	affected, err := c.svcCtx.Dao.MsgClient.MsgReadMessageContents(c.ctx, &msgpb.TLMsgReadMessageContents{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  mtproto.PEER_EMPTY,
		Id:        contents,
	})
	if err != nil {
		c.Logger.Errorf("messages.readMessageContents - %v", err)
		return nil, err
	}
	return affected, nil
}

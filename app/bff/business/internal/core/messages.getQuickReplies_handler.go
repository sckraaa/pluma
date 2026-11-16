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
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// MessagesGetQuickReplies
// messages.getQuickReplies#d483f2a8 hash:long = messages.QuickReplies;
//
// The quick reply shortcuts of the account in its order, each with its
// first message (task 105; service msg keeps them). The hash the clients
// count from what they have is quickRepliesNotModified.
func (c *BusinessCore) MessagesGetQuickReplies(in *mtproto.TLMessagesGetQuickReplies) (*mtproto.Messages_QuickReplies, error) {
	const method = "messages.getQuickReplies"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Dao.MsgClient.MsgGetQuickReplies(c.ctx, &msgpb.TLMsgGetQuickReplies{
		UserId: c.MD.UserId,
		Hash:   in.Hash,
	})
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return r, nil
}

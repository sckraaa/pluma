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

// MessagesCheckQuickReplyShortcut
// messages.checkQuickReplyShortcut#f1d0fbd3 shortcut:string = Bool;
//
// Whether a message can be added to the account's shortcut named so
// (task 105).
func (c *BusinessCore) MessagesCheckQuickReplyShortcut(in *mtproto.TLMessagesCheckQuickReplyShortcut) (*mtproto.Bool, error) {
	const method = "messages.checkQuickReplyShortcut"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Dao.MsgClient.MsgCheckQuickReplyShortcut(c.ctx, &msgpb.TLMsgCheckQuickReplyShortcut{
		UserId:   c.MD.UserId,
		Shortcut: in.Shortcut,
	})
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return r, nil
}

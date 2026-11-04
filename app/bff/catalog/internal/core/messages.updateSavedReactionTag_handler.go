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

// MessagesUpdateSavedReactionTag
// messages.updateSavedReactionTag#60297dec flags:# reaction:Reaction title:flags.0?string = Bool;
//
// A tag of the account's Saved Messages gets a title, or loses it (task 053).
func (c *CatalogCore) MessagesUpdateSavedReactionTag(in *mtproto.TLMessagesUpdateSavedReactionTag) (*mtproto.Bool, error) {
	r, err := c.svcCtx.Msg.MsgUpdateSavedReactionTag(c.ctx, &msgpb.TLMsgUpdateSavedReactionTag{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		Reaction:  in.Reaction,
		Title:     in.GetTitle().GetValue(),
	})
	if err != nil {
		c.Logger.Errorf("messages.updateSavedReactionTag - error: %v", err)
		return nil, err
	}
	return r, nil
}

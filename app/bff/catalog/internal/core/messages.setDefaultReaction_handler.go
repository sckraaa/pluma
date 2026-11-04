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
	"github.com/teamgram/teamgram-server/pkg/reactions"
)

// MessagesSetDefaultReaction
// messages.setDefaultReaction#4f47a016 reaction:Reaction = Bool;
//
// The account's quick reaction, which help.getConfig gives as
// reactions_default (task 053).
func (c *CatalogCore) MessagesSetDefaultReaction(in *mtproto.TLMessagesSetDefaultReaction) (*mtproto.Bool, error) {
	key, err := reactions.Key(in.GetReaction_REACTION())
	if err != nil {
		return nil, err
	}
	if err = c.checkAvailable([]*mtproto.Reaction{in.GetReaction_REACTION()}); err != nil {
		return nil, err
	}
	if err = reactions.SetDefault(c.ctx, c.svcCtx.Reactions, c.MD.UserId, key); err != nil {
		c.Logger.Errorf("messages.setDefaultReaction - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

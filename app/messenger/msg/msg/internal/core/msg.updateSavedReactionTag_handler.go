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
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/reactions"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgUpdateSavedReactionTag
// msg.updateSavedReactionTag user_id:long auth_key_id:long reaction:Reaction title:string = Bool;
//
// The tag reaction of the user's Saved Messages gets the title, or none
// when empty (task 053). The user's devices get updateSavedReactionTags
// with seq and load the tags again.
func (c *MsgCore) MsgUpdateSavedReactionTag(in *msg.TLMsgUpdateSavedReactionTag) (*mtproto.Bool, error) {
	key, err := reactions.Key(in.Reaction)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(in.Title) > reactions.TagTitleMax {
		return nil, mtproto.ErrReactionInvalid
	}
	_, err = c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		changed, err := c.svcCtx.Dao.SetTagTitle(b, key, in.Title)
		if err != nil || !changed {
			return err
		}
		return c.svcCtx.Dao.EmitSeqUpdate(b, mtproto.MakeTLUpdateSavedReactionTags(nil).To_Update(), nil, nil)
	})
	if err != nil {
		c.Logger.Errorf("msg.updateSavedReactionTag - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

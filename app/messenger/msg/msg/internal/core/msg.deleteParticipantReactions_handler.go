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
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MsgDeleteParticipantReactions
// msg.deleteParticipantReactions user_id:long auth_key_id:long channel_id:long participant_id:long msg_id:int = Updates;
//
// An admin who may delete the messages of others takes away the reactions of
// a participant (dao.DeleteParticipantReactions), in its box transaction
// with the channel locked; the members get each message's reactions by push.
// The reply holds the reactions of a single message as the admin sees them.
func (c *MsgCore) MsgDeleteParticipantReactions(in *msg.TLMsgDeleteParticipantReactions) (*mtproto.Updates, error) {
	var changed []dao.MessageReactions
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		var err error
		if changed, err = c.svcCtx.Dao.DeleteParticipantReactions(b, in.ChannelId, in.ParticipantId, in.MsgId); err != nil {
			return err
		}
		for _, m := range changed {
			if err = c.svcCtx.Dao.EmitChannelReactionsPush(b, in.ChannelId, in.AuthKeyId, m.MsgId, m.Reactions); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		c.Logger.Errorf("msg.deleteParticipantReactions - error: %v", err)
		return nil, err
	}
	var updates []*mtproto.Update
	for _, m := range changed {
		if m.MsgId == in.MsgId {
			updates = append(updates, dao.ChannelReactionsUpdate(in.UserId, in.ChannelId, m.MsgId, m.Reactions))
		}
	}
	return mtproto.MakeUpdatesByUpdates(updates...), nil
}

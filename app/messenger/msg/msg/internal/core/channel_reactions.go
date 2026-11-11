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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// sendChannelReaction sets the user's reactions to a message of a channel
// or a supergroup (task 044, dao.SetChannelReactions), in its box
// transaction with the channel locked. The members get
// updateMessageReactions by push, min, as every member sees it; the reply
// holds it as the user sees it, with the channel and the reacting users.
func (c *MsgCore) sendChannelReaction(in *msg.TLMsgSendReaction, keys []string) (*mtproto.Updates, error) {
	if err := c.ghostJoins(in.UserId, in.PeerId); err != nil {
		return nil, err
	}
	var r *dao.ChannelReactions
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, nil, func(b *userbox.Box) error {
		var err error
		if r, err = c.svcCtx.Dao.SetChannelReactions(b, in.PeerId, in.MsgId, keys, mtproto.FromBool(in.Big)); err != nil || !r.Changed {
			return err
		}
		return c.svcCtx.Dao.EmitChannelReactionsPush(b, in.PeerId, in.AuthKeyId, in.MsgId, r)
	}); err != nil {
		return nil, err
	}
	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.PeerId, Id: []int64{in.UserId}})
	if err != nil {
		return nil, err
	}
	update := dao.ChannelReactionsUpdate(in.UserId, in.PeerId, in.MsgId, r)
	ids := []int64{in.UserId}
	for _, p := range update.GetReactions_MESSAGEREACTIONS().GetRecentReactions() {
		if id := p.GetPeerId().GetUserId(); id != 0 {
			ids = append(ids, id)
		}
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids, To: []int64{in.UserId}})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeUpdatesByUpdatesUsersChats(users.GetUserListByIdList(in.UserId, ids...),
		[]*mtproto.Chat{mc.ToChat(in.UserId, int32(time.Now().Unix()))}, update), nil
}

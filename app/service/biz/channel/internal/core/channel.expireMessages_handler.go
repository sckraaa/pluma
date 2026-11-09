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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChannelExpireMessages
// channel.expireMessages channel_id:long now:long = Int32;
//
// The auto-delete worker of msg (task 063) calls this for each channel with
// messages due: the deletion is this service's (dao.ExpireMessages).
func (c *ChannelCore) ChannelExpireMessages(in *channel.TLChannelExpireMessages) (*mtproto.Int32, error) {
	n, err := c.svcCtx.Dao.ExpireMessages(c.ctx, in.ChannelId, in.Now)
	if err != nil {
		c.Logger.Errorf("channel.expireMessages - error: %v", err)
		return nil, err
	}
	return &mtproto.Int32{V: int32(n)}, nil
}

// ChannelGetUnreadReactions
// channel.getUnreadReactions self_id:long channel_id:long top_msg_id:int offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
//
// The messages of self_id with reactions it has not seen, as channel.search
// answers (dao.Search), of a topic or a thread unless top_msg_id is 0.
func (c *ChannelCore) ChannelGetUnreadReactions(in *channel.TLChannelGetUnreadReactions) (*mtproto.Messages_Messages, error) {
	q := dao.SearchQuery{MediaType: mtproto.MEDIA_EMPTY, UnreadReactionsOf: in.SelfId}
	if in.TopMsgId != 0 {
		if err := c.narrowToTopic(&q, in.ChannelId, in.TopMsgId); err != nil {
			return nil, err
		}
	}
	r, err := c.search(in.SelfId, in.ChannelId, q, in.OffsetId, in.AddOffset, in.Limit, in.MaxId, in.MinId)
	if err != nil {
		return nil, c.membershipError("channel.getUnreadReactions", err)
	}
	return r, nil
}

// ChannelReadReactions
// channel.readReactions self_id:long perm_auth_key_id:long channel_id:long top_msg_id:int = messages.AffectedHistory;
//
// The answer is the channel's pts with pts_count 0, offset 0, as for
// channel.readMentions: nothing of the channel's journal changes.
func (c *ChannelCore) ChannelReadReactions(in *channel.TLChannelReadReactions) (*mtproto.Messages_AffectedHistory, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.ReadReactions(b, in.ChannelId, in.TopMsgId)
	}); err != nil {
		return nil, c.membershipError("channel.readReactions", err)
	}
	pts, err := c.svcCtx.Dao.ChannelPts(c.ctx, in.ChannelId)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{Pts: pts}).To_Messages_AffectedHistory(), nil
}

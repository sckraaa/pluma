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

// ChannelReadHistory
// channel.readHistory self_id:long perm_auth_key_id:long channel_id:long max_id:int = Bool;
//
// self_id read the channel up to max_id (dao.Read), in its own box
// transaction: the device that read already knows it, the others get
// updateReadChannelInbox with seq.
func (c *ChannelCore) ChannelReadHistory(in *channel.TLChannelReadHistory) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.Read(b, in.ChannelId, in.MaxId)
	}); err != nil {
		return nil, c.membershipError("channel.readHistory", err)
	}
	return mtproto.BoolTrue, nil
}

// ChannelReadMessageContents
// channel.readMessageContents self_id:long perm_auth_key_id:long channel_id:long id:Vector<int> = Bool;
//
// self_id has seen messages id of the channel (dao.ReadContents), in its
// box transaction; the channel is not locked.
func (c *ChannelCore) ChannelReadMessageContents(in *channel.TLChannelReadMessageContents) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.ReadContents(b, in.ChannelId, in.Id)
	}); err != nil {
		return nil, c.membershipError("channel.readMessageContents", err)
	}
	return mtproto.BoolTrue, nil
}

// ChannelReadMentions
// channel.readMentions self_id:long perm_auth_key_id:long channel_id:long = messages.AffectedHistory;
//
// self_id reads all its unread mentions of the channel (dao.ReadMentions).
// The answer is the channel's pts with pts_count 0, offset 0: nothing of
// the channel's journal changes, and tdesktop applies the pts of this
// answer to the channel (apiwrap.cpp:2264-2273).
func (c *ChannelCore) ChannelReadMentions(in *channel.TLChannelReadMentions) (*mtproto.Messages_AffectedHistory, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.ReadMentions(b, in.ChannelId)
	}); err != nil {
		return nil, c.membershipError("channel.readMentions", err)
	}
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, in.ChannelId, nil)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{Pts: mc.GetChannel().GetPts()}).To_Messages_AffectedHistory(), nil
}

// ChannelGetUnreadMentions
// channel.getUnreadMentions self_id:long channel_id:long offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
//
// The messages of the channel that mention self_id and that it has not
// read (dao.Search), as channel.search answers.
func (c *ChannelCore) ChannelGetUnreadMentions(in *channel.TLChannelGetUnreadMentions) (*mtproto.Messages_Messages, error) {
	r, err := c.search(in.SelfId, in.ChannelId, dao.SearchQuery{MediaType: mtproto.MEDIA_EMPTY, UnreadMentionsOf: in.SelfId},
		in.OffsetId, in.AddOffset, in.Limit, in.MaxId, in.MinId)
	if err != nil {
		c.Logger.Errorf("channel.getUnreadMentions - error: %v", err)
	}
	return r, err
}

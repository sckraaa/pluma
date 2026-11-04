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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// ChannelDeleteChannel
// channel.deleteChannel self_id:long perm_auth_key_id:long channel_id:long = Updates;
//
// self_id, the creator, deletes the channel for everyone (dao.DeleteChannel,
// task 055). Its own devices get updateChannel with seq and the answer the
// same: the channel as channelForbidden, on which every client drops it
// (Android isNotInChat, MessagesController.java:20146-20155; iOS .kicked,
// UpdatePeers.swift:378-389; tdesktop data_channel.cpp:215-226; Web K
// dialogs.ts:178-183). The other members get the same in their own box
// through inbox.channelDeletedToInbox.
func (c *ChannelCore) ChannelDeleteChannel(in *channel.TLChannelDeleteChannel) (*mtproto.Updates, error) {
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, in.ChannelId, []int64{in.SelfId})
	if err != nil {
		return nil, c.membershipError("channel.deleteChannel", err)
	}
	var (
		view   *mtproto.Chat
		others []int64
	)
	_, err = c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		if others, err = c.svcCtx.Dao.DeleteChannel(b, in.ChannelId); err != nil {
			return err
		}
		after := proto.Clone(mc).(*channel.MutableChannel)
		after.Channel.Deleted = true
		after.Channel.ParticipantsCount = 0
		after.Participants = nil
		view = after.ToChat(in.SelfId, int32(b.Date()))
		u := mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: in.ChannelId}).To_Update()
		return c.svcCtx.Dao.EmitSeqUpdate(b, u, nil, []*mtproto.Chat{view})
	})
	if err != nil {
		return nil, c.membershipError("channel.deleteChannel", err)
	}
	c.Logger.Infof("channel.deleteChannel - channel %d deleted by %d, %d other members", in.ChannelId, in.SelfId, len(others))
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: in.ChannelId}).To_Update()},
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{view},
		Date:    int32(time.Now().Unix()),
	}).To_Updates(), nil
}

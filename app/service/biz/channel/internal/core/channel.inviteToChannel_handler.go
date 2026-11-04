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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// ChannelInviteToChannel
// channel.inviteToChannel self_id:long perm_auth_key_id:long channel_id:long id:Vector<long> = Updates;
//
// self_id adds users to the channel (dao.AddMembers) in its box transaction
// with the channel locked: their participant rows, the counter, the
// service message of a supergroup pushed to the members, and an inbox
// event for each added user, whose own transaction writes its dialog and
// updateChannel with seq (ADR 0021). The callers have left out the users
// whose privacy keeps self_id from adding them. The answer holds the
// service message and the channel; nobody added is an answer without
// updates.
func (c *ChannelCore) ChannelInviteToChannel(in *channel.TLChannelInviteToChannel) (*mtproto.Updates, error) {
	if len(in.Id) > dao.InviteMax {
		return nil, mtproto.ErrUsersTooMuch
	}
	var added *dao.Invited
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		if added, err = c.svcCtx.Dao.AddMembers(b, in.ChannelId, in.Id); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, &added.ChannelChange)
	}); err != nil {
		return nil, c.membershipError("channel.inviteToChannel", err)
	}
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, in.ChannelId, []int64{in.SelfId})
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	updates := []*mtproto.Update{}
	if added.Message != nil {
		shown := proto.Clone(added.Message).(*mtproto.Message)
		shown.Out = true
		updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: added.Pts, PtsCount: 1}).To_Update())
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: updates,
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{mc.ToChat(in.SelfId, now)},
		Date:    now,
	}).To_Updates(), nil
}

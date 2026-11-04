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

// ChannelEditTitle
// channel.editTitle self_id:long perm_auth_key_id:long channel_id:long title:string = Updates;
//
// self_id renames the channel (dao.EditTitle) in its box transaction with
// the channel locked; the members get the service message and
// updateChannel by push, the editor's other devices as well. The answer
// holds the service message, updateChannel and the channel renamed.
func (c *ChannelCore) ChannelEditTitle(in *channel.TLChannelEditTitle) (*mtproto.Updates, error) {
	var change *dao.ChannelChange
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		if change, err = c.svcCtx.Dao.EditTitle(b, in.ChannelId, in.Title); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, change)
	}); err != nil {
		return nil, c.membershipError("channel.editTitle", err)
	}
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, in.ChannelId, []int64{in.SelfId})
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	shown := proto.Clone(change.Message).(*mtproto.Message)
	shown.Out = true
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{
			mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: change.Pts, PtsCount: 1}).To_Update(),
			mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: in.ChannelId}).To_Update(),
		},
		Users: []*mtproto.User{},
		Chats: []*mtproto.Chat{mc.ToChat(in.SelfId, now)},
		Date:  now,
	}).To_Updates(), nil
}

// ChannelEditAbout
// channel.editAbout self_id:long perm_auth_key_id:long channel_id:long about:string = Bool;
//
// self_id changes the description of the channel (dao.EditAbout); the
// members, the editor's other devices among them, get updateChannel by
// push and load the full channel again.
func (c *ChannelCore) ChannelEditAbout(in *channel.TLChannelEditAbout) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		change, err := c.svcCtx.Dao.EditAbout(b, in.ChannelId, in.About)
		if err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, change)
	}); err != nil {
		return nil, c.membershipError("channel.editAbout", err)
	}
	return mtproto.BoolTrue, nil
}

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

// ChannelSetBlocked
// channel.setBlocked channel_id:long blocked:Bool = Bool;
//
// The owner of the server blocks a channel or a supergroup, or unblocks it
// (the panel, docs/ops/admin-panel.md): nobody may post, join or read it
// while it is blocked, every member sees it restricted. The same state
// asked for again changes nothing.
func (c *ChannelCore) ChannelSetBlocked(in *channel.TLChannelSetBlocked) (*mtproto.Bool, error) {
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, in.ChannelId, nil)
	if err != nil {
		return nil, err
	}
	if mc.GetChannel().GetDeleted() {
		return nil, mtproto.ErrChannelInvalid
	}
	blocked := mtproto.FromBool(in.Blocked)
	var change *dao.ChannelChange
	if _, err = c.svcCtx.Dao.RunBox(c.ctx, mc.GetChannel().GetCreatorUserId(), func(b *userbox.Box) error {
		var err error
		if change, err = c.svcCtx.Dao.SetBlocked(b, in.ChannelId, blocked); err != nil || change == nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, 0, change)
	}); err != nil {
		c.Logger.Errorf("channel.setBlocked - channel %d: %v", in.ChannelId, err)
		return nil, err
	}
	c.Logger.Infof("channel.setBlocked - channel %d, blocked %v", in.ChannelId, blocked)
	return mtproto.BoolTrue, nil
}

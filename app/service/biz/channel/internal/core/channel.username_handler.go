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
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChannelCheckUsername
// channel.checkUsername self_id:long channel_id:long username:string = Bool;
//
// dao.CheckUsername, in a box transaction of self_id that writes nothing:
// the channel's row is locked as its change would lock it.
func (c *ChannelCore) ChannelCheckUsername(in *channel.TLChannelCheckUsername) (*mtproto.Bool, error) {
	var free bool
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		free, err = c.svcCtx.Dao.CheckUsername(b, in.ChannelId, in.Username)
		return err
	}); err != nil {
		return nil, c.membershipError("channel.checkUsername", err)
	}
	return mtproto.ToBool(free), nil
}

// ChannelUpdateUsername
// channel.updateUsername self_id:long perm_auth_key_id:long channel_id:long username:string = Bool;
//
// dao.UpdateUsername in self_id's box transaction with the channel locked;
// the members, the editor's other devices among them, get updateChannel by
// push and load the channel again.
func (c *ChannelCore) ChannelUpdateUsername(in *channel.TLChannelUpdateUsername) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		change, err := c.svcCtx.Dao.UpdateUsername(b, in.ChannelId, in.Username)
		if err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, change)
	}); err != nil {
		return nil, c.membershipError("channel.updateUsername", err)
	}
	return mtproto.BoolTrue, nil
}

// ChannelGetAdminedPublicChannels
// channel.getAdminedPublicChannels self_id:long = Vector<long>;
func (c *ChannelCore) ChannelGetAdminedPublicChannels(in *channel.TLChannelGetAdminedPublicChannels) (*channel.Vector_Long, error) {
	ids, err := c.svcCtx.Dao.AdminedPublicChannels(c.ctx, in.SelfId)
	if err != nil {
		c.Logger.Errorf("channel.getAdminedPublicChannels - error: %v", err)
		return nil, err
	}
	return &channel.Vector_Long{Datas: ids}, nil
}

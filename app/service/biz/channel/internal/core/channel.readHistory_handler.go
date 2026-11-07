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

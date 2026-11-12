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

// ChannelAntispamDelete
// channel.antispamDelete user_id:long channel_id:long id:Vector<int> rule:int = Bool;
//
// The antispam deletes the messages id of user_id in the supergroup, which
// broke rule (task 082, ADR 0050; inbox.channelAntispamToInbox): in
// user_id's box with the channel locked (dao.AntispamDelete), the members,
// every device of user_id among them, get the deletion by push. The event
// that asks for it may come again: the answer is true either way.
func (c *ChannelCore) ChannelAntispamDelete(in *channel.TLChannelAntispamDelete) (*mtproto.Bool, error) {
	if len(in.Id) > dao.DeleteMax {
		return nil, mtproto.ErrLimitInvalid
	}
	var del *dao.Deleted
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.UserId, func(b *userbox.Box) error {
		var err error
		if del, err = c.svcCtx.Dao.AntispamDelete(b, in.ChannelId, in.Id, in.Rule); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitDeletePush(b, 0, del)
	}); err != nil {
		c.Logger.Errorf("channel.antispamDelete - channel %d, user %d: %v", in.ChannelId, in.UserId, err)
		return nil, err
	}
	if len(del.Ids) > 0 {
		c.Logger.Infof("channel.antispamDelete - channel %d, user %d: %d deleted by rule %d", in.ChannelId, in.UserId, len(del.Ids), in.Rule)
	}
	return mtproto.BoolTrue, nil
}

// ChannelAntispamFalsePositive
// channel.antispamFalsePositive self_id:long channel_id:long msg_id:int = Bool;
//
// self_id, an admin of the supergroup, reports that the antispam deleted
// the message msg_id wrongly (channels.reportAntiSpamFalsePositive,
// dao.AntispamFalsePositive): false for a message the antispam did not
// delete.
func (c *ChannelCore) ChannelAntispamFalsePositive(in *channel.TLChannelAntispamFalsePositive) (*mtproto.Bool, error) {
	var done bool
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		done, err = c.svcCtx.Dao.AntispamFalsePositive(b, in.ChannelId, in.MsgId)
		return err
	}); err != nil {
		return nil, c.membershipError("channel.antispamFalsePositive", err)
	}
	return mtproto.ToBool(done), nil
}

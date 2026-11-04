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

// ChannelDeleteMessages
// channel.deleteMessages self_id:long perm_auth_key_id:long channel_id:long id:Vector<int> = messages.AffectedMessages;
//
// self_id deletes messages of the channel for everyone (dao.Delete), in its
// box transaction with the channel locked; the members get the deletion by
// push. The answer is the pts of the deletion and its count, or the
// channel's pts with count 0 when nothing was deleted, as clients take it
// (messages.affectedMessages).
func (c *ChannelCore) ChannelDeleteMessages(in *channel.TLChannelDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	if len(in.Id) > dao.DeleteMax {
		return nil, mtproto.ErrLimitInvalid
	}
	var del *dao.Deleted
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		if del, err = c.svcCtx.Dao.Delete(b, in.ChannelId, in.Id); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitDeletePush(b, in.PermAuthKeyId, del)
	}); err != nil {
		return nil, c.membershipError("channel.deleteMessages", err)
	}
	pts := del.Pts
	if pts == 0 {
		var err error
		if pts, err = c.svcCtx.Dao.ChannelPts(c.ctx, in.ChannelId); err != nil {
			return nil, err
		}
	}
	return mtproto.MakeTLMessagesAffectedMessages(&mtproto.Messages_AffectedMessages{
		Pts:      pts,
		PtsCount: del.PtsCount,
	}).To_Messages_AffectedMessages(), nil
}

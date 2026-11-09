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

// ChannelEditAdmin
// channel.editAdmin self_id:long perm_auth_key_id:long channel_id:long user_id:long admin_rights:int rank:string keep_rank:Bool = Updates;
//
// self_id changes the admin rights of user_id (dao.EditAdmin); the members
// load the channel again on updateChannel, user_id seeing its new rights.
func (c *ChannelCore) ChannelEditAdmin(in *channel.TLChannelEditAdmin) (*mtproto.Updates, error) {
	return c.editRights("channel.editAdmin", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.EditAdmin(b, in.ChannelId, in.UserId, in.AdminRights, in.Rank, mtproto.FromBool(in.KeepRank))
	})
}

// ChannelEditBanned
// channel.editBanned self_id:long perm_auth_key_id:long channel_id:long user_id:long banned_rights:int until_date:int = Updates;
//
// self_id restricts, kicks or frees user_id (dao.EditBanned); the members
// load the channel again, a kicked user loses it by an inbox event.
func (c *ChannelCore) ChannelEditBanned(in *channel.TLChannelEditBanned) (*mtproto.Updates, error) {
	return c.editRights("channel.editBanned", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.EditBanned(b, in.ChannelId, in.UserId, in.BannedRights, in.UntilDate)
	})
}

// ChannelEditDefaultBannedRights
// channel.editDefaultBannedRights self_id:long perm_auth_key_id:long channel_id:long banned_rights:int = Updates;
//
// self_id sets the restrictions of every member of the supergroup
// (dao.EditDefaultBannedRights); the members load the channel again.
func (c *ChannelCore) ChannelEditDefaultBannedRights(in *channel.TLChannelEditDefaultBannedRights) (*mtproto.Updates, error) {
	return c.editRights("channel.editDefaultBannedRights", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.EditDefaultBannedRights(b, in.ChannelId, in.BannedRights)
	})
}

// ChannelEditCreator
// channel.editCreator self_id:long perm_auth_key_id:long channel_id:long user_id:long = Updates;
//
// self_id, the creator, hands the channel to user_id (dao.EditCreator); the
// caller has checked its 2FA password. The members load the channel again.
func (c *ChannelCore) ChannelEditCreator(in *channel.TLChannelEditCreator) (*mtproto.Updates, error) {
	return c.editRights("channel.editCreator", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.EditCreator(b, in.ChannelId, in.UserId)
	})
}

// editRights runs a change of rights by selfId in its box transaction and
// pushes it to the members in the same transaction. The answer holds the
// service message of a kick, updateChannel and the channel as selfId sees
// it afterwards.
func (c *ChannelCore) editRights(method string, selfId, permAuthKeyId, channelId int64, fn func(b *userbox.Box) (*dao.ChannelChange, error)) (*mtproto.Updates, error) {
	var change *dao.ChannelChange
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, selfId, func(b *userbox.Box) error {
		var err error
		if change, err = fn(b); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, permAuthKeyId, change)
	}); err != nil {
		return nil, c.membershipError(method, err)
	}
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, channelId, []int64{selfId})
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	var updates []*mtproto.Update
	if change.Message != nil {
		shown := proto.Clone(change.Message).(*mtproto.Message)
		shown.Out = true
		updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: change.Pts, PtsCount: 1}).To_Update())
	}
	if change.TTL != nil {
		updates = append(updates, change.TTL)
	}
	if change.Peer != nil {
		updates = append(updates, change.Peer)
	}
	updates = append(updates, mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: channelId}).To_Update())
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: updates,
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{mc.ToChat(selfId, now)},
		Date:    now,
	}).To_Updates(), nil
}

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
	"math/rand/v2"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// pinChannelMessage pins or unpins message id of a channel or a supergroup
// for everyone (task 043, dao.PinChannel), in the pinner's box transaction
// with the channel locked, pushed to the members by one event. The answer
// holds updatePinnedChannelMessages and, for a pin, the service message
// with its updateMessageID, the channel and the users.
func (c *MsgCore) pinChannelMessage(userId, authKeyId, channelId int64, id int32, unpin, silent bool) (*mtproto.Updates, error) {
	randomId := rand.Int64() //nolint:gosec // random_id of a service message, not a secret
	var pinned *dao.ChannelPinned
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, userId, nil, func(b *userbox.Box) error {
		var err error
		if pinned, err = c.svcCtx.Dao.PinChannel(b, channelId, id, unpin, silent, randomId); err != nil || pinned == nil {
			return err
		}
		updates := []*mtproto.Update{pinned.Update}
		if pinned.Service != nil {
			updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{
				Message_MESSAGE: pinned.Service.Message, Pts_INT32: pinned.Service.Pts, PtsCount: 1,
			}).To_Update())
		}
		return c.svcCtx.Dao.EmitChannelUpdatesPush(b, channelId, authKeyId, updates...)
	}); err != nil {
		return nil, err
	}
	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId, Id: []int64{userId}})
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	r := mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{},
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{mc.ToChat(userId, now)},
		Date:    now,
	}).To_Updates()
	if pinned == nil {
		return r, nil
	}
	r.Updates = append(r.Updates, pinned.Update)
	if pinned.Service != nil {
		shown := channelpb.ViewMessage(pinned.Service.Message, userId)
		r.Updates = append(r.Updates,
			mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: shown.Id, RandomId: randomId}).To_Update(),
			mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: pinned.Service.Pts, PtsCount: 1}).To_Update())
		h := mtproto.NewIDListHelper(userId)
		h.PickByMessage(shown)
		users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: h.UserIdList, To: []int64{userId}})
		if err != nil {
			return nil, err
		}
		r.Users = users.GetUserListByIdList(userId, h.UserIdList...)
	}
	return r, nil
}

// unpinAllChannelMessages unpins every pinned message of a channel for
// everyone (dao.UnpinAllChannel). The answer is the channel's pts and the
// one pts of the change, as for channels.deleteMessages; offset 0, all at
// once.
func (c *MsgCore) unpinAllChannelMessages(userId, authKeyId, channelId int64) (*mtproto.Messages_AffectedHistory, error) {
	var u *mtproto.Update
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, userId, nil, func(b *userbox.Box) error {
		var err error
		if u, err = c.svcCtx.Dao.UnpinAllChannel(b, channelId); err != nil || u == nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelUpdatesPush(b, channelId, authKeyId, u)
	}); err != nil {
		return nil, err
	}
	out := &mtproto.Messages_AffectedHistory{}
	if u != nil {
		out.Pts, out.PtsCount = u.GetPts_INT32(), u.GetPtsCount()
	} else {
		mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId})
		if err != nil {
			return nil, err
		}
		out.Pts = mc.GetChannel().GetPts()
	}
	return mtproto.MakeTLMessagesAffectedHistory(out).To_Messages_AffectedHistory(), nil
}

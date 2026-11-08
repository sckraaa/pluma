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
	"strings"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/plugin"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"
	"google.golang.org/protobuf/proto"
)

// sendChannelMessages sends messages of fromUserId to a channel or a
// supergroup (task 039, ADR 0034): one copy each with the channel's next id
// and pts, in the sender's box transaction with the channel locked, and one
// event that pushes them to the members (sync.pushChannelUpdates). A send
// repeated with the same random ids gets the first answer from the
// database, whatever a cache kept. The answer holds updateMessageID and
// updateNewChannelMessage of each message, the channel and the users.
func (c *MsgCore) sendChannelMessages(fromUserId, fromAuthKeyId, channelId int64, outBoxList []*msg.OutboxMessage) (*mtproto.Updates, error) {
	randomIds, err := randomIds(outBoxList)
	if err != nil {
		return nil, err
	}
	// The author's name, for a channel that signs its posts (task 044).
	author, err := c.authorName(fromUserId)
	if err != nil {
		return nil, err
	}
	outgoing := make([]dao.Outgoing, len(outBoxList))
	for i, o := range outBoxList {
		outgoing[i] = dao.Outgoing{RandomId: o.RandomId, Message: o.Message}
	}
	var sent []dao.ChannelSent
	_, err = c.svcCtx.Dao.RunBox(c.ctx, fromUserId, nil, func(b *userbox.Box) error {
		before, err := c.svcCtx.Dao.ChannelSentBefore(b, channelId, randomIds)
		if err != nil || before != nil {
			sent = before
			return err
		}
		var (
			mentioned []int64
			disc      *dao.Discussion
		)
		if sent, mentioned, disc, err = c.svcCtx.Dao.SendChannel(b, channelId, outgoing, author); err != nil {
			return err
		}
		if err = c.svcCtx.Dao.EmitChannelPush(b, channelId, fromAuthKeyId, sent, mentioned); err != nil {
			return err
		}
		if disc != nil && len(disc.Copies) > 0 {
			// The copies in the discussion group, to its members.
			return c.svcCtx.Dao.EmitChannelPush(b, disc.GroupId, fromAuthKeyId, disc.Copies, nil)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId, Id: []int64{fromUserId}})
	if err != nil {
		return nil, err
	}
	h := mtproto.NewIDListHelper(fromUserId)
	updates := make([]*mtproto.Update, 0, 2*len(sent))
	for _, s := range sent {
		m := proto.Clone(s.Message).(*mtproto.Message)
		m.Out = true
		h.PickByMessage(m)
		if s.RandomId != 0 {
			updates = append(updates, mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: m.Id, RandomId: s.RandomId}).To_Update())
		}
		updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m, Pts_INT32: s.Pts, PtsCount: 1}).To_Update())
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: h.UserIdList, To: []int64{fromUserId}})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: updates,
		Users:   users.GetUserListByIdList(fromUserId, h.UserIdList...),
		Chats:   []*mtproto.Chat{mc.ToChat(fromUserId, int32(time.Now().Unix()))},
		Date:    int32(time.Now().Unix()),
	}).To_Updates(), nil
}

// editChannelMessage applies fromUserId's edit to message id of a channel or
// a supergroup (task 043, dao.EditChannel): in the editor's box transaction
// with the channel locked, pushed to the members by one event. The answer
// holds updateEditChannelMessage, the channel and the users.
func (c *MsgCore) editChannelMessage(fromUserId, fromAuthKeyId, channelId int64, editBox *msg.OutboxMessage, id int32) (*mtproto.Updates, error) {
	edit := plugin.RemakeMessage(c.ctx, c.svcCtx.MsgPlugin, editBox.Message, fromUserId, editBox.NoWebpage, func() bool { return false })
	var (
		edited *mtproto.Message
		pts    int32
	)
	_, err := c.svcCtx.Dao.RunBox(c.ctx, fromUserId, nil, func(b *userbox.Box) error {
		var err error
		if edited, pts, err = c.svcCtx.Dao.EditChannel(b, channelId, id, edit); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelEditPush(b, channelId, fromAuthKeyId, edited, pts)
	})
	if err != nil {
		return nil, err
	}
	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: channelId, Id: []int64{fromUserId}})
	if err != nil {
		return nil, err
	}
	shown := channelpb.ViewMessage(edited, fromUserId)
	h := mtproto.NewIDListHelper(fromUserId)
	h.PickByMessage(shown)
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: h.UserIdList, To: []int64{fromUserId}})
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: pts, PtsCount: 1}).To_Update()},
		Users:   users.GetUserListByIdList(fromUserId, h.UserIdList...),
		Chats:   []*mtproto.Chat{mc.ToChat(fromUserId, int32(time.Now().Unix()))},
		Date:    int32(time.Now().Unix()),
	}).To_Updates(), nil
}

// authorName is the name a signed post shows: the user's first and last
// name, as Telegram signs posts (channels.toggleSignatures).
func (c *MsgCore) authorName(userId int64) (string, error) {
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: []int64{userId}})
	if err != nil {
		return "", err
	}
	u, ok := users.GetImmutableUser(userId)
	if !ok {
		return "", nil
	}
	return strings.TrimSpace(u.FirstName() + " " + u.LastName()), nil
}

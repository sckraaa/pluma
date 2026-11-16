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
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/business"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// businessAct checks a request c's bot makes through its business
// connection (invokeWithBusinessConnection, task 105) to call m in the
// chat of peer (nil for a method without one), and makes c run as the
// account of the connection: c.MD.UserId becomes the account, so that the
// handler reads and writes the account's box and pushes to every device
// of the account. PermAuthKeyId stays the bot's: it names the files the bot
// uploaded (the owner of an uploaded file is the key that uploaded it), and
// as the key of none of the account's devices it leaves out none of them
// when the change is pushed. A request without a connection is not
// changed, and nil is returned.
func (c *MessagesCore) businessAct(m business.Method, peer *mtproto.InputPeer) (*business.Act, error) {
	connectionId := c.MD.GetBusinessConnectionId()
	if connectionId == "" {
		return nil, nil
	}
	var p *mtproto.PeerUtil
	if peer != nil {
		p = mtproto.FromInputPeer2(c.MD.UserId, peer)
	}
	act, err := c.svcCtx.Business.Act(c.ctx, c.svcCtx.Dao.UserClient, c.MD.UserId, connectionId, m, p, time.Now())
	if err != nil {
		return nil, err
	}
	c.business = act
	c.MD.UserId = act.Owner()
	return act, nil
}

// businessSend checks the parts of a send through a business connection
// that the bot cannot use: a message waiting for a time, a quick reply
// shortcut and a sender other than the account belong to the account's own
// apps (Bot API sends business messages now, as the account). Nil without
// a connection.
func (c *MessagesCore) businessSend(scheduleDate *wrapperspb.Int32Value, shortcut *mtproto.InputQuickReplyShortcut, sendAs *mtproto.InputPeer) error {
	if c.business == nil {
		return nil
	}
	if scheduleDate != nil || shortcut != nil || sendAs != nil {
		return mtproto.ErrBotAccessForbidden
	}
	return nil
}

// businessMessage marks m, a message the bot sends through its connection,
// as sent by it: via_business_bot_id of the account's copy, "messages sent
// by business bots on behalf of the user using this method will have the
// via_business_bot_id flag set" (core.telegram.org/api/bots/
// connected-business-bots). The clients sign the account's message with
// the name of the bot (tdesktop history/history_item.cpp:2812-2817,
// Android ui/Cells/ChatMessageCell.java:15563-15569). The copy of the
// other side is without it (msg ReceiveIncoming).
func (c *MessagesCore) businessMessage(m *mtproto.Message) {
	if c.business == nil {
		return
	}
	m.ViaBusinessBotId = wrapperspb.Int64(c.business.BotID())
}

// markupBot is the bot whose keyboards c's message carries: the bot of a
// business connection writes them into the account's message, which
// otherwise would carry none (botMarkup).
func (c *MessagesCore) markupBot() int64 {
	if c.business != nil {
		return c.business.BotID()
	}
	return c.MD.UserId
}

// businessReply is r, the account's answer to a request through a
// business connection, as the bot gets it (business.Act.BotUpdates), with
// the users as the bot sees them; r itself without a connection.
func (c *MessagesCore) businessReply(r *mtproto.Updates) (*mtproto.Updates, error) {
	if c.business == nil || r == nil {
		return r, nil
	}
	out, ids := c.business.BotUpdates(r)
	botId := c.business.BotID()
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id:      append(ids, botId),
		Privacy: true,
		HasTo:   true,
		To:      []int64{botId},
	})
	if err != nil {
		return nil, err
	}
	out.Users = users.GetUserListByIdList(botId, ids...)
	return out, nil
}

// businessEditable checks that the bot may edit message id of the
// account's box through its connection: one it sent there itself.
func (c *MessagesCore) businessEditable(id int32) error {
	if c.business == nil {
		return nil
	}
	boxes, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessageList(c.ctx, &message.TLMessageGetUserMessageList{
		UserId: c.MD.UserId,
		IdList: []int32{id},
	})
	if err != nil {
		return err
	}
	if len(boxes.GetDatas()) != 1 {
		return mtproto.ErrMessageIdInvalid
	}
	return c.business.CheckEdit(boxes.GetDatas()[0].GetMessage())
}

// businessDeletable checks that the bot may delete messages ids of the
// account's box through its connection (business.Store.CheckDelete). An
// id the box does not have deletes nothing, as for the account.
func (c *MessagesCore) businessDeletable(ids []int32) error {
	if c.business == nil {
		return nil
	}
	boxes, err := c.svcCtx.Dao.MessageClient.MessageGetUserMessageList(c.ctx, &message.TLMessageGetUserMessageList{
		UserId: c.MD.UserId,
		IdList: ids,
	})
	if err != nil {
		return err
	}
	return c.svcCtx.Business.CheckDelete(c.ctx, c.svcCtx.Dao.UserClient, c.business, boxes.GetDatas())
}

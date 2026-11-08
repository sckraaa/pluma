// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
)

// The dialogs with channels (task 038): the row of dialogs holds the user's
// own state of the channel, the rest comes from the channel at each read
// (ADR 0034, p. 1).

// fillChannelDialogs sets pts, top_message, the read marks and the unread
// counts of the channel dialogs of list from their channels: the clients
// start a channel's state from dialog.pts (Web K throws on 0,
// apiUpdatesManager.ts:531-534; tdesktop asks no difference from it,
// api_updates.cpp:726). A channel dialog biz channel does not give (the
// channel is gone) keeps what its row says.
func (c *DialogsCore) fillChannelDialogs(list dialog.DialogExtList) error {
	var ids []int64
	for _, ex := range list {
		if p := ex.GetDialog().GetPeer(); p.GetPredicateName() == mtproto.Predicate_peerChannel {
			ids = append(ids, p.GetChannelId())
		}
	}
	if len(ids) == 0 {
		return nil
	}
	dialogs, err := c.svcCtx.Dao.Channel.ChannelGetChannelDialogs(c.ctx, &channelpb.TLChannelGetChannelDialogs{SelfId: c.MD.UserId, IdList: ids})
	if err != nil {
		return err
	}
	for _, ex := range list {
		p := ex.GetDialog().GetPeer()
		if p.GetPredicateName() != mtproto.Predicate_peerChannel {
			continue
		}
		for _, d := range dialogs.GetDatas() {
			if d.GetPeer().GetChannelId() != p.GetChannelId() {
				continue
			}
			ex.Dialog.Pts = d.GetPts()
			ex.Dialog.TopMessage = d.GetTopMessage()
			ex.Dialog.ReadInboxMaxId = d.GetReadInboxMaxId()
			ex.Dialog.ReadOutboxMaxId = d.GetReadOutboxMaxId()
			ex.Dialog.UnreadCount = d.GetUnreadCount()
			ex.Dialog.UnreadMentionsCount = d.GetUnreadMentionsCount()
			ex.Dialog.ViewForumAsMessages = d.GetViewForumAsMessages()
			ex.Dialog.TtlPeriod = d.GetTtlPeriod()
		}
	}
	return nil
}

// channelTopMessages are the last messages of the channels ids, as the
// account sees them.
func (c *DialogsCore) channelTopMessages(ids []int64) []*mtproto.Message {
	if len(ids) == 0 {
		return nil
	}
	messages, err := c.svcCtx.Dao.Channel.ChannelGetTopMessages(c.ctx, &channelpb.TLChannelGetTopMessages{SelfId: c.MD.UserId, IdList: ids})
	if err != nil {
		c.Logger.Errorf("the top messages of channels %v: %v", ids, err)
		return nil
	}
	return messages.GetDatas()
}

// channelChats are the channels ids as the account sees them.
func (c *DialogsCore) channelChats(ids []int64) []*mtproto.Chat {
	if len(ids) == 0 {
		return []*mtproto.Chat{}
	}
	list, err := c.svcCtx.Dao.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: ids})
	if err != nil {
		c.Logger.Errorf("the channels %v: %v", ids, err)
		return []*mtproto.Chat{}
	}
	return list.GetChannelListByIdList(c.MD.UserId)
}

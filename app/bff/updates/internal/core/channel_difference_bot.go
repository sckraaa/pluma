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
	"slices"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botprivacy"
)

// hideFromBot takes out of the difference of a supergroup what a bot must not
// get (pkg/botprivacy). The push leaves those messages out as well, so a bot
// sees a gap in pts at every one of them and asks for this difference: without
// the filter the first gap would hand it all it was not meant to read. The
// pts of the answer stay, which closes the gap.
func (c *UpdatesCore) hideFromBot(channelId int64, mc *channelpb.MutableChannel, diff *mtproto.Updates_ChannelDifference) error {
	if mc.GetChannel().GetBroadcast() {
		return nil
	}
	me, err := c.svcCtx.Dao.UserGetImmutableUser(c.ctx, &userpb.TLUserGetImmutableUser{Id: c.MD.UserId})
	if err != nil {
		return err
	}
	if !me.IsBot() {
		return nil
	}
	p, _ := mc.GetParticipant(me.Id())
	bot := botprivacy.Bot{
		Id:       me.Id(),
		Username: me.Username(),
		ReadsAll: me.BotChatHistory() || p.GetRole() == channelpb.RoleCreator || p.GetRole() == channelpb.RoleAdmin,
	}

	var (
		senders []int64
		replied []int32
	)
	eachMessage(diff, func(m *mtproto.Message) {
		if id := m.GetFromId().GetUserId(); id != 0 && !slices.Contains(senders, id) {
			senders = append(senders, id)
		}
		if id := botprivacy.ReplyToInChat(m); id != 0 && !slices.Contains(replied, id) {
			replied = append(replied, id)
		}
	})
	bots, err := c.svcCtx.Dao.UserCheckBots(c.ctx, &userpb.TLUserCheckBots{Id: senders})
	if err != nil {
		return err
	}
	var mine []int32
	if len(replied) > 0 {
		messages, err := c.svcCtx.Dao.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: me.Id(), ChannelId: channelId, Id: replied})
		if err != nil {
			return err
		}
		for _, m := range messages.GetDatas() {
			if m.GetFromId().GetUserId() == me.Id() {
				mine = append(mine, m.GetId())
			}
		}
	}
	keepVisible(diff, func(m *mtproto.Message) bool {
		fromBot := slices.Contains(bots.GetDatas(), m.GetFromId().GetUserId())
		return botprivacy.Visible(bot, m, fromBot, slices.Contains(mine, botprivacy.ReplyToInChat(m)))
	})
	return nil
}

// eachMessage visits the messages of the difference, those of its updates too.
func eachMessage(diff *mtproto.Updates_ChannelDifference, visit func(*mtproto.Message)) {
	for _, list := range [][]*mtproto.Message{diff.GetNewMessages(), diff.GetMessages()} {
		for _, m := range list {
			visit(m)
		}
	}
	for _, u := range diff.GetOtherUpdates() {
		if m := u.GetMessage_MESSAGE(); m != nil {
			visit(m)
		}
	}
}

// keepVisible leaves in the difference the messages visible tells, and the
// updates without a message.
func keepVisible(diff *mtproto.Updates_ChannelDifference, visible func(*mtproto.Message) bool) {
	hidden := func(m *mtproto.Message) bool { return !visible(m) }
	diff.NewMessages = slices.DeleteFunc(diff.NewMessages, hidden)
	diff.Messages = slices.DeleteFunc(diff.Messages, hidden)
	diff.OtherUpdates = slices.DeleteFunc(diff.OtherUpdates, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m != nil && !visible(m)
	})
}

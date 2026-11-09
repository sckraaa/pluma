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
	"errors"
	"math/rand"
	"regexp"
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// maxStartParam and startParamRe: core.telegram.org/bots/features#deep-linking.
const maxStartParam = 64

var startParamRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// MessagesStartBot
// messages.startBot#e6df7378 bot:InputUser peer:InputPeer random_id:long start_param:string = Updates;
//
// The deep link of a bot: the user's message /start <param> to the bot, in a
// private chat, or /start@bot <param> in a group the bot is added to first.
// Clients send this method and no messages.addChatUser for a group with a
// parameter (Android MessagesController.addUserToChat,
// tdesktop ApiWrap::sendBotStart with ShowAddParticipantsError). tdesktop
// names a private chat with inputPeerEmpty.
func (c *BotsCore) MessagesStartBot(in *mtproto.TLMessagesStartBot) (*mtproto.Updates, error) {
	target := mtproto.FromInputUser(c.MD.UserId, in.Bot)
	if !target.IsUser() {
		return nil, mtproto.ErrBotInvalid
	}
	bot, err := c.botUser(target.PeerId)
	if err != nil {
		return nil, err
	}
	switch {
	case in.StartParam == "":
		return nil, mtproto.ErrStartParamEmpty
	case len(in.StartParam) > maxStartParam:
		return nil, mtproto.ErrStartParamTooLong
	case !startParamRe.MatchString(in.StartParam):
		return nil, mtproto.ErrStartParamInvalid
	}

	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	private := in.Peer.GetPredicateName() == mtproto.Predicate_inputPeerEmpty || peer.IsUser() && peer.PeerId == bot.Id()
	var (
		text  string
		added *mtproto.Updates
	)
	switch {
	case private:
		peer = mtproto.MakeUserPeerUtil(bot.Id())
		text = "/start " + in.StartParam
	case peer.IsChat(), peer.IsChannel():
		if bot.BotNochats() {
			return nil, mtproto.ErrBotGroupsBlocked
		}
		if added, err = c.addBot(peer, bot.Id()); err != nil {
			c.Logger.Errorf("messages.startBot - error: %v", err)
			return nil, err
		}
		text = "/start@" + bot.Username() + " " + in.StartParam
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}

	updates, err := c.svcCtx.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
		Message: []*msgpb.OutboxMessage{msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			RandomId: in.RandomId,
			Message: mtproto.MakeTLMessage(&mtproto.Message{
				Out:     true,
				FromId:  mtproto.MakePeerUser(c.MD.UserId),
				PeerId:  peer.ToPeer(),
				Date:    int32(time.Now().Unix()),
				Message: text,
			}).To_Message(),
		}).To_OutboxMessage()},
	})
	if err != nil {
		c.Logger.Errorf("messages.startBot - error: %v", err)
		return nil, err
	}
	if added != nil {
		updates.Updates = append(added.GetUpdates(), updates.GetUpdates()...)
		updates.Users = append(updates.Users, added.GetUsers()...)
		updates.Chats = append(updates.Chats, added.GetChats()...)
	}
	return updates, nil
}

// addBot adds the bot to the group or supergroup, unless it is a member; nil
// when it was.
func (c *BotsCore) addBot(peer *mtproto.PeerUtil, botId int64) (*mtproto.Updates, error) {
	if peer.IsChannel() {
		return c.svcCtx.Channel.ChannelInviteToChannel(c.ctx, &channelpb.TLChannelInviteToChannel{
			SelfId:        c.MD.UserId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			ChannelId:     peer.PeerId,
			Id:            []int64{botId},
		})
	}
	chat, err := c.svcCtx.Chat.ChatAddChatUser(c.ctx, &chatpb.TLChatAddChatUser{
		ChatId:    peer.PeerId,
		InviterId: c.MD.UserId,
		UserId:    botId,
		IsBot:     true,
	})
	if errors.Is(err, mtproto.ErrUserAlreadyParticipant) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// The service message of the addition, as messages.addChatUser sends it.
	return c.svcCtx.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  mtproto.PEER_CHAT,
		PeerId:    peer.PeerId,
		Message: []*msgpb.OutboxMessage{msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: true,
			RandomId:  rand.Int63(),
			Message:   chat.MakeMessageService(c.MD.UserId, mtproto.MakeMessageActionChatAddUser(botId)),
		}).To_OutboxMessage()},
	})
}

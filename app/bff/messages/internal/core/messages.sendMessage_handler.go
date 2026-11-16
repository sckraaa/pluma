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
	"time"

	"github.com/teamgram/proto/mtproto"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/business"

	"github.com/zeromicro/go-zero/core/contextx"
	"github.com/zeromicro/go-zero/core/threading"
)

// MessagesSendMessage
// messages.sendMessage#d9d75a4 flags:# no_webpage:flags.1?true silent:flags.5?true background:flags.6?true clear_draft:flags.7?true noforwards:flags.14?true peer:InputPeer reply_to_msg_id:flags.0?int message:string random_id:long reply_markup:flags.2?ReplyMarkup entities:flags.3?Vector<MessageEntity> schedule_date:flags.10?int send_as:flags.13?InputPeer = Updates;
func (c *MessagesCore) MessagesSendMessage(in *mtproto.TLMessagesSendMessage) (*mtproto.Updates, error) {
	// A connected business bot writes for its account (task 105).
	if _, err := c.businessAct(business.MethodSendMessage, in.Peer); err != nil {
		c.Logger.Errorf("messages.sendMessage - business connection: %v", err)
		return nil, err
	}
	if err := c.businessSend(in.ScheduleDate, in.GetQuickReplyShortcut(), in.GetSendAs()); err != nil {
		c.Logger.Errorf("messages.sendMessage - business connection: %v", err)
		return nil, err
	}

	var (
		peer = mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	)

	if !peer.IsChatOrUser() && !peer.IsChannel() {
		c.Logger.Errorf("invalid peer: %v", in.Peer)
		err := mtproto.ErrPeerIdInvalid
		return nil, err
	}

	if peer.IsUser() && peer.IsSelfUser(c.MD.UserId) {
		peer.PeerType = mtproto.PEER_USER
	}

	if in.Message == "" {
		err := mtproto.ErrMessageEmpty
		c.Logger.Errorf("message empty: %v", err)
		return nil, err
	}
	// A message for a quick reply shortcut (task 105).
	if err := c.checkQuickReplySend(peer, in.GetQuickReplyShortcut(), in.ScheduleDate); err != nil {
		c.Logger.Errorf("messages.sendMessage - error: %v", err)
		return nil, err
	}
	// The peer the messages come from (send_as, task 079).
	from, err := c.sendFrom(peer, in.GetSendAs())
	if err != nil {
		c.Logger.Errorf("messages.sendMessage - error: %v", err)
		return nil, err
	}

	// TODO(@benqi): calc utf16len(message)
	//else if len(request.Message) > 4000 {
	//	err = mtproto.ErrMessageTooLong
	//	c.Logger.Errorf("messages.sendMessage: %v", err)
	//	return
	//}

	// messages.sendMessage#983f9745 flags:#
	//no_webpage:flags.1?true
	//silent:flags.5?true
	//background:flags.6?true
	//clear_draft:flags.7?true
	//noforwards:flags.14?true
	//update_stickersets_order:flags.15?true
	//invert_media:flags.16?true
	//peer:InputPeer
	//reply_to:flags.0?InputReplyTo
	//message:string
	//random_id:long
	//reply_markup:flags.2?ReplyMarkup
	//entities:flags.3?Vector<MessageEntity>
	//schedule_date:flags.10?int
	//send_as:flags.13?InputPeer
	//quick_reply_shortcut:flags.17?InputQuickReplyShortcut
	//effect:flags.18?long = Updates;
	replyMarkup, err := c.botMarkup(in.ReplyMarkup, false)
	if err != nil {
		c.Logger.Errorf("messages.sendMessage - reply markup: %v", err)
		return nil, err
	}

	outMessage := mtproto.MakeTLMessage(&mtproto.Message{
		Out:                  true,
		Mentioned:            false,
		MediaUnread:          false,
		Silent:               in.Silent,
		Post:                 false,
		FromScheduled:        false,
		Legacy:               false,
		EditHide:             false,
		Pinned:               false,
		Noforwards:           in.Noforwards,
		InvertMedia:          in.InvertMedia,
		Id:                   0,
		FromId:               from,
		PeerId:               peer.ToPeer(),
		SavedPeerId:          nil,
		FwdFrom:              nil,
		ViaBotId:             nil,
		ReplyTo:              nil,
		Date:                 int32(time.Now().Unix()),
		Message:              in.Message,
		Media:                nil,
		ReplyMarkup:          replyMarkup,
		Entities:             in.Entities,
		Views:                nil,
		Forwards:             nil,
		Replies:              nil,
		EditDate:             nil,
		PostAuthor:           nil,
		GroupedId:            nil,
		Reactions:            nil,
		RestrictionReason:    nil,
		TtlPeriod:            nil,
		QuickReplyShortcutId: nil,
		Effect:               in.Effect,
		Factcheck:            nil,
	}).To_Message()

	c.businessMessage(outMessage)

	// Fix SavedPeerId
	if peer.IsSelfUser(c.MD.UserId) {
		outMessage.SavedPeerId = peer.ToPeer()
	}
	// The sublist of a monoforum, and a post suggested through one.
	if sp := sublistPeer(c.MD.UserId, peer, in.GetReplyTo()); sp != nil {
		outMessage.SavedPeerId = sp
	}
	if err := withSuggestedPost(outMessage, peer, in.GetSuggestedPost()); err != nil {
		c.Logger.Errorf("%s - error: %v", "messages.sendMessage", err)
		return nil, err
	}

	// Fix ReplyToMsgId
	if outMessage.ReplyTo, err = c.makeReplyHeader(peer, in.GetReplyToMsgId(), in.GetReplyTo()); err != nil {
		c.Logger.Errorf("messages.sendMessage - error: %v", err)
		return nil, err
	}
	// A reply to a story (task 084).
	if story, err := c.storyReplyHeader(in.GetReplyToMsgId(), in.GetReplyTo()); err != nil {
		c.Logger.Errorf("messages.sendMessage - error: %v", err)
		return nil, err
	} else if story != nil {
		outMessage.ReplyTo = story
	}

	//outMessage, _ = c.fixMessageEntities(c.MD.UserId, peer, in.NoWebpage, outMessage, func() bool {
	//	hasBot := c.MD.IsBot
	//	if !hasBot {
	//		isBot, _ := c.svcCtx.Dao.UserClient.UserIsBot(c.ctx, &userpb.TLUserIsBot{
	//			Id: peer.PeerId,
	//		})
	//		hasBot = mtproto.FromBool(isBot)
	//	}
	//
	//	return hasBot
	//})

	outbox := msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
		NoWebpage:    in.NoWebpage,
		Background:   in.Background,
		RandomId:     in.RandomId,
		Message:      withRepeatPeriod(outMessage, in.ScheduleDate, in.GetScheduleRepeatPeriod()),
		ScheduleDate: in.ScheduleDate,
	}).To_OutboxMessage()
	if shortcut := in.GetQuickReplyShortcut(); shortcut != nil {
		// The message is kept for later: the draft of Saved Messages is
		// not the shortcut's.
		rUpdate, err := c.addQuickReplyMessages(shortcut, []*msgpb.OutboxMessage{outbox})
		if err != nil {
			c.Logger.Errorf("messages.sendMessage - quick reply: %v", err)
			return nil, err
		}
		c.invalidateSentLoginCodes(in.Message)
		return rUpdate, nil
	}

	rUpdate, err := c.svcCtx.Dao.MsgClient.MsgSendMessageV2(c.ctx, &msgpb.TLMsgSendMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
		Message:   []*msgpb.OutboxMessage{outbox},
	})

	if err != nil {
		c.Logger.Errorf("messages.sendMessage#fa88427a - error: %v", err)
		return nil, err
	}
	c.invalidateSentLoginCodes(in.Message)

	// The draft is the account's, not the bot's: a bot's send leaves it.
	if in.ClearDraft && c.business == nil {
		ctx := contextx.ValueOnlyFrom(c.ctx)
		threading.GoSafe(func() {
			c.clearSentDraft(ctx, peer, in.GetReplyTo())
		})
	}

	return c.businessReply(rUpdate)
}

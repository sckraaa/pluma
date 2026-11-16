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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/botgames"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// MessagesEditMessage
// messages.editMessage#48f71778 flags:# no_webpage:flags.1?true peer:InputPeer id:int message:flags.11?string media:flags.14?InputMedia reply_markup:flags.2?ReplyMarkup entities:flags.3?Vector<MessageEntity> schedule_date:flags.15?int = Updates;
func (c *MessagesCore) MessagesEditMessage(in *mtproto.TLMessagesEditMessage) (*mtproto.Updates, error) {
	var (
		peer         = mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
		editMessages *message.Vector_MessageBox
		dstMessage   *mtproto.MessageBox
		err          error
	)

	// A message of a quick reply shortcut (task 105).
	if in.GetQuickReplyShortcutId() != nil {
		return c.editQuickReplyMessage(peer, in)
	}

	// A poll is only closed through an edit; a checklist changes under its
	// own lock, so that a simultaneous tick is not lost (task 062).
	switch in.GetMedia().GetPredicateName() {
	case mtproto.Predicate_inputMediaPoll:
		return c.closePollByEdit(peer, in)
	case mtproto.Predicate_inputMediaTodo:
		return c.svcCtx.Dao.MsgClient.MsgEditTodoList(c.ctx, &msgpb.TLMsgEditTodoList{
			UserId: c.MD.UserId, AuthKeyId: c.MD.PermAuthKeyId, PeerType: peer.PeerType, PeerId: peer.PeerId, MsgId: in.Id, Todo: in.Media.GetTodo(),
		})
	}

	scheduled := in.GetScheduleDate() != nil
	peerType := peer.PeerType
	if scheduled {
		// A waiting message is read from its queue, by its queue id (task 061).
		if peerType == mtproto.PEER_SELF {
			peer.PeerType = mtproto.PEER_USER
			peer.PeerId = c.MD.UserId
		}
		peerType = mtproto.PEER_EMPTY
		if editMessages, err = c.scheduledMessageToEdit(peer, in.Id); err != nil {
			c.Logger.Errorf("messages.editMessage - error: %v", err)
			return nil, err
		}
	}
	switch peerType {
	case mtproto.PEER_EMPTY:
	case mtproto.PEER_SELF, mtproto.PEER_USER, mtproto.PEER_CHAT:
		if peer.PeerType == mtproto.PEER_SELF {
			peer.PeerType = mtproto.PEER_USER
		}
		if peer.PeerType == mtproto.PEER_USER {
			//if !md.IsBot {
			//	hasBot = s.UserFacade.IsBot(ctx, peer.PeerId)
			//}
		}
		editMessages, _ = c.svcCtx.MessageClient.MessageGetUserMessageList(c.ctx, &message.TLMessageGetUserMessageList{
			UserId: c.MD.UserId,
			IdList: []int32{in.Id},
		})
	case mtproto.PEER_CHANNEL:
		// The one copy of a channel's message (task 043); who may edit it
		// is msg's to check, as admins edit others' posts.
		messages, err2 := c.svcCtx.Dao.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{
			SelfId:    c.MD.UserId,
			ChannelId: peer.PeerId,
			Id:        []int32{in.Id},
		})
		if err2 != nil {
			c.Logger.Errorf("messages.editMessage - error: %v", err2)
			return nil, err2
		}
		editMessages = &message.Vector_MessageBox{}
		for _, m := range messages.GetDatas() {
			editMessages.Datas = append(editMessages.Datas, &mtproto.MessageBox{
				UserId:       c.MD.UserId,
				MessageId:    m.GetId(),
				SenderUserId: m.GetFromId().GetUserId(),
				PeerType:     mtproto.PEER_CHANNEL,
				PeerId:       peer.PeerId,
				Message:      m,
			})
		}
	default:
		c.Logger.Errorf("invalid peer: %v", in.Peer)
		err = mtproto.ErrPeerIdInvalid
		return nil, err
	}

	if (scheduled || peer.PeerType == mtproto.PEER_CHANNEL) && len(editMessages.GetDatas()) == 0 {
		// Deleted, or hidden from the account (available_min_id): as msg
		// answers for a message gone in the meantime.
		return nil, mtproto.ErrMessageIdInvalid
	}
	if len(editMessages.GetDatas()) != 1 {
		err = mtproto.ErrMessageEmpty
		c.Logger.Errorf("messages.editMessage - emptyMessage(%d)", in.Id)
		return nil, err
	}

	// TODO(@benqi): check
	// editMessage := editMessages[0]
	if peer.PeerType != mtproto.PEER_CHANNEL && editMessages.Datas[0].SenderUserId != c.MD.UserId {
		err = mtproto.ErrMessageAuthorRequired
		c.Logger.Errorf("messages.editMessage - emptyMessage(%d)", in.Id)
		return nil, err
	}
	// ...
	dstMessage = proto.Clone(editMessages.Datas[0]).(*mtproto.MessageBox)

	outMessage := editMessages.Datas[0].Message
	if err = c.applyEdit(outMessage, in); err != nil {
		return nil, err
	}

	rUpdates, err := c.svcCtx.Dao.MsgClient.MsgEditMessageV2(c.ctx, &msgpb.TLMsgEditMessageV2{
		UserId:    c.MD.UserId,
		AuthKeyId: c.MD.PermAuthKeyId,
		PeerType:  peer.PeerType,
		PeerId:    peer.PeerId,
		EditType:  msgpb.EditTypeNormal,
		NewMessage: msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage:    in.NoWebpage,
			Background:   false,
			RandomId:     0,
			Message:      withRepeatPeriod(outMessage, in.ScheduleDate, in.GetScheduleRepeatPeriod()),
			ScheduleDate: in.ScheduleDate,
		}).To_OutboxMessage(),
		DstMessage: dstMessage,
	})
	if err != nil {
		c.Logger.Errorf("messages.editMessage - error: %v", err)
		return nil, err
	}
	if in.Message != nil {
		// A code in the new text (task 098).
		c.invalidateSentLoginCodes(in.Message.GetValue())
	}

	return rUpdates, nil
}

// applyEdit applies in to outMessage, a message the account may edit: the
// edit date, the bot's keyboard, the media, the text and the entities.
func (c *MessagesCore) applyEdit(outMessage *mtproto.Message, in *mtproto.TLMessagesEditMessage) error {
	var err error
	outMessage.EditDate = &wrapperspb.Int32Value{Value: int32(time.Now().Unix())}
	outMessage.EditHide = false

	// reply_markup. A message with a game keeps the button that starts it
	// first, and the game stays its media: the button would open nothing
	// else (botgames.Markup, task 106).
	game := outMessage.GetMedia().GetPredicateName() == mtproto.Predicate_messageMediaGame
	if in.ReplyMarkup != nil {
		if outMessage.ReplyMarkup, err = c.botMarkup(in.ReplyMarkup, game); err != nil {
			c.Logger.Errorf("messages.editMessage - reply markup: %v", err)
			return err
		}
		if game {
			if outMessage.ReplyMarkup, err = botgames.Markup(outMessage.ReplyMarkup, outMessage.GetMedia().GetGame().GetTitle(), c.MD.LangCode); err != nil {
				c.Logger.Errorf("messages.editMessage - reply markup of a game: %v", err)
				return err
			}
		}
	}
	if game && in.Media != nil {
		return mtproto.ErrMediaInvalid
	}

	if in.Media != nil {
		if in.Media.GetPredicateName() == mtproto.Predicate_inputMediaGeoLive &&
			outMessage.GetMedia().GetPredicateName() == mtproto.Predicate_messageMediaGeoLive {
			// A live location moves and stops by edits that send only what
			// changes (editedLiveLocation).
			outMessage.Media = editedLiveLocation(outMessage, in.Media.To_InputMediaGeoLive(), outMessage.EditDate.GetValue())
		} else if outMessage.Media, err = c.makeMediaByInputMedia(in.Media); err != nil {
			c.Logger.Errorf("messages.editMessage - media error: %v", err)
			return err
		}
	}
	// message
	if in.Message != nil {
		if in.Message.Value == "" &&
			(outMessage.Media == nil ||
				outMessage.Media.PredicateName == mtproto.Predicate_messageMediaEmpty) {
			err = mtproto.ErrMessageEmpty
			c.Logger.Errorf("message empty: %v", err)
			return err
		}
		if hasCaption(outMessage) {
			if err = c.checkCaption(in.Message.Value); err != nil {
				c.Logger.Errorf("messages.editMessage: %v", err)
				return err
			}
		}
		outMessage.Message = in.Message.Value
	}

	outMessage.Entities = editedEntities(outMessage.Entities, in)
	return nil
}

// editQuickReplyMessage edits a message of a quick reply shortcut of the
// account (task 105, core.telegram.org/api/business#quick-reply-shortcuts):
// messages.editMessage with quick_reply_shortcut_id, the id from the
// shortcut's own sequence and the peer the account itself, as tdesktop
// (api_editing.cpp:90-92, 98-104, 340-352), Android
// (SendMessagesHelper.java:3525, 3615, 3667) and iOS
// (RequestEditMessage.swift:206-229) ask. The message changes as any other
// edit changes it, invert_media included (tdesktop api_editing.cpp:76-79
// sets it with the edit); the answer and the other devices get
// updateQuickReplyMessage. A shortcut that is not the account's is
// SHORTCUT_INVALID, a message the shortcut does not hold
// MESSAGE_ID_INVALID. A poll is closed and a checklist ticked in a chat,
// not in a shortcut: MEDIA_INVALID.
func (c *MessagesCore) editQuickReplyMessage(peer *mtproto.PeerUtil, in *mtproto.TLMessagesEditMessage) (*mtproto.Updates, error) {
	const method = "messages.editMessage"
	shortcutId := in.GetQuickReplyShortcutId().GetValue()
	var err error
	switch {
	case c.MD.IsBot:
		err = mtproto.ErrBotMethodInvalid
	case !peer.IsSelfUser(c.MD.UserId):
		err = mtproto.ErrPeerIdInvalid
	case in.ScheduleDate != nil:
		// A shortcut has no time to go out at (not verified which error
		// Telegram answers).
		err = mtproto.ErrScheduleDateInvalid
	case in.GetMedia().GetPredicateName() == mtproto.Predicate_inputMediaPoll,
		in.GetMedia().GetPredicateName() == mtproto.Predicate_inputMediaTodo:
		err = mtproto.ErrMediaInvalid
	}
	if err != nil {
		c.Logger.Errorf("%s - quick reply %d: %v", method, shortcutId, err)
		return nil, err
	}
	found, err := c.svcCtx.Dao.MsgClient.MsgGetQuickReplyMessages(c.ctx, &msgpb.TLMsgGetQuickReplyMessages{
		UserId:     c.MD.UserId,
		ShortcutId: shortcutId,
		Id:         []int32{in.Id},
	})
	if err != nil {
		c.Logger.Errorf("%s - quick reply %d: %v", method, shortcutId, err)
		return nil, err
	}
	if len(found.GetMessages()) != 1 {
		return nil, mtproto.ErrMessageIdInvalid
	}
	outMessage := found.GetMessages()[0]
	if err = c.applyEdit(outMessage, in); err != nil {
		return nil, err
	}
	outMessage.InvertMedia = in.InvertMedia
	r, err := c.svcCtx.Dao.MsgClient.MsgEditQuickReplyMessage(c.ctx, &msgpb.TLMsgEditQuickReplyMessage{
		UserId:     c.MD.UserId,
		AuthKeyId:  c.MD.PermAuthKeyId,
		ShortcutId: shortcutId,
		Message: msgpb.MakeTLOutboxMessage(&msgpb.OutboxMessage{
			NoWebpage: in.NoWebpage,
			Message:   outMessage,
		}).To_OutboxMessage(),
	})
	if err != nil {
		c.Logger.Errorf("%s - quick reply %d: %v", method, shortcutId, err)
		return nil, err
	}
	if in.Message != nil {
		// A code in the new text (task 098).
		c.invalidateSentLoginCodes(in.Message.GetValue())
	}
	return r, nil
}

// scheduledMessageToEdit reads message id from the queue of the dialog;
// who may edit it is msg's to check, as it is for a channel's.
func (c *MessagesCore) scheduledMessageToEdit(peer *mtproto.PeerUtil, id int32) (*message.Vector_MessageBox, error) {
	found, err := c.svcCtx.Dao.MsgClient.MsgGetScheduledMessages(c.ctx, &msgpb.TLMsgGetScheduledMessages{
		UserId:   c.MD.UserId,
		PeerType: peer.PeerType,
		PeerId:   peer.PeerId,
		Id:       []int32{id},
	})
	if err != nil {
		return nil, err
	}
	boxes := &message.Vector_MessageBox{}
	for _, m := range found.GetMessages() {
		boxes.Datas = append(boxes.Datas, &mtproto.MessageBox{
			UserId:       c.MD.UserId,
			MessageId:    m.GetId(),
			SenderUserId: c.MD.UserId,
			PeerType:     peer.PeerType,
			PeerId:       peer.PeerId,
			Message:      m,
		})
	}
	return boxes, nil
}

// editedEntities is what the entities of an edited message start from;
// msg then adds the automatic ones, links, @usernames, #hashtags and bot
// commands (plugin.RemakeMessage), as for a new message.
//   - A new text comes with the client's own entities for it, as Android
//     sends them (DrKLO/Telegram aac1efcb messenger/SendMessagesHelper.java:
//     3371-3386). No entities mean plain text. Dropping them lost every
//     bold, italic, spoiler and text link of an edited message.
//   - Entities alone restyle the text the message has.
//   - An edit that keeps the text, such as a new photo, keeps its entities
//     without the automatic ones, which msg makes again.
func editedEntities(old []*mtproto.MessageEntity, in *mtproto.TLMessagesEditMessage) []*mtproto.MessageEntity {
	if in.Message != nil || in.Entities != nil {
		return in.Entities
	}
	kept := make([]*mtproto.MessageEntity, 0, len(old))
	for _, e := range old {
		switch e.GetPredicateName() {
		case mtproto.Predicate_messageEntityUrl, mtproto.Predicate_messageEntityMention,
			mtproto.Predicate_messageEntityHashtag, mtproto.Predicate_messageEntityBotCommand:
			continue
		}
		kept = append(kept, e)
	}
	return kept
}

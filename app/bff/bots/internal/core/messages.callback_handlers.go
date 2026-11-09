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
	"bytes"
	"encoding/binary"
	"hash/fnv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
)

// maxCallbackText: core.telegram.org/bots/api#answercallbackquery, text.
const maxCallbackText = 200

// pressed is the message a button belongs to and how the bot knows it.
type pressed struct {
	message *mtproto.Message
	peer    *mtproto.PeerUtil
	botId   int64
	inline  bool  // sent via the bot: the update names it by inline message id
	botMsg  int32 // the id of the message in the bot's box
}

// pressedMessage finds the message of the press and its bot. It is the
// viewer's copy; a bot's message is the bot's own in a private chat (its peer)
// and a group (its from_id), or the post's author in a channel. The clients
// name the chat as they see it, the bot in a private chat (Android
// SendMessagesHelper.sendCallback, tdesktop api_bot.cpp history->peer->input()),
// and only the dialog of the copy says so: its peer_id is the chat as its
// sender addressed it, the viewer on an incoming copy.
func (c *BotsCore) pressedMessage(peer *mtproto.PeerUtil, msgId int32) (*pressed, error) {
	p := &pressed{peer: peer}
	switch {
	case peer.IsUser(), peer.IsChat():
		box, err := c.svcCtx.MessageGetUserMessage(c.ctx, &message.TLMessageGetUserMessage{UserId: c.MD.UserId, Id: msgId})
		if err != nil {
			return nil, err
		}
		if (mtproto.DialogID{A: box.GetDialogId1(), B: box.GetDialogId2()}) != mtproto.MakeDialogId(c.MD.UserId, peer.PeerType, peer.PeerId) {
			return nil, mtproto.ErrMessageIdInvalid
		}
		p.message = box.GetMessage()
		if via := p.message.GetViaBotId(); via != nil {
			p.botId, p.inline = via.GetValue(), true
		} else if peer.IsUser() {
			p.botId = peer.PeerId
		} else {
			p.botId = p.message.GetFromId().GetUserId()
		}
	case peer.IsChannel():
		messages, err := c.svcCtx.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: peer.PeerId, Id: []int32{msgId}})
		if err != nil {
			return nil, err
		}
		if len(messages.GetDatas()) != 1 || messages.GetDatas()[0].GetPredicateName() != mtproto.Predicate_message {
			return nil, mtproto.ErrMessageIdInvalid
		}
		p.message, p.botMsg = messages.GetDatas()[0], msgId
		if via := p.message.GetViaBotId(); via != nil {
			p.botId, p.inline = via.GetValue(), true
		} else if p.botId, err = c.svcCtx.ChannelPostAuthor(c.ctx, peer.PeerId, msgId); err != nil {
			c.Logger.Errorf("messages.getBotCallbackAnswer - error: %v", err)
			return nil, mtproto.ErrInternalServerError
		}
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
	return p, nil
}

// hasCallback tells whether the inline keyboard of m has the button.
func hasCallback(m *mtproto.Message, data []byte) bool {
	if m.GetReplyMarkup().GetPredicateName() != mtproto.Predicate_replyInlineMarkup {
		return false
	}
	for _, row := range m.GetReplyMarkup().GetRows() {
		for _, b := range row.GetButtons() {
			if b.GetPredicateName() == mtproto.Predicate_keyboardButtonCallback && bytes.Equal(b.GetData(), data) {
				return true
			}
		}
	}
	return false
}

// chatInstance is the id of the chat of a press, the same for all its
// presses (core.telegram.org/constructor/updateBotCallbackQuery): a hash of
// the chat, so the bot learns no more than the peer it already has.
func chatInstance(botId int64, p *pressed, userId int64) int64 {
	id := p.peer.PeerId
	if p.peer.IsUser() {
		// One value for a private chat, whoever presses in it.
		id = userId
	}
	h := fnv.New64a()
	var b [8 + 1 + 8]byte
	binary.BigEndian.PutUint64(b[:8], uint64(botId))
	b[8] = byte(p.peer.PeerType)
	binary.BigEndian.PutUint64(b[9:], uint64(id))
	_, _ = h.Write(b[:])
	return int64(h.Sum64() >> 1)
}

// MessagesGetBotCallbackAnswer
// messages.getBotCallbackAnswer#9342ca07 flags:# game:flags.1?true peer:InputPeer msg_id:int data:flags.0?bytes password:flags.2?InputCheckPasswordSRP = messages.BotCallbackAnswer;
func (c *BotsCore) MessagesGetBotCallbackAnswer(in *mtproto.TLMessagesGetBotCallbackAnswer) (*mtproto.Messages_BotCallbackAnswer, error) {
	if in.Game {
		// Games are not served.
		return nil, mtproto.ErrDataInvalid
	}
	peer := c.chatPeer(in.Peer)
	p, err := c.pressedMessage(peer, in.MsgId)
	if err != nil {
		c.Logger.Errorf("messages.getBotCallbackAnswer - error: %v", err)
		return nil, err
	}
	data := in.Data
	if !hasCallback(p.message, data) {
		return nil, mtproto.ErrDataInvalid
	}
	if _, err = c.botUser(p.botId); err != nil {
		return nil, mtproto.ErrDataInvalid
	}

	key := dao.CallbackCacheKey(c.MD.UserId, peer.PeerType, peer.PeerId, in.MsgId, data)
	if cached, err := c.svcCtx.CachedCallback(c.ctx, key); err != nil {
		c.Logger.Errorf("messages.getBotCallbackAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	} else if cached != nil {
		return callbackResult(cached), nil
	}

	queryId, err := dao.NewQueryId()
	if err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	if err = c.svcCtx.BeginCallback(c.ctx, queryId, p.botId); err != nil {
		c.Logger.Errorf("messages.getBotCallbackAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	u, chats, err := c.callbackUpdate(queryId, p, in.MsgId, data)
	if err != nil {
		return nil, err
	}
	users, err := c.usersFor(p.botId, c.MD.UserId)
	if err != nil {
		return nil, err
	}
	if err = c.svcCtx.PushToBot(c.ctx, p.botId, u, users, chats); err != nil {
		c.Logger.Errorf("messages.getBotCallbackAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}

	answer, err := c.svcCtx.WaitCallbackAnswer(c.ctx, queryId)
	if err != nil {
		c.Logger.Errorf("messages.getBotCallbackAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if answer == nil {
		return nil, mtproto.ErrBotResponseTimeout
	}
	if err = c.svcCtx.CacheCallback(c.ctx, key, answer); err != nil {
		// The cache spares the bot a repeat of the press; the answer stands.
		c.Logger.Errorf("messages.getBotCallbackAnswer - cache: %v", err)
	}
	return callbackResult(answer), nil
}

// callbackUpdate makes the update of the press for the bot: of its message
// by the bot's own id in private chats and groups, by inline message id for
// a message sent via the bot (core.telegram.org/api/bots/buttons).
func (c *BotsCore) callbackUpdate(queryId int64, p *pressed, msgId int32, data []byte) (*mtproto.Update, []*mtproto.Chat, error) {
	instance := chatInstance(p.botId, p, c.MD.UserId)
	if p.inline {
		return mtproto.MakeTLUpdateInlineBotCallbackQuery(&mtproto.Update{
			QueryId_INT64: queryId,
			UserId:        c.MD.UserId,
			MsgId_FLAGINPUTBOTINLINEMESSAGEID: mtproto.MakeTLInputBotInlineMessageID64(&mtproto.InputBotInlineMessageID{
				OwnerId:  c.MD.UserId,
				Id_INT32: msgId,
			}).To_InputBotInlineMessageID(),
			ChatInstance:   instance,
			Data_FLAGBYTES: data,
		}).To_Update(), nil, nil
	}

	var (
		peer  *mtproto.Peer
		chats []*mtproto.Chat
	)
	switch {
	case p.peer.IsChannel():
		peer = mtproto.MakePeerChannel(p.peer.PeerId)
		mc, err := c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: p.peer.PeerId, Id: []int64{p.botId}})
		if err != nil {
			return nil, nil, err
		}
		chats = []*mtproto.Chat{mc.ToChat(p.botId, int32(time.Now().Unix()))}
	case p.peer.IsChat():
		peer = mtproto.MakePeerChat(p.peer.PeerId)
		chat, err := c.svcCtx.Chat.ChatGetChatBySelfId(c.ctx, &chatpb.TLChatGetChatBySelfId{SelfId: p.botId, ChatId: p.peer.PeerId})
		if err != nil {
			return nil, nil, err
		}
		chats = []*mtproto.Chat{chat.ToUnsafeChat(p.botId)}
	default:
		peer = mtproto.MakePeerUser(c.MD.UserId)
	}
	if p.botMsg == 0 {
		box, err := c.svcCtx.MessageGetPeerUserMessage(c.ctx, &message.TLMessageGetPeerUserMessage{UserId: c.MD.UserId, PeerUserId: p.botId, MsgId: msgId})
		if err != nil {
			return nil, nil, err
		}
		p.botMsg = box.GetMessageId()
	}
	return mtproto.MakeTLUpdateBotCallbackQuery(&mtproto.Update{
		QueryId_INT64:  queryId,
		UserId:         c.MD.UserId,
		Peer_PEER:      peer,
		MsgId_INT32:    p.botMsg,
		ChatInstance:   instance,
		Data_FLAGBYTES: data,
	}).To_Update(), chats, nil
}

func callbackResult(a *dao.CallbackAnswer) *mtproto.Messages_BotCallbackAnswer {
	r := &mtproto.Messages_BotCallbackAnswer{Alert: a.Alert, CacheTime: a.CacheTime}
	if a.Message != "" {
		r.Message = mtproto.MakeFlagsString(a.Message)
	}
	if a.Url != "" {
		r.HasUrl = true
		r.Url = mtproto.MakeFlagsString(a.Url)
	}
	return mtproto.MakeTLMessagesBotCallbackAnswer(r).To_Messages_BotCallbackAnswer()
}

// MessagesSetBotCallbackAnswer
// messages.setBotCallbackAnswer#d58f130a flags:# alert:flags.1?true query_id:long message:flags.0?string url:flags.2?string cache_time:int = Bool;
func (c *BotsCore) MessagesSetBotCallbackAnswer(in *mtproto.TLMessagesSetBotCallbackAnswer) (*mtproto.Bool, error) {
	if _, err := c.meBot(); err != nil {
		return nil, err
	}
	a := &dao.CallbackAnswer{Alert: in.Alert, CacheTime: in.CacheTime}
	if in.Message != nil {
		if utf8.RuneCountInString(in.Message.GetValue()) > maxCallbackText {
			return nil, mtproto.ErrMessageTooLong
		}
		a.Message = in.Message.GetValue()
	}
	if in.Url != nil {
		if !strings.Contains(in.Url.GetValue(), ":") {
			return nil, mtproto.ErrUrlInvalid
		}
		a.Url = in.Url.GetValue()
	}
	ok, err := c.svcCtx.SetCallbackAnswer(c.ctx, in.QueryId, c.MD.UserId, a)
	if err != nil {
		c.Logger.Errorf("messages.setBotCallbackAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if !ok {
		return nil, mtproto.ErrQueryIdInvalid
	}
	return mtproto.BoolTrue, nil
}

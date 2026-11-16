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
	"errors"
	"hash/fnv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/business"

	"google.golang.org/protobuf/types/known/wrapperspb"
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
	// business is the message of a connected business bot (task 105), set
	// by businessCallback for a callback press; nil for any other.
	business *businessPress
}

// businessPress is a press in a message a connected business bot sent for
// its account: the connection and the message as the account has it.
type businessPress struct {
	connection *business.Connection
	message    *mtproto.Message
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

// businessCallback makes p, the message of a callback press, the press of
// a connected business bot's message when it is one (businessPressed): its
// bot is then the connected bot. Only a callback goes to the business bot;
// the other uses of a pressed message (the login button of URL auth, whose
// bot is the button's, and widgets, which business bots do not send) keep
// the bot of the chat and skip the look-up.
func (c *BotsCore) businessCallback(p *pressed, msgId int32) error {
	if !p.peer.IsUser() || p.inline {
		return nil
	}
	biz, err := c.businessPressed(p.peer, msgId, p.message)
	if err != nil {
		return err
	}
	if biz != nil {
		p.botId, p.business = biz.connection.BotID, biz
	}
	return nil
}

// businessPressed finds whether the message of a press in the private chat
// with peer, m being the presser's copy of message msgId, is one a
// connected business bot sent for its account (task 105): the account's
// copy has via_business_bot_id, and the bot is still the account's
// connected bot. Its presses go to the bot as
// updateBusinessBotCallbackQuery ("pressing a callback button in a message
// sent by the bot will emit an updateBusinessBotCallbackQuery",
// core.telegram.org/api/bots/connected-business-bots). The other side's
// copy has no via_business_bot_id (msg ReceiveIncoming), so it is read
// from the account's box; the account pressing in its own message has it
// at hand. nil for any other message; DATA_INVALID when that bot is no
// longer the account's connected bot, so the press goes to no bot.
func (c *BotsCore) businessPressed(peer *mtproto.PeerUtil, msgId int32, m *mtproto.Message) (*businessPress, error) {
	owner, ownerMsg := c.MD.UserId, m
	if !m.GetOut() {
		box, err := c.svcCtx.MessageGetPeerUserMessage(c.ctx, &message.TLMessageGetPeerUserMessage{UserId: c.MD.UserId, PeerUserId: peer.PeerId, MsgId: msgId})
		if errors.Is(err, mtproto.ErrMsgIdInvalid) {
			return nil, nil
		}
		if err != nil {
			c.Logger.Errorf("messages.getBotCallbackAnswer - error: %v", err)
			return nil, mtproto.ErrInternalServerError
		}
		owner, ownerMsg = peer.PeerId, box.GetMessage()
	}
	via := ownerMsg.GetViaBusinessBotId()
	if via == nil || !ownerMsg.GetOut() {
		return nil, nil
	}
	conn, err := c.svcCtx.Business.Connection(c.ctx, owner)
	if err != nil {
		c.Logger.Errorf("messages.getBotCallbackAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	if conn == nil || conn.BotID != via.GetValue() {
		return nil, mtproto.ErrDataInvalid
	}
	return &businessPress{connection: conn, message: ownerMsg}, nil
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
	h := fnv.New64a()
	if p.peer.IsUser() {
		// One value for a private chat, whoever presses in it: the chat with
		// the bot by the user, any other (Saved Messages too) by the pair of
		// its users, the same from either side.
		id = userId
		if other := p.peer.PeerId; other != botId {
			lo, hi := min(userId, other), max(userId, other)
			var b [8 + 1 + 8 + 8]byte
			binary.BigEndian.PutUint64(b[:8], uint64(botId))
			b[8] = byte(p.peer.PeerType)
			binary.BigEndian.PutUint64(b[9:], uint64(lo))
			binary.BigEndian.PutUint64(b[17:], uint64(hi))
			_, _ = h.Write(b[:])
			return int64(h.Sum64() >> 1)
		}
	}
	var b [8 + 1 + 8]byte
	binary.BigEndian.PutUint64(b[:8], uint64(botId))
	b[8] = byte(p.peer.PeerType)
	binary.BigEndian.PutUint64(b[9:], uint64(id))
	_, _ = h.Write(b[:])
	return int64(h.Sum64() >> 1)
}

// MessagesGetBotCallbackAnswer
// messages.getBotCallbackAnswer#9342ca07 flags:# game:flags.1?true peer:InputPeer msg_id:int data:flags.0?bytes password:flags.2?InputCheckPasswordSRP = messages.BotCallbackAnswer;
//
// With game the press is of the game's button (keyboardButtonGame: tdesktop
// api_bot.cpp:76-82, iOS RequestMessageActionCallback.swift:42-43), which
// carries no data: the bot gets the short name of the game in
// game_short_name and answers with the address of the game, which the
// clients open as the game (core.telegram.org/api/bots/games: "a callback
// query that returns the game URL").
func (c *BotsCore) MessagesGetBotCallbackAnswer(in *mtproto.TLMessagesGetBotCallbackAnswer) (*mtproto.Messages_BotCallbackAnswer, error) {
	peer := c.chatPeer(in.Peer)
	p, err := c.pressedMessage(peer, in.MsgId)
	if err != nil {
		c.Logger.Errorf("messages.getBotCallbackAnswer - error: %v", err)
		return nil, err
	}
	if err = c.businessCallback(p, in.MsgId); err != nil {
		return nil, err
	}
	data := in.Data
	gameShortName := ""
	if in.Game {
		// The press of a game has no data: an empty key of the cache, which
		// no button with data has (botmarkup: 1 to 64 bytes).
		data = nil
		// updateBusinessBotCallbackQuery has no game_short_name: a
		// connected bot sends no game for its account.
		if p.business != nil {
			return nil, mtproto.ErrDataInvalid
		}
		if gameShortName, err = c.pressedGame(p); err != nil {
			return nil, err
		}
	} else if !hasCallback(p.message, data) {
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

	if !c.svcCtx.EnterWait(c.MD.UserId) {
		return nil, mtproto.ErrBotResponseTimeout
	}
	defer c.svcCtx.LeaveWait(c.MD.UserId)
	queryId, err := dao.NewQueryId()
	if err != nil {
		return nil, mtproto.ErrInternalServerError
	}
	if err = c.svcCtx.BeginCallback(c.ctx, queryId, p.botId); err != nil {
		c.Logger.Errorf("messages.getBotCallbackAnswer - error: %v", err)
		return nil, mtproto.ErrInternalServerError
	}
	u, chats, err := c.callbackUpdate(queryId, p, in.MsgId, data, gameShortName)
	if err != nil {
		return nil, err
	}
	userIds := []int64{c.MD.UserId}
	if p.business != nil {
		// The account and those its message names, as the bot sees them.
		h := mtproto.NewIDListHelper(c.MD.UserId, p.business.connection.UserID)
		h.PickByMessage(p.business.message)
		userIds = h.UserIdList
	}
	users, err := c.usersFor(p.botId, userIds...)
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
// a message sent via the bot (core.telegram.org/api/bots/buttons). A press
// of a game's button has the game's short name instead of data.
func (c *BotsCore) callbackUpdate(queryId int64, p *pressed, msgId int32, data []byte, gameShortName string) (*mtproto.Update, []*mtproto.Chat, error) {
	instance := chatInstance(p.botId, p, c.MD.UserId)
	var game *wrapperspb.StringValue
	if gameShortName != "" {
		game = wrapperspb.String(gameShortName)
	}
	if p.business != nil {
		// The message as the account has it, as the bot gets every message
		// of the connection (updateBotNewBusinessMessage); reply_to_message
		// is left out (flags.2).
		return mtproto.MakeTLUpdateBusinessBotCallbackQuery(&mtproto.Update{
			QueryId_INT64:      queryId,
			UserId:             c.MD.UserId,
			ConnectionId:       p.business.connection.ConnectionID,
			Message_MESSAGE:    p.business.message,
			ChatInstance_INT64: instance,
			Data_FLAGBYTES:     data,
		}).To_Update(), nil, nil
	}
	if p.inline {
		return mtproto.MakeTLUpdateInlineBotCallbackQuery(&mtproto.Update{
			QueryId_INT64:                     queryId,
			UserId:                            c.MD.UserId,
			MsgId_FLAGINPUTBOTINLINEMESSAGEID: inlineMessageId(p.peer, c.MD.UserId, msgId),
			ChatInstance_INT64:                instance,
			Data_FLAGBYTES:                    data,
			GameShortName:                     game,
		}).To_Update(), nil, nil
	}

	peer, chats, err := c.botView(p, msgId)
	if err != nil {
		return nil, nil, err
	}
	return mtproto.MakeTLUpdateBotCallbackQuery(&mtproto.Update{
		QueryId_INT64:      queryId,
		UserId:             c.MD.UserId,
		Peer_PEER:          peer,
		MsgId_INT32:        p.botMsg,
		ChatInstance_INT64: instance,
		Data_FLAGBYTES:     data,
		GameShortName:      game,
	}).To_Update(), chats, nil
}

// botView is the message of a press as its bot names it, in an update of
// a press or a request: the chat (the user for a private chat) with the
// chats the update carries, and p.botMsg set to the id of the message in
// the bot's box.
func (c *BotsCore) botView(p *pressed, msgId int32) (*mtproto.Peer, []*mtproto.Chat, error) {
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
	return peer, chats, nil
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

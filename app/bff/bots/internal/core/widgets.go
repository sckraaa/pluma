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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/botmarkup"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/botwidgets"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The widgets of bot messages (RPCBotWidgets, task 106, ADR 0068 p. 8).
// Telegram documents none of it; what the server does is what the
// official clients' widget framework says Telegram's server does
// (Android TMessagesProj/jni/twidget/widget-sandbox/docs/tgw-format.md,
// "The widget's state and requests"), the TL of the server's layer and
// what the clients call (tdesktop tgw/tgw_bridge.cpp, Android
// utils/twidget/WidgetMedia.java):
//   - the media is made by pkg/botwidgets, for messages.sendMedia (bff
//     messages) and for inline results (inlineWidgetResult here);
//   - a widget asks its bot with messages.getBotWidgetQueryAnswer, which
//     waits for the bot's messages.setBotWidgetQueryAnswer as a press of a
//     button waits for its answer (messages.callback_handlers.go);
//   - the bot writes the state: of one message (editMessageWidgetData,
//     editInlineBotMessageWidgetData), which reaches every copy as a
//     silent edit (msg: updateMessageWidgetData in private chats and basic
//     groups, updateEditChannelMessage in supergroups), and of a shared
//     widget (editWidgetData), which clients poll with
//     messages.getWidgetPublicData.

// widgetChatType is chat_type of a widget's request: the kind of the chat
// of the message, by the names the clients give it to the widget (tdesktop
// tgw/tgw_media.cpp ChatTypeOf: private, group, supergroup, channel).
// Which names Telegram sends the bot is not published (not verified).
func widgetChatType(peer *mtproto.PeerUtil, chats []*mtproto.Chat) string {
	switch {
	case peer.IsUser():
		return "private"
	case peer.IsChat():
		return "group"
	}
	for _, ch := range chats {
		if ch.GetId() == peer.PeerId && ch.GetBroadcast() {
			return "channel"
		}
	}
	return "supergroup"
}

// widgetUpdate makes the update of a widget's request for its bot: of its
// message by the bot's own id in private chats and groups
// (updateBotWidgetQuery), by inline message id for a message sent via the
// bot (updateInlineBotWidgetQuery), as the press of a button
// (callbackUpdate). widget_id names a shared widget.
func (c *BotsCore) widgetUpdate(queryId int64, p *pressed, msgId int32, queryType *mtproto.WidgetQueryType, data []byte) (*mtproto.Update, []*mtproto.Chat, error) {
	u := &mtproto.Update{
		QueryId_INT64:      queryId,
		QueryType:          queryType,
		UserId:             c.MD.UserId,
		ChatInstance_INT64: chatInstance(p.botId, p, c.MD.UserId),
		Data:               data,
		WidgetId:           p.message.GetMedia().GetWidgetId(),
	}
	if p.inline {
		var chats []*mtproto.Chat
		if p.peer.IsChannel() {
			// The kind of a channel is in its chat, which the update of a
			// message sent via the bot does not carry. The bot need not be
			// a member of it, so the channel is read as the user sees it.
			mc, err := c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: p.peer.PeerId, Id: []int64{c.MD.UserId}})
			if err != nil {
				return nil, nil, err
			}
			chats = []*mtproto.Chat{mc.ToChat(c.MD.UserId, int32(time.Now().Unix()))}
		}
		u.ChatType = widgetChatType(p.peer, chats)
		u.InlineMessageId = inlineMessageId(p.peer, c.MD.UserId, msgId)
		return mtproto.MakeTLUpdateInlineBotWidgetQuery(u).To_Update(), nil, nil
	}
	peer, chats, err := c.botView(p, msgId)
	if err != nil {
		return nil, nil, err
	}
	u.ChatType = widgetChatType(p.peer, chats)
	u.Peer_PEER, u.MsgId_INT32 = peer, p.botMsg
	return mtproto.MakeTLUpdateBotWidgetQuery(u).To_Update(), chats, nil
}

// checkWidgetEdit checks the state of a bot's edit: text of at most
// botwidgets.MaxData bytes.
func checkWidgetEdit(publicData, chatData *wrapperspb.StringValue) error {
	for _, s := range []*wrapperspb.StringValue{publicData, chatData} {
		if err := botwidgets.CheckData(s, mtproto.ErrWidgetDataTooLarge, mtproto.ErrWidgetDataInvalid); err != nil {
			return err
		}
	}
	return nil
}

// widgetEdit is stored, a message of the calling bot with a widget, with
// the state of the bot's edit: the media with the fields the edit names
// (botwidgets.WithData), the rest as it was, with its entities but the
// automatic ones, which msg makes again (as inlineEdit). The edit is
// silent: no edit date ("all silent (no "edited" mark, no notification)",
// tgw-format.md). A public state of a shared widget is its object's, so it
// is written there first. MESSAGE_ID_INVALID for a message without a
// widget, MESSAGE_NOT_MODIFIED for an edit that changes nothing ("refused
// with message is not modified, as editMessageText is").
func (c *BotsCore) widgetEdit(stored *mtproto.Message, publicData, chatData *wrapperspb.StringValue) (*mtproto.Message, error) {
	if stored.GetPredicateName() != mtproto.Predicate_message || !botwidgets.Is(stored.GetMedia()) {
		return nil, mtproto.ErrMessageIdInvalid
	}
	if err := checkWidgetEdit(publicData, chatData); err != nil {
		return nil, err
	}
	media := botwidgets.WithData(stored.GetMedia(), publicData, chatData)
	if proto.Equal(media, stored.GetMedia()) {
		return nil, mtproto.ErrMessageNotModified
	}
	if id := media.GetWidgetId(); id != nil && publicData != nil {
		if _, err := c.setWidgetPublicData(id.GetValue(), publicData.GetValue(), nil); err != nil {
			return nil, err
		}
	}
	out := proto.Clone(stored).(*mtproto.Message)
	out.Media = media
	out.Entities = withoutAutoEntities(stored.GetEntities())
	return out, nil
}

// setWidgetPublicData writes the public state of the calling bot's object
// id, with the period of cacheTime when the bot gives one (the only way to
// set it: "0 sets it back to 30 s") and the one it has otherwise.
// WIDGET_ID_INVALID for an object of another bot or none.
func (c *BotsCore) setWidgetPublicData(id int64, data string, cacheTime *wrapperspb.Int32Value) (botstore.Widget, error) {
	w, err := c.svcCtx.Bots.BotWidget(c.ctx, c.MD.UserId, id)
	if errors.Is(err, botstore.ErrNoWidget) {
		return botstore.Widget{}, mtproto.ErrWidgetIdInvalid
	}
	if err != nil {
		c.Logger.Errorf("widget %d - error: %v", id, err)
		return botstore.Widget{}, mtproto.ErrInternalServerError
	}
	period := w.Period
	if cacheTime != nil {
		period = botstore.ClampWidgetPeriod(cacheTime.GetValue())
	}
	set, err := c.svcCtx.Bots.SetWidgetPublicData(c.ctx, c.MD.UserId, id, data, period, int32(time.Now().Unix()))
	switch {
	case errors.Is(err, botstore.ErrNoWidget):
		return botstore.Widget{}, mtproto.ErrWidgetIdInvalid
	case err != nil:
		c.Logger.Errorf("widget %d - error: %v", id, err)
		return botstore.Widget{}, mtproto.ErrInternalServerError
	}
	return set, nil
}

// inlineWidgetResult turns an inline result of the bot whose message is a
// widget (inputBotInlineMessageWidget, inputBotInlineMessageWidgetId) into
// the result clients list: botInlineMessageWidget with the widget checked
// and compressed, its state, and the object's for a result by reference
// ("a result by reference takes the payload, the anchors and the shared
// state from its object and shows its id", tgw-format.md, "Who may send").
// ref keeps for the send what the result does not show. A widget goes in
// an article result (inputBotInlineResult) only.
func (c *BotsCore) inlineWidgetResult(r *mtproto.InputBotInlineResult, w botwidgets.Input) (*mtproto.BotInlineResult, dao.WidgetRef, error) {
	if r.GetPredicateName() != mtproto.Predicate_inputBotInlineResult {
		return nil, dao.WidgetRef{}, mtproto.ErrSendMessageTypeInvalid
	}
	m := r.GetSendMessage()
	markup, err := botmarkup.Normalize(m.GetReplyMarkup(), c.markupEnv())
	if err != nil {
		return nil, dao.WidgetRef{}, err
	}
	send, err := botwidgets.Preview(c.ctx, c.svcCtx.Bots, c.MD.UserId, w)
	if err != nil {
		return nil, dao.WidgetRef{}, c.clientError("messages.setInlineBotResults", err)
	}
	send.InvertMedia, send.Message, send.Entities, send.ReplyMarkup = m.GetInvertMedia(), m.GetMessage(), m.GetEntities(), markup
	return mtproto.MakeTLBotInlineResult(&mtproto.BotInlineResult{
		Id: r.GetId(), Type: r.GetType(), Title: r.GetTitle(), Description: r.GetDescription(), Url: r.GetUrl(),
		Thumb:       webDocument(c.svcCtx.WebFileKey, r.GetThumb()),
		SendMessage: mtproto.MakeTLBotInlineMessageWidget(send).To_BotInlineMessage(),
	}).To_BotInlineResult(), dao.WidgetRef{AllowQueries: w.AllowQueries, Shared: w.Shared, WidgetId: w.WidgetId}, nil
}

// inlineWidgetMedia is the media of a widget result a user sends via the
// bot: a shared widget becomes an object now, "a new object on every
// send", and one by reference shows its object's state as it is now.
func (c *BotsCore) inlineWidgetMedia(botId int64, send *mtproto.BotInlineMessage, ref *dao.WidgetRef) (*mtproto.MessageMedia, error) {
	media, err := botwidgets.Media(c.ctx, c.svcCtx.Bots, botId, botwidgets.FromPreview(send, ref.AllowQueries, ref.Shared, ref.WidgetId), botwidgets.ViaBot, int32(time.Now().Unix()))
	if errors.Is(err, mtproto.ErrWidgetIdInvalid) {
		// The bot's object is gone since the answer: as a result gone.
		return nil, mtproto.ErrInlineResultExpired
	}
	if err != nil {
		return nil, c.clientError("messages.sendInlineBotResult", err)
	}
	return media, nil
}

// clientError is err when it is an answer for the client (an error of
// mtproto); any other failure is logged and INTERNAL_SERVER_ERROR.
func (c *BotsCore) clientError(method string, err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	c.Logger.Errorf("%s - error: %v", method, err)
	return mtproto.ErrInternalServerError
}

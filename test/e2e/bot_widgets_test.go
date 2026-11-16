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

package e2e

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

const widgetHTML = "<!doctype html><div id=n>0</div><script>TelegramWidget.ready()</script>"

// TestWidgets: a bot's widgets in messages (task 106, ADR 0068 p. 8). The
// bot sends a shared widget that may ask it; the user's client reads its
// public state by the message and gets "not modified" for the same hash;
// the bot's editWidgetData changes the hash and the period; the bot's edit
// of the message's chat state reaches the user as updateMessageWidgetData;
// the widget's request reaches the bot, whose answer comes back; the
// wallet's query, a widget that may not ask and a widget from a user are
// refused.
func TestWidgets(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot := botLoggedIn(ctx, t, phone, username)
	getState(ctx, t, bot)

	toBot := resolveBot(ctx, t, alice, username)
	toAlice := &mtproto.InputPeer{Constructor: bot.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: alice.self.GetId(), AccessHash: alice.self.GetAccessHash().GetValue()}
	sendWidget := func(c *user, peer *mtproto.InputPeer, media *mtproto.InputMedia) (mtproto.TLObject, error) {
		return c.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
			Constructor: c.c.Constructor(mtproto.Predicate_messages_sendMedia),
			Peer:        peer, Media: media, Message: "Counter", RandomId: randomID(),
		})
	}
	widget := func(c *user, allow, shared bool) *mtproto.InputMedia {
		return &mtproto.InputMedia{
			Constructor:  c.c.Constructor(mtproto.Predicate_inputMediaWidget),
			AllowQueries: allow, SharedWidget: shared,
			Width: 320, Height: 0, Html: widgetHTML,
			PublicData: wrapperspb.String(`{"n":1}`), ChatData: wrapperspb.String("turn 1"),
		}
	}

	// A user sends no widgets.
	if _, err := sendWidget(alice, toBot, widget(alice, true, false)); !strings.Contains(fmt.Sprint(err), "INPUT_MEDIA_WIDGET_NOT_ALLOWED") {
		t.Fatalf("a widget from a user: %v", err)
	}

	o, err := sendWidget(bot, toAlice, widget(bot, true, true))
	if err != nil {
		t.Fatalf("messages.sendMedia of a widget: %v", err)
	}
	sent := sentMessage(t, o)
	botMsgId, widgetId := sent.GetId(), sent.GetMedia().GetWidgetId().GetValue()
	if widgetId == 0 {
		t.Fatalf("a shared widget without its id: %v", sent)
	}
	var msgId int32
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		w := m.GetMedia()
		if w.GetPredicateName() != mtproto.Predicate_messageMediaWidget {
			return false
		}
		r, err := zlib.NewReader(bytes.NewReader(w.GetPayload()))
		if err != nil {
			t.Fatalf("the payload is not zlib: %v", err)
		}
		html, err := io.ReadAll(r)
		if err != nil || string(html) != widgetHTML || !w.GetCanQuery() || !w.GetBackgroundQuery() || w.GetIsCopy() ||
			w.GetWidth() != 320 || w.GetWidgetId().GetValue() != widgetId || w.GetChatData().GetValue() != "turn 1" ||
			w.GetPublicData().GetValue() != `{"n":1}` || m.GetMessage() != "Counter" {
			t.Fatalf("the widget message: %v (%v)", m, err)
		}
		msgId = m.GetId()
		return true
	})

	// The public state of the shared widget, by the message.
	publicData := func(hash int64) *mtproto.Messages_WidgetPublicData {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetWidgetPublicData{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getWidgetPublicData),
			Peer:        toBot, MsgId: msgId, Hash: hash,
		})
		if err != nil {
			t.Fatalf("messages.getWidgetPublicData: %v", err)
		}
		return o.(interface {
			To_Messages_WidgetPublicData() *mtproto.Messages_WidgetPublicData
		}).To_Messages_WidgetPublicData()
	}
	first := publicData(0)
	if first.GetPredicateName() != mtproto.Predicate_messages_widgetPublicData || first.GetData() != `{"n":1}` || first.GetPeriod() != 30 {
		t.Fatalf("the public state: %v", first)
	}
	if same := publicData(first.GetHash()); same.GetPredicateName() != mtproto.Predicate_messages_widgetPublicDataNotModified || same.GetPeriod() != 30 {
		t.Fatalf("the same hash: %v", same)
	}
	editWidget := func(data string) error {
		_, err := bot.c.Invoke(ctx, &mtproto.TLMessagesEditWidgetData{
			Constructor: bot.c.Constructor(mtproto.Predicate_messages_editWidgetData),
			WidgetId:    widgetId, PublicData: data, CacheTime: wrapperspb.Int32(60),
		})
		return err
	}
	if err = editWidget(`{"n":2}`); err != nil {
		t.Fatalf("messages.editWidgetData: %v", err)
	}
	if err = editWidget(`{"n":3}`); !strings.Contains(fmt.Sprint(err), "FLOOD_WAIT_") {
		t.Fatalf("a second edit in the second: %v", err)
	}
	if next := publicData(first.GetHash()); next.GetData() != `{"n":2}` || next.GetHash() == first.GetHash() || next.GetPeriod() != 60 {
		t.Fatalf("the edited public state: %v", next)
	}

	// The bot's edit of the chat state of its message.
	if _, err = bot.c.Invoke(ctx, &mtproto.TLMessagesEditMessageWidgetData{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_editMessageWidgetData),
		Peer:        toAlice, Id: botMsgId, ChatData: wrapperspb.String("turn 2"),
	}); err != nil {
		t.Fatalf("messages.editMessageWidgetData: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateMessageWidgetData, func(u *mtproto.Update) bool {
		return u.GetMsgId_INT32() == msgId && u.GetPeer_PEER().GetUserId() == bot.self.GetId() &&
			u.GetChatData().GetValue() == "turn 2" && u.GetPublicData() == nil
	})

	// The widget's request and the bot's answer.
	ask := func(wallet bool, id int32) (*mtproto.Messages_BotWidgetQueryAnswer, error) {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetBotWidgetQueryAnswer{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getBotWidgetQueryAnswer),
			Peer:        toBot, MsgId: id, Data: []byte("ping"),
			QueryType: &mtproto.WidgetQueryType{Constructor: alice.c.Constructor(mtproto.Predicate_widgetQueryTypeData), WithWallet: wallet},
		})
		if err != nil {
			return nil, err
		}
		return o.(interface {
			To_Messages_BotWidgetQueryAnswer() *mtproto.Messages_BotWidgetQueryAnswer
		}).To_Messages_BotWidgetQueryAnswer(), nil
	}
	if _, err = ask(true, msgId); !strings.Contains(fmt.Sprint(err), "WIDGET_QUERY_TYPE_UNSUPPORTED") {
		t.Fatalf("the wallet's query: %v", err)
	}
	asked := make(chan error, 1)
	var answer *mtproto.Messages_BotWidgetQueryAnswer
	go func() {
		var err error
		answer, err = ask(false, msgId)
		asked <- err
	}()
	var queryId int64
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateBotWidgetQuery, func(u *mtproto.Update) bool {
		if string(u.GetData()) != "ping" || u.GetMsgId_INT32() != botMsgId || u.GetUserId() != alice.self.GetId() ||
			u.GetWidgetId().GetValue() != widgetId || u.GetChatType() != "private" {
			return false
		}
		queryId = u.GetQueryId_INT64()
		return true
	})
	if _, err = bot.c.Invoke(ctx, &mtproto.TLMessagesSetBotWidgetQueryAnswer{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setBotWidgetQueryAnswer),
		QueryId:     queryId, Result: wrapperspb.String("pong"),
	}); err != nil {
		t.Fatalf("messages.setBotWidgetQueryAnswer: %v", err)
	}
	if err = <-asked; err != nil {
		t.Fatalf("messages.getBotWidgetQueryAnswer: %v", err)
	}
	if answer.GetResult().GetValue() != "pong" {
		t.Fatalf("the answer: %v", answer)
	}

	// A static widget sent without allow_queries asks nothing, and has no
	// object to poll.
	if o, err = sendWidget(bot, toAlice, widget(bot, false, false)); err != nil {
		t.Fatalf("a static widget: %v", err)
	}
	if m := sentMessage(t, o); m.GetMedia().GetWidgetId() != nil || m.GetMedia().GetCanQuery() {
		t.Fatalf("the static widget: %v", m)
	}
	var staticId int32
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		if m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaWidget || m.GetMedia().GetWidgetId() != nil {
			return false
		}
		staticId = m.GetId()
		return true
	})
	if _, err = ask(false, staticId); !strings.Contains(fmt.Sprint(err), "WIDGET_QUERY_NOT_ALLOWED") {
		t.Fatalf("a widget that may not ask: %v", err)
	}
}

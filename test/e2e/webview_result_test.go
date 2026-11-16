//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestWebViewResultMessage: the bot answers the mini app of its menu button
// (Bot API answerWebAppQuery): the message comes into the chat from the
// user, via the bot; the user's other device is told to close the web view
// of the query; the query cannot be answered twice. (task 106, ADR 0068
// p. 4.)
func TestWebViewResultMessage(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	laptop := login(ctx, t, phone, "Alice")
	getState(ctx, t, laptop)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot := botLoggedIn(ctx, t, phone, username)
	getState(ctx, t, bot)
	toBot := resolveBot(ctx, t, alice, username)
	botUser := inputUserOfPeer(alice.c, toBot)

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesRequestWebView{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestWebView),
		FromBotMenu: true, Peer: toBot, Bot: botUser, Url: wrapperspb.String("https://shop.example/app"),
		Platform: "android",
	})
	queryId := webViewResult(t, o, err, "messages.requestWebView").GetQueryId_FLAGINT64().GetValue()
	if queryId == 0 {
		t.Fatal("the menu button's web view has no query_id")
	}

	answer := func() error {
		_, err := bot.c.Invoke(ctx, &mtproto.TLMessagesSendWebViewResultMessage{
			Constructor: bot.c.Constructor(mtproto.Predicate_messages_sendWebViewResultMessage),
			BotQueryId:  fmt.Sprint(queryId),
			Result: &mtproto.InputBotInlineResult{
				Constructor: bot.c.Constructor(mtproto.Predicate_inputBotInlineResult),
				Id:          "1", Type: "article", Title: wrapperspb.String("Order"),
				SendMessage: &mtproto.InputBotInlineMessage{
					Constructor: bot.c.Constructor(mtproto.Predicate_inputBotInlineMessageText),
					Message:     "Order #7 is paid",
				},
			},
		})
		return err
	}
	if err = answer(); err != nil {
		t.Fatalf("messages.sendWebViewResultMessage: %v", err)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateWebViewResultSent, func(u *mtproto.Update) bool {
		return u.GetQueryId_INT64() == queryId
	})
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetMessage() == "Order #7 is paid" && m.GetOut() && m.GetViaBotId().GetValue() == bot.self.GetId()
	})
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetMessage() == "Order #7 is paid" && !m.GetOut() && m.GetPeerId().GetUserId() == alice.self.GetId()
	})
	if err = answer(); !strings.Contains(fmt.Sprint(err), "QUERY_ID_INVALID") {
		t.Errorf("a second answer: %v", err)
	}
	// The answered web view is closed: its prolongation fails.
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesProlongWebView{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_prolongWebView),
		Peer:        toBot, Bot: botUser, QueryId: queryId,
	}); !strings.Contains(fmt.Sprint(err), "QUERY_ID_INVALID") {
		t.Errorf("prolonging the answered web view: %v", err)
	}
}

// TestWebViewData: the mini app of a keyboard button sends data to the bot
// (Telegram.WebApp.sendData): the user's chat has
// messageActionWebViewDataSent with the button's text, the bot's
// messageActionWebViewDataSentMe with the data too. A button the bot did
// not show and data over 4096 bytes are refused. (task 106, ADR 0068 p. 4.)
func TestWebViewData(t *testing.T) {
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
	botUser := inputUserOfPeer(alice.c, toBot)

	toAlice := &mtproto.InputPeer{Constructor: bot.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: alice.self.GetId(), AccessHash: alice.self.GetAccessHash().GetValue()}
	markup := mtproto.MakeTLReplyKeyboardMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{
			Buttons: []*mtproto.KeyboardButton{mtproto.MakeTLKeyboardButtonSimpleWebView(&mtproto.KeyboardButton{Text: "Order", Url: "https://shop.example/order"}).To_KeyboardButton()},
		}).To_KeyboardButtonRow()},
	}).To_ReplyMarkup()
	if _, err := bot.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        toAlice, Message: "Make an order", RandomId: randomID(), ReplyMarkup: markup,
	}); err != nil {
		t.Fatalf("messages.sendMessage from the bot: %v", err)
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == "Make an order"
	})

	send := func(text, data string) (mtproto.TLObject, error) {
		return alice.c.Invoke(ctx, &mtproto.TLMessagesSendWebViewData{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendWebViewData),
			Bot:         botUser, RandomId: randomID(), ButtonText: text, Data: data,
		})
	}
	if _, err := send("Order", strings.Repeat("x", 4097)); !strings.Contains(fmt.Sprint(err), "DATA_TOO_LONG") {
		t.Errorf("4097 bytes: %v", err)
	}
	if _, err := send("Pay", `{"item":7}`); !strings.Contains(fmt.Sprint(err), "BUTTON_TEXT_INVALID") {
		t.Errorf("a button the bot did not show: %v", err)
	}
	o, err := send("Order", `{"item":7}`)
	if err != nil {
		t.Fatalf("messages.sendWebViewData: %v", err)
	}
	var mine *mtproto.MessageAction
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			mine = u.GetMessage_MESSAGE().GetAction()
		}
	}
	if mine.GetPredicateName() != mtproto.Predicate_messageActionWebViewDataSent || mine.GetText() != "Order" {
		t.Errorf("the user's message: %v", mine)
	}
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		a := u.GetMessage_MESSAGE().GetAction()
		return a.GetPredicateName() == mtproto.Predicate_messageActionWebViewDataSentMe && a.GetText() == "Order" && a.GetData() == `{"item":7}`
	})
}

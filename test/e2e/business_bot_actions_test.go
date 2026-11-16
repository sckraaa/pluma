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

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// viaConnection calls req for the account of the business connection
// connectionId, as a connected bot does (invokeWithBusinessConnection).
func viaConnection(ctx context.Context, bot *user, connectionId string, req mtproto.TLObject) (mtproto.TLObject, error) {
	x := mtproto.NewEncodeBuf(512)
	if err := req.Encode(x, bot.c.Layer()); err != nil {
		return nil, err
	}
	return bot.c.Invoke(ctx, &mtproto.TLInvokeWithBusinessConnection{
		Constructor:  bot.c.Constructor(mtproto.Predicate_invokeWithBusinessConnection),
		ConnectionId: connectionId,
		Query:        x.GetBuf(),
	})
}

// What a connected bot of Telegram Business does for its account (task
// 105, part 8): it answers the other side through its connection, which
// gets the message from the account; the account's devices see the
// message with via_business_bot_id; the bot gets the message as a business
// update, not the account's pts. It edits and deletes what it sent, types
// and reads as the account, and a press of its button reaches it as
// updateBusinessBotCallbackQuery. A method outside the list, a chat it is
// paused in, a right it lacks and a connection of someone else are refused.
func TestBusinessBotActions(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot, _ := botLoggedInWithToken(ctx, t, phones[0], username, mtclient.DefaultLayer)
	if out := admin(t, "bot", "set", username, "-business", "on"); !strings.Contains(out, "updated") {
		t.Fatalf("bot set -business on: %q", out)
	}
	getState(ctx, t, bot)
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	botPeer := resolveBot(ctx, t, alice, username)
	toBob := inputPeerOf(ctx, t, alice, bob)
	toAlice := inputPeerOf(ctx, t, bob, alice)
	aliceId, bobId := alice.self.GetId(), bob.self.GetId()
	botId := botPeer.GetUserId()
	// The bot names Bob by his id, as it learns him from its updates.
	bobForBot := &mtproto.InputPeer{Constructor: bot.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: bobId}

	connect := func(rights *mtproto.BusinessBotRights) {
		t.Helper()
		rights.Constructor = alice.c.Constructor(mtproto.Predicate_businessBotRights)
		if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateConnectedBot{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_updateConnectedBot),
			Rights:      rights,
			Bot:         inputUserOfPeer(alice.c, botPeer),
			Recipients_INPUTBUSINESSBOTRECIPIENTS: &mtproto.InputBusinessBotRecipients{
				Constructor:   alice.c.Constructor(mtproto.Predicate_inputBusinessBotRecipients),
				ExistingChats: true,
				NewChats:      true,
			},
		}); err != nil {
			t.Fatalf("account.updateConnectedBot: %v", err)
		}
	}
	refused := func(err error, want string) {
		t.Helper()
		if !strings.Contains(fmt.Sprint(err), want) {
			t.Fatalf("got %v, want %s", err, want)
		}
	}

	connect(&mtproto.BusinessBotRights{Reply: true, ReadMessages: true, DeleteSentMessages: true, EditBio: true})
	id := waitBusinessUpdate(ctx, t, bot, mtproto.Predicate_updateBotBusinessConnect, func(u *mtproto.Update) bool {
		return !u.GetConnection().GetDisabled()
	}).GetConnection().GetConnectionId()

	// Before Bob writes, the chat had no incoming message: the bot may not
	// write there yet.
	send := func(text string, markup *mtproto.ReplyMarkup) (mtproto.TLObject, error) {
		return viaConnection(ctx, bot, id, &mtproto.TLMessagesSendMessage{
			Constructor: bot.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        bobForBot,
			Message:     text,
			RandomId:    randomID(),
			ReplyMarkup: markup,
		})
	}
	_, err := send("too early", nil)
	refused(err, "BOT_ACCESS_FORBIDDEN")

	sendMessageID(ctx, t, bob, toAlice, "do you deliver", randomID())
	waitBusinessUpdate(ctx, t, bot, mtproto.Predicate_updateBotNewBusinessMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == "do you deliver"
	})

	// The answer with a button.
	markup := mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{
			Buttons: []*mtproto.KeyboardButton{mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: "Order", Data: []byte("order")}).To_KeyboardButton()},
		}).To_KeyboardButtonRow()},
	}).To_ReplyMarkup()
	o, err := send("yes, we do", markup)
	if err != nil {
		t.Fatalf("messages.sendMessage through the connection: %v", err)
	}
	reply := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	if len(reply.GetUpdates()) != 1 || reply.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateBotNewBusinessMessage {
		t.Fatalf("the answer to the bot: %v", reply)
	}
	sent := reply.GetUpdates()[0].GetMessage_MESSAGE()
	if reply.GetUpdates()[0].GetConnectionId() != id || sent.GetMessage() != "yes, we do" || !sent.GetOut() ||
		sent.GetViaBusinessBotId().GetValue() != botId || sent.GetPeerId().GetUserId() != bobId {
		t.Fatalf("the message in the answer: %v", reply.GetUpdates()[0])
	}
	for _, u := range reply.GetUsers() {
		if u.GetId() == aliceId && u.GetSelf() {
			t.Fatalf("the account is self to its bot: %v", u)
		}
	}

	// Bob gets it from Alice, without the bot; Alice's devices with it.
	got, _ := waitNewMessage(ctx, t, bob, "yes, we do")
	if got.GetPeerId().GetUserId() != aliceId || got.GetViaBusinessBotId() != nil || got.GetOut() {
		t.Fatalf("Bob's copy: %v", got)
	}
	if got.GetReplyMarkup().GetPredicateName() != mtproto.Predicate_replyInlineMarkup {
		t.Fatalf("Bob's copy has no keyboard: %v", got.GetReplyMarkup())
	}
	own, _ := waitNewMessage(ctx, t, alice, "yes, we do")
	if own.GetId() != sent.GetId() || own.GetViaBusinessBotId().GetValue() != botId {
		t.Fatalf("Alice's copy: %v", own)
	}

	// Bob presses the button: the bot gets the press of its connection.
	press := make(chan error, 1)
	var answer *mtproto.Messages_BotCallbackAnswer
	go func() {
		o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesGetBotCallbackAnswer{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_getBotCallbackAnswer),
			Peer:        toAlice, MsgId: got.GetId(), Data: []byte("order"),
		})
		if err == nil {
			answer = o.(interface {
				To_Messages_BotCallbackAnswer() *mtproto.Messages_BotCallbackAnswer
			}).To_Messages_BotCallbackAnswer()
		}
		press <- err
	}()
	q := waitBusinessUpdate(ctx, t, bot, mtproto.Predicate_updateBusinessBotCallbackQuery, func(u *mtproto.Update) bool {
		return string(u.GetData_FLAGBYTES()) == "order"
	})
	if q.GetUserId() != bobId || q.GetConnectionId() != id || q.GetMessage_MESSAGE().GetId() != sent.GetId() {
		t.Fatalf("the press: %v", q)
	}
	if _, err = bot.c.Invoke(ctx, &mtproto.TLMessagesSetBotCallbackAnswer{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setBotCallbackAnswer),
		QueryId:     q.GetQueryId_INT64(), Message: wrapperspb.String("ordered"),
	}); err != nil {
		t.Fatalf("messages.setBotCallbackAnswer: %v", err)
	}
	if err = <-press; err != nil {
		t.Fatalf("messages.getBotCallbackAnswer: %v", err)
	}
	if answer.GetMessage().GetValue() != "ordered" {
		t.Fatalf("the answer of the press: %q", answer.GetMessage().GetValue())
	}

	// The bot edits its message; not Alice's own.
	o, err = viaConnection(ctx, bot, id, &mtproto.TLMessagesEditMessage{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_editMessage),
		Peer:        bobForBot,
		Id:          sent.GetId(),
		Message:     wrapperspb.String("yes, we do, from 9"),
	})
	if err != nil {
		t.Fatalf("messages.editMessage through the connection: %v", err)
	}
	if us := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates(); len(us) != 1 ||
		us[0].GetPredicateName() != mtproto.Predicate_updateBotEditBusinessMessage {
		t.Fatalf("the answer to the edit: %v", us)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateEditMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == "yes, we do, from 9"
	})
	mine := sendMessageID(ctx, t, alice, toBob, "Alice herself", randomID())
	_, err = viaConnection(ctx, bot, id, &mtproto.TLMessagesEditMessage{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_editMessage),
		Peer:        bobForBot,
		Id:          mine,
		Message:     wrapperspb.String("not hers"),
	})
	refused(err, "BOT_ACCESS_FORBIDDEN")

	// Typing and reading as Alice.
	if _, err = viaConnection(ctx, bot, id, &mtproto.TLMessagesSetTyping{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setTyping),
		Peer:        bobForBot,
		Action:      mtproto.MakeTLSendMessageTypingAction(nil).To_SendMessageAction(),
	}); err != nil {
		t.Fatalf("messages.setTyping through the connection: %v", err)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateUserTyping, func(u *mtproto.Update) bool { return u.GetUserId() == aliceId })
	if _, err = viaConnection(ctx, bot, id, &mtproto.TLMessagesReadHistory{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_readHistory),
		Peer:        bobForBot,
	}); err != nil {
		t.Fatalf("messages.readHistory through the connection: %v", err)
	}

	// The bio with edit_bio; the name without edit_name.
	o, err = viaConnection(ctx, bot, id, &mtproto.TLAccountUpdateProfile{
		Constructor: bot.c.Constructor(mtproto.Predicate_account_updateProfile),
		About:       wrapperspb.String("Open 9 to 6"),
	})
	if err != nil {
		t.Fatalf("account.updateProfile through the connection: %v", err)
	}
	if u := o.(interface{ To_User() *mtproto.User }).To_User(); u.GetId() != aliceId || u.GetSelf() {
		t.Fatalf("the account as its bot sees it: %v", u)
	}
	_, err = viaConnection(ctx, bot, id, &mtproto.TLAccountUpdateProfile{
		Constructor: bot.c.Constructor(mtproto.Predicate_account_updateProfile),
		FirstName:   wrapperspb.String("Shop"),
	})
	refused(err, "BOT_ACCESS_FORBIDDEN")

	// The bot deletes its message; not one it did not send.
	if _, err = viaConnection(ctx, bot, id, &mtproto.TLMessagesDeleteMessages{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_deleteMessages),
		Revoke:      true,
		Id:          []int32{mine},
	}); err == nil {
		t.Fatal("the bot deleted the account's own message")
	}
	refused(err, "BOT_ACCESS_FORBIDDEN")
	if _, err = viaConnection(ctx, bot, id, &mtproto.TLMessagesDeleteMessages{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_deleteMessages),
		Revoke:      true,
		Id:          []int32{sent.GetId()},
	}); err != nil {
		t.Fatalf("messages.deleteMessages through the connection: %v", err)
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateDeleteMessages, func(u *mtproto.Update) bool { return len(u.GetMessages()) == 1 })

	// Refused: a method outside the list, a connection of nobody, a user
	// with the bot's connection, the upload of a paused chat.
	_, err = viaConnection(ctx, bot, id, &mtproto.TLMessagesGetHistory{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        bobForBot,
		Limit:       10,
	})
	refused(err, "BUSINESS_CONNECTION_NOT_ALLOWED")
	_, err = viaConnection(ctx, bot, "no-such-connection", &mtproto.TLMessagesSetTyping{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setTyping),
		Peer:        bobForBot,
		Action:      mtproto.MakeTLSendMessageTypingAction(nil).To_SendMessageAction(),
	})
	refused(err, "BUSINESS_CONNECTION_INVALID")
	_, err = viaConnection(ctx, bob, id, &mtproto.TLMessagesSendMessage{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: bobId},
		Message:     "as Alice",
		RandomId:    randomID(),
	})
	refused(err, "BUSINESS_CONNECTION_NOT_ALLOWED")
	if _, err = alice.c.Invoke(ctx, &mtproto.TLAccountToggleConnectedBotPaused{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_toggleConnectedBotPaused),
		Peer:        toBob,
		Paused:      mtproto.BoolTrue,
	}); err != nil {
		t.Fatalf("account.toggleConnectedBotPaused: %v", err)
	}
	_, err = send("while paused", nil)
	refused(err, "BOT_ACCESS_FORBIDDEN")
	_, err = bot.c.Invoke(ctx, &mtproto.TLMessagesUploadMedia{
		Constructor:          bot.c.Constructor(mtproto.Predicate_messages_uploadMedia),
		BusinessConnectionId: wrapperspb.String(id),
		Peer:                 bobForBot,
		Media: mtproto.MakeTLInputMediaGeoPoint(&mtproto.InputMedia{
			GeoPoint: mtproto.MakeTLInputGeoPoint(&mtproto.InputGeoPoint{Lat: 52.52, Long: 13.40}).To_InputGeoPoint(),
		}).To_InputMedia(),
	})
	refused(err, "BOT_ACCESS_FORBIDDEN")
}

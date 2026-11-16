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

//go:build integration

package dao_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// A service message with an incoming action (messages.sendWebViewData,
// task 106): the sender keeps its action, the recipient gets the other one,
// both copies are one message (one dialog_message_id), and a deletion for
// both removes both.
func TestIncomingAction(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	ctx := context.Background()
	user, bot := newUser(), newUser()

	out := msg.MakeTLOutboxMessage(&msg.OutboxMessage{
		NoWebpage: true,
		RandomId:  randomId(),
		Message: mtproto.MakeTLMessageService(&mtproto.Message{
			Out:    true,
			FromId: mtproto.MakePeerUser(user),
			PeerId: mtproto.MakePeerUser(bot),
			Date:   int32(time.Now().Unix()),
			Action: mtproto.MakeTLMessageActionWebViewDataSent(&mtproto.MessageAction{Text: "Order"}).To_MessageAction(),
		}).To_Message(),
		IncomingAction: mtproto.MakeTLMessageActionWebViewDataSentMe(&mtproto.MessageAction{Text: "Order", Data: `{"item":7}`}).To_MessageAction(),
	}).To_OutboxMessage()
	r, err := w.rpc.MsgSendMessageV2(ctx, &msg.TLMsgSendMessageV2{
		UserId: user, AuthKeyId: user * 10, PeerType: mtproto.PEER_USER, PeerId: bot,
		Message: []*msg.OutboxMessage{out},
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := sentIds(r)
	if len(ids) != 1 {
		t.Fatalf("sent ids %v", ids)
	}
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			if a := u.GetMessage_MESSAGE().GetAction(); a.GetPredicateName() != mtproto.Predicate_messageActionWebViewDataSent {
				t.Errorf("the sender's reply has %v", a)
			}
		}
	}
	w.pub.drain()

	action := func(row messageRow) *mtproto.MessageAction {
		t.Helper()
		var m mtproto.Message
		if err := json.Unmarshal([]byte(row.Data), &m); err != nil {
			t.Fatal(err)
		}
		return m.GetAction()
	}
	userMsgs, botMsgs := w.messages(user), w.messages(bot)
	if len(userMsgs) != 1 || len(botMsgs) != 1 {
		t.Fatalf("messages: user %+v, bot %+v", userMsgs, botMsgs)
	}
	if a := action(userMsgs[0]); a.GetPredicateName() != mtproto.Predicate_messageActionWebViewDataSent || a.GetText() != "Order" || a.GetData() != "" {
		t.Errorf("the user's copy: %v", a)
	}
	if a := action(botMsgs[0]); a.GetPredicateName() != mtproto.Predicate_messageActionWebViewDataSentMe || a.GetText() != "Order" || a.GetData() != `{"item":7}` {
		t.Errorf("the bot's copy: %v", a)
	}
	if userMsgs[0].DialogMessageId != botMsgs[0].DialogMessageId {
		t.Errorf("dialog message ids: user %d, bot %d", userMsgs[0].DialogMessageId, botMsgs[0].DialogMessageId)
	}
	// What the bot was pushed is its own side.
	var pushed bool
	for _, p := range w.pub.pushesTo(bot) {
		for _, u := range p.updates.GetUpdates() {
			if u.GetPredicateName() != mtproto.Predicate_updateNewMessage {
				continue
			}
			pushed = true
			if a := u.GetMessage_MESSAGE().GetAction(); a.GetPredicateName() != mtproto.Predicate_messageActionWebViewDataSentMe {
				t.Errorf("the bot was pushed %v", a)
			}
		}
	}
	if !pushed {
		t.Error("the bot was pushed no message")
	}

	// A repeat of the send is the same message.
	if _, err = w.rpc.MsgSendMessageV2(ctx, &msg.TLMsgSendMessageV2{
		UserId: user, AuthKeyId: user * 10, PeerType: mtproto.PEER_USER, PeerId: bot,
		Message: []*msg.OutboxMessage{out},
	}); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()
	if n, m := len(w.messages(user)), len(w.messages(bot)); n != 1 || m != 1 {
		t.Errorf("after a repeat: user %d messages, bot %d", n, m)
	}

	if _, err = w.rpc.MsgDeleteMessages(ctx, &msg.TLMsgDeleteMessages{UserId: user, AuthKeyId: user * 10, Revoke: true, Id: ids}); err != nil {
		t.Fatal(err)
	}
	w.pub.drain()
	if left := alive(w.messages(user)); len(left) != 0 {
		t.Errorf("the user's messages after the deletion %+v", left)
	}
	if left := alive(w.messages(bot)); len(left) != 0 {
		t.Errorf("the bot's messages after the deletion %+v", left)
	}
	w.checkBox(user)
	w.checkBox(bot)
}

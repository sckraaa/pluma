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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

func webViewDataOutbox(text, data string) *msg.OutboxMessage {
	return msg.MakeTLOutboxMessage(&msg.OutboxMessage{
		RandomId: 1,
		Message: mtproto.MakeTLMessageService(&mtproto.Message{
			Action: mtproto.MakeTLMessageActionWebViewDataSent(&mtproto.MessageAction{Text: text}).To_MessageAction(),
		}).To_Message(),
		IncomingAction: mtproto.MakeTLMessageActionWebViewDataSentMe(&mtproto.MessageAction{Text: text, Data: data}).To_MessageAction(),
	}).To_OutboxMessage()
}

// An incoming action is for a service message sent now to another user:
// a group, Saved Messages, a scheduled send or an ordinary message has no
// second side of its own.
func TestCheckIncomingActions(t *testing.T) {
	const me, bot = 10, 20
	plain := msg.MakeTLOutboxMessage(&msg.OutboxMessage{Message: mtproto.MakeTLMessage(&mtproto.Message{Message: "hi"}).To_Message()}).To_OutboxMessage()
	text := webViewDataOutbox("Open", "x")
	text.Message = mtproto.MakeTLMessage(&mtproto.Message{Message: "hi"}).To_Message()
	scheduled := webViewDataOutbox("Open", "x")
	scheduled.ScheduleDate = wrapperspb.Int32(1)

	for _, tc := range []struct {
		name     string
		peerType int32
		peerId   int64
		out      *msg.OutboxMessage
		ok       bool
	}{
		{"none", mtproto.PEER_CHAT, 5, plain, true},
		{"private service", mtproto.PEER_USER, bot, webViewDataOutbox("Open", "x"), true},
		{"group", mtproto.PEER_CHAT, 5, webViewDataOutbox("Open", "x"), false},
		{"channel", mtproto.PEER_CHANNEL, 5, webViewDataOutbox("Open", "x"), false},
		{"saved messages", mtproto.PEER_USER, me, webViewDataOutbox("Open", "x"), false},
		{"scheduled", mtproto.PEER_USER, bot, scheduled, false},
		{"not a service message", mtproto.PEER_USER, bot, text, false},
	} {
		err := checkIncomingActions(&msg.TLMsgSendMessageV2{UserId: me, PeerType: tc.peerType, PeerId: tc.peerId, Message: []*msg.OutboxMessage{tc.out}})
		if tc.ok && err != nil || !tc.ok && !errors.Is(err, mtproto.ErrInputRequestInvalid) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

// The recipient's copy of a message with an incoming action has that
// action; the sender's box and the other messages are untouched.
func TestRecipientBoxes(t *testing.T) {
	data := webViewDataOutbox("Open", "payload")
	other := msg.MakeTLOutboxMessage(&msg.OutboxMessage{Message: mtproto.MakeTLMessage(&mtproto.Message{Message: "hi"}).To_Message()}).To_OutboxMessage()
	list := []*msg.OutboxMessage{other, data}
	msgs := []dao.Outgoing{{DialogMessageId: 100, Message: other.Message}, {DialogMessageId: 101, Message: data.Message}}
	incoming := incomingActions(list, msgs)
	if len(incoming) != 1 || incoming[101] == nil {
		t.Fatalf("incoming actions %v", incoming)
	}
	if incomingActions([]*msg.OutboxMessage{other}, msgs[:1]) != nil {
		t.Fatal("incoming actions of a send without any")
	}

	boxes := []*mtproto.MessageBox{
		{DialogMessageId: 100, Message: other.Message},
		{DialogMessageId: 101, Message: data.Message},
	}
	got := recipientBoxes(boxes, incoming)
	if got[0] != boxes[0] {
		t.Error("a message without an incoming action was copied")
	}
	a := got[1].GetMessage().GetAction()
	if a.GetPredicateName() != mtproto.Predicate_messageActionWebViewDataSentMe || a.GetText() != "Open" || a.GetData() != "payload" {
		t.Errorf("recipient's action %v", a)
	}
	if got[1].GetDialogMessageId() != 101 {
		t.Errorf("recipient's dialog_message_id %d", got[1].GetDialogMessageId())
	}
	if s := boxes[1].GetMessage().GetAction(); s.GetPredicateName() != mtproto.Predicate_messageActionWebViewDataSent || s.GetData() != "" {
		t.Errorf("sender's action changed to %v", s)
	}
	if same := recipientBoxes(boxes, nil); &same[0] != &boxes[0] {
		t.Error("boxes copied without incoming actions")
	}
}

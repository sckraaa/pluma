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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// A bot's edit of the state of a widget in a basic group (task 106)
// reaches every copy as updateMessageWidgetData in its owner's pts, with
// the chat as the owner sees it and the id in its box; the stored message
// has the new state and no edit date.
func TestWidgetStateEditInAGroup(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	bot, a := newUser(), newUser()
	chat := newChat()
	w.chats.members[chat] = []int64{bot, a}
	m := textMessage(bot, mtproto.MakePeerChat(chat), "")
	m.Media = mtproto.MakeTLMessageMediaWidget(&mtproto.MessageMedia{
		Payload: []byte{0x78, 0x9c, 3, 0}, CanQuery: true, ChatData: wrapperspb.String("turn 1"),
	}).To_MessageMedia()
	sent, err := w.rpc.MsgSendMessageV2(context.Background(), &msg.TLMsgSendMessageV2{
		UserId: bot, AuthKeyId: bot * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, Message: []*msg.OutboxMessage{outbox(m)},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.pub.drain()
	ids := sentIds(sent)
	if len(ids) != 1 {
		t.Fatalf("sent %v", ids)
	}
	box := &mtproto.MessageBox{UserId: bot, MessageId: ids[0], PeerType: mtproto.PEER_CHAT, PeerId: chat}
	for _, r := range w.messages(bot) {
		if r.Id == ids[0] {
			box.Message, box.DialogMessageId = storedMessage(t, r), r.DialogMessageId
		}
	}
	edited := proto.Clone(box.Message).(*mtproto.Message)
	edited.Media.ChatData = wrapperspb.String("turn 2")

	r, err := w.rpc.MsgEditMessageV2(context.Background(), &msg.TLMsgEditMessageV2{
		UserId: bot, AuthKeyId: bot * 10, PeerType: mtproto.PEER_CHAT, PeerId: chat, EditType: msg.EditTypeNormal,
		NewMessage: msg.MakeTLOutboxMessage(&msg.OutboxMessage{NoWebpage: true, Message: edited}).To_OutboxMessage(),
		DstMessage: box,
	})
	if err != nil {
		t.Fatal(err)
	}
	if us := r.GetUpdates(); len(us) != 1 || us[0].GetPredicateName() != mtproto.Predicate_updateMessageWidgetData ||
		us[0].GetChatData().GetValue() != "turn 2" || us[0].GetPublicData() != nil || us[0].GetPeer_PEER().GetChatId() != chat {
		t.Fatalf("reply %v", us)
	}
	w.pub.drain()
	for _, u := range []int64{bot, a} {
		rows := w.journal(u)
		last := rows[len(rows)-1]
		up := last.update(t)
		if last.Type != mtproto.PTS_UPDATE_MESSAGE_WIDGET_DATA || up.GetPredicateName() != mtproto.Predicate_updateMessageWidgetData ||
			up.GetChatData().GetValue() != "turn 2" || up.GetPeer_PEER().GetChatId() != chat {
			t.Errorf("user %d: last journal row %+v", u, last)
		}
		var found bool
		for _, mr := range w.messages(u) {
			if mr.DialogMessageId != box.DialogMessageId {
				continue
			}
			found = true
			sm := storedMessage(t, mr)
			if up.GetMsgId_INT32() != mr.Id || sm.GetMedia().GetChatData().GetValue() != "turn 2" || sm.GetEditDate() != nil {
				t.Errorf("user %d: copy %d %v, update of %d", u, mr.Id, sm, up.GetMsgId_INT32())
			}
		}
		if !found {
			t.Errorf("user %d: no copy", u)
		}
		w.checkBox(u)
	}
}

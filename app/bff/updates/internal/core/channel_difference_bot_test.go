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
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestKeepVisible(t *testing.T) {
	msg := func(text string) *mtproto.Message {
		return mtproto.MakeTLMessage(&mtproto.Message{Message: text}).To_Message()
	}
	edit := func(text string) *mtproto.Update {
		return mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: msg(text)}).To_Update()
	}
	other := mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: 1}).To_Update()
	diff := &mtproto.Updates_ChannelDifference{
		NewMessages:  []*mtproto.Message{msg("no"), msg("yes")},
		Messages:     []*mtproto.Message{msg("no")},
		OtherUpdates: []*mtproto.Update{edit("no"), edit("yes"), other},
	}
	keepVisible(diff, func(m *mtproto.Message) bool { return m.GetMessage() == "yes" })
	if len(diff.NewMessages) != 1 || diff.NewMessages[0].GetMessage() != "yes" {
		t.Fatalf("new messages: %v", diff.NewMessages)
	}
	if len(diff.Messages) != 0 {
		t.Fatalf("messages: %v", diff.Messages)
	}
	if len(diff.OtherUpdates) != 2 || diff.OtherUpdates[0].GetMessage_MESSAGE().GetMessage() != "yes" || diff.OtherUpdates[1] != other {
		t.Fatalf("other updates: %v", diff.OtherUpdates)
	}
}

func TestReplyToInChat(t *testing.T) {
	m := mtproto.MakeTLMessage(&mtproto.Message{ReplyTo: mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(9)}).To_MessageReplyHeader()}).To_Message()
	if got := replyToInChat(m); got != 9 {
		t.Fatalf("reply: %d", got)
	}
	if got := replyToInChat(mtproto.MakeTLMessage(&mtproto.Message{}).To_Message()); got != 0 {
		t.Fatalf("no reply: %d", got)
	}
}

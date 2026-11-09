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

package botprivacy

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func text(s string) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{Message: s}).To_Message()
}

func TestVisible(t *testing.T) {
	bot := Bot{Id: 5, Username: "my_bot"}
	cases := []struct {
		name    string
		m       *mtproto.Message
		replied bool
		want    bool
	}{
		{"plain text", text("hello"), false, false},
		{"command for it", text("/help@my_bot now"), false, true},
		{"same name, other case", text("/help@MY_Bot"), false, true},
		{"command for another", text("/help@other_bot"), false, false},
		{"general command", text("/start"), false, true},
		{"command with parameter", text("/start abc"), false, true},
		{"slash in text", text("a /start"), false, false},
		{"bare slash", text("/"), false, false},
		{"path", text("/usr/bin"), false, false},
		{"reply to it", text("sure"), true, true},
		{"service", mtproto.MakeTLMessageService(&mtproto.Message{Action: mtproto.MakeTLMessageActionChatJoinedByLink(&mtproto.MessageAction{}).To_MessageAction()}).To_Message(), false, true},
		{"via it", mtproto.MakeTLMessage(&mtproto.Message{Message: "x", ViaBotId: wrapperspb.Int64(5)}).To_Message(), false, true},
		{"via another", mtproto.MakeTLMessage(&mtproto.Message{Message: "x", ViaBotId: wrapperspb.Int64(6)}).To_Message(), false, false},
	}
	for _, c := range cases {
		if got := Visible(bot, c.m, false, c.replied); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if !Visible(Bot{Id: 5, ReadsAll: true}, text("hello"), false, false) {
		t.Error("privacy off hides a message")
	}
}

// core.telegram.org/bots/faq: no mode shows a bot what another bot wrote; its
// service messages stay, and the bot's own messages are its own.
func TestVisibleOtherBots(t *testing.T) {
	reader := Bot{Id: 5, Username: "my_bot", ReadsAll: true}
	if Visible(reader, text("/help@my_bot"), true, true) {
		t.Error("a message of another bot reached a bot that reads all")
	}
	service := mtproto.MakeTLMessageService(&mtproto.Message{Action: mtproto.MakeTLMessageActionChatJoinedByLink(&mtproto.MessageAction{}).To_MessageAction()}).To_Message()
	if !Visible(reader, service, true, false) {
		t.Error("a service message of another bot was hidden")
	}
	own := mtproto.MakeTLMessage(&mtproto.Message{Message: "plain", FromId: mtproto.MakePeerUser(5)}).To_Message()
	if !Visible(Bot{Id: 5, Username: "my_bot"}, own, false, false) {
		t.Error("the bot's own message was hidden")
	}
}

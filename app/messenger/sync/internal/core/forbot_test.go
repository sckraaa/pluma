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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// The push of a supergroup to a bot in privacy mode holds only what is
// meant for it (pkg/botprivacy); another update stays.
func TestForBotSupergroup(t *testing.T) {
	const botId = 5
	users := mtproto.MakeTLMutableUsers(&mtproto.MutableUsers{Users: []*mtproto.ImmutableUser{mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{
		User: mtproto.MakeTLUserData(&mtproto.UserData{Id: botId, Username: "my_bot", Bot: mtproto.MakeTLBotData(&mtproto.BotData{}).To_BotData()}).To_UserData(),
	}).To_ImmutableUser()}}).To_MutableUsers()
	mc := &channelpb.MutableChannel{Channel: &channelpb.ImmutableChannel{Megagroup: true}}

	msg := func(text string) *mtproto.Update {
		return mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: mtproto.MakeTLMessage(&mtproto.Message{Message: text}).To_Message()}).To_Update()
	}
	other := mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: 1}).To_Update()

	got := forBot(mtproto.MakeUpdatesByUpdates(msg("/help@my_bot"), msg("plain"), other), mc, users, botId, false)
	if got == nil || len(got.GetUpdates()) != 2 || got.GetUpdates()[0].GetMessage_MESSAGE().GetMessage() != "/help@my_bot" {
		t.Fatalf("updates: %v", got.GetUpdates())
	}
	if forBot(mtproto.MakeUpdatesByUpdates(msg("plain")), mc, users, botId, false) != nil {
		t.Fatal("a push with nothing for the bot")
	}
	reply := msg("plain")
	reply.GetMessage_MESSAGE().ReplyTo = mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(7)}).To_MessageReplyHeader()
	if forBot(mtproto.MakeUpdatesByUpdates(reply), mc, users, botId, true) == nil {
		t.Fatal("a reply to the bot was dropped")
	}
	// Mentioned, but in no reply: as in a basic group, the bot does not get it.
	if forBot(mtproto.MakeUpdatesByUpdates(msg("plain")), mc, users, botId, true) != nil {
		t.Fatal("a mention reached a bot in privacy mode")
	}

	// A broadcast channel's posts reach all its bots.
	broadcast := &channelpb.MutableChannel{Channel: &channelpb.ImmutableChannel{Broadcast: true}}
	if forBot(mtproto.MakeUpdatesByUpdates(msg("post")), broadcast, users, botId, false) == nil {
		t.Fatal("a channel post was dropped")
	}
}

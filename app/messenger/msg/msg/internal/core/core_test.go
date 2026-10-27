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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

func TestWithRemoved(t *testing.T) {
	p := func(userId int64, state int32) *mtproto.ImmutableChatParticipant {
		return mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{ChatId: 5, UserId: userId, State: state}).To_ImmutableChatParticipant()
	}
	chat := mtproto.MakeTLMutableChat(&mtproto.MutableChat{
		Chat: mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{Id: 5}).To_ImmutableChat(),
		ChatParticipants: []*mtproto.ImmutableChatParticipant{
			p(1, mtproto.ChatMemberStateNormal), p(2, mtproto.ChatMemberStateNormal),
			p(3, mtproto.ChatMemberStateKicked), p(4, mtproto.ChatMemberStateLeft),
		},
	}).To_MutableChat()
	service := func(action *mtproto.MessageAction) []*msg.OutboxMessage {
		return []*msg.OutboxMessage{msg.MakeTLOutboxMessage(&msg.OutboxMessage{
			Message: mtproto.MakeTLMessageService(&mtproto.Message{Action: action}).To_Message(),
		}).To_OutboxMessage()}
	}
	text := []*msg.OutboxMessage{msg.MakeTLOutboxMessage(&msg.OutboxMessage{
		Message: mtproto.MakeTLMessage(&mtproto.Message{Message: "hi"}).To_Message(),
	}).To_OutboxMessage()}

	for _, c := range []struct {
		name string
		from int64
		msgs []*msg.OutboxMessage
		want []int64
	}{
		{"a text goes to the members", 1, text, []int64{2}},
		{"the removal goes to the removed user", 1, service(mtproto.MakeMessageActionChatDeleteUser(3)), []int64{2, 3}},
		{"who left sends it", 4, service(mtproto.MakeMessageActionChatDeleteUser(4)), []int64{1, 2}},
		{"a stranger gets nothing", 1, service(mtproto.MakeMessageActionChatDeleteUser(9)), []int64{2}},
		{"another action changes nothing", 1, service(mtproto.MakeMessageActionChatAddUser(3)), []int64{2}},
	} {
		if got := withRemoved(chatRecipients(chat, c.from, true), chat, c.from, c.msgs); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

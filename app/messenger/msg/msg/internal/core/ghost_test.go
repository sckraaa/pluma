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

func ghostTestChat() *mtproto.MutableChat {
	p := func(userId int64, state int32) *mtproto.ImmutableChatParticipant {
		return mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{ChatId: 5, UserId: userId, State: state}).To_ImmutableChatParticipant()
	}
	return mtproto.MakeTLMutableChat(&mtproto.MutableChat{
		Chat: mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{Id: 5, Title: "g", ParticipantsCount: 2, Version: 4}).To_ImmutableChat(),
		ChatParticipants: []*mtproto.ImmutableChatParticipant{
			p(1, mtproto.ChatMemberStateNormal), p(2, mtproto.ChatMemberStateNormal), p(9, mtproto.ChatMemberStateLeft),
		},
	}).To_MutableChat()
}

// TestMentionedOutside: the users the messages mention by an entity who are
// not members now (one who left included), once each; members and plain
// text are none.
func TestMentionedOutside(t *testing.T) {
	entity := func(predicate string, userId int64) *mtproto.MessageEntity {
		return &mtproto.MessageEntity{PredicateName: predicate, UserId_INT64: userId}
	}
	out := func(entities ...*mtproto.MessageEntity) *msg.OutboxMessage {
		return &msg.OutboxMessage{Message: &mtproto.Message{Entities: entities}}
	}
	got := mentionedOutside(ghostTestChat(), []*msg.OutboxMessage{
		out(entity(mtproto.Predicate_messageEntityMention, 7), entity(mtproto.Predicate_messageEntityMention, 2)),
		out(entity(mtproto.Predicate_messageEntityMentionName, 9), entity(mtproto.Predicate_messageEntityMention, 7), entity(mtproto.Predicate_messageEntityBold, 0)),
	})
	if !slices.Equal(got, []int64{7, 9}) {
		t.Errorf("mentioned outside %v, want [7 9]", got)
	}
}

// TestHiddenMemberView: the ghost a send brought in sees the group as a
// member (not left, not forbidden), with the count of the others, and the
// chat the others are told of stays as it was.
func TestHiddenMemberView(t *testing.T) {
	chat := ghostTestChat()
	for _, id := range []int64{7, 9} {
		v := hiddenMemberView(chat, id)
		if v.GetPredicateName() != mtproto.Predicate_chat || v.GetLeft() || v.GetParticipantsCount_INT32() != 2 {
			t.Errorf("user %d sees %+v", id, v)
		}
	}
	if len(chat.ChatParticipants) != 3 || chat.ChatParticipants[2].State != mtproto.ChatMemberStateLeft {
		t.Errorf("the chat changed: %+v", chat.ChatParticipants)
	}
	if v := chat.ToUnsafeChat(7); v.GetPredicateName() != mtproto.Predicate_chatEmpty {
		t.Errorf("an outsider sees %s", v.GetPredicateName())
	}
}

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

package chat

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func futureChat(creator int64, participants ...*mtproto.ImmutableChatParticipant) *mtproto.MutableChat {
	return &mtproto.MutableChat{
		Chat:             &mtproto.ImmutableChat{Id: 1, Creator: creator},
		ChatParticipants: participants,
	}
}

func futureMember(userId int64, kind, state int32, date int64, bot bool) *mtproto.ImmutableChatParticipant {
	return &mtproto.ImmutableChatParticipant{UserId: userId, ParticipantType: kind, State: state, Date: date, IsBot: bot}
}

// The next creator of a basic group: an admin before a member, the oldest
// first, the lowest id on a tie; never the creator, a bot, one gone, one
// who left or a hidden member.
func TestFutureCreator(t *testing.T) {
	const (
		normal = mtproto.ChatMemberStateNormal
		left   = mtproto.ChatMemberStateLeft
	)
	none := func(int64) bool { return false }
	cases := []struct {
		name string
		chat *mtproto.MutableChat
		gone func(int64) bool
		want int64
	}{
		{"nobody else", futureChat(1, futureMember(1, mtproto.ChatMemberCreator, normal, 10, false)), none, 0},
		{"the oldest member", futureChat(1,
			futureMember(1, mtproto.ChatMemberCreator, normal, 10, false),
			futureMember(3, mtproto.ChatMemberNormal, normal, 30, false),
			futureMember(2, mtproto.ChatMemberNormal, normal, 20, false)), none, 2},
		{"an admin before an older member", futureChat(1,
			futureMember(2, mtproto.ChatMemberNormal, normal, 20, false),
			futureMember(3, mtproto.ChatMemberAdmin, normal, 30, false)), none, 3},
		{"the lowest id on a tie", futureChat(1,
			futureMember(5, mtproto.ChatMemberNormal, normal, 20, false),
			futureMember(4, mtproto.ChatMemberNormal, normal, 20, false)), none, 4},
		{"not a bot, nor one who left", futureChat(1,
			futureMember(2, mtproto.ChatMemberAdmin, normal, 5, true),
			futureMember(3, mtproto.ChatMemberAdmin, left, 5, false),
			futureMember(4, mtproto.ChatMemberNormal, normal, 50, false)), none, 4},
		{"not one gone", futureChat(1,
			futureMember(2, mtproto.ChatMemberNormal, normal, 20, false),
			futureMember(3, mtproto.ChatMemberNormal, normal, 30, false)), func(id int64) bool { return id == 2 }, 3},
		{"not a hidden member (the ghost)", futureChat(1,
			&mtproto.ImmutableChatParticipant{UserId: 2, State: normal, Date: 5, Hidden: true},
			futureMember(3, mtproto.ChatMemberNormal, normal, 30, false)), none, 3},
	}
	for _, tc := range cases {
		if got := FutureCreator(tc.chat, tc.gone); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

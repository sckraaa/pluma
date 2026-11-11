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
	"github.com/teamgram/proto/mtproto"
)

// FutureCreator picks who owns the basic group chat after its creator
// leaves it (task 078, core.telegram.org/api/channel, "Leaving
// groups/channels": a basic group goes to that user at once): a member in
// it now other than the creator, neither a bot, a hidden member (the ghost,
// ADR 0048) nor an account gone (gone tells), the admins before the other members, and among them the one in
// the chat the longest (date is the join), then the lowest id. As a
// supergroup's (biz channel, futureCreatorQuery): the admins first, as
// tdesktop lists them first where the owner picks another one
// (select_future_owner_box.cpp, LegacyParticipantsController). 0 for
// nobody. messages.getFutureChatCreatorAfterLeave answers it, and the leave
// of the creator (chat.deleteChatUser) hands the chat to it.
func FutureCreator(chat *mtproto.MutableChat, gone func(userId int64) bool) int64 {
	var best *mtproto.ImmutableChatParticipant
	chat.Walk(func(userId int64, p *mtproto.ImmutableChatParticipant) error {
		if !p.IsChatMemberStateNormal() || p.IsChatMemberCreator() || p.GetIsBot() || p.GetHidden() || userId == chat.Creator() || gone(userId) {
			return nil
		}
		if best == nil || before(p, best) {
			best = p
		}
		return nil
	})
	return best.GetUserId()
}

// before tells whether p comes before q as the next creator.
func before(p, q *mtproto.ImmutableChatParticipant) bool {
	if pa, qa := p.IsChatMemberAdmin(), q.IsChatMemberAdmin(); pa != qa {
		return pa
	}
	if p.GetDate() != q.GetDate() {
		return p.GetDate() < q.GetDate()
	}
	return p.GetUserId() < q.GetUserId()
}

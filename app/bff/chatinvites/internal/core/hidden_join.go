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
	"github.com/teamgram/proto/mtproto"
)

// hiddenMember tells whether userId is a hidden member of chat: the support
// ghost (ADR 0048), which biz chat let in by the link unseen.
func hiddenMember(chat *mtproto.MutableChat, userId int64) bool {
	p, ok := chat.GetImmutableChatParticipant(userId)
	return ok && p.IsChatMemberStateNormal() && p.GetHidden()
}

// hiddenJoinUpdates answers the ghost that joined a basic group by its link
// hidden: no service message (chatJoinedByLink would show it to all), the
// chat as the ghost sees it, which the clients open from the answer's
// chats (Android JoinGroupAlert.java:384-391). Its devices get the members
// with seq from biz chat, and the history from its inbox.
func hiddenJoinUpdates(chat *mtproto.MutableChat, userId int64) *mtproto.Updates {
	return mtproto.MakeUpdatesByUpdatesUsersChats(nil, []*mtproto.Chat{chat.ToUnsafeChat(userId)})
}

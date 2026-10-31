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

package dialog

import (
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func userPeer(id int64) *mtproto.Peer {
	return mtproto.MakeTLPeerUser(&mtproto.Peer{UserId: id}).To_Peer()
}

func chatPeer(id int64) *mtproto.Peer {
	return mtproto.MakeTLPeerChat(&mtproto.Peer{ChatId: id}).To_Peer()
}

// The answer keeps the users of the top messages, not only the peers of
// private chats: the sender of a group's last message, and the account
// itself. A dialog whose top message or peer did not load goes.
func TestMessagesDialogsUsers(t *testing.T) {
	const me, bob, lev, gone, group = 1, 2, 3, 4, 100
	dialog := func(peer *mtproto.Peer, top int32) *mtproto.Dialog {
		return mtproto.MakeTLDialog(&mtproto.Dialog{Peer: peer, TopMessage: top}).To_Dialog()
	}
	message := func(id int32, peer *mtproto.Peer, from *mtproto.Peer) *mtproto.Message {
		return mtproto.MakeTLMessage(&mtproto.Message{Id: id, PeerId: peer, FromId: from}).To_Message()
	}
	user := func(id int64) *mtproto.User {
		return mtproto.MakeTLUser(&mtproto.User{Id: id}).To_User()
	}
	d := &DialogsDataHelper{
		Dialogs: []*mtproto.Dialog{
			dialog(userPeer(bob), 10),
			dialog(chatPeer(group), 11),
			dialog(userPeer(gone), 12), // no user loaded
			dialog(userPeer(bob), 13),  // no message loaded
		},
		Messages: []*mtproto.Message{
			message(10, userPeer(bob), nil),
			message(11, chatPeer(group), userPeer(lev)),
			message(12, userPeer(gone), nil),
		},
		Users: []*mtproto.User{user(me), user(bob), user(lev)},
		Chats: []*mtproto.Chat{mtproto.MakeTLChat(&mtproto.Chat{Id: group}).To_Chat()},
	}
	r := d.ToMessagesDialogs(2)

	var users []int64
	for _, u := range r.GetUsers() {
		users = append(users, u.GetId())
	}
	if !slices.Equal(users, []int64{me, bob, lev}) {
		t.Errorf("users %v, want the account, the peer and the group's sender", users)
	}
	var tops []int32
	for _, dlg := range r.GetDialogs() {
		tops = append(tops, dlg.GetTopMessage())
	}
	if !slices.Equal(tops, []int32{10, 11}) || len(r.GetMessages()) != 2 || len(r.GetChats()) != 1 {
		t.Errorf("dialogs %v, %d messages, %d chats", tops, len(r.GetMessages()), len(r.GetChats()))
	}
}

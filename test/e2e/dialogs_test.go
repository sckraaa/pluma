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

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// messages.getDialogs carries the users its top messages name: the sender of
// a group's last message, whom the reader may not know, and the account
// itself (task 020). Android, freshly signed in, showed the last message of a
// group as from "DELETED".
func TestDialogsUsers(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	group := createChat(ctx, t, alice, "Dialogs", bob, carol)
	sendMessageID(ctx, t, carol, chatPeer(carol, group), fmt.Sprintf("hello %d", randomID()), randomID())

	// Bob's copy arrives with an inbox event after Carol's answer.
	var users map[int64]bool
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesGetDialogs{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_getDialogs),
			OffsetPeer:  &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
			Limit:       20,
		})
		if err != nil {
			t.Fatalf("messages.getDialogs: %v", err)
		}
		d := o.(interface {
			To_Messages_Dialogs() *mtproto.Messages_Dialogs
		}).To_Messages_Dialogs()
		fromCarol := false
		for _, m := range d.GetMessages() {
			if m.GetPeerId().GetChatId() == group && m.GetFromId().GetUserId() == carol.self.GetId() {
				fromCarol = true
			}
		}
		if !fromCarol {
			continue
		}
		users = map[int64]bool{}
		for _, u := range d.GetUsers() {
			users[u.GetId()] = true
		}
		break
	}
	if users == nil {
		t.Fatal("the group's last message from Carol did not reach Bob's dialogs")
	}
	if !users[carol.self.GetId()] || !users[bob.self.GetId()] {
		t.Errorf("users %v: want Carol, the sender, and Bob himself", users)
	}
}

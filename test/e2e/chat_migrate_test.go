//go:build e2e

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

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// TestChatMigrate (task 047) end to end: Alice turns her basic group with
// Bob and Carol into a supergroup. Her answer, within the 5 s iOS waits,
// holds chatMigrateTo, updateChannel and channelMigrateFrom with the group
// (migrated_to) and the supergroup in chats; a member may not do it; Bob
// gets his chatMigrateTo and writes to the supergroup; the old group takes
// no more messages; a repeat gives the same supergroup.
func TestChatMigrate(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	chatId := createChat(ctx, t, alice, "to migrate", bob, carol)
	getState(ctx, t, bob)

	migrate := func(u *user) (*mtproto.Updates, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesMigrateChat{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_migrateChat),
			ChatId:      chatId,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
	}
	if _, err := migrate(bob); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member migrates: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	start := time.Now()
	r, err := migrate(alice)
	if err != nil {
		t.Fatalf("messages.migrateChat: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the answer took %v, iOS waits 5 s", d)
	}
	var migrateTo, migrateFrom, updated bool
	var channelId int64
	for _, u := range r.GetUpdates() {
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateNewMessage:
			migrateTo = migrateTo || u.GetMessage_MESSAGE().GetAction().GetPredicateName() == mtproto.Predicate_messageActionChatMigrateTo
		case mtproto.Predicate_updateNewChannelMessage:
			a := u.GetMessage_MESSAGE().GetAction()
			migrateFrom = a.GetPredicateName() == mtproto.Predicate_messageActionChannelMigrateFrom && a.GetChatId() == chatId
			channelId = u.GetMessage_MESSAGE().GetPeerId().GetChannelId()
		case mtproto.Predicate_updateChannel:
			updated = true
		}
	}
	if !migrateTo || !migrateFrom || !updated || channelId == 0 {
		t.Fatalf("the answer: %v", r.GetUpdates())
	}
	var group, super *mtproto.Chat
	for _, c := range r.GetChats() {
		switch c.GetPredicateName() {
		case mtproto.Predicate_chat:
			group = c
		case mtproto.Predicate_channel:
			super = c
		}
	}
	if group.GetMigratedTo().GetChannelId() != channelId || !super.GetMegagroup() || super.GetId() != channelId {
		t.Fatalf("chats: %v", r.GetChats())
	}

	if _, err = bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, u := range us.GetUpdates() {
			if u.GetMessage_MESSAGE().GetAction().GetPredicateName() == mtproto.Predicate_messageActionChatMigrateTo {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("Bob's chatMigrateTo: %v", err)
	}
	hash := super.GetAccessHash_FLAGINT64().GetValue()
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: channelId, AccessHash: hash},
		Message:     "in the supergroup", RandomId: randomID(),
	}); err != nil {
		t.Errorf("Bob writes to the supergroup: %v", err)
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        chatPeer(bob, chatId),
		Message:     "in the old group", RandomId: randomID(),
	}); !mtclient.IsRPCError(err, "CHAT_WRITE_FORBIDDEN") {
		t.Errorf("Bob writes to the old group: %v, want CHAT_WRITE_FORBIDDEN", err)
	}

	again, err := migrate(alice)
	if err != nil {
		t.Fatalf("a repeat: %v", err)
	}
	for _, c := range again.GetChats() {
		if c.GetPredicateName() == mtproto.Predicate_channel && c.GetId() != channelId {
			t.Errorf("a repeat made another supergroup: %d", c.GetId())
		}
	}
}

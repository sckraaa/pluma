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
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// A checklist Alice writes to a supergroup as her channel stays hers when
// Bob ticks an item (task 081): her devices get the edit as out, Bob's
// answer is not out.
func TestSendAsChecklist(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	_, mine := createChannel(ctx, t, alice, fmt.Sprintf("mine %d", randomID()), false)
	_, group := createChannel(ctx, t, alice, fmt.Sprintf("checklist as %d", randomID()), true)
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel: &mtproto.InputChannel{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel),
			ChannelId:   group.GetId(),
			AccessHash:  group.GetAccessHash_FLAGINT64().GetValue(),
		},
		Users: []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	groupPeer := channelInputPeer(group)
	getState(ctx, t, alice)
	getState(ctx, t, laptop)

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        groupPeer,
		Media: &mtproto.InputMedia{Constructor: alice.c.Constructor(mtproto.Predicate_inputMediaTodo), Todo: &mtproto.TodoList{
			Constructor:       alice.c.Constructor(mtproto.Predicate_todoList),
			OthersCanComplete: true,
			Title:             pollText(alice, "Trip"),
			List:              []*mtproto.TodoItem{todoItem(alice, 1, "Tickets"), todoItem(alice, 2, "Hotel")},
		}},
		RandomId: randomID(),
		SendAs:   channelInputPeer(mine),
	})
	if err != nil {
		t.Fatalf("messages.sendMedia of a checklist as the channel: %v", err)
	}
	var msgId int32
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if m := u.GetMessage_MESSAGE(); u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage && m.GetFromId().GetChannelId() == mine.GetId() {
			msgId = m.GetId()
		}
	}
	if msgId == 0 {
		t.Fatal("the checklist was not sent as the channel")
	}

	o, err = bob.c.Invoke(ctx, &mtproto.TLMessagesToggleTodoCompleted{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_toggleTodoCompleted),
		Peer:        groupPeer, MsgId: msgId, Completed: []int32{1}, Incompleted: []int32{},
	})
	if err != nil {
		t.Fatalf("messages.toggleTodoCompleted: %v", err)
	}
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if m := u.GetMessage_MESSAGE(); u.GetPredicateName() == mtproto.Predicate_updateEditChannelMessage && m.GetOut() {
			t.Error("Bob's answer shows Alice's checklist as his own")
		}
	}
	for _, device := range []*user{alice, laptop} {
		waitUpdate(ctx, t, device, mtproto.Predicate_updateEditChannelMessage, func(u *mtproto.Update) bool {
			m := u.GetMessage_MESSAGE()
			if m.GetId() != msgId || len(m.GetMedia().GetCompletions()) != 1 {
				return false
			}
			if !m.GetOut() {
				t.Error("Alice's device got her checklist as another's")
			}
			return true
		})
	}
}

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

// messages.getMessages with inputMessageReplyTo gives the message the given
// one replies to, as iOS asks for replies out of the loaded history
// (task 020).
func TestGetRepliedMessage(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob := peerOf(ctx, t, alice, bob)

	question := fmt.Sprintf("question %d", randomID())
	first := sendMessageID(ctx, t, alice, toBob, question, randomID())
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        toBob,
		Message:     "and a reply",
		RandomId:    randomID(),
		ReplyTo:     mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: first}).To_InputReplyTo(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	var reply int32
	for _, up := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if up.GetPredicateName() == mtproto.Predicate_updateMessageID {
			reply = up.GetId_INT32()
		}
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getMessages),
		Id_VECTORINPUTMESSAGE: []*mtproto.InputMessage{
			mtproto.MakeTLInputMessageReplyTo(&mtproto.InputMessage{Id: reply}).To_InputMessage(),
		},
	})
	if err != nil {
		t.Fatalf("messages.getMessages: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	})
	if !ok {
		t.Fatalf("messages.getMessages returned %T", o)
	}
	msgs := r.To_Messages_Messages().GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("messages.getMessages(inputMessageReplyTo %d): %d messages", reply, len(msgs))
	}
	if msgs[0].GetId() != first || msgs[0].GetMessage() != question {
		t.Errorf("messages.getMessages(inputMessageReplyTo %d): message %d, want %d", reply, msgs[0].GetId(), first)
	}
}

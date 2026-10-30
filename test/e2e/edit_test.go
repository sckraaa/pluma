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
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// An edit keeps the formatting the client sends with the new text: the
// recipient sees the bold word and the text link, and the link in the text
// once, as for a new message (task 020).
func TestEditKeepsFormatting(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob := peerOf(ctx, t, alice, bob)
	toAlice := peerOf(ctx, t, bob, alice)

	id := sendMessageID(ctx, t, alice, toBob, fmt.Sprintf("draft %d", randomID()), randomID())
	text := "bold link https://pluma.chat"
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesEditMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_editMessage),
		Peer:        toBob,
		Id:          id,
		Message:     wrapperspb.String(text),
		Entities: []*mtproto.MessageEntity{
			mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 4}).To_MessageEntity(),
			mtproto.MakeTLMessageEntityTextUrl(&mtproto.MessageEntity{Offset: 5, Length: 4, Url: "https://pluma.chat/faq"}).To_MessageEntity(),
		},
	}); err != nil {
		t.Fatalf("messages.editMessage: %v", err)
	}

	// The recipient's copy is edited by an inbox event after the reply.
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_getHistory),
			Peer:        toAlice,
			Limit:       10,
		})
		if err != nil {
			t.Fatalf("messages.getHistory: %v", err)
		}
		h, ok := o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		})
		if !ok {
			t.Fatalf("messages.getHistory returned %T", o)
		}
		for _, m := range h.To_Messages_Messages().GetMessages() {
			if m.GetMessage() != text {
				continue
			}
			count := map[string]int{}
			for _, e := range m.GetEntities() {
				count[e.GetPredicateName()]++
			}
			if count[mtproto.Predicate_messageEntityBold] != 1 || count[mtproto.Predicate_messageEntityTextUrl] != 1 ||
				count[mtproto.Predicate_messageEntityUrl] != 1 || len(m.GetEntities()) != 3 {
				t.Fatalf("entities of the edited message: %v", count)
			}
			return
		}
	}
	t.Fatal("the edited message is not in the recipient's history")
}

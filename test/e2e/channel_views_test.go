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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestChannelViewsAndLinks: a subscriber may not post; a post carries its
// author's signature and one view, grows by one per user however often and
// however concurrently it is viewed, shows the views in the history and in
// its edits, and counts a forward once per random_id; the members are told
// by updateChannelMessageViews and updateChannelMessageForwards; the link
// of the post is on the server's host.
func TestChannelViewsAndLinks(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")

	_, ch := createChannel(ctx, t, alice, "views", false)
	inputOf := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	}
	peerOf := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	}
	hash := channelInviteHash(ctx, t, alice, inputOf(alice))
	for _, u := range []*user{bob, carol} {
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
			Hash:        hash,
		}); err != nil {
			t.Fatalf("messages.importChatInvite: %v", err)
		}
	}
	getState(ctx, t, alice)

	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peerOf(bob), Message: "from a subscriber", RandomId: randomID(),
	}); !mtclient.IsRPCError(err, "CHAT_WRITE_FORBIDDEN") {
		t.Errorf("a subscriber posts: %v, want CHAT_WRITE_FORBIDDEN", err)
	}

	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsToggleSignatures{
		Constructor:       alice.c.Constructor(mtproto.Predicate_channels_toggleSignatures),
		Channel:           inputOf(alice),
		SignaturesEnabled: true,
	}); err != nil {
		t.Fatalf("channels.toggleSignatures: %v", err)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peerOf(alice), Message: "the post", RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("the post: %v", err)
	}
	var post int32
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() != mtproto.Predicate_updateNewChannelMessage {
			continue
		}
		m := u.GetMessage_MESSAGE()
		post = m.GetId()
		if m.GetViews().GetValue() != 1 || m.GetForwards() == nil || m.GetForwards().GetValue() != 0 {
			t.Errorf("the new post: views %v forwards %v, want 1 and 0", m.GetViews(), m.GetForwards())
		}
		if a := m.GetPostAuthor().GetValue(); a != "Alice E2E" {
			t.Errorf("the post's author %q, want Alice E2E", a)
		}
	}
	if post == 0 {
		t.Fatal("no updateNewChannelMessage in the answer")
	}

	views := func(u *user, increment *mtproto.Bool) (*mtproto.MessageViews, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetMessagesViews{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getMessagesViews),
			Peer:        peerOf(u), Id: []int32{post}, Increment: increment,
		})
		if err != nil {
			return nil, err
		}
		list := o.(interface {
			To_Messages_MessageViews() *mtproto.Messages_MessageViews
		}).To_Messages_MessageViews().GetViews()
		if len(list) != 1 {
			return nil, fmt.Errorf("views %v", list)
		}
		return list[0], nil
	}
	if v, err := views(bob, mtproto.BoolFalse); err != nil || v.GetViews().GetValue() != 1 || v.GetForwards() == nil {
		t.Errorf("before anyone viewed it, without increment: %v, %v", v, err)
	}
	// Android, iOS and Web K ask with increment for the posts on the screen.
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := views(bob, mtproto.BoolTrue); err != nil {
				t.Errorf("Bob's view: %v", err)
			}
		}()
	}
	wg.Wait()
	if v, err := views(bob, mtproto.BoolFalse); err != nil || v.GetViews().GetValue() != 2 {
		t.Errorf("after Bob's views: %v, %v, want 2", v, err)
	}
	if v, err := views(carol, mtproto.BoolTrue); err != nil || v.GetViews().GetValue() != 3 {
		t.Errorf("Carol's view: %v, %v, want 3", v, err)
	}
	if v, err := views(alice, mtproto.BoolTrue); err != nil || v.GetViews().GetValue() != 3 {
		t.Errorf("the author's view: %v, %v, want 3", v, err)
	}

	waitCounter := func(predicate string, want int32) {
		t.Helper()
		if _, err := alice.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
			for _, u := range us.GetUpdates() {
				if u.GetPredicateName() == predicate && u.GetChannelId() == ch.GetId() && u.GetId_INT32() == post &&
					(u.GetViews() == want || u.GetForwards() == want) {
					return true
				}
			}
			return false
		}); err != nil {
			t.Errorf("%s with %d: %v", predicate, want, err)
		}
	}
	waitCounter(mtproto.Predicate_updateChannelMessageViews, 3)

	o, err = bob.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peerOf(bob), Limit: 10,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	found := false
	for _, m := range o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages() {
		if m.GetId() == post {
			found = true
			if m.GetViews().GetValue() != 3 {
				t.Errorf("the post in the history: views %v, want 3", m.GetViews())
			}
		}
	}
	if !found {
		t.Error("the post is not in the history")
	}

	random := randomID()
	for range 2 {
		if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_forwardMessages),
			FromPeer:    peerOf(bob), Id: []int32{post}, RandomId: []int64{random},
			ToPeer: &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		}); err != nil {
			t.Fatalf("messages.forwardMessages: %v", err)
		}
	}
	if v, err := views(alice, mtproto.BoolFalse); err != nil || v.GetForwards().GetValue() != 1 {
		t.Errorf("a forward asked for twice: %v, %v, want 1", v, err)
	}
	waitCounter(mtproto.Predicate_updateChannelMessageForwards, 1)

	// The forward keeps the views it had, with its own forwards: one flag of
	// message carries both.
	o, err = bob.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerSelf)}, Limit: 1,
	})
	if err != nil {
		t.Fatalf("messages.getHistory of Saved Messages: %v", err)
	}
	saved := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages()
	if len(saved) != 1 || saved[0].GetFwdFrom() == nil || saved[0].GetViews().GetValue() != 3 ||
		saved[0].GetForwards() == nil || saved[0].GetForwards().GetValue() != 0 {
		t.Errorf("the forward in Saved Messages: %v, want 3 views and 0 forwards", saved)
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesEditMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_editMessage),
		Peer:        peerOf(alice), Id: post, Message: wrapperspb.String("edited"),
	})
	if err != nil {
		t.Fatalf("messages.editMessage: %v", err)
	}
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateEditChannelMessage {
			if m := u.GetMessage_MESSAGE(); m.GetViews().GetValue() != 3 || m.GetForwards().GetValue() != 1 {
				t.Errorf("the edited post: views %v forwards %v, want 3 and 1", m.GetViews(), m.GetForwards())
			}
		}
	}

	o, err = bob.c.Invoke(ctx, &mtproto.TLChannelsExportMessageLink{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_exportMessageLink),
		Channel:     inputOf(bob), Id: post,
	})
	if err != nil {
		t.Fatalf("channels.exportMessageLink: %v", err)
	}
	link := o.(interface {
		To_ExportedMessageLink() *mtproto.ExportedMessageLink
	}).To_ExportedMessageLink().GetLink()
	if want := fmt.Sprintf("/c/%d/%d", ch.GetId(), post); !strings.HasPrefix(link, "https://") || !strings.HasSuffix(link, want) {
		t.Errorf("the link %q, want one ending in %s", link, want)
	}
}

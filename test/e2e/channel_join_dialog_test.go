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

// TestChannelJoinDialog: a channel Bob joins is in his dialogs (the owner's
// report of 11.11: after joining, the channel is not in the chat list on
// Android), whether he joins a public channel by its username
// (channels.joinChannel) or a private one by its link
// (messages.importChatInvite), and whether it has posts.
func TestChannelJoinDialog(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)

	for _, c := range []struct {
		name  string
		posts bool
		link  bool
	}{
		{"public with posts", true, false},
		{"public without posts", false, false},
		{"private with posts", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, ch := createChannel(ctx, t, alice, c.name, false)
			hash := ch.GetAccessHash_FLAGINT64().GetValue()
			inputOf := func(u *user) *mtproto.InputChannel {
				return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
			}
			peerOf := func(u *user) *mtproto.InputPeer {
				return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
			}
			if c.posts {
				sendMessageID(ctx, t, alice, peerOf(alice), "a post before Bob", randomID())
			}
			if c.link {
				if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
					Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
					Hash:        channelInviteHash(ctx, t, alice, inputOf(alice)),
				}); err != nil {
					t.Fatalf("messages.importChatInvite: %v", err)
				}
			} else {
				if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{
					Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateUsername),
					Channel:     inputOf(alice),
					Username:    fmt.Sprintf("pluma_join_%d", ch.GetId()),
				}); err != nil {
					t.Fatalf("channels.updateUsername: %v", err)
				}
				if _, err := bob.c.Invoke(ctx, &mtproto.TLChannelsJoinChannel7F6A1E22{
					Constructor: bob.c.Constructor(mtproto.Predicate_channels_joinChannel7F6A1E22),
					Channel:     inputOf(bob),
				}); err != nil {
					t.Fatalf("channels.joinChannel: %v", err)
				}
			}

			o, err := bob.c.Invoke(ctx, &mtproto.TLMessagesGetDialogs{
				Constructor: bob.c.Constructor(mtproto.Predicate_messages_getDialogs),
				OffsetPeer:  &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
				Limit:       100,
			})
			if err != nil {
				t.Fatalf("messages.getDialogs: %v", err)
			}
			ds := o.(interface {
				To_Messages_Dialogs() *mtproto.Messages_Dialogs
			}).To_Messages_Dialogs()
			var dialog *mtproto.Dialog
			for _, d := range ds.GetDialogs() {
				if d.GetPeer().GetChannelId() == ch.GetId() {
					dialog = d
				}
			}
			if dialog == nil {
				t.Fatalf("the joined channel is not in Bob's dialogs (%d dialogs)", len(ds.GetDialogs()))
			}
			var top bool
			for _, m := range ds.GetMessages() {
				top = top || m.GetPeerId().GetChannelId() == ch.GetId() && m.GetId() == dialog.GetTopMessage()
			}
			if !top {
				t.Errorf("the dialog's top message %d is not among the messages", dialog.GetTopMessage())
			}
			var chat bool
			for _, x := range ds.GetChats() {
				chat = chat || x.GetId() == ch.GetId() && x.GetPredicateName() == mtproto.Predicate_channel && !x.GetLeft()
			}
			if !chat {
				t.Errorf("the channel is not among the chats as a member's")
			}
			if pd := peerDialogs(ctx, t, bob, peerOf(bob)).GetDialogs(); len(pd) != 1 {
				t.Errorf("messages.getPeerDialogs: %d dialogs", len(pd))
			}

			// Android learns of the new channel from updateChannel and asks
			// for its difference from pts 1 with limit 1; it makes the dialog
			// only from channelDifferenceTooLong (MessagesController.java:
			// 16531-16541, MessagesStorage.java:11328-11333 at aac1efcb). A
			// page from pts 1 put the channel's creation at the top of the new
			// dialog, and the channel fell out of the chat list (the owner's
			// report of 11.11).
			o, err = bob.c.Invoke(ctx, &mtproto.TLUpdatesGetChannelDifference{
				Constructor: bob.c.Constructor(mtproto.Predicate_updates_getChannelDifference),
				Force:       true,
				Channel:     inputOf(bob),
				Filter:      &mtproto.ChannelMessagesFilter{Constructor: bob.c.Constructor(mtproto.Predicate_channelMessagesFilterEmpty)},
				Pts:         1,
				Limit:       1,
			})
			if err != nil {
				t.Fatalf("updates.getChannelDifference: %v", err)
			}
			d := o.(interface {
				To_Updates_ChannelDifference() *mtproto.Updates_ChannelDifference
			}).To_Updates_ChannelDifference()
			// The updates before the join are not in Bob's box: the dialog
			// as it is, which Android makes the chat list's row of.
			if d.GetPredicateName() != mtproto.Predicate_updates_channelDifferenceTooLong {
				t.Fatalf("the difference from pts 1 after joining: %s, want channelDifferenceTooLong", d.GetPredicateName())
			}
			if d.GetDialog().GetPeer().GetChannelId() != ch.GetId() || !d.GetFinal() {
				t.Errorf("channelDifferenceTooLong: dialog %v, final %v", d.GetDialog(), d.GetFinal())
			}
			if c.posts && len(d.GetMessages()) == 0 {
				t.Error("channelDifferenceTooLong without the channel's last messages")
			}
		})
	}
}

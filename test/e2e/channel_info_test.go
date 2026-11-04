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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The creator renames a channel and changes its description (task 044):
// the rename is a post the subscribers get with updateChannel, the same
// title again is CHAT_NOT_MODIFIED, a subscriber may change neither, and
// the description shows in the full channel.
func TestChannelInfo(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)

	_, ch := createChannel(ctx, t, alice, "info", false)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        channelInviteHash(ctx, t, alice, input(alice)),
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}

	rename := func(u *user, title string) (*mtproto.Updates, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsEditTitle{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_editTitle),
			Channel:     input(u),
			Title:       title,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
	}
	r, err := rename(alice, "renamed")
	if err != nil {
		t.Fatalf("channels.editTitle: %v", err)
	}
	var post *mtproto.Message
	for _, u := range r.GetUpdates() {
		if m := u.GetMessage_MESSAGE(); m != nil {
			post = m
		}
	}
	if post.GetAction().GetPredicateName() != mtproto.Predicate_messageActionChatEditTitle || post.GetAction().GetTitle_STRING() != "renamed" || !post.GetPost() {
		t.Errorf("the rename's message %v", post)
	}
	if len(r.GetChats()) != 1 || r.GetChats()[0].GetTitle() != "renamed" {
		t.Errorf("the rename's chats %v", r.GetChats())
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateChannel, func(u *mtproto.Update) bool { return u.GetChannelId() == ch.GetId() })
	if _, err = rename(alice, "renamed"); !mtclient.IsRPCError(err, "CHAT_NOT_MODIFIED") {
		t.Errorf("the same title: %v, want CHAT_NOT_MODIFIED", err)
	}
	if _, err = rename(bob, "mine"); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a subscriber renames: %v, want CHAT_ADMIN_REQUIRED", err)
	}

	// The creator may edit a post of the channel: Android asks
	// messages.getMessageEditData before it opens the editor.
	postId := sendMessageID(ctx, t, alice, &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}, "a post", randomID())
	for _, c := range []struct {
		u    *user
		want string
	}{{alice, ""}, {bob, "CHAT_ADMIN_REQUIRED"}} {
		_, err := c.u.c.Invoke(ctx, &mtproto.TLMessagesGetMessageEditData{
			Constructor: c.u.c.Constructor(mtproto.Predicate_messages_getMessageEditData),
			Peer:        &mtproto.InputPeer{Constructor: c.u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash},
			Id:          postId,
		})
		if (c.want == "" && err != nil) || (c.want != "" && !mtclient.IsRPCError(err, c.want)) {
			t.Errorf("messages.getMessageEditData of a post by %s: %v, want %q", c.u.self.GetFirstName(), err, c.want)
		}
	}

	about := func(u *user, text string) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLMessagesEditChatAbout{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_editChatAbout),
			Peer:        &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash},
			About:       text,
		})
		return err
	}
	if err = about(alice, "what it is about"); err != nil {
		t.Fatalf("messages.editChatAbout: %v", err)
	}
	if err = about(bob, "not mine"); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a subscriber changes the description: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	o, err := bob.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_getFullChannel),
		Channel:     input(bob),
	})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	if got := o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat().GetAbout(); got != "what it is about" {
		t.Errorf("about %q", got)
	}
}

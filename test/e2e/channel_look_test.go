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
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestChannelLook (task 078) end to end: Alice gives her channel the
// color 3, a built-in one; the answer carries it, Bob, a subscriber, gets
// updateChannel and reads it, and may not change it; the same color again
// is CHAT_NOT_MODIFIED, as is the status the channel has (none). A status
// that is no custom emoji, or a collectible, is DOCUMENT_INVALID. The
// methods of more usernames reach the server (RPCFragment) and, with the
// one editable username, answer as Telegram does: nothing to show, hide or
// order, and deactivateAllUsernames is done, which tdesktop waits for when
// it makes a channel private.
func TestChannelLook(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	_, ch := createChannel(ctx, t, alice, "look", false)
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        channelInviteHash(ctx, t, alice, input(alice)),
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}
	getState(ctx, t, bob)

	color := func(u *user, forProfile bool, c *wrapperspb.Int32Value) (*mtproto.Updates, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsUpdateColor{
			Constructor:     u.c.Constructor(mtproto.Predicate_channels_updateColor),
			ForProfile:      forProfile,
			Channel:         input(u),
			Color_FLAGINT32: c,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
	}
	if _, err := color(bob, false, wrapperspb.Int32(3)); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a subscriber sets the color: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	r, err := color(alice, false, wrapperspb.Int32(3))
	if err != nil {
		t.Fatalf("channels.updateColor: %v", err)
	}
	if len(r.GetChats()) != 1 || r.GetChats()[0].GetColor_FLAGPEERCOLOR().GetColor().GetValue() != 3 {
		t.Errorf("the answer's channel: %v", r.GetChats())
	}
	waitUpdate(ctx, t, bob, mtproto.Predicate_updateChannel, func(u *mtproto.Update) bool { return u.GetChannelId() == ch.GetId() })
	o, err := bob.c.Invoke(ctx, &mtproto.TLChannelsGetChannels{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_getChannels),
		Id:          []*mtproto.InputChannel{input(bob)},
	})
	if err != nil {
		t.Fatalf("channels.getChannels: %v", err)
	}
	if chats := o.(interface {
		To_Messages_Chats() *mtproto.Messages_Chats
	}).To_Messages_Chats().GetChats(); len(chats) != 1 ||
		chats[0].GetColor_FLAGPEERCOLOR().GetColor().GetValue() != 3 {
		t.Errorf("Bob reads the channel: %v", chats)
	}
	if _, err = color(alice, false, wrapperspb.Int32(3)); !mtclient.IsRPCError(err, "CHAT_NOT_MODIFIED") {
		t.Errorf("the same color: %v, want CHAT_NOT_MODIFIED", err)
	}
	if _, err = color(alice, false, wrapperspb.Int32(-5)); !mtclient.IsRPCError(err, "COLOR_INVALID") {
		t.Errorf("a color that is none: %v, want COLOR_INVALID", err)
	}

	status := func(s *mtproto.EmojiStatus) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateEmojiStatus{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateEmojiStatus),
			Channel:     input(alice),
			EmojiStatus: s,
		})
		return err
	}
	if err = status(mtproto.MakeTLEmojiStatusEmpty(nil).To_EmojiStatus()); !mtclient.IsRPCError(err, "CHAT_NOT_MODIFIED") {
		t.Errorf("no status for a channel without one: %v, want CHAT_NOT_MODIFIED", err)
	}
	if err = status(mtproto.MakeTLEmojiStatus(&mtproto.EmojiStatus{DocumentId: 1}).To_EmojiStatus()); !mtclient.IsRPCError(err, "DOCUMENT_INVALID") {
		t.Errorf("a status of no custom emoji: %v, want DOCUMENT_INVALID", err)
	}
	if err = status(mtproto.MakeTLInputEmojiStatusCollectible(&mtproto.EmojiStatus{CollectibleId: 1}).To_EmojiStatus()); !mtclient.IsRPCError(err, "DOCUMENT_INVALID") {
		t.Errorf("a collectible status: %v, want DOCUMENT_INVALID", err)
	}

	name := fmt.Sprintf("pluma_look_%d", ch.GetId())
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateUsername),
		Channel:     input(alice),
		Username:    name,
	}); err != nil {
		t.Fatalf("channels.updateUsername: %v", err)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsToggleUsername{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_toggleUsername),
		Channel:     input(alice), Username: name, Active: mtproto.BoolTrue,
	}); !mtclient.IsRPCError(err, "USERNAME_NOT_MODIFIED") {
		t.Errorf("showing the shown link: %v, want USERNAME_NOT_MODIFIED", err)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsReorderUsernames{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_reorderUsernames),
		Channel:     input(alice), Order: []string{name},
	}); !mtclient.IsRPCError(err, "CHAT_NOT_MODIFIED") {
		t.Errorf("the order of one link, as tdesktop sends it: %v, want CHAT_NOT_MODIFIED", err)
	}
	deactivate := func(u *user) error {
		_, err := u.c.Invoke(ctx, &mtproto.TLChannelsDeactivateAllUsernames{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_deactivateAllUsernames),
			Channel:     input(u),
		})
		return err
	}
	if err = deactivate(bob); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a subscriber: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	if err = deactivate(alice); err != nil {
		t.Errorf("channels.deactivateAllUsernames with nothing to hide: %v", err)
	}
}

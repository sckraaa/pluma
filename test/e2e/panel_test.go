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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// What the panel changes for the clients (task 072), switched through
// pluma-admin, which writes the same state by the same RPC: the registration
// mode that auth.sendCode reads and the block of a channel.

// TestRegistrationSwitch: a number off the list is refused until
// registration opens, and after it goes back to the list.
func TestRegistrationSwitch(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]

	// An open registration affects the whole run, so back to the list at
	// the end.
	defer admin(t, "registration", "list")

	sendCodeFails(ctx, t, phone, "PHONE_NUMBER_INVALID")
	admin(t, "registration", "open")

	c, err := mtclient.Dial(ctx, config(t, "e2e registration"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	closeChecked(t, "registration", c)
	sendCode := &mtproto.TLAuthSendCode{
		Constructor: c.Constructor(mtproto.Predicate_auth_sendCode),
		PhoneNumber: phone,
		ApiId:       1,
		ApiHash:     "e2e",
		Settings:    &mtproto.CodeSettings{Constructor: c.Constructor(mtproto.Predicate_codeSettings)},
	}
	o, err := c.Invoke(ctx, sendCode)
	if e := (*mtclient.RPCError)(nil); errors.As(err, &e) && strings.HasPrefix(e.Message, "FLOOD_WAIT_") {
		resetLoginsPerAddress(t)
		o, err = c.Invoke(ctx, sendCode)
	}
	if err != nil {
		t.Fatalf("auth.sendCode with open registration: %v", err)
	}
	if _, ok := o.(*mtproto.TLAuthSentCode); !ok {
		t.Fatalf("auth.sendCode returned %T", o)
	}

	admin(t, "registration", "list")
	sendCodeFails(ctx, t, phone, "PHONE_NUMBER_INVALID")
}

// TestChannelBlockedByOwner: a blocked channel is restricted for its members
// and takes no messages; an unblock brings it back.
func TestChannelBlockedByOwner(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	_, ch := createChannel(ctx, t, alice, "blocked channel", true)
	peer := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	input := &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	sendTo(ctx, t, alice, peer, "before the block")

	id := fmt.Sprint(ch.GetId())
	admin(t, "moderation", "block-channel", id)
	defer admin(t, "moderation", "unblock-channel", id)

	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer,
		Message:     "during the block",
		RandomId:    randomID(),
	}); !mtclient.IsRPCError(err, "CHANNEL_PRIVATE") {
		t.Fatalf("send to a blocked channel: %v, want CHANNEL_PRIVATE", err)
	}

	restricted := func() bool {
		o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsGetChannels{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_getChannels),
			Id:          []*mtproto.InputChannel{input},
		})
		if err != nil {
			t.Fatalf("channels.getChannels: %v", err)
		}
		got := o.(interface {
			To_Messages_Chats() *mtproto.Messages_Chats
		}).To_Messages_Chats().GetChats()
		if len(got) != 1 {
			t.Fatalf("channels.getChannels: %d chats", len(got))
		}
		c := got[0]
		if c.GetRestricted() && (len(c.GetRestrictionReason()) != 1 || c.GetRestrictionReason()[0].GetPlatform() != "all") {
			t.Fatalf("restriction reason: %v", c.GetRestrictionReason())
		}
		return c.GetRestricted()
	}
	if !restricted() {
		t.Error("the channel is not restricted")
	}

	admin(t, "moderation", "unblock-channel", id)
	if restricted() {
		t.Error("the channel is still restricted")
	}
	sendTo(ctx, t, alice, peer, "after the unblock")
}

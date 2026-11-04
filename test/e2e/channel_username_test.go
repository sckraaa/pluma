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
)

// A channel made public as Android makes it (task 044): the probe with no
// channel and "1" is USERNAME_INVALID, not a limit; a free link is true and
// is set; the link resolves to the channel for another user, who joins by
// it and finds it taken; the channel's boosts are none, without an error
// (Android shows one on every open, ChatActivity.java:22397).
func TestChannelUsername(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")

	_, ch := createChannel(ctx, t, alice, "public", false)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	check := func(u *user, channel *mtproto.InputChannel, name string) (bool, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLChannelsCheckUsername{
			Constructor: u.c.Constructor(mtproto.Predicate_channels_checkUsername),
			Channel:     channel,
			Username:    name,
		})
		if err != nil {
			return false, err
		}
		b, _ := o.(interface{ To_Bool() *mtproto.Bool })
		return b != nil && mtproto.FromBool(b.To_Bool()), nil
	}
	empty := &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannelEmpty)}
	if _, err := check(alice, empty, "1"); !mtclient.IsRPCError(err, "USERNAME_INVALID") {
		t.Errorf("the probe of the creation screen: %v, want USERNAME_INVALID", err)
	}
	name := fmt.Sprintf("pluma_e2e_%d", ch.GetId())
	if free, err := check(alice, input(alice), name); err != nil || !free {
		t.Fatalf("a free link: %v, %v", free, err)
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateUsername),
		Channel:     input(alice),
		Username:    name,
	}); err != nil {
		t.Fatalf("channels.updateUsername: %v", err)
	}

	o, err := bob.c.Invoke(ctx, &mtproto.TLContactsResolveUsername{
		Constructor: bob.c.Constructor(mtproto.Predicate_contacts_resolveUsername),
		Username:    name,
	})
	if err != nil {
		t.Fatalf("contacts.resolveUsername of a channel: %v", err)
	}
	r := o.(interface {
		To_Contacts_ResolvedPeer() *mtproto.Contacts_ResolvedPeer
	}).To_Contacts_ResolvedPeer()
	if r.GetPeer().GetChannelId() != ch.GetId() || len(r.GetChats()) != 1 || r.GetChats()[0].GetUsername().GetValue() != name {
		t.Fatalf("resolved %v, chats %v", r.GetPeer(), r.GetChats())
	}
	bobHash := r.GetChats()[0].GetAccessHash_FLAGINT64().GetValue()
	if _, err = bob.c.Invoke(ctx, &mtproto.TLChannelsJoinChannel7F6A1E22{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_joinChannel7F6A1E22),
		Channel:     &mtproto.InputChannel{Constructor: bob.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: bobHash},
	}); err != nil {
		t.Errorf("joining the public channel by its name: %v", err)
	}
	if free, err := check(bob, &mtproto.InputChannel{Constructor: bob.c.Constructor(mtproto.Predicate_inputChannelEmpty)}, name); err != nil || free {
		t.Errorf("another's link: %v, %v (want false)", free, err)
	}

	o, err = bob.c.Invoke(ctx, &mtproto.TLPremiumGetBoostsStatus{
		Constructor: bob.c.Constructor(mtproto.Predicate_premium_getBoostsStatus),
		Peer:        &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: bobHash},
	})
	if err != nil {
		t.Fatalf("premium.getBoostsStatus: %v", err)
	}
	if s := o.(interface {
		To_Premium_BoostsStatus() *mtproto.Premium_BoostsStatus
	}).To_Premium_BoostsStatus(); s.GetBoosts() != 0 || s.GetBoostUrl() == "" {
		t.Errorf("boosts %d, url %q", s.GetBoosts(), s.GetBoostUrl())
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLPremiumGetMyBoosts{Constructor: bob.c.Constructor(mtproto.Predicate_premium_getMyBoosts)}); err != nil {
		t.Errorf("premium.getMyBoosts: %v", err)
	}
}

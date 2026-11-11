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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

func channelRecommendations(ctx context.Context, t *testing.T, u *user, channel *mtproto.InputChannel) *mtproto.Messages_Chats {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetChannelRecommendations{
		Constructor: u.c.Constructor(mtproto.Predicate_channels_getChannelRecommendations),
		Channel:     channel,
	})
	if err != nil {
		t.Fatalf("channels.getChannelRecommendations: %v", err)
	}
	r, ok := o.(interface {
		To_Messages_Chats() *mtproto.Messages_Chats
	})
	if !ok {
		t.Fatalf("channels.getChannelRecommendations returned %T", o)
	}
	return r.To_Messages_Chats()
}

func chatIds(chats []*mtproto.Chat) []int64 {
	ids := make([]int64, 0, len(chats))
	for _, c := range chats {
		ids = append(ids, c.GetId())
	}
	return ids
}

// Similar channels (task 081, ADR 0047): under a channel Dave reads, the
// public channels its readers are in, the one most of them read first, each
// as a channel Dave has not joined; a private channel is not offered. For
// the account the same two come first.
func TestChannelRecommendations(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 4)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	dave := login(ctx, t, phones[3], "Dave")

	input := func(u *user, ch *mtproto.Chat) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	}
	digits := strings.TrimPrefix(phones[0], "+")
	public := func(name string) *mtproto.Chat {
		_, ch := createChannel(ctx, t, alice, fmt.Sprintf("%s %d", name, randomID()), false)
		if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateUsername),
			Channel:     input(alice, ch),
			Username:    "rec" + name + digits,
		}); err != nil {
			t.Fatalf("channels.updateUsername: %v", err)
		}
		return ch
	}
	join := func(u *user, chs ...*mtproto.Chat) {
		for _, ch := range chs {
			if _, err := u.c.Invoke(ctx, &mtproto.TLChannelsJoinChannel7F6A1E22{
				Constructor: u.c.Constructor(mtproto.Predicate_channels_joinChannel7F6A1E22),
				Channel:     input(u, ch),
			}); err != nil {
				t.Fatalf("channels.joinChannel: %v", err)
			}
		}
	}

	source, two, one := public("src"), public("two"), public("one")
	_, private := createChannel(ctx, t, alice, fmt.Sprintf("private %d", randomID()), false)
	join(bob, source, two, one)
	join(carol, source, two)
	join(dave, source)

	similar := channelRecommendations(ctx, t, dave, input(dave, source))
	if similar.GetPredicateName() != mtproto.Predicate_messages_chats {
		t.Errorf("%s, want messages.chats", similar.GetPredicateName())
	}
	want := []int64{two.GetId(), one.GetId()}
	if got := chatIds(similar.GetChats()); !slices.Equal(got, want) {
		t.Fatalf("like the source: %v, want %v (private %d)", got, want, private.GetId())
	}
	for _, c := range similar.GetChats() {
		if c.GetPredicateName() != mtproto.Predicate_channel || !c.GetLeft() || c.GetAccessHash_FLAGINT64() == nil || c.GetParticipantsCount_FLAGINT32() == nil {
			t.Errorf("recommended %v: want a channel Dave left, with its access_hash and size", c)
		}
	}

	if got := chatIds(channelRecommendations(ctx, t, dave, nil).GetChats()); len(got) < 2 || !slices.Equal(got[:2], want) {
		t.Errorf("for the account: %v, want %v first", got, want)
	}
	if got := chatIds(channelRecommendations(ctx, t, dave, nil).GetChats()); slices.Contains(got, source.GetId()) || slices.Contains(got, private.GetId()) {
		t.Errorf("for the account: %v offers its own or a private channel", got)
	}

	// A private channel Dave is not in is not his to ask about.
	if _, err := dave.c.Invoke(ctx, &mtproto.TLChannelsGetChannelRecommendations{
		Constructor: dave.c.Constructor(mtproto.Predicate_channels_getChannelRecommendations),
		Channel:     input(dave, private),
	}); !rpcErrorIs(err, "CHANNEL_PRIVATE") {
		t.Errorf("a private channel of others: %v, want CHANNEL_PRIVATE", err)
	}
}

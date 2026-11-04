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

// channelDifference is u's updates.getChannelDifference of ch from pts.
func channelDifference(ctx context.Context, u *user, ch *mtproto.Chat, accessHash int64, pts, limit int32) (*mtproto.Updates_ChannelDifference, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLUpdatesGetChannelDifference{
		Constructor: u.c.Constructor(mtproto.Predicate_updates_getChannelDifference),
		Channel:     &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: accessHash},
		Filter:      &mtproto.ChannelMessagesFilter{Constructor: u.c.Constructor(mtproto.Predicate_channelMessagesFilterEmpty)},
		Pts:         pts,
		Limit:       limit,
	})
	if err != nil {
		return nil, err
	}
	return o.(interface {
		To_Updates_ChannelDifference() *mtproto.Updates_ChannelDifference
	}).To_Updates_ChannelDifference(), nil
}

// A device that missed 150 posts catches up (task 040): updates.getDifference
// marks the channel with updateChannelTooLong and its pts, and pages of
// updates.getChannelDifference reach the final answer, each moving pts
// forward, the final one with a timeout; nothing more after it. A wrong
// access_hash is CHANNEL_INVALID, one not in a private channel gets
// CHANNEL_PRIVATE.
func TestChannelDifference(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	st := getState(ctx, t, laptop)

	_, ch := createChannel(ctx, t, alice, "catch up", false)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	peer := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
	const posts = 150
	for i := range posts {
		if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        peer,
			Message:     fmt.Sprintf("post %d", i),
			RandomId:    randomID(),
		}); err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
	}
	// The creation (pts 2, message 1) and the posts.
	const lastPts, lastId = 2 + posts, 1 + posts

	d := differenceOnce(ctx, t, laptop, st)
	var marked bool
	for _, u := range d.GetOtherUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateChannelTooLong && u.GetChannelId() == ch.GetId() {
			marked = true
			if u.GetPts_FLAGINT32().GetValue() != lastPts {
				t.Errorf("updateChannelTooLong pts %v, want %d", u.GetPts_FLAGINT32(), lastPts)
			}
		}
	}
	if !marked {
		t.Fatalf("updates.getDifference %s without updateChannelTooLong of the channel", d.GetPredicateName())
	}
	var inChats bool
	for _, c := range d.GetChats() {
		inChats = inChats || c.GetId() == ch.GetId()
	}
	if !inChats {
		t.Error("the channel is not in the difference's chats")
	}

	var (
		pts  = int32(1)
		seen = map[int32]bool{}
	)
	for page := 0; ; page++ {
		if page > 3 {
			t.Fatal("no final answer in 4 pages")
		}
		cd, err := channelDifference(ctx, laptop, ch, hash, pts, 100)
		if err != nil {
			t.Fatalf("updates.getChannelDifference from %d: %v", pts, err)
		}
		if cd.GetPredicateName() != mtproto.Predicate_updates_channelDifference {
			t.Fatalf("page %d: %s", page, cd.GetPredicateName())
		}
		if cd.GetPts() <= pts {
			t.Fatalf("page %d: pts %d does not move from %d", page, cd.GetPts(), pts)
		}
		pts = cd.GetPts()
		for _, m := range cd.GetNewMessages() {
			seen[m.GetId()] = true
		}
		if len(cd.GetChats()) == 0 {
			t.Errorf("page %d without the channel in chats", page)
		}
		if cd.GetFinal() {
			if cd.GetTimeout().GetValue() <= 0 {
				t.Errorf("final answer with timeout %v", cd.GetTimeout())
			}
			break
		}
	}
	if pts != lastPts || len(seen) != lastId {
		t.Errorf("caught up to pts %d with %d messages, want %d and %d", pts, len(seen), lastPts, lastId)
	}
	cd, err := channelDifference(ctx, laptop, ch, hash, pts, 100)
	if err != nil {
		t.Fatal(err)
	}
	if cd.GetPredicateName() != mtproto.Predicate_updates_channelDifferenceEmpty || cd.GetPts() != lastPts || cd.GetTimeout().GetValue() <= 0 {
		t.Errorf("caught up: %s pts %d timeout %v", cd.GetPredicateName(), cd.GetPts(), cd.GetTimeout())
	}

	if _, err = channelDifference(ctx, laptop, ch, hash+1, pts, 100); !mtclient.IsRPCError(err, "CHANNEL_INVALID") {
		t.Errorf("wrong access_hash: %v, want CHANNEL_INVALID", err)
	}
	if _, err = channelDifference(ctx, bob, ch, hash, 1, 100); !mtclient.IsRPCError(err, "CHANNEL_PRIVATE") {
		t.Errorf("not in the channel: %v, want CHANNEL_PRIVATE", err)
	}
}

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
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func geoPointAt(lat float64) *mtproto.InputGeoPoint {
	return mtproto.MakeTLInputGeoPoint(&mtproto.InputGeoPoint{Lat: lat, Long: 37.62}).To_InputGeoPoint()
}

func liveLocationMedia(lat float64, period int32) *mtproto.InputMedia {
	return mtproto.MakeTLInputMediaGeoLive(&mtproto.InputMedia{GeoPoint: geoPointAt(lat), Period: wrapperspb.Int32(period)}).To_InputMedia()
}

func recentLocations(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, hash int64) *mtproto.Messages_Messages {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetRecentLocations{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getRecentLocations),
		Peer:        peer,
		Limit:       100,
		Hash:        hash,
	})
	if err != nil {
		t.Fatalf("messages.getRecentLocations: %v", err)
	}
	return messagesOf(t, o)
}

// locationsOf is who shares where in r: the sender's user id and the
// latitude of each message, in order.
func locationsOf(r *mtproto.Messages_Messages) string {
	out := ""
	for _, m := range r.GetMessages() {
		out += fmt.Sprintf("[%d %.0f]", m.GetFromId().GetUserId(), m.GetMedia().GetGeo().GetLat())
	}
	return out
}

// recentLocationsUntil waits until u's answer for peer is want: the copies
// of a basic group's members arrive after the sender's answer.
func recentLocationsUntil(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, want string) *mtproto.Messages_Messages {
	t.Helper()
	var r *mtproto.Messages_Messages
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if r = recentLocations(ctx, t, u, peer, 0); locationsOf(r) == want {
			return r
		}
	}
	t.Fatalf("recent locations %s, want %s", locationsOf(r), want)
	return nil
}

// recentLocationsHashOf is the hash of an answer as core.telegram.org/api/
// offsets counts it: id and edit_date of each message.
func recentLocationsHashOf(r *mtproto.Messages_Messages) int64 {
	var h uint64
	for _, m := range r.GetMessages() {
		for _, v := range []int64{int64(m.GetId()), int64(m.GetEditDate().GetValue())} {
			h ^= h >> 21
			h ^= h << 35
			h ^= h >> 4
			h += uint64(v)
		}
	}
	return int64(h)
}

// editedLocation edits u's live location id in peer and returns its media
// as the answer gives it.
func editedLocation(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, id int32, media *mtproto.InputMedia) *mtproto.MessageMedia {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesEditMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_editMessage),
		Peer:        peer,
		Id:          id,
		Media:       media,
	})
	if err != nil {
		t.Fatalf("messages.editMessage: %v", err)
	}
	for _, up := range updatesOf(t, "messages.editMessage", o).GetUpdates() {
		switch up.GetPredicateName() {
		case mtproto.Predicate_updateEditMessage, mtproto.Predicate_updateEditChannelMessage:
			return up.GetMessage_MESSAGE().GetMedia()
		}
	}
	t.Fatalf("messages.editMessage answered without the message")
	return nil
}

// TestRecentLocations: Alice (twice) and Bob share live locations in a
// group, Carl a plain one; the map of the group has one location per
// sender, the newest first, and the hash of the answer gives
// messages.messagesNotModified. Alice moves hers as Android does (no
// period): it stays live, the answer changes; she stops it as Android does
// (no point): the point stays, the location ends, and her earlier one,
// still live, takes its place. The same in a supergroup; a broadcast
// channel has none.
func TestRecentLocations(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carl := login(ctx, t, phones[2], "Carl")
	aliceId, bobId := alice.self.GetId(), bob.self.GetId()

	check := func(t *testing.T, peer func(u *user) *mtproto.InputPeer, channel bool) {
		sendMedia(ctx, t, alice, peer(alice), liveLocationMedia(10, 900))
		second := sendMedia(ctx, t, alice, peer(alice), liveLocationMedia(20, 900))
		sendMedia(ctx, t, bob, peer(bob), liveLocationMedia(30, 3600))
		sendMedia(ctx, t, carl, peer(carl), mtproto.MakeTLInputMediaGeoPoint(&mtproto.InputMedia{GeoPoint: geoPointAt(40)}).To_InputMedia())

		want := fmt.Sprintf("[%d 30][%d 20]", bobId, aliceId)
		r := recentLocationsUntil(ctx, t, carl, peer(carl), want)
		wantType := mtproto.Predicate_messages_messages
		if channel {
			wantType = mtproto.Predicate_messages_channelMessages
		}
		if r.GetPredicateName() != wantType || (channel && r.GetPts() == 0) {
			t.Errorf("the answer is %s with pts %d, want %s", r.GetPredicateName(), r.GetPts(), wantType)
		}
		if len(r.GetUsers()) < 2 {
			t.Errorf("the answer has %d users", len(r.GetUsers()))
		}
		hash := recentLocationsHashOf(r)
		if same := recentLocations(ctx, t, carl, peer(carl), hash); same.GetPredicateName() != mtproto.Predicate_messages_messagesNotModified || same.GetCount() != 2 {
			t.Errorf("with the hash: %s count %d, want messages.messagesNotModified count 2", same.GetPredicateName(), same.GetCount())
		}

		// Android moves the location without the period.
		moved := editedLocation(ctx, t, alice, peer(alice), second, mtproto.MakeTLInputMediaGeoLive(&mtproto.InputMedia{
			GeoPoint: geoPointAt(21), Heading: wrapperspb.Int32(90),
		}).To_InputMedia())
		if moved.GetPeriod() != 900 || moved.GetGeo().GetLat() != 21 || moved.GetHeading().GetValue() != 90 {
			t.Errorf("the moved location: %v", moved)
		}
		want = fmt.Sprintf("[%d 30][%d 21]", bobId, aliceId)
		recentLocationsUntil(ctx, t, carl, peer(carl), want)
		if again := recentLocations(ctx, t, carl, peer(carl), hash); again.GetPredicateName() == mtproto.Predicate_messages_messagesNotModified {
			t.Error("the hash from before the edit gives messages.messagesNotModified")
		}

		// Android stops it with an empty point.
		stopped := editedLocation(ctx, t, alice, peer(alice), second, mtproto.MakeTLInputMediaGeoLive(&mtproto.InputMedia{
			Stopped: true, GeoPoint: mtproto.MakeTLInputGeoPointEmpty(nil).To_InputGeoPoint(),
		}).To_InputMedia())
		if stopped.GetGeo().GetPredicateName() != mtproto.Predicate_geoPoint || stopped.GetGeo().GetLat() != 21 || stopped.GetPeriod() >= 900 {
			t.Errorf("the stopped location: %v", stopped)
		}
		recentLocationsUntil(ctx, t, carl, peer(carl), fmt.Sprintf("[%d 30][%d 10]", bobId, aliceId))
	}

	t.Run("group", func(t *testing.T) {
		chatId := createChat(ctx, t, alice, "map", bob, carl)
		check(t, func(u *user) *mtproto.InputPeer {
			return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChat), ChatId: chatId}
		}, false)
	})
	t.Run("supergroup", func(t *testing.T) {
		_, ch := createChannel(ctx, t, alice, "map", true)
		_, input := channelInputs(alice, ch)
		inviteToChannel(ctx, t, alice, input, bob, carl)
		check(t, func(u *user) *mtproto.InputPeer { p, _ := channelInputs(u, ch); return p }, true)
	})
	t.Run("channel", func(t *testing.T) {
		_, ch := createChannel(ctx, t, alice, "map posts", false)
		peer, _ := channelInputs(alice, ch)
		sendMedia(ctx, t, alice, peer, liveLocationMedia(50, 900))
		r := recentLocations(ctx, t, alice, peer, 0)
		if r.GetPredicateName() != mtproto.Predicate_messages_channelMessages || len(r.GetMessages()) != 0 {
			t.Errorf("a broadcast channel: %s with %s", r.GetPredicateName(), locationsOf(r))
		}
	})
}

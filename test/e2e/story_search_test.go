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
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// storyArea is the place of an area on the picture, which every area of a
// story has.
func storyArea() *mtproto.MediaAreaCoordinates {
	return mtproto.MakeTLMediaAreaCoordinates(&mtproto.MediaAreaCoordinates{X: 50, Y: 50, W: 20, H: 10}).To_MediaAreaCoordinates()
}

func venueArea(provider, id string) *mtproto.MediaArea {
	return mtproto.MakeTLMediaAreaVenue(&mtproto.MediaArea{
		Coordinates:    storyArea(),
		Geo:            mtproto.MakeTLGeoPoint(&mtproto.GeoPoint{Lat: 52.52, Long: 13.40}).To_GeoPoint(),
		Title:          "Cafe",
		Address_STRING: "Main 1",
		Provider:       provider,
		VenueId:        id,
		VenueType:      "food",
	}).To_MediaArea()
}

// geoArea is a geo point with an address of country and city, without one
// when both are empty.
func geoArea(country, city string) *mtproto.MediaArea {
	area := &mtproto.MediaArea{
		Coordinates: storyArea(),
		Geo:         mtproto.MakeTLGeoPoint(&mtproto.GeoPoint{Lat: 52.52, Long: 13.40}).To_GeoPoint(),
	}
	if country != "" || city != "" {
		area.Address_FLAGGEOPOINTADDRESS = mtproto.MakeTLGeoPointAddress(&mtproto.GeoPointAddress{
			CountryIso2: country,
			City:        wrapperspb.String(city),
		}).To_GeoPointAddress()
	}
	return mtproto.MakeTLMediaAreaGeoPoint(area).To_MediaArea()
}

// sendAreaStory posts a photo story of u to peer with areas and returns
// its id.
func sendAreaStory(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, caption string, areas []*mtproto.MediaArea, rules ...*mtproto.InputPrivacyRule) int32 {
	t.Helper()
	randomId := randomID()
	o, err := u.c.Invoke(ctx, &mtproto.TLStoriesSendStory{
		Constructor:  u.c.Constructor(mtproto.Predicate_stories_sendStory),
		Peer:         peer,
		Media:        uploadPhoto(ctx, t, u),
		MediaAreas:   areas,
		Caption:      mtproto.MakeFlagsString(caption),
		PrivacyRules: rules,
		RandomId:     randomId,
	})
	if err != nil {
		t.Fatalf("stories.sendStory: %v", err)
	}
	for _, x := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateStoryID && x.GetRandomId() == randomId {
			return x.GetId_INT32()
		}
	}
	t.Fatalf("stories.sendStory: no updateStoryID")
	return 0
}

// storySearch is one stories.searchPosts: by hashtag when area is nil.
type storySearch struct {
	hashtag string
	area    *mtproto.MediaArea
	peer    *mtproto.InputPeer
	offset  string
	limit   int32
}

func searchStories(ctx context.Context, u *user, q storySearch) (*mtproto.Stories_FoundStories, error) {
	req := &mtproto.TLStoriesSearchPosts{
		Constructor: u.c.Constructor(mtproto.Predicate_stories_searchPosts),
		Area:        q.area,
		Peer:        q.peer,
		Offset:      q.offset,
		Limit:       q.limit,
	}
	if q.area == nil {
		req.Hashtag = mtproto.MakeFlagsString(q.hashtag)
	}
	if req.Limit == 0 {
		req.Limit = 100
	}
	o, err := u.c.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	return o.(interface {
		To_Stories_FoundStories() *mtproto.Stories_FoundStories
	}).To_Stories_FoundStories(), nil
}

// foundKey names one found story: its peer and id.
type foundKey struct {
	peer string
	id   int32
}

func foundKeys(r *mtproto.Stories_FoundStories) []foundKey {
	var out []foundKey
	for _, s := range r.GetStories() {
		p := s.GetPeer()
		key := foundKey{id: s.GetStory().GetId()}
		switch p.GetPredicateName() {
		case mtproto.Predicate_peerUser:
			key.peer = fmt.Sprintf("u%d", p.GetUserId())
		case mtproto.Predicate_peerChannel:
			key.peer = fmt.Sprintf("c%d", p.GetChannelId())
		}
		out = append(out, key)
	}
	return out
}

func userStory(u *user, id int32) foundKey {
	return foundKey{peer: fmt.Sprintf("u%d", u.self.GetId()), id: id}
}

func channelStory(ch *mtproto.Chat, id int32) foundKey {
	return foundKey{peer: fmt.Sprintf("c%d", ch.GetId()), id: id}
}

// sameFound tells whether got holds exactly want, in any order.
func sameFound(got, want []foundKey) bool {
	if len(got) != len(want) {
		return false
	}
	for _, k := range want {
		if !slices.Contains(got, k) {
			return false
		}
	}
	return true
}

// expectFound runs q as u and checks that it finds exactly want, in any
// order, with count and the owners it refers to.
func expectFound(ctx context.Context, t *testing.T, u *user, name string, q storySearch, want ...foundKey) *mtproto.Stories_FoundStories {
	t.Helper()
	r, err := searchStories(ctx, u, q)
	if err != nil {
		t.Errorf("%s: stories.searchPosts: %v", name, err)
		return nil
	}
	if got := foundKeys(r); !sameFound(got, want) || int(r.GetCount()) != len(want) || r.GetNextOffset() != nil {
		t.Errorf("%s: found %v (count %d, next_offset %v), want %v", name, got, r.GetCount(), r.GetNextOffset(), want)
	}
	for _, s := range r.GetStories() {
		p := s.GetPeer()
		switch p.GetPredicateName() {
		case mtproto.Predicate_peerUser:
			if !slices.ContainsFunc(r.GetUsers(), func(x *mtproto.User) bool { return x.GetId() == p.GetUserId() }) {
				t.Errorf("%s: user %d of a story is not in users", name, p.GetUserId())
			}
		case mtproto.Predicate_peerChannel:
			if !slices.ContainsFunc(r.GetChats(), func(x *mtproto.Chat) bool { return x.GetId() == p.GetChannelId() }) {
				t.Errorf("%s: channel %d of a story is not in chats", name, p.GetChannelId())
			}
		}
	}
	return r
}

// The search of stories (task 104): a hashtag, a cashtag, a venue and a
// city find the public stories only, peer narrows the search, pages follow
// next_offset, a public channel is found and a private one is not, and one
// the owner blocked finds none of the owner's stories.
func TestStorySearch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	aliceForBob := peerOf(ctx, t, bob, alice)

	// The tags and places are this run's own: the index is shared with
	// every other test of the stand.
	run := fmt.Sprintf("%d", uint32(randomID()))
	tag := "кот" + run
	cash := "T" + run
	venueId := "venue" + run
	city := "Searchtown" + run
	everyone := storyRule(mtproto.Predicate_inputPrivacyValueAllowAll)
	contacts := storyRule(mtproto.Predicate_inputPrivacyValueAllowContacts)

	public := sendAreaStory(ctx, t, alice, selfPeer(), "Hi #"+tag+" $"+cash, []*mtproto.MediaArea{venueArea("foursquare", venueId)}, everyone)
	sendAreaStory(ctx, t, alice, selfPeer(), "#"+tag, nil, contacts)
	geo := sendAreaStory(ctx, t, alice, selfPeer(), "", []*mtproto.MediaArea{geoArea("DE", city)}, everyone)

	// Bob is no contact of Alice: the story for her contacts is not found.
	expectFound(ctx, t, bob, "the tag without its sign", storySearch{hashtag: tag}, userStory(alice, public))
	expectFound(ctx, t, bob, "the tag with its sign", storySearch{hashtag: "#" + tag}, userStory(alice, public))
	expectFound(ctx, t, bob, "the tag in upper case", storySearch{hashtag: "#КОТ" + run}, userStory(alice, public))
	expectFound(ctx, t, bob, "the cashtag", storySearch{hashtag: "$" + cash}, userStory(alice, public))
	expectFound(ctx, t, bob, "the tag as a cashtag", storySearch{hashtag: "$" + tag})
	expectFound(ctx, t, bob, "the venue", storySearch{area: venueArea("foursquare", venueId)}, userStory(alice, public))
	expectFound(ctx, t, bob, "another venue", storySearch{area: venueArea("foursquare", venueId+"x")})
	expectFound(ctx, t, bob, "the city", storySearch{area: geoArea("DE", city)}, userStory(alice, geo))
	expectFound(ctx, t, bob, "the city in another country", storySearch{area: geoArea("FR", city)})
	// The owner finds her own public story, and not the one for contacts.
	expectFound(ctx, t, alice, "the tag of her own", storySearch{hashtag: tag}, userStory(alice, public))

	// peer narrows the search to one owner.
	expectFound(ctx, t, bob, "of Alice", storySearch{hashtag: tag, peer: aliceForBob}, userStory(alice, public))
	expectFound(ctx, t, bob, "of Bob", storySearch{hashtag: tag, peer: selfPeer()})

	// Two pages of one story each: the newer first.
	newer := sendAreaStory(ctx, t, alice, selfPeer(), "more #"+tag, nil, everyone)
	first, err := searchStories(ctx, bob, storySearch{hashtag: tag, peer: aliceForBob, limit: 1})
	if err != nil {
		t.Fatalf("the first page: %v", err)
	}
	if got := foundKeys(first); !slices.Equal(got, []foundKey{userStory(alice, newer)}) || first.GetCount() != 2 || first.GetNextOffset().GetValue() == "" {
		t.Fatalf("the first page: %v (count %d, next_offset %v)", got, first.GetCount(), first.GetNextOffset())
	}
	second, err := searchStories(ctx, bob, storySearch{hashtag: tag, peer: aliceForBob, limit: 1, offset: first.GetNextOffset().GetValue()})
	if err != nil {
		t.Fatalf("the second page: %v", err)
	}
	if got := foundKeys(second); !slices.Equal(got, []foundKey{userStory(alice, public)}) || second.GetCount() != 2 || second.GetNextOffset() != nil {
		t.Errorf("the second page: %v (count %d, next_offset %v)", got, second.GetCount(), second.GetNextOffset())
	}

	// A public channel is found; a private one is not, though Bob is in it.
	_, pub := createChannel(ctx, t, alice, "Search public", false)
	inputPub := mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: pub.GetId(), AccessHash: pub.GetAccessHash_FLAGINT64().GetValue()}).To_InputChannel()
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateUsername),
		Channel:     inputPub,
		Username:    "storysearch" + run,
	}); err != nil {
		t.Fatalf("channels.updateUsername: %v", err)
	}
	_, priv := createChannel(ctx, t, alice, "Search private", false)
	if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: priv.GetId(), AccessHash: priv.GetAccessHash_FLAGINT64().GetValue()}).To_InputChannel(),
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	pubStory := sendAreaStory(ctx, t, alice, channelInputPeer(pub), "news #"+tag, nil)
	sendAreaStory(ctx, t, alice, channelInputPeer(priv), "secret #"+tag, nil)
	expectFound(ctx, t, bob, "with the channels", storySearch{hashtag: tag},
		userStory(alice, public), userStory(alice, newer), channelStory(pub, pubStory))
	expectFound(ctx, t, bob, "of the private channel", storySearch{hashtag: tag, peer: channelInputPeer(priv)})

	// The errors of the query.
	for name, tc := range map[string]struct {
		q    storySearch
		want string
	}{
		"a tag with a space":      {storySearch{hashtag: "a b"}, "HASHTAG_INVALID"},
		"a sign alone":            {storySearch{hashtag: "#"}, "HASHTAG_INVALID"},
		"a point without address": {storySearch{area: geoArea("", "")}, "MEDIA_INVALID"},
		"a venue without its id":  {storySearch{area: venueArea("foursquare", "")}, "MEDIA_INVALID"},
		"a bad offset":            {storySearch{hashtag: tag, offset: "x"}, "OFFSET_INVALID"},
	} {
		if _, err := searchStories(ctx, bob, tc.q); !mtclient.IsRPCError(err, tc.want) {
			t.Errorf("%s: %v, want %s", name, err, tc.want)
		}
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLStoriesSearchPosts{
		Constructor: bob.c.Constructor(mtproto.Predicate_stories_searchPosts),
		Limit:       10,
	}); !mtclient.IsRPCError(err, "SEARCH_QUERY_EMPTY") {
		t.Errorf("no query: %v, want SEARCH_QUERY_EMPTY", err)
	}

	// Alice blocks Bob: he finds none of her stories, her channel's still.
	if _, err = alice.c.Invoke(ctx, &mtproto.TLContactsBlock{
		Constructor: alice.c.Constructor(mtproto.Predicate_contacts_block),
		Id:          inputPeerOf(ctx, t, alice, bob),
	}); err != nil {
		t.Fatalf("contacts.block: %v", err)
	}
	expectFound(ctx, t, bob, "blocked", storySearch{hashtag: tag}, channelStory(pub, pubStory))
	expectFound(ctx, t, bob, "blocked, the city", storySearch{area: geoArea("DE", city)})
}

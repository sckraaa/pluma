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

package core

import (
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/stories"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The hashtag of stories.searchPosts: "$" makes a cashtag, one "#" is cut,
// and what is left must be one whole tag.
func TestStorySearchHashtag(t *testing.T) {
	for in, want := range map[string]storySearch{
		"#a":                          {kind: stories.TagHashtag, tag: "a"},
		"a":                           {kind: stories.TagHashtag, tag: "a"},
		"$A":                          {kind: stories.TagCashtag, tag: "a"},
		"#Кот":                        {kind: stories.TagHashtag, tag: "кот"},
		"snake_case1":                 {kind: stories.TagHashtag, tag: "snake_case1"},
		"#" + strings.Repeat("я", 64): {kind: stories.TagHashtag, tag: strings.Repeat("я", 64)},
	} {
		got, err := storySearchOf(wrapperspb.String(in), nil)
		if err != nil || got.kind != want.kind || got.tag != want.tag || got.place != nil {
			t.Errorf("%q: %+v, %v, want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "#", "$", "a b", "##a", "#a#b", "a$b", "$100", "#a@channel", "a-b", strings.Repeat("a", 65)} {
		if _, err := storySearchOf(wrapperspb.String(in), nil); err != errHashtagInvalid {
			t.Errorf("%q: %v, want HASHTAG_INVALID", in, err)
		}
	}
}

func searchVenue(provider, id string) *mtproto.MediaArea {
	return mtproto.MakeTLMediaAreaVenue(&mtproto.MediaArea{Provider: provider, VenueId: id, Title: "Cafe"}).To_MediaArea()
}

func searchGeo(address *mtproto.GeoPointAddress) *mtproto.MediaArea {
	area := &mtproto.MediaArea{Geo: mtproto.MakeTLGeoPoint(&mtproto.GeoPoint{Lat: 55.75, Long: 37.61}).To_GeoPoint()}
	if address != nil {
		area.Address_FLAGGEOPOINTADDRESS = mtproto.MakeTLGeoPointAddress(address).To_GeoPointAddress()
	}
	return mtproto.MakeTLMediaAreaGeoPoint(area).To_MediaArea()
}

// The area of stories.searchPosts: a venue with its id or a point with an
// address; exactly one of hashtag and area.
func TestStorySearchArea(t *testing.T) {
	got, err := storySearchOf(nil, searchVenue("foursquare", "v1"))
	if err != nil || got.place == nil || got.place.Kind != stories.PlaceVenue || got.place.VenueKey != "foursquare:v1" {
		t.Errorf("a venue: %+v, %v", got, err)
	}
	got, err = storySearchOf(nil, searchGeo(&mtproto.GeoPointAddress{CountryIso2: "DE", City: wrapperspb.String("Berlin")}))
	if err != nil || got.place == nil || got.place.Kind != stories.PlaceGeo || got.place.Country != "de" || got.place.City != "berlin" || got.place.Street != "" {
		t.Errorf("a point with a city: %+v, %v", got, err)
	}
	url := mtproto.MakeTLMediaAreaUrl(&mtproto.MediaArea{Url: "https://example.com"}).To_MediaArea()
	for name, area := range map[string]*mtproto.MediaArea{
		"a venue without its id":     searchVenue("foursquare", ""),
		"a point without address":    searchGeo(nil),
		"a point with empty address": searchGeo(&mtproto.GeoPointAddress{}),
		"a link":                     url,
	} {
		if _, err := storySearchOf(nil, area); err != mtproto.ErrMediaInvalid {
			t.Errorf("%s: %v, want MEDIA_INVALID", name, err)
		}
	}
	if _, err := storySearchOf(nil, nil); err != mtproto.ErrSearchQueryEmpty {
		t.Errorf("no query: %v, want SEARCH_QUERY_EMPTY", err)
	}
	if _, err := storySearchOf(wrapperspb.String("a"), searchVenue("foursquare", "v1")); err != mtproto.ErrSearchQueryEmpty {
		t.Errorf("both: %v, want SEARCH_QUERY_EMPTY", err)
	}
}

// The offset of a page comes back as the cursor of the story it was made
// of; anything else is OFFSET_INVALID.
func TestSearchOffset(t *testing.T) {
	for _, st := range []*stories.Story{
		{Peer: stories.UserPeer(7), Id: 3, Date: 1_800_000_000},
		{Peer: stories.ChannelPeer(1 << 40), Id: 1 << 30, Date: 1},
	} {
		got, err := parseSearchOffset(searchOffset(st))
		if err != nil || got != stories.CursorOf(st) {
			t.Errorf("%q: %+v, %v, want %+v", searchOffset(st), got, err, stories.CursorOf(st))
		}
	}
	if got, err := parseSearchOffset(""); err != nil || got != (stories.SearchCursor{}) {
		t.Errorf("the first page: %+v, %v", got, err)
	}
	for _, in := range []string{"x", "1.2.3", "1.2.3.4.5", "1.2.3.x", "0.0.0.0", "99999999999.2.3.4", "1.2.3.-", " 1.2.3.4"} {
		if _, err := parseSearchOffset(in); err != mtproto.ErrOffsetInvalid {
			t.Errorf("%q: %v, want OFFSET_INVALID", in, err)
		}
	}
}

// The pages of the results: limit 1 to 100, the offset of the last story
// leads to the next one, the last page has no offset, and a story gone
// since the offset was given does not lose the place.
func TestSearchPage(t *testing.T) {
	found := []*stories.Story{
		{Peer: stories.UserPeer(9), Id: 5, Date: 300},
		{Peer: stories.UserPeer(9), Id: 4, Date: 300},
		{Peer: stories.UserPeer(7), Id: 8, Date: 300},
		{Peer: stories.ChannelPeer(5), Id: 2, Date: 200},
		{Peer: stories.UserPeer(7), Id: 1, Date: 100},
	}
	var (
		got    []int32
		offset string
		pages  int
	)
	for {
		after, err := parseSearchOffset(offset)
		if err != nil {
			t.Fatalf("offset %q: %v", offset, err)
		}
		page, next := searchPage(found, after, 2)
		pages++
		got = append(got, idsOf(page)...)
		if next == "" {
			break
		}
		if pages > len(found) {
			t.Fatalf("no end after %d pages", pages)
		}
		offset = next
	}
	if pages != 3 || !equalIds(got, []int32{5, 4, 8, 2, 1}) {
		t.Errorf("%d pages of %v", pages, got)
	}
	if page, next := searchPage(found, stories.SearchCursor{}, 0); len(page) != 1 || next == "" {
		t.Errorf("limit 0: %v, %q", idsOf(page), next)
	}
	if page, next := searchPage(found, stories.SearchCursor{}, 1000); len(page) != len(found) || next != "" {
		t.Errorf("limit 1000: %v, %q", idsOf(page), next)
	}
	// The story of the offset was deleted: the page goes on after its place
	// (user 8 comes between user 9 and user 7 of the same date).
	gone := stories.CursorOf(&stories.Story{Peer: stories.UserPeer(8), Id: 1, Date: 300})
	if page, _ := searchPage(found, gone, 10); !equalIds(idsOf(page), []int32{8, 2, 1}) {
		t.Errorf("after a story that is gone: %v", idsOf(page))
	}
	if page, next := searchPage(found, stories.CursorOf(found[4]), 10); len(page) != 0 || next != "" {
		t.Errorf("after the last: %v, %q", idsOf(page), next)
	}
	many := make([]*stories.Story, 150)
	for i := range many {
		many[i] = &stories.Story{Peer: stories.UserPeer(7), Id: int32(200 - i), Date: 1}
	}
	if page, next := searchPage(many, stories.SearchCursor{}, 150); len(page) != maxSearchPage || next == "" {
		t.Errorf("a page of %d with offset %q, want %d", len(page), next, maxSearchPage)
	}
}

// Whose stories are found: a user who did not shut the caller out, with
// the rules of each story; a channel with a public username.
func TestSearchFinder(t *testing.T) {
	c := testCore()
	everyone := []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowAll}}
	contacts := []*mtproto.PrivacyRule{{PredicateName: mtproto.Predicate_privacyValueAllowContacts}}
	stranger := mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: &mtproto.UserData{Id: 7}}).To_ImmutableUser()
	public := channelView(channelpb.RoleMember, 0, channelpb.StateLeft)
	public.channel.Channel.Username = "news"
	private := channelView(channelpb.RoleMember, 0, channelpb.StateIn)
	private.peer = stories.ChannelPeer(6)
	f := &finder{c: c, views: map[stories.Peer]*peerView{
		stories.UserPeer(testSelf): findable(&peerView{peer: stories.UserPeer(testSelf), own: true}),
		stories.UserPeer(7):        findable(&peerView{peer: stories.UserPeer(7), owner: stranger}),
		stories.UserPeer(8):        findable(&peerView{peer: stories.UserPeer(8), owner: stranger, blocked: true}),
		stories.ChannelPeer(5):     findable(public),
		stories.ChannelPeer(6):     findable(private),
	}}
	found, err := f.found([]*stories.Story{
		{Peer: stories.UserPeer(7), Id: 1, Privacy: everyone},
		{Peer: stories.UserPeer(7), Id: 2, Privacy: contacts},
		{Peer: stories.UserPeer(8), Id: 3, Privacy: everyone},
		{Peer: stories.ChannelPeer(5), Id: 4},
		{Peer: stories.ChannelPeer(6), Id: 5},
		{Peer: stories.UserPeer(testSelf), Id: 6, Privacy: everyone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ids := idsOf(found); !equalIds(ids, []int32{1, 4, 6}) {
		t.Errorf("found %v, want [1 4 6]", ids)
	}

	// A channel whose editable username is hidden but another is shown is
	// public.
	ch := &channelpb.ImmutableChannel{Id: 5, Usernames: []*mtproto.Username{
		mtproto.MakeTLUsername(&mtproto.Username{Editable: true, Username: "main"}).To_Username(),
		mtproto.MakeTLUsername(&mtproto.Username{Active: true, Username: "extra"}).To_Username(),
	}}
	if !publicChannel(ch) {
		t.Error("a channel with an active username is not public")
	}
	ch.Usernames[1].Active = false
	if publicChannel(ch) {
		t.Error("a channel without an active username is public")
	}
}

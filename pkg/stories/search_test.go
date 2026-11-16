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

package stories

import (
	"slices"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

func hashtagEntity(offset, length int32) *mtproto.MessageEntity {
	return mtproto.MakeTLMessageEntityHashtag(&mtproto.MessageEntity{Offset: offset, Length: length}).To_MessageEntity()
}

func cashtagEntity(offset, length int32) *mtproto.MessageEntity {
	return mtproto.MakeTLMessageEntityCashtag(&mtproto.MessageEntity{Offset: offset, Length: length}).To_MessageEntity()
}

func TestTagsFromText(t *testing.T) {
	for _, c := range []struct {
		name    string
		caption string
		want    []Tag
	}{
		{"none", "a cat", nil},
		{"hashtag", "a #Cat", []Tag{{TagHashtag, "cat"}}},
		{"cyrillic", "#Кот и #пёс", []Tag{{TagHashtag, "кот"}, {TagHashtag, "пёс"}}},
		{"cashtag", "buy $TSLA now", []Tag{{TagCashtag, "tsla"}}},
		{"a price is no cashtag", "for $100", nil},
		{"punctuation ends a tag", "#cat, #dog. #a_b!", []Tag{{TagHashtag, "cat"}, {TagHashtag, "dog"}, {TagHashtag, "a_b"}}},
		{"the channel of a tag", "#news@pluma", []Tag{{TagHashtag, "news"}}},
		{"case and repeats fold", "#Cat #CAT #cat", []Tag{{TagHashtag, "cat"}}},
		{"a sign alone", "# and $", nil},
		{"inside a word", "a#cat", nil},
		{"in brackets", "(#cat) [x]#dog ($TSLA)", []Tag{{TagHashtag, "cat"}, {TagHashtag, "dog"}, {TagCashtag, "tsla"}}},
		{"after a comma", "#cat,#dog", []Tag{{TagHashtag, "cat"}}},
		{"a doubled sign", "##cat", []Tag{{TagHashtag, "cat"}}},
		{"after a line break", "a\n#cat", []Tag{{TagHashtag, "cat"}}},
		{"both kinds of one word", "#tsla $TSLA", []Tag{{TagHashtag, "tsla"}, {TagCashtag, "tsla"}}},
		{"64 letters", "#" + strings.Repeat("я", 64), []Tag{{TagHashtag, strings.Repeat("я", 64)}}},
		{"65 letters", "#" + strings.Repeat("я", 65), nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := Tags(c.caption, nil); !slices.Equal(got, c.want) {
				t.Errorf("Tags(%q) = %v, want %v", c.caption, got, c.want)
			}
		})
	}
}

// The entities of the client count UTF-16 units, so an emoji before a tag
// moves it by two. The tags here are inside words, where the text alone
// finds none.
func TestTagsFromEntities(t *testing.T) {
	caption := "😀a#Кот b$TSLA #"
	// 😀 is two units: "a" at 2, "#Кот" at 3..6, "b" at 8, "$TSLA" at 9..13.
	if got := Tags(caption, nil); len(got) != 0 {
		t.Fatalf("Tags without entities = %v", got)
	}
	got := Tags(caption, []*mtproto.MessageEntity{
		hashtagEntity(3, 4),
		cashtagEntity(9, 5),
		hashtagEntity(15, 1),  // the sign alone
		hashtagEntity(14, 10), // past the end
		hashtagEntity(2, 3),   // does not start with the sign
		mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 4}).To_MessageEntity(),
	})
	want := []Tag{{TagHashtag, "кот"}, {TagCashtag, "tsla"}}
	if !slices.Equal(got, want) {
		t.Errorf("Tags = %v, want %v", got, want)
	}
}

// A tag the client marked and the text has is one tag.
func TestTagsEntityAndTextOnce(t *testing.T) {
	got := Tags("#cat", []*mtproto.MessageEntity{hashtagEntity(0, 4)})
	if want := []Tag{{TagHashtag, "cat"}}; !slices.Equal(got, want) {
		t.Errorf("Tags = %v, want %v", got, want)
	}
}

func TestNormalizeTag(t *testing.T) {
	for _, c := range []struct {
		kind int8
		body string
		want string
		ok   bool
	}{
		{TagHashtag, "Cat", "cat", true},
		{TagHashtag, "cat dog", "cat", true},
		{TagHashtag, "100", "100", true},
		{TagHashtag, "", "", false},
		{TagHashtag, "#", "", false},
		{TagCashtag, "TSLA", "tsla", true},
		{TagCashtag, "100", "", false},
		{TagCashtag, "_A", "", false},
	} {
		got, ok := NormalizeTag(c.kind, c.body)
		if got != c.want || ok != c.ok {
			t.Errorf("NormalizeTag(%d, %q) = %q, %v; want %q, %v", c.kind, c.body, got, ok, c.want, c.ok)
		}
	}
}

func venue(provider, id string) *mtproto.MediaArea {
	return mtproto.MakeTLMediaAreaVenue(&mtproto.MediaArea{Provider: provider, VenueId: id, Title: "Cafe"}).To_MediaArea()
}

func geoPoint(country, state, city, street string) *mtproto.MediaArea {
	area := &mtproto.MediaArea{Geo: mtproto.MakeTLGeoPoint(&mtproto.GeoPoint{Lat: 55.75, Long: 37.61}).To_GeoPoint()}
	if country != "" || state != "" || city != "" || street != "" {
		a := &mtproto.GeoPointAddress{CountryIso2: country}
		if state != "" {
			a.State = wrapperspb.String(state)
		}
		if city != "" {
			a.City = wrapperspb.String(city)
		}
		if street != "" {
			a.Street = wrapperspb.String(street)
		}
		area.Address_FLAGGEOPOINTADDRESS = mtproto.MakeTLGeoPointAddress(a).To_GeoPointAddress()
	}
	return mtproto.MakeTLMediaAreaGeoPoint(area).To_MediaArea()
}

func TestPlaces(t *testing.T) {
	got := Places([]*mtproto.MediaArea{
		venue("foursquare", "4b5"),
		venue("foursquare", ""), // no venue id
		geoPoint("", "", "", ""),
		geoPoint("RU", "", "", ""),
		mtproto.MakeTLMediaAreaUrl(&mtproto.MediaArea{Url: "https://pluma.chat"}).To_MediaArea(),
		geoPoint("RU", "Moscow", " Moscow ", "Tverskaya Street"),
	})
	want := []Place{
		{Seq: 0, Kind: PlaceVenue, VenueKey: "foursquare:4b5"},
		{Seq: 3, Kind: PlaceGeo, Country: "ru"},
		{Seq: 5, Kind: PlaceGeo, Country: "ru", State: "moscow", City: "moscow", Street: "tverskaya street"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Places = %+v\nwant %+v", got, want)
	}
}

func TestPlaceOfWidths(t *testing.T) {
	p, ok := PlaceOf(geoPoint("RU", "", strings.Repeat("я", 200), ""))
	if !ok || len([]rune(p.City)) != maxCity {
		t.Errorf("a long city: %q, %v", p.City, ok)
	}
	if _, ok := PlaceOf(venue("p", strings.Repeat("x", 300))); ok {
		t.Errorf("a venue key wider than its column is indexed")
	}
}

func TestSearchable(t *testing.T) {
	everyone := []*mtproto.PrivacyRule{
		mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule(),
		mtproto.MakeTLPrivacyValueDisallowUsers(&mtproto.PrivacyRule{Users: []int64{5}}).To_PrivacyRule(),
	}
	contacts := []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule()}
	if !searchable(&Story{Peer: UserPeer(1), Privacy: everyone}) {
		t.Error("a story for everyone with exceptions is not searchable")
	}
	if searchable(&Story{Peer: UserPeer(1), Privacy: contacts}) {
		t.Error("a story for contacts is searchable")
	}
	if !searchable(&Story{Peer: ChannelPeer(1)}) {
		t.Error("a channel's story is not searchable")
	}
}

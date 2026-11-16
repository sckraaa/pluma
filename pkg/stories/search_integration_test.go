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

//go:build integration

package stories

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

var (
	ruleAll      = []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()}
	ruleContacts = []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowContacts(nil).To_PrivacyRule()}
)

// uniqueTag is a tag no other test of the run indexes.
func uniqueTag(name string) string {
	return fmt.Sprintf("%s%d", name, itId())
}

// taggedStory is a story for everyone of peer at date with caption.
func taggedStory(peer Peer, date int32, caption string, areas ...*mtproto.MediaArea) *Story {
	st := itStory(peer, peer.Id, date)
	st.Privacy, st.Caption, st.Entities, st.MediaAreas = ruleAll, caption, nil, areas
	if !peer.IsUser() {
		st.Privacy = nil
	}
	return st
}

type ref struct {
	peer Peer
	id   int32
}

func refsOf(list []*Story) []ref {
	out := make([]ref, 0, len(list))
	for _, st := range list {
		out = append(out, ref{st.Peer, st.Id})
	}
	return out
}

func searchTag(t *testing.T, s *Store, kind int8, tag string, peer *Peer, now int32) []ref {
	t.Helper()
	list, err := s.SearchTags(context.Background(), kind, tag, peer, SearchCursor{}, MaxSearchScan, now)
	if err != nil {
		t.Fatalf("search %q: %v", tag, err)
	}
	return refsOf(list)
}

func searchPlace(t *testing.T, s *Store, place Place, now int32) []ref {
	t.Helper()
	list, err := s.SearchPlaces(context.Background(), place, nil, SearchCursor{}, MaxSearchScan, now)
	if err != nil {
		t.Fatalf("search %+v: %v", place, err)
	}
	return refsOf(list)
}

func indexRows(t *testing.T, db *sqlx.DB, peer Peer, id int32) (tags, places int32, indexed bool) {
	t.Helper()
	ctx := context.Background()
	var err error
	if tags, err = count(ctx, db, "SELECT COUNT(*) FROM story_tags WHERE peer_type = ? AND peer_id = ? AND story_id = ?", peer.Type, peer.Id, id); err != nil {
		t.Fatal(err)
	}
	if places, err = count(ctx, db, "SELECT COUNT(*) FROM story_places WHERE peer_type = ? AND peer_id = ? AND story_id = ?", peer.Type, peer.Id, id); err != nil {
		t.Fatal(err)
	}
	flag, err := count(ctx, db, "SELECT COUNT(*) FROM stories WHERE peer_type = ? AND peer_id = ? AND story_id = ? AND search_indexed = 1", peer.Type, peer.Id, id)
	if err != nil {
		t.Fatal(err)
	}
	return tags, places, flag == 1
}

// Create writes the index of a story, Update writes it anew from the new
// caption and rules, Delete removes it.
func TestSearchIndexFollowsTheStory(t *testing.T) {
	db := itDB()
	s, ctx := NewStore(db), context.Background()
	peer := UserPeer(itId())
	cat, dog, cash := uniqueTag("cat"), uniqueTag("dog"), uniqueTag("T")
	key := "foursquare:" + uniqueTag("v")
	st := mustCreate(t, s, taggedStory(peer, itNow, "#"+cat+" $"+cash, venue("foursquare", key[len("foursquare:"):]), geoPoint("RU", "", "Moscow", "")))
	me := []ref{{peer, st.Id}}

	if tags, places, indexed := indexRows(t, db, peer, st.Id); tags != 2 || places != 2 || !indexed {
		t.Fatalf("after Create: %d tags, %d places, indexed %v", tags, places, indexed)
	}
	if got := searchTag(t, s, TagHashtag, cat, nil, itNow); !slices.Equal(got, me) {
		t.Errorf("by the hashtag: %v", got)
	}
	if got := searchTag(t, s, TagHashtag, "#"+cat, nil, itNow); len(got) != 0 {
		t.Errorf("the sign is the caller's to cut: %v", got)
	}
	if got := searchTag(t, s, TagHashtag, cat+"!", nil, itNow); !slices.Equal(got, me) {
		t.Errorf("by the hashtag with punctuation: %v", got)
	}
	if got := searchTag(t, s, TagCashtag, cash, nil, itNow); !slices.Equal(got, me) {
		t.Errorf("by the cashtag: %v", got)
	}
	if got := searchTag(t, s, TagHashtag, cash, nil, itNow); len(got) != 0 {
		t.Errorf("a cashtag found as a hashtag: %v", got)
	}
	if got := searchPlace(t, s, Place{Kind: PlaceVenue, VenueKey: key}, itNow); !slices.Equal(got, me) {
		t.Errorf("by the venue: %v", got)
	}

	// A new caption replaces the tags; rules for contacts take the story
	// out of the search, rules for everyone bring it back.
	edit := func(f func(st *Story)) {
		t.Helper()
		if _, after, err := s.Update(ctx, peer, st.Id, itNow+1, f); err != nil || after == nil {
			t.Fatalf("update: %v %v", after, err)
		}
	}
	edit(func(st *Story) { st.Caption = "#" + dog })
	if got := searchTag(t, s, TagHashtag, cat, nil, itNow); len(got) != 0 {
		t.Errorf("the old tag after the edit: %v", got)
	}
	if got := searchTag(t, s, TagHashtag, dog, nil, itNow); !slices.Equal(got, me) {
		t.Errorf("the new tag after the edit: %v", got)
	}
	edit(func(st *Story) { st.Privacy = ruleContacts })
	if got := searchTag(t, s, TagHashtag, dog, nil, itNow); len(got) != 0 {
		t.Errorf("a story for contacts is found: %v", got)
	}
	if got := searchPlace(t, s, Place{Kind: PlaceVenue, VenueKey: key}, itNow); len(got) != 0 {
		t.Errorf("a story for contacts is found by its venue: %v", got)
	}
	edit(func(st *Story) { st.Privacy = ruleAll })
	if got := searchTag(t, s, TagHashtag, dog, nil, itNow); !slices.Equal(got, me) {
		t.Errorf("a story for everyone again is not found: %v", got)
	}

	if _, err := s.Delete(ctx, peer, []int32{st.Id}, itNow+2); err != nil {
		t.Fatal(err)
	}
	if got := searchTag(t, s, TagHashtag, dog, nil, itNow); len(got) != 0 {
		t.Errorf("a deleted story is found: %v", got)
	}
	if tags, places, _ := indexRows(t, db, peer, st.Id); tags != 0 || places != 0 {
		t.Errorf("after Delete: %d tags, %d places", tags, places)
	}
}

// A story stored before the index existed is found once the worker has
// indexed it, and indexing it again changes nothing.
func TestUnindexedAndReindex(t *testing.T) {
	db := itDB()
	s, ctx := NewStore(db), context.Background()
	peer := UserPeer(itId())
	tag := uniqueTag("old")
	st := mustCreate(t, s, taggedStory(peer, itNow, "#"+tag, geoPoint("RU", "", "Kazan", "")))
	// As the migration leaves a story of before it.
	for _, q := range []string{
		"DELETE FROM story_tags WHERE peer_type = ? AND peer_id = ?",
		"DELETE FROM story_places WHERE peer_type = ? AND peer_id = ?",
		"UPDATE stories SET search_indexed = 0 WHERE peer_type = ? AND peer_id = ?",
	} {
		if _, err := db.Exec(ctx, q, peer.Type, peer.Id); err != nil {
			t.Fatal(err)
		}
	}
	if got := searchTag(t, s, TagHashtag, tag, nil, itNow); len(got) != 0 {
		t.Fatalf("found before it is indexed: %v", got)
	}
	pending, err := s.Unindexed(ctx, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(pending, Ref{Peer: peer, Id: st.Id}) {
		t.Fatalf("the story is not among the unindexed")
	}
	for range 2 {
		if err := s.Reindex(ctx, peer, st.Id); err != nil {
			t.Fatal(err)
		}
		if tags, places, indexed := indexRows(t, db, peer, st.Id); tags != 1 || places != 1 || !indexed {
			t.Errorf("after Reindex: %d tags, %d places, indexed %v", tags, places, indexed)
		}
	}
	if got := searchTag(t, s, TagHashtag, tag, nil, itNow); !slices.Equal(got, []ref{{peer, st.Id}}) {
		t.Errorf("after Reindex: %v", got)
	}
	if pending, err = s.Unindexed(ctx, 1_000_000); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(pending, Ref{Peer: peer, Id: st.Id}) {
		t.Errorf("the story is still among the unindexed")
	}
	// A story that is gone needs nothing.
	if err := s.Reindex(ctx, peer, st.Id+100); err != nil {
		t.Errorf("Reindex of a missing story: %v", err)
	}
}

// A story out of the feeds is found while it is in the profile; the
// results go newest first and a cursor goes on after the story it names.
func TestSearchCandidates(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	alice, bob, news := UserPeer(itId()), UserPeer(itId()), ChannelPeer(itId())
	tag := uniqueTag("pets")
	a1 := mustCreate(t, s, taggedStory(alice, itNow, "#"+tag))
	b1 := mustCreate(t, s, taggedStory(bob, itNow+1, "#"+tag))
	n1 := mustCreate(t, s, taggedStory(news, itNow+2, "#"+tag))
	a2 := mustCreate(t, s, taggedStory(alice, itNow+2, "#"+tag))

	// Same date: the channel (type 4) before the user (type 2).
	all := []ref{{news, n1.Id}, {alice, a2.Id}, {bob, b1.Id}, {alice, a1.Id}}
	if got := searchTag(t, s, TagHashtag, tag, nil, itNow); !slices.Equal(got, all) {
		t.Fatalf("all: %v, want %v", got, all)
	}
	if got := searchTag(t, s, TagHashtag, tag, &alice, itNow); !slices.Equal(got, []ref{{alice, a2.Id}, {alice, a1.Id}}) {
		t.Errorf("of one peer: %v", got)
	}

	// Page by page with the cursor of the last one.
	var (
		paged []ref
		after SearchCursor
	)
	for range len(all) + 1 {
		page, err := s.SearchTags(ctx, TagHashtag, tag, nil, after, 1, itNow)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		paged = append(paged, refsOf(page)...)
		after = CursorOf(page[len(page)-1])
	}
	if !slices.Equal(paged, all) {
		t.Errorf("by pages: %v, want %v", paged, all)
	}

	// Past their period: only the stories in the profile.
	if _, err := s.SetPinned(ctx, bob, []int32{b1.Id}, true); err != nil {
		t.Fatal(err)
	}
	later := itNow + 3 + DefaultPeriod
	if got := searchTag(t, s, TagHashtag, tag, nil, later); !slices.Equal(got, []ref{{bob, b1.Id}}) {
		t.Errorf("after the period: %v", got)
	}
}

// A geo point is found by every level of its address the query names; two
// areas of one place give one result.
func TestSearchPlaces(t *testing.T) {
	s := NewStore(itDB())
	peer := UserPeer(itId())
	city := uniqueTag("city")
	street := mustCreate(t, s, taggedStory(peer, itNow, "", geoPoint("RU", "", city, "Main Street"), geoPoint("RU", "", city, "Main Street")))
	other := mustCreate(t, s, taggedStory(peer, itNow+1, "", geoPoint("RU", "", city, "")))
	both := []ref{{peer, other.Id}, {peer, street.Id}}

	if got := searchPlace(t, s, Place{Kind: PlaceGeo, Country: "ru", City: city}, itNow); !slices.Equal(got, both) {
		t.Errorf("by the city: %v, want %v", got, both)
	}
	if got := searchPlace(t, s, Place{Kind: PlaceGeo, Country: "ru", City: city, Street: "main street"}, itNow); !slices.Equal(got, []ref{{peer, street.Id}}) {
		t.Errorf("by the street: %v", got)
	}
	if got := searchPlace(t, s, Place{Kind: PlaceGeo, Country: "kz", City: city}, itNow); len(got) != 0 {
		t.Errorf("by another country: %v", got)
	}
	// The query goes through PlaceOf as the index does.
	q, ok := PlaceOf(geoPoint("RU", "", " "+city+" ", ""))
	if !ok {
		t.Fatal("no place of the query")
	}
	if got := searchPlace(t, s, q, itNow); !slices.Equal(got, both) {
		t.Errorf("by the place of an area: %v", got)
	}
	if got := searchPlace(t, s, Place{Kind: PlaceGeo}, itNow); len(got) != 0 {
		t.Errorf("by no level: %v", got)
	}
}

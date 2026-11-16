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
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// getAlbums is stories.getAlbums of peer as u sees it.
func getAlbums(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, hash int64) *mtproto.Stories_Albums {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLStoriesGetAlbums{
		Constructor: u.c.Constructor(mtproto.Predicate_stories_getAlbums),
		Peer:        peer,
		Hash:        hash,
	})
	if err != nil {
		t.Fatalf("stories.getAlbums: %v", err)
	}
	return o.(interface {
		To_Stories_Albums() *mtproto.Stories_Albums
	}).To_Stories_Albums()
}

// createAlbum is stories.createAlbum of peer by u.
func createAlbum(ctx context.Context, u *user, peer *mtproto.InputPeer, title string, ids ...int32) (*mtproto.StoryAlbum, error) {
	if ids == nil {
		ids = []int32{}
	}
	o, err := u.c.Invoke(ctx, &mtproto.TLStoriesCreateAlbum{
		Constructor: u.c.Constructor(mtproto.Predicate_stories_createAlbum),
		Peer:        peer,
		Title:       title,
		Stories:     ids,
	})
	if err != nil {
		return nil, err
	}
	return o.(interface{ To_StoryAlbum() *mtproto.StoryAlbum }).To_StoryAlbum(), nil
}

// updateAlbum is stories.updateAlbum by u.
func updateAlbum(ctx context.Context, u *user, req *mtproto.TLStoriesUpdateAlbum) (*mtproto.StoryAlbum, error) {
	req.Constructor = u.c.Constructor(mtproto.Predicate_stories_updateAlbum)
	o, err := u.c.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	return o.(interface{ To_StoryAlbum() *mtproto.StoryAlbum }).To_StoryAlbum(), nil
}

// albumStories is a page of stories.getAlbumStories as u sees it.
func albumStories(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, album, offset, limit int32) *mtproto.Stories_Stories {
	t.Helper()
	return storiesOf(ctx, t, u, "stories.getAlbumStories", &mtproto.TLStoriesGetAlbumStories{
		Constructor: u.c.Constructor(mtproto.Predicate_stories_getAlbumStories),
		Peer:        peer,
		AlbumId:     album,
		Offset:      offset,
		Limit:       limit,
	})
}

func albumIds(list []*mtproto.StoryAlbum) []int32 {
	out := make([]int32, 0, len(list))
	for _, a := range list {
		out = append(out, a.GetAlbumId())
	}
	return out
}

func photoIdOf(item *mtproto.StoryItem) int64 {
	return item.GetMedia().GetPhoto_FLAGPHOTO().GetId()
}

// The albums of an account (task 104): made with stories and without,
// listed with their covers and a hash that answers albumsNotModified,
// refilled, reordered and renamed with the changed stories on the other
// device, paged by the number of stories loaded, reordered among
// themselves; a new story goes straight into the albums it names that
// exist, a deleted story leaves its albums, and a deleted album leaves its
// stories.
func TestStoryAlbums(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	laptop := login(ctx, t, phone, "Alice")
	getState(ctx, t, laptop)
	self := selfPeer()

	// The profile of an account without albums no longer fails.
	if empty := getAlbums(ctx, t, alice, self, 0); empty.GetPredicateName() != mtproto.Predicate_stories_albums || len(empty.GetAlbums()) != 0 {
		t.Fatalf("stories.getAlbums without albums: %v", empty)
	}
	one, first := sendStory(ctx, t, alice, self, randomID(), "one", storyRule(mtproto.Predicate_inputPrivacyValueAllowAll))
	two, _ := sendStory(ctx, t, alice, self, randomID(), "two", storyRule(mtproto.Predicate_inputPrivacyValueAllowAll))
	three, third := sendStory(ctx, t, alice, self, randomID(), "three", storyRule(mtproto.Predicate_inputPrivacyValueAllowContacts))
	// The laptop has the stories without albums; what it waits for below is
	// newer.
	for _, id := range []int32{one, two, three} {
		waitUpdate(ctx, t, laptop, mtproto.Predicate_updateStory, func(u *mtproto.Update) bool {
			return u.GetStory().GetId() == id && len(u.GetStory().GetAlbums()) == 0
		})
	}

	trips, err := createAlbum(ctx, alice, self, "Trips", one, two)
	if err != nil {
		t.Fatalf("stories.createAlbum: %v", err)
	}
	if trips.GetTitle() != "Trips" || trips.GetIconPhoto().GetId() != photoIdOf(first) {
		t.Errorf("the new album: %v", trips)
	}
	cats, err := createAlbum(ctx, alice, self, "Cats")
	if err != nil {
		t.Fatalf("stories.createAlbum without stories: %v", err)
	}
	if _, err = createAlbum(ctx, alice, self, "A title too long"); !mtclient.IsRPCError(err, "ALBUM_TITLE_INVALID") {
		t.Errorf("a title of 16 characters: %v, want ALBUM_TITLE_INVALID", err)
	}
	list := getAlbums(ctx, t, alice, self, 0)
	if !slices.Equal(albumIds(list.GetAlbums()), []int32{trips.GetAlbumId(), cats.GetAlbumId()}) || list.GetHash() == 0 ||
		list.GetAlbums()[0].GetIconPhoto().GetId() != photoIdOf(first) || list.GetAlbums()[1].GetIconPhoto() != nil {
		t.Fatalf("stories.getAlbums: %v", list)
	}
	if same := getAlbums(ctx, t, alice, self, list.GetHash()); same.GetPredicateName() != mtproto.Predicate_stories_albumsNotModified {
		t.Errorf("stories.getAlbums by its hash: %v", same)
	}

	// The third story joins; the laptop learns its albums.
	if _, err = updateAlbum(ctx, alice, &mtproto.TLStoriesUpdateAlbum{Peer: self, AlbumId: trips.GetAlbumId(), AddStories: []int32{three}}); err != nil {
		t.Fatalf("stories.updateAlbum add_stories: %v", err)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateStory, func(u *mtproto.Update) bool {
		return u.GetStory().GetId() == three && slices.Equal(u.GetStory().GetAlbums(), []int32{trips.GetAlbumId()})
	})
	// The first leaves, the third goes first, and the album is renamed.
	edited, err := updateAlbum(ctx, alice, &mtproto.TLStoriesUpdateAlbum{
		Peer: self, AlbumId: trips.GetAlbumId(), Title: mtproto.MakeFlagsString("Travel"),
		DeleteStories: []int32{one}, Order: []int32{three},
	})
	if err != nil {
		t.Fatalf("stories.updateAlbum: %v", err)
	}
	if edited.GetTitle() != "Travel" || edited.GetIconPhoto().GetId() != photoIdOf(third) {
		t.Errorf("the album after stories.updateAlbum: %v", edited)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateStory, func(u *mtproto.Update) bool {
		return u.GetStory().GetId() == one && len(u.GetStory().GetAlbums()) == 0
	})
	if again := getAlbums(ctx, t, alice, self, list.GetHash()); again.GetPredicateName() != mtproto.Predicate_stories_albums || again.GetHash() == list.GetHash() {
		t.Errorf("stories.getAlbums after the changes: %v", again)
	}

	// Page by page, by the number of stories loaded.
	page := albumStories(ctx, t, alice, self, trips.GetAlbumId(), 0, 1)
	if page.GetCount() != 2 || !slices.Equal(storyIds(page.GetStories()), []int32{three}) || !slices.Equal(page.GetStories()[0].GetAlbums(), []int32{trips.GetAlbumId()}) {
		t.Errorf("the first page: %v", page)
	}
	if page = albumStories(ctx, t, alice, self, trips.GetAlbumId(), 1, 1); page.GetCount() != 2 || !slices.Equal(storyIds(page.GetStories()), []int32{two}) {
		t.Errorf("the second page: %v", page)
	}
	if page = albumStories(ctx, t, alice, self, trips.GetAlbumId(), 2, 1); len(page.GetStories()) != 0 {
		t.Errorf("past the end: %v", page)
	}

	invokeTrue(ctx, t, alice, "stories.reorderAlbums", &mtproto.TLStoriesReorderAlbums{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_reorderAlbums),
		Peer:        self,
		Order:       []int32{cats.GetAlbumId()},
	})
	if ids := albumIds(getAlbums(ctx, t, alice, self, 0).GetAlbums()); !slices.Equal(ids, []int32{cats.GetAlbumId(), trips.GetAlbumId()}) {
		t.Errorf("the albums after stories.reorderAlbums: %v", ids)
	}

	// A new story straight into an album; the album that is not there is
	// passed over.
	o, err := alice.c.Invoke(ctx, &mtproto.TLStoriesSendStory{
		Constructor:  alice.c.Constructor(mtproto.Predicate_stories_sendStory),
		Peer:         self,
		Media:        uploadPhoto(ctx, t, alice),
		PrivacyRules: []*mtproto.InputPrivacyRule{storyRule(mtproto.Predicate_inputPrivacyValueAllowAll)},
		RandomId:     randomID(),
		Albums:       []int32{cats.GetAlbumId(), 9999},
	})
	if err != nil {
		t.Fatalf("stories.sendStory with albums: %v", err)
	}
	var four int32
	for _, x := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateStory {
			four = x.GetStory().GetId()
			if !slices.Equal(x.GetStory().GetAlbums(), []int32{cats.GetAlbumId()}) {
				t.Errorf("the albums of the new story: %v", x.GetStory().GetAlbums())
			}
		}
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateStory, func(u *mtproto.Update) bool {
		return u.GetStory().GetId() == four && slices.Equal(u.GetStory().GetAlbums(), []int32{cats.GetAlbumId()})
	})
	if page = albumStories(ctx, t, alice, self, cats.GetAlbumId(), 0, 10); !slices.Equal(storyIds(page.GetStories()), []int32{four}) {
		t.Errorf("the album the story was posted to: %v", page)
	}
	// Deleted, it leaves the album.
	if deleted := invokeInts(ctx, t, alice, "stories.deleteStories", &mtproto.TLStoriesDeleteStories{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_deleteStories),
		Peer:        self,
		Id:          []int32{four},
	}); !slices.Equal(deleted, []int32{four}) {
		t.Fatalf("stories.deleteStories: %v", deleted)
	}
	if page = albumStories(ctx, t, alice, self, cats.GetAlbumId(), 0, 10); page.GetCount() != 0 {
		t.Errorf("the album after its story was deleted: %v", page)
	}

	// A deleted album leaves its stories.
	invokeTrue(ctx, t, alice, "stories.deleteAlbum", &mtproto.TLStoriesDeleteAlbum{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_deleteAlbum),
		Peer:        self,
		AlbumId:     trips.GetAlbumId(),
	})
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateStory, func(u *mtproto.Update) bool {
		return u.GetStory().GetId() == two && len(u.GetStory().GetAlbums()) == 0
	})
	if ids := albumIds(getAlbums(ctx, t, alice, self, 0).GetAlbums()); !slices.Equal(ids, []int32{cats.GetAlbumId()}) {
		t.Errorf("the albums after stories.deleteAlbum: %v", ids)
	}
	if _, err = updateAlbum(ctx, alice, &mtproto.TLStoriesUpdateAlbum{Peer: self, AlbumId: trips.GetAlbumId(), Title: mtproto.MakeFlagsString("Back")}); !mtclient.IsRPCError(err, "ALBUM_ID_INVALID") {
		t.Errorf("stories.updateAlbum of a deleted album: %v, want ALBUM_ID_INVALID", err)
	}
	if ps := peerStories(ctx, t, alice, self); !slices.Contains(storyIds(ps.GetStories()), two) {
		t.Errorf("the stories of a deleted album are gone: %v", storyIds(ps.GetStories()))
	}
}

// The albums of others (task 104): a stranger sees an album without the
// story for the contacts, whose cover is not that story, and not an album
// of such stories only; a contact sees both; nobody changes another's
// albums. In a channel an admin needs edit_stories for them, and a member
// sees them.
func TestStoryAlbumsOfOthers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	// Carol is Alice's contact, Bob is nobody's.
	importContacts(ctx, t, alice, phoneContact(alice.c, 1, carol.phone, "Carol", "E2E"))
	self := selfPeer()

	public, publicItem := sendStory(ctx, t, alice, self, randomID(), "everyone", storyRule(mtproto.Predicate_inputPrivacyValueAllowAll))
	private, privateItem := sendStory(ctx, t, alice, self, randomID(), "contacts", storyRule(mtproto.Predicate_inputPrivacyValueAllowContacts))
	mixed, err := createAlbum(ctx, alice, self, "Mixed", private, public)
	if err != nil {
		t.Fatalf("stories.createAlbum: %v", err)
	}
	hidden, err := createAlbum(ctx, alice, self, "Hidden", private)
	if err != nil {
		t.Fatalf("stories.createAlbum: %v", err)
	}

	aliceForBob := peerOf(ctx, t, bob, alice)
	list := getAlbums(ctx, t, bob, aliceForBob, 0)
	if !slices.Equal(albumIds(list.GetAlbums()), []int32{mixed.GetAlbumId()}) || list.GetAlbums()[0].GetIconPhoto().GetId() != photoIdOf(publicItem) {
		t.Errorf("the albums a stranger sees: %v", list)
	}
	if page := albumStories(ctx, t, bob, aliceForBob, mixed.GetAlbumId(), 0, 10); page.GetCount() != 1 || !slices.Equal(storyIds(page.GetStories()), []int32{public}) {
		t.Errorf("the album a stranger sees: %v", page)
	}
	aliceForCarol := peerOf(ctx, t, carol, alice)
	list = getAlbums(ctx, t, carol, aliceForCarol, 0)
	if !slices.Equal(albumIds(list.GetAlbums()), []int32{mixed.GetAlbumId(), hidden.GetAlbumId()}) || list.GetAlbums()[0].GetIconPhoto().GetId() != photoIdOf(privateItem) {
		t.Errorf("the albums a contact sees: %v", list)
	}
	if _, err = createAlbum(ctx, bob, aliceForBob, "Mine"); !mtclient.IsRPCError(err, "PEER_ID_INVALID") {
		t.Errorf("an album of another user: %v, want PEER_ID_INVALID", err)
	}

	// A channel: Carol may post its stories but not arrange its albums
	// until she may edit its stories; Bob, a member, sees them.
	_, ch := createChannel(ctx, t, alice, "Albums", false)
	peer := channelInputPeer(ch)
	input := mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}).To_InputChannel()
	inviteToChannel(ctx, t, alice, input, bob, carol)
	promote := func(rights *mtproto.ChatAdminRights) {
		t.Helper()
		rights.Constructor = alice.c.Constructor(mtproto.Predicate_chatAdminRights)
		if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsEditAdmin{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_editAdmin),
			Channel:     input,
			UserId:      inputUserOf(ctx, t, alice, carol),
			AdminRights: rights,
		}); err != nil {
			t.Fatalf("channels.editAdmin: %v", err)
		}
	}
	promote(&mtproto.ChatAdminRights{PostStories: true})
	if _, err = createAlbum(ctx, carol, peer, "News"); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("an admin without edit_stories makes an album: %v, want CHAT_ADMIN_REQUIRED", err)
	}
	promote(&mtproto.ChatAdminRights{PostStories: true, EditStories: true})
	news, err := createAlbum(ctx, carol, peer, "News")
	if err != nil {
		t.Fatalf("an admin with edit_stories makes an album: %v", err)
	}
	story, item := sendStory(ctx, t, alice, peer, randomID(), "news")
	if _, err = updateAlbum(ctx, carol, &mtproto.TLStoriesUpdateAlbum{Peer: peer, AlbumId: news.GetAlbumId(), AddStories: []int32{story}}); err != nil {
		t.Fatalf("stories.updateAlbum of the channel: %v", err)
	}
	list = getAlbums(ctx, t, bob, peer, 0)
	if !slices.Equal(albumIds(list.GetAlbums()), []int32{news.GetAlbumId()}) || list.GetAlbums()[0].GetIconPhoto().GetId() != photoIdOf(item) {
		t.Errorf("the albums of the channel for a member: %v", list)
	}
	if page := albumStories(ctx, t, bob, peer, news.GetAlbumId(), 0, 10); !slices.Equal(storyIds(page.GetStories()), []int32{story}) ||
		!slices.Equal(page.GetStories()[0].GetAlbums(), []int32{news.GetAlbumId()}) {
		t.Errorf("the album of the channel for a member: %v", page)
	}
	if _, err = createAlbum(ctx, bob, peer, "Mine"); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Errorf("a member makes an album of the channel: %v, want CHAT_ADMIN_REQUIRED", err)
	}
}

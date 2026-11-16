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
	"errors"
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/layercompat"
	"github.com/teamgram/teamgram-server/pkg/stories"

	"google.golang.org/grpc/status"
)

func photoStory(id int32, photo int64) *stories.Story {
	return &stories.Story{Id: id, Media: mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{
		Photo_FLAGPHOTO: mtproto.MakeTLPhoto(&mtproto.Photo{Id: photo, AccessHash: 1, FileReference: []byte{}, Sizes: []*mtproto.PhotoSize{}, DcId: 1}).To_Photo(),
	}).To_MessageMedia()}
}

func videoStory(id int32, document int64) *stories.Story {
	return &stories.Story{Id: id, Media: mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{
		Document: mtproto.MakeTLDocument(&mtproto.Document{
			Id: document, AccessHash: 1, FileReference: []byte{}, MimeType: "video/mp4", DcId: 1, Attributes: []*mtproto.DocumentAttribute{},
		}).To_Document(),
		Video_FLAGBOOLEAN: true,
	}).To_MessageMedia()}
}

// The cover of an album is the media of the first story the viewer is
// shown: a photo is icon_photo, a video icon_video; no story, no cover.
func TestAlbumCover(t *testing.T) {
	a := &stories.Album{Id: 3, Title: "Trips"}
	got := storyAlbum(a, []*stories.Story{photoStory(1, 50), videoStory(2, 60)})
	if got.GetAlbumId() != 3 || got.GetTitle() != "Trips" || got.GetIconPhoto().GetId() != 50 || got.GetIconVideo() != nil {
		t.Errorf("a photo first: %v", got)
	}
	got = storyAlbum(a, []*stories.Story{videoStory(2, 60), photoStory(1, 50)})
	if got.GetIconVideo().GetId() != 60 || got.GetIconPhoto() != nil {
		t.Errorf("a video first: %v", got)
	}
	if got = storyAlbum(a, nil); got.GetIconPhoto() != nil || got.GetIconVideo() != nil {
		t.Errorf("an album the viewer is shown nothing of: %v", got)
	}
	if got = storyAlbum(a, []*stories.Story{{Id: 4}}); got.GetIconPhoto() != nil || got.GetIconVideo() != nil {
		t.Errorf("a story without media: %v", got)
	}
}

// The hash of the list follows the albums, their order, titles and covers
// and nothing else; the client's hash 0 always gets the list.
func TestAlbumsHash(t *testing.T) {
	list := func(order []int32, titles map[int32]string, covers map[int32]*stories.Story) []*mtproto.StoryAlbum {
		out := make([]*mtproto.StoryAlbum, 0, len(order))
		for _, id := range order {
			var shown []*stories.Story
			if st := covers[id]; st != nil {
				shown = []*stories.Story{st}
			}
			out = append(out, storyAlbum(&stories.Album{Id: id, Title: titles[id]}, shown))
		}
		return out
	}
	titles := map[int32]string{1: "Trips", 2: "Cats"}
	covers := map[int32]*stories.Story{1: photoStory(1, 50), 2: videoStory(2, 60)}
	base := albumsHash(list([]int32{1, 2}, titles, covers))
	if base == 0 {
		t.Fatalf("the hash of two albums is 0")
	}
	if again := albumsHash(list([]int32{1, 2}, titles, covers)); again != base {
		t.Errorf("the same list: %d, then %d", base, again)
	}
	for name, other := range map[string][]*mtproto.StoryAlbum{
		"renamed":       list([]int32{1, 2}, map[int32]string{1: "Trip", 2: "Cats"}, covers),
		"reordered":     list([]int32{2, 1}, titles, covers),
		"another cover": list([]int32{1, 2}, titles, map[int32]*stories.Story{1: photoStory(3, 51), 2: videoStory(2, 60)}),
		"no cover":      list([]int32{1, 2}, titles, map[int32]*stories.Story{2: videoStory(2, 60)}),
		"one less":      list([]int32{1}, titles, covers),
		"another album": list([]int32{1, 3}, map[int32]string{1: "Trips", 3: "Cats"}, map[int32]*stories.Story{1: photoStory(1, 50), 3: videoStory(2, 60)}),
	} {
		if albumsHash(other) == base {
			t.Errorf("%s: the hash did not change", name)
		}
	}
	if albumsHash(nil) != 0 {
		t.Errorf("the hash of no albums is %d", albumsHash(nil))
	}
	if !albumsUnchanged(base, base) || albumsUnchanged(0, 0) || albumsUnchanged(base+1, base) || albumsUnchanged(0, base) {
		t.Errorf("albumsNotModified is answered for the wrong hashes")
	}
}

// Who changes albums: the account its own; in a channel the creator and
// the admins with edit_stories, not those who only post or delete; a bot
// none.
func TestAlbumEditors(t *testing.T) {
	for name, tc := range map[string]struct {
		v    *peerView
		want bool
	}{
		"own account":       {&peerView{peer: stories.UserPeer(testSelf), own: true}, true},
		"another user":      {&peerView{peer: stories.UserPeer(7)}, false},
		"creator":           {channelView(channelpb.RoleCreator, 0, channelpb.StateIn), true},
		"admin who edits":   {channelView(channelpb.RoleAdmin, mtproto.ADMIN_EDIT_STORIES, channelpb.StateIn), true},
		"admin who posts":   {channelView(channelpb.RoleAdmin, mtproto.ADMIN_POST_STORIES, channelpb.StateIn), false},
		"admin who deletes": {channelView(channelpb.RoleAdmin, mtproto.ADMIN_DELETE_STORIES, channelpb.StateIn), false},
		"member":            {channelView(channelpb.RoleMember, 0, channelpb.StateIn), false},
		"creator who left":  {channelView(channelpb.RoleCreator, 0, channelpb.StateLeft), false},
	} {
		if got := tc.v.editsAlbums(); got != tc.want {
			t.Errorf("%s: edits albums %v, want %v", name, got, tc.want)
		}
	}

	bot := testCore()
	bot.MD.IsBot = true
	if _, err := bot.albumEditor(selfInputPeer()); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("a bot changes albums: %v", err)
	}
	if v, err := testCore().albumEditor(selfInputPeer()); err != nil || !v.own {
		t.Errorf("the account changes its own albums: %v, %v", v, err)
	}
	if _, err := testCore().albumEditor(nil); !errors.Is(err, mtproto.ErrPeerIdInvalid) {
		t.Errorf("no peer: %v", err)
	}
}

func selfInputPeer() *mtproto.InputPeer {
	return mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()
}

// The errors of pkg/stories as the clients read them.
func TestAlbumErrors(t *testing.T) {
	for want, err := range map[string]error{
		"ALBUMS_TOO_MANY":        stories.ErrAlbumsTooMany,
		"ALBUM_ID_INVALID":       stories.ErrAlbumInvalid,
		"ALBUM_TITLE_INVALID":    stories.ErrAlbumTitleInvalid,
		"ALBUM_STORIES_TOO_MANY": stories.ErrAlbumFull,
		"STORY_ID_INVALID":       fmt.Errorf("wrapped: %w", stories.ErrStoryInvalid),
	} {
		s, ok := status.FromError(albumError(err))
		if !ok || s.Code() != mtproto.ErrBadRequest || s.Message() != want {
			t.Errorf("%v: %v, want 400 %s", err, albumError(err), want)
		}
	}
	other := errors.New("connection reset")
	if albumError(other) != other {
		t.Errorf("an error of the database is turned into %v", albumError(other))
	}
}

// The pages of an album: offset is the number of stories before the page,
// limit is 1 to 100, and past the end the page is empty.
func TestAlbumPage(t *testing.T) {
	for _, tc := range []struct {
		n, offset, limit int
		want             [2]int
	}{
		{0, 0, 20, [2]int{0, 0}},
		{5, 0, 20, [2]int{0, 5}},
		{50, 0, 20, [2]int{0, 20}},
		{50, 40, 20, [2]int{40, 50}},
		{50, 50, 20, [2]int{50, 50}},
		{50, 90, 20, [2]int{50, 50}},
		{50, 0, 0, [2]int{0, 1}},
		{50, 3, -7, [2]int{3, 4}},
		{500, 0, 1000, [2]int{0, 100}},
	} {
		if got := albumPage(tc.n, tc.offset, tc.limit); got != tc.want {
			t.Errorf("%d stories from %d by %d: %v, want %v", tc.n, tc.offset, tc.limit, got, tc.want)
		}
	}
	if got := uniqueStoryIds([]int32{3, 1, 3, 2, 1}); !equalIds(got, []int32{3, 1, 2}) {
		t.Errorf("unique ids: %v", got)
	}
}

// The albums and a story in them are encoded whole for both layers the
// server speaks.
func TestAlbumsEncodeForBothLayers(t *testing.T) {
	st := photoStory(3, 50)
	st.Peer, st.Date, st.ExpireDate = stories.UserPeer(7), 100, 200
	st.Privacy = []*mtproto.PrivacyRule{mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()}
	item := stories.Item(st, stories.Seen{Albums: []int32{4, 9}})
	albums := mtproto.MakeTLStoriesAlbums(&mtproto.Stories_Albums{Hash: 77, Albums: []*mtproto.StoryAlbum{
		storyAlbum(&stories.Album{Id: 4, Title: "Trips"}, []*stories.Story{st}),
		storyAlbum(&stories.Album{Id: 9, Title: "Cats"}, []*stories.Story{videoStory(5, 60)}),
		storyAlbum(&stories.Album{Id: 10, Title: "Empty"}, nil),
	}}).To_Stories_Albums()
	same := mtproto.MakeTLStoriesAlbumsNotModified(&mtproto.Stories_Albums{}).To_Stories_Albums()

	for _, layer := range []int32{228, 233} {
		roundTrip := func(name string, object mtproto.TLObject, into interface {
			Decode(*mtproto.DecodeBuf) error
		}) {
			t.Helper()
			body, err := layercompat.EncodeFor(object, layer)
			if err != nil {
				t.Fatalf("layer %d, %s: %v", layer, name, err)
			}
			d := mtproto.NewDecodeBuf(body)
			if err = into.Decode(d); err != nil || d.GetError() != nil {
				t.Fatalf("layer %d, %s: decode: %v, %v", layer, name, err, d.GetError())
			}
		}
		gotItem := &mtproto.StoryItem{}
		roundTrip("a story in albums", item, gotItem)
		if gotItem.GetId() != 3 || !equalIds(gotItem.GetAlbums(), []int32{4, 9}) {
			t.Errorf("layer %d, a story in albums: %v", layer, gotItem)
		}
		gotAlbums := &mtproto.Stories_Albums{}
		roundTrip("stories.albums", albums, gotAlbums)
		if gotAlbums.GetPredicateName() != mtproto.Predicate_stories_albums || gotAlbums.GetHash() != 77 || len(gotAlbums.GetAlbums()) != 3 ||
			gotAlbums.GetAlbums()[0].GetIconPhoto().GetId() != 50 || gotAlbums.GetAlbums()[1].GetIconVideo().GetId() != 60 ||
			gotAlbums.GetAlbums()[2].GetTitle() != "Empty" || gotAlbums.GetAlbums()[2].GetIconPhoto() != nil {
			t.Errorf("layer %d, stories.albums: %v", layer, gotAlbums)
		}
		gotSame := &mtproto.Stories_Albums{}
		roundTrip("stories.albumsNotModified", same, gotSame)
		if gotSame.GetPredicateName() != mtproto.Predicate_stories_albumsNotModified {
			t.Errorf("layer %d, stories.albumsNotModified: %v", layer, gotSame)
		}
	}
}

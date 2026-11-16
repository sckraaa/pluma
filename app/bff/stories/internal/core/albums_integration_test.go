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

package core

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/svc"
	sync_client "github.com/teamgram/teamgram-server/app/messenger/sync/client"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/stories"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

var itMySQL *testinfra.MySQL

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	if itMySQL, err = testinfra.StartMySQL(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = itMySQL.Terminate(ctx)
	os.Exit(code)
}

var itSeq atomic.Int64

// itId is an id no other test of the run uses.
func itId() int64 {
	return time.Now().UnixMilli()%1_000_000_000*1000 + itSeq.Add(1)
}

// devices records what the caller's other devices are sent.
type devices struct {
	sync_client.SyncClient
	mu   sync.Mutex
	sent []*mtproto.Updates
}

func (d *devices) SyncUpdatesNotMe(_ context.Context, in *syncpb.TLSyncUpdatesNotMe) (*mtproto.Void, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sent = append(d.sent, in.Updates)
	return mtproto.EmptyVoid, nil
}

// take returns the albums of each story sent since the last take, by id.
func (d *devices) take() map[int32][]int32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[int32][]int32)
	for _, u := range d.sent {
		for _, x := range u.GetUpdates() {
			if x.GetPredicateName() == mtproto.Predicate_updateStory {
				out[x.GetStory().GetId()] = x.GetStory().GetAlbums()
			}
		}
	}
	d.sent = nil
	return out
}

// people answers for the users of a test: owner has contact as its
// contact, nobody blocks anybody.
type people struct {
	user_client.UserClient
	owner, contact int64
}

func (p people) user(id int64) *mtproto.ImmutableUser {
	u := &mtproto.ImmutableUser{User: mtproto.MakeTLUserData(&mtproto.UserData{Id: id, AccessHash: 1}).To_UserData()}
	if id == p.owner {
		u.Contacts = []*mtproto.ContactData{{UserId: p.owner, ContactUserId: p.contact}}
	}
	return mtproto.MakeTLImmutableUser(u).To_ImmutableUser()
}

func (p people) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	return p.user(in.Id), nil
}

func (p people) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		out.Datas = append(out.Datas, p.user(id))
	}
	return out, nil
}

func (p people) UserBlockedByUser(context.Context, *userpb.TLUserBlockedByUser) (*mtproto.Bool, error) {
	return mtproto.BoolFalse, nil
}

func itDB() *sqlx.DB {
	c := itMySQL.Config()
	return sqlx.NewMySQL(&c)
}

func itCore(d *dao.Dao, userId int64) *StoriesCore {
	ctx := context.Background()
	return &StoriesCore{ctx: ctx, svcCtx: &svc.ServiceContext{Dao: d}, Logger: logx.WithContext(ctx),
		MD: &metadata.RpcMetadata{UserId: userId, PermAuthKeyId: 1}}
}

// postStory stores a photo story of the user owner for the rules given.
func postStory(t *testing.T, s *stories.Store, owner, photo int64, audience string) int32 {
	t.Helper()
	at := now()
	st, created, err := s.Create(context.Background(), &stories.Story{
		Peer: stories.UserPeer(owner), RandomId: itId(), SenderId: owner, Date: at, ExpireDate: at + stories.DefaultPeriod,
		Privacy: []*mtproto.PrivacyRule{{PredicateName: audience}},
		Media: mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Photo_FLAGPHOTO: mtproto.MakeTLPhoto(&mtproto.Photo{
			Id: photo, AccessHash: 1, FileReference: []byte{}, Sizes: []*mtproto.PhotoSize{}, DcId: 1,
		}).To_Photo()}).To_MessageMedia(),
	})
	if err != nil || !created {
		t.Fatalf("story: %v, created %v", err, created)
	}
	return st.Id
}

func wantError(t *testing.T, what string, err error, name string) {
	t.Helper()
	if s, ok := status.FromError(err); !ok || s.Message() != name {
		t.Errorf("%s: %v, want %s", what, err, name)
	}
}

func albumIdsOf(list []*mtproto.StoryAlbum) []int32 {
	out := make([]int32, 0, len(list))
	for _, a := range list {
		out = append(out, a.GetAlbumId())
	}
	return out
}

// The owner's albums through the methods: made with stories and without,
// listed with covers and a hash that answers albumsNotModified while it
// holds, renamed, refilled and reordered, paged, reordered among
// themselves and deleted; every change of the albums of a story reaches
// the other devices with updateStory.
func TestAlbumMethodsOfOwner(t *testing.T) {
	db := itDB()
	owner := itId()
	others := &devices{}
	d := &dao.Dao{Store: stories.NewStore(db), DB: db, SyncClient: others, UserClient: people{owner: owner}}
	c := itCore(d, owner)
	self := selfInputPeer()
	one := postStory(t, d.Store, owner, 501, mtproto.Predicate_privacyValueAllowAll)
	two := postStory(t, d.Store, owner, 502, mtproto.Predicate_privacyValueAllowAll)
	three := postStory(t, d.Store, owner, 503, mtproto.Predicate_privacyValueAllowContacts)

	empty, err := c.StoriesGetAlbums(&mtproto.TLStoriesGetAlbums{Peer: self})
	if err != nil || empty.GetPredicateName() != mtproto.Predicate_stories_albums || len(empty.GetAlbums()) != 0 || empty.GetHash() != 0 {
		t.Fatalf("no albums: %v, %v", empty, err)
	}

	trips, err := c.StoriesCreateAlbum(&mtproto.TLStoriesCreateAlbum{Peer: self, Title: " Trips ", Stories: []int32{two, one, two}})
	if err != nil {
		t.Fatalf("stories.createAlbum: %v", err)
	}
	if trips.GetTitle() != "Trips" || trips.GetIconPhoto().GetId() != 502 {
		t.Errorf("the new album: %v", trips)
	}
	if got := others.take(); len(got) != 2 || !slices.Equal(got[one], []int32{trips.GetAlbumId()}) || !slices.Equal(got[two], []int32{trips.GetAlbumId()}) {
		t.Errorf("the other devices after stories.createAlbum: %v", got)
	}
	cats, err := c.StoriesCreateAlbum(&mtproto.TLStoriesCreateAlbum{Peer: self, Title: "Cats"})
	if err != nil || cats.GetIconPhoto() != nil || cats.GetIconVideo() != nil {
		t.Fatalf("an album without stories: %v, %v", cats, err)
	}
	if got := others.take(); len(got) != 0 {
		t.Errorf("an empty album changed stories: %v", got)
	}
	_, err = c.StoriesCreateAlbum(&mtproto.TLStoriesCreateAlbum{Peer: self, Title: "Gone", Stories: []int32{one, 9999}})
	wantError(t, "an album with a story of nobody", err, "STORY_ID_INVALID")
	_, err = c.StoriesCreateAlbum(&mtproto.TLStoriesCreateAlbum{Peer: self, Title: "   "})
	wantError(t, "a blank title", err, "ALBUM_TITLE_INVALID")
	_, err = c.StoriesCreateAlbum(&mtproto.TLStoriesCreateAlbum{Peer: self, Title: "Thirteen char"})
	wantError(t, "a title of 13 units", err, "ALBUM_TITLE_INVALID")

	list, err := c.StoriesGetAlbums(&mtproto.TLStoriesGetAlbums{Peer: self})
	if err != nil || !slices.Equal(albumIdsOf(list.GetAlbums()), []int32{trips.GetAlbumId(), cats.GetAlbumId()}) || list.GetHash() == 0 {
		t.Fatalf("stories.getAlbums: %v, %v", list, err)
	}
	same, err := c.StoriesGetAlbums(&mtproto.TLStoriesGetAlbums{Peer: self, Hash: list.GetHash()})
	if err != nil || same.GetPredicateName() != mtproto.Predicate_stories_albumsNotModified {
		t.Errorf("stories.getAlbums by its hash: %v, %v", same, err)
	}

	// Out goes two, in comes three, which is put first.
	edited, err := c.StoriesUpdateAlbum(&mtproto.TLStoriesUpdateAlbum{Peer: self, AlbumId: trips.GetAlbumId(),
		DeleteStories: []int32{two}, AddStories: []int32{three}, Order: []int32{three}})
	if err != nil || edited.GetIconPhoto().GetId() != 503 {
		t.Fatalf("stories.updateAlbum: %v, %v", edited, err)
	}
	if got := others.take(); len(got) != 2 || len(got[two]) != 0 || !slices.Equal(got[three], []int32{trips.GetAlbumId()}) {
		t.Errorf("the other devices after stories.updateAlbum: %v", got)
	}
	renamed, err := c.StoriesUpdateAlbum(&mtproto.TLStoriesUpdateAlbum{Peer: self, AlbumId: trips.GetAlbumId(), Title: mtproto.MakeFlagsString("Travel")})
	if err != nil || renamed.GetTitle() != "Travel" {
		t.Errorf("a rename: %v, %v", renamed, err)
	}
	if unchanged, err := c.StoriesUpdateAlbum(&mtproto.TLStoriesUpdateAlbum{Peer: self, AlbumId: trips.GetAlbumId()}); err != nil || unchanged.GetTitle() != "Travel" {
		t.Errorf("stories.updateAlbum without changes: %v, %v", unchanged, err)
	}
	_, err = c.StoriesUpdateAlbum(&mtproto.TLStoriesUpdateAlbum{Peer: self, AlbumId: 9999, Title: mtproto.MakeFlagsString("x")})
	wantError(t, "stories.updateAlbum of no album", err, "ALBUM_ID_INVALID")
	if again, _ := c.StoriesGetAlbums(&mtproto.TLStoriesGetAlbums{Peer: self, Hash: list.GetHash()}); again.GetPredicateName() != mtproto.Predicate_stories_albums || again.GetHash() == list.GetHash() {
		t.Errorf("stories.getAlbums after the changes: %v", again)
	}

	page, err := c.StoriesGetAlbumStories(&mtproto.TLStoriesGetAlbumStories{Peer: self, AlbumId: trips.GetAlbumId(), Offset: 0, Limit: 1})
	if err != nil || page.GetCount() != 2 || !slices.Equal(storyIdsOf(page.GetStories()), []int32{three}) || page.GetPinnedToTop() != nil {
		t.Fatalf("the first page: %v, %v", page, err)
	}
	page, err = c.StoriesGetAlbumStories(&mtproto.TLStoriesGetAlbumStories{Peer: self, AlbumId: trips.GetAlbumId(), Offset: 1, Limit: 1})
	if err != nil || page.GetCount() != 2 || !slices.Equal(storyIdsOf(page.GetStories()), []int32{one}) ||
		!slices.Equal(page.GetStories()[0].GetAlbums(), []int32{trips.GetAlbumId()}) {
		t.Errorf("the second page: %v, %v", page, err)
	}
	if page, err = c.StoriesGetAlbumStories(&mtproto.TLStoriesGetAlbumStories{Peer: self, AlbumId: trips.GetAlbumId(), Offset: 2, Limit: 1}); err != nil || len(page.GetStories()) != 0 {
		t.Errorf("past the end: %v, %v", page, err)
	}
	_, err = c.StoriesGetAlbumStories(&mtproto.TLStoriesGetAlbumStories{Peer: self, AlbumId: 9999, Limit: 10})
	wantError(t, "stories.getAlbumStories of no album", err, "ALBUM_ID_INVALID")

	if ok, err := c.StoriesReorderAlbums(&mtproto.TLStoriesReorderAlbums{Peer: self, Order: []int32{cats.GetAlbumId(), 9999}}); err != nil || !mtproto.FromBool(ok) {
		t.Fatalf("stories.reorderAlbums: %v, %v", ok, err)
	}
	if list, _ = c.StoriesGetAlbums(&mtproto.TLStoriesGetAlbums{Peer: self}); !slices.Equal(albumIdsOf(list.GetAlbums()), []int32{cats.GetAlbumId(), trips.GetAlbumId()}) {
		t.Errorf("the albums after stories.reorderAlbums: %v", albumIdsOf(list.GetAlbums()))
	}

	if ok, err := c.StoriesDeleteAlbum(&mtproto.TLStoriesDeleteAlbum{Peer: self, AlbumId: trips.GetAlbumId()}); err != nil || !mtproto.FromBool(ok) {
		t.Fatalf("stories.deleteAlbum: %v, %v", ok, err)
	}
	if got := others.take(); len(got) != 2 || len(got[one]) != 0 || len(got[three]) != 0 {
		t.Errorf("the other devices after stories.deleteAlbum: %v", got)
	}
	_, err = c.StoriesDeleteAlbum(&mtproto.TLStoriesDeleteAlbum{Peer: self, AlbumId: trips.GetAlbumId()})
	wantError(t, "a second stories.deleteAlbum", err, "ALBUM_ID_INVALID")
	if list, _ = c.StoriesGetAlbums(&mtproto.TLStoriesGetAlbums{Peer: self}); !slices.Equal(albumIdsOf(list.GetAlbums()), []int32{cats.GetAlbumId()}) {
		t.Errorf("the albums after stories.deleteAlbum: %v", albumIdsOf(list.GetAlbums()))
	}
}

// What other users see of the albums: a stranger is shown the albums with
// a story it may open, with the first of them as the cover, and only those
// stories; a contact sees the contacts' story as well; nobody changes
// another's albums.
func TestAlbumsOfOthers(t *testing.T) {
	db := itDB()
	owner, contact, stranger := itId(), itId(), itId()
	users := people{owner: owner, contact: contact}
	d := &dao.Dao{Store: stories.NewStore(db), DB: db, SyncClient: &devices{}, UserClient: users}
	ownerCore := itCore(d, owner)
	self := selfInputPeer()
	public := postStory(t, d.Store, owner, 601, mtproto.Predicate_privacyValueAllowAll)
	private := postStory(t, d.Store, owner, 602, mtproto.Predicate_privacyValueAllowContacts)

	mixed, err := ownerCore.StoriesCreateAlbum(&mtproto.TLStoriesCreateAlbum{Peer: self, Title: "Mixed", Stories: []int32{private, public}})
	if err != nil {
		t.Fatalf("stories.createAlbum: %v", err)
	}
	hidden, err := ownerCore.StoriesCreateAlbum(&mtproto.TLStoriesCreateAlbum{Peer: self, Title: "Hidden", Stories: []int32{private}})
	if err != nil {
		t.Fatalf("stories.createAlbum: %v", err)
	}
	if _, err = ownerCore.StoriesCreateAlbum(&mtproto.TLStoriesCreateAlbum{Peer: self, Title: "Empty"}); err != nil {
		t.Fatalf("stories.createAlbum: %v", err)
	}
	ownerPeer := mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: owner, AccessHash: 1}).To_InputPeer()

	strangerCore := itCore(d, stranger)
	list, err := strangerCore.StoriesGetAlbums(&mtproto.TLStoriesGetAlbums{Peer: ownerPeer})
	if err != nil || !slices.Equal(albumIdsOf(list.GetAlbums()), []int32{mixed.GetAlbumId()}) || list.GetAlbums()[0].GetIconPhoto().GetId() != 601 {
		t.Fatalf("the albums a stranger sees: %v, %v", list, err)
	}
	page, err := strangerCore.StoriesGetAlbumStories(&mtproto.TLStoriesGetAlbumStories{Peer: ownerPeer, AlbumId: mixed.GetAlbumId(), Limit: 10})
	if err != nil || page.GetCount() != 1 || !slices.Equal(storyIdsOf(page.GetStories()), []int32{public}) {
		t.Errorf("the stories of the album a stranger sees: %v, %v", page, err)
	}
	if page, err = strangerCore.StoriesGetAlbumStories(&mtproto.TLStoriesGetAlbumStories{Peer: ownerPeer, AlbumId: hidden.GetAlbumId(), Limit: 10}); err != nil || page.GetCount() != 0 {
		t.Errorf("an album of hidden stories for a stranger: %v, %v", page, err)
	}

	contactCore := itCore(d, contact)
	list, err = contactCore.StoriesGetAlbums(&mtproto.TLStoriesGetAlbums{Peer: ownerPeer})
	if err != nil || !slices.Equal(albumIdsOf(list.GetAlbums()), []int32{mixed.GetAlbumId(), hidden.GetAlbumId()}) || list.GetAlbums()[0].GetIconPhoto().GetId() != 602 {
		t.Errorf("the albums a contact sees: %v, %v", list, err)
	}

	_, err = strangerCore.StoriesCreateAlbum(&mtproto.TLStoriesCreateAlbum{Peer: ownerPeer, Title: "Mine"})
	wantError(t, "an album of another user", err, "PEER_ID_INVALID")
	_, err = strangerCore.StoriesDeleteAlbum(&mtproto.TLStoriesDeleteAlbum{Peer: ownerPeer, AlbumId: mixed.GetAlbumId()})
	wantError(t, "a delete of another user's album", err, "PEER_ID_INVALID")
}

func storyIdsOf(items []*mtproto.StoryItem) []int32 {
	out := make([]int32, 0, len(items))
	for _, it := range items {
		out = append(out, it.GetId())
	}
	return out
}

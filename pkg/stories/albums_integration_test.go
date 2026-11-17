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
	"database/sql"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	_ "github.com/go-sql-driver/mysql"
)

func mustAlbum(t *testing.T, s *Store, peer Peer, title string, ids ...int32) *Album {
	t.Helper()
	a, err := s.CreateAlbum(context.Background(), peer, title, ids, itNow)
	if err != nil {
		t.Fatalf("create album %q: %v", title, err)
	}
	return a
}

func albumIds(list []*Album) []int32 {
	out := make([]int32, 0, len(list))
	for _, a := range list {
		out = append(out, a.Id)
	}
	return out
}

func albumStoryIds(t *testing.T, s *Store, peer Peer, album int32) []int32 {
	t.Helper()
	list, err := s.AlbumStories(context.Background(), peer, album)
	if err != nil {
		t.Fatalf("stories of album %d: %v", album, err)
	}
	return idsOfStories(list)
}

// rawStories writes the stories from..from+n-1 of peer straight into the
// table, past the posting limits, for the limits of albums. They are dated
// before the windows of the limits and out of the feeds, so they stop no
// new story.
func rawStories(t *testing.T, db *sqlx.DB, peer Peer, from, n int32) {
	t.Helper()
	var (
		values []string
		args   []any
	)
	for id := from; id < from+n; id++ {
		values = append(values, "(?, ?, ?, ?, ?, ?, ?, '', '', '', '', '', '', '', '')")
		date := itNow - 40*86400
		args = append(args, peer.Type, peer.Id, id, itId(), peer.Id, date, date+DefaultPeriod)
	}
	if _, err := db.Exec(context.Background(), "INSERT INTO stories (peer_type, peer_id, story_id, random_id, sender_user_id, `date`, expire_date,"+
		" privacy, caption, entities, media, media_areas, fwd_from, music, mentions) VALUES "+strings.Join(values, ", "), args...); err != nil {
		t.Fatal(err)
	}
}

// Albums get ids that are never used again, go after the others, and take
// only the peer's own stories, each once.
func TestAlbumIdsAndCreate(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	one := mustCreate(t, s, itStory(peer, peer.Id, itNow))
	two := mustCreate(t, s, itStory(peer, peer.Id, itNow+1))

	a := mustAlbum(t, s, peer, "  Trips ", two.Id, one.Id, two.Id)
	if a.Id != 1 || a.Title != "Trips" || a.Peer != peer {
		t.Fatalf("the first album: %+v", a)
	}
	if got := albumStoryIds(t, s, peer, a.Id); !slices.Equal(got, []int32{two.Id, one.Id}) {
		t.Errorf("the stories of a new album: %v, want %v", got, []int32{two.Id, one.Id})
	}
	b := mustAlbum(t, s, peer, "Empty")
	if b.Id != 2 {
		t.Fatalf("the second album: %+v", b)
	}
	if _, err := s.DeleteAlbum(ctx, peer, b.Id); err != nil {
		t.Fatal(err)
	}
	c := mustAlbum(t, s, peer, "Again")
	if c.Id != 3 {
		t.Errorf("the id after a deletion: %d, want 3", c.Id)
	}
	list, err := s.Albums(ctx, peer)
	if err != nil || !slices.Equal(albumIds(list), []int32{1, 3}) || list[1].Title != "Again" {
		t.Errorf("albums: %+v, %v", list, err)
	}

	other := UserPeer(itId())
	foreign := mustCreate(t, s, itStory(other, other.Id, itNow))
	if _, err = s.CreateAlbum(ctx, peer, "Bad", []int32{one.Id, foreign.Id + 50}, itNow); !errors.Is(err, ErrStoryInvalid) {
		t.Errorf("an album with a story of none: %v", err)
	}
	if _, err = s.CreateAlbum(ctx, other, "Bad", []int32{foreign.Id, 2}, itNow); !errors.Is(err, ErrStoryInvalid) {
		t.Errorf("an album with a story of another peer: %v", err)
	}
	if _, err = s.CreateAlbum(ctx, peer, strings.Repeat("a", MaxAlbumTitle+1), nil, itNow); !errors.Is(err, ErrAlbumTitleInvalid) {
		t.Errorf("a long title: %v", err)
	}
	if _, err = s.CreateAlbum(ctx, peer, " ", nil, itNow); !errors.Is(err, ErrAlbumTitleInvalid) {
		t.Errorf("an empty title: %v", err)
	}
	if list, _ = s.Albums(ctx, peer); len(list) != 2 {
		t.Errorf("failed requests made albums: %+v", list)
	}
	if _, err = s.AlbumStories(ctx, peer, b.Id); !errors.Is(err, ErrAlbumInvalid) {
		t.Errorf("the stories of a deleted album: %v", err)
	}
	// Each peer counts its own ids.
	if d := mustAlbum(t, s, other, "Mine"); d.Id != 1 {
		t.Errorf("the first album of another peer: %d", d.Id)
	}
}

// Two requests at once at the limit of albums: one passes.
func TestAlbumsLimitConcurrently(t *testing.T) {
	s := NewStore(itDB())
	peer := ChannelPeer(itId())
	for range MaxAlbums - 1 {
		mustAlbum(t, s, peer, "A")
	}
	var (
		wg              sync.WaitGroup
		mu              sync.Mutex
		passed, refused int
	)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateAlbum(context.Background(), peer, "Last", nil, itNow)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				passed++
			case errors.Is(err, ErrAlbumsTooMany):
				refused++
			default:
				t.Errorf("create album: %v", err)
			}
		}()
	}
	wg.Wait()
	if passed != 1 || refused != 1 {
		t.Errorf("at the limit: %d passed, %d refused", passed, refused)
	}
	if list, _ := s.Albums(context.Background(), peer); len(list) != MaxAlbums {
		t.Errorf("%d albums, want %d", len(list), MaxAlbums)
	}
}

// Two requests at once that each fill the last place of an album: one
// passes; an album is never made over the limit.
func TestAlbumFullConcurrently(t *testing.T) {
	db := itDB()
	s, ctx := NewStore(db), context.Background()
	peer := UserPeer(itId())
	rawStories(t, db, peer, 1, MaxAlbumStories+1)
	all := make([]int32, 0, MaxAlbumStories+1)
	for id := int32(1); id <= MaxAlbumStories+1; id++ {
		all = append(all, id)
	}
	if _, err := s.CreateAlbum(ctx, peer, "Big", all, itNow); !errors.Is(err, ErrAlbumFull) {
		t.Fatalf("an album over the limit: %v", err)
	}
	a := mustAlbum(t, s, peer, "Big", all[:MaxAlbumStories-1]...)
	var (
		wg              sync.WaitGroup
		mu              sync.Mutex
		passed, refused int
	)
	for _, id := range all[MaxAlbumStories-1:] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := s.EditAlbum(context.Background(), peer, a.Id, nil, nil, []int32{id}, nil)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				passed++
			case errors.Is(err, ErrAlbumFull):
				refused++
			default:
				t.Errorf("add: %v", err)
			}
		}()
	}
	wg.Wait()
	if passed != 1 || refused != 1 {
		t.Errorf("at the limit: %d passed, %d refused", passed, refused)
	}
	if got := albumStoryIds(t, s, peer, a.Id); len(got) != MaxAlbumStories {
		t.Errorf("%d stories in the album, want %d", len(got), MaxAlbumStories)
	}
	// A new story is passed over by the full album.
	placed, err := func() ([]int32, error) {
		var placed []int32
		err := db.Transact(ctx, func(tx *sqlx.Tx) error {
			if _, err := lockPeerTx(tx, peer); err != nil {
				return err
			}
			var err error
			placed, err = placeTx(tx, peer, all[len(all)-1], []int32{a.Id})
			return err
		})
		return placed, err
	}()
	if err != nil || len(placed) != 0 {
		t.Errorf("a full album took a new story: %v, %v", placed, err)
	}
}

// One edit takes stories out, puts others in and orders them; the named
// ones come first.
func TestEditAlbum(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	var ids []int32
	for i := range 6 {
		ids = append(ids, mustCreate(t, s, itStory(peer, peer.Id, itNow+int32(i))).Id)
	}
	a := mustAlbum(t, s, peer, "Album", ids[0], ids[1], ids[2], ids[3])

	title := " Renamed "
	// Out: 2 (and 6, which is not in it); in: 5, 6 and 1 again (it stays);
	// first: 4, then 5; 9 is not in the album.
	got, changed, err := s.EditAlbum(ctx, peer, a.Id, &title, []int32{ids[1], ids[5]}, []int32{ids[4], ids[5], ids[0], ids[4]}, []int32{ids[3], 9, ids[4]})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Renamed" || got.Id != a.Id {
		t.Errorf("the album: %+v", got)
	}
	slices.Sort(changed)
	if !slices.Equal(changed, []int32{ids[1], ids[4], ids[5]}) {
		t.Errorf("changed: %v", changed)
	}
	want := []int32{ids[3], ids[4], ids[0], ids[2], ids[5]}
	if order := albumStoryIds(t, s, peer, a.Id); !slices.Equal(order, want) {
		t.Errorf("the order: %v, want %v", order, want)
	}
	if list, _ := s.Albums(ctx, peer); len(list) != 1 || list[0].Title != "Renamed" {
		t.Errorf("the title is not stored: %+v", list)
	}

	// Nothing to change: the album as it is.
	got, changed, err = s.EditAlbum(ctx, peer, a.Id, nil, nil, nil, nil)
	if err != nil || got.Title != "Renamed" || len(changed) != 0 {
		t.Errorf("an empty edit: %+v, %v, %v", got, changed, err)
	}
	// An order alone changes no story's albums.
	if _, changed, err = s.EditAlbum(ctx, peer, a.Id, nil, nil, nil, []int32{ids[5]}); err != nil || len(changed) != 0 {
		t.Errorf("an order: %v, %v", changed, err)
	}
	if order := albumStoryIds(t, s, peer, a.Id); order[0] != ids[5] || len(order) != 5 {
		t.Errorf("after the order: %v", order)
	}
	// Taken out and put back in one edit: at the end, named once.
	if _, changed, err = s.EditAlbum(ctx, peer, a.Id, nil, []int32{ids[5]}, []int32{ids[5]}, nil); err != nil || !slices.Equal(changed, []int32{ids[5]}) {
		t.Errorf("out and in again: %v, %v", changed, err)
	}
	if order := albumStoryIds(t, s, peer, a.Id); order[len(order)-1] != ids[5] || len(order) != 5 {
		t.Errorf("after out and in again: %v", order)
	}

	// A story of none refuses the whole edit.
	bad := "Bad"
	if _, _, err = s.EditAlbum(ctx, peer, a.Id, &bad, []int32{ids[3]}, []int32{99}, nil); !errors.Is(err, ErrStoryInvalid) {
		t.Errorf("adding a story of none: %v", err)
	}
	if list, _ := s.Albums(ctx, peer); list[0].Title != "Renamed" {
		t.Errorf("a refused edit renamed the album: %q", list[0].Title)
	}
	if order := albumStoryIds(t, s, peer, a.Id); len(order) != 5 {
		t.Errorf("a refused edit changed the stories: %v", order)
	}
	empty := ""
	if _, _, err = s.EditAlbum(ctx, peer, a.Id, &empty, nil, nil, nil); !errors.Is(err, ErrAlbumTitleInvalid) {
		t.Errorf("an empty title: %v", err)
	}
	if _, _, err = s.EditAlbum(ctx, peer, a.Id+1, nil, nil, nil, nil); !errors.Is(err, ErrAlbumInvalid) {
		t.Errorf("an album of none: %v", err)
	}

	// A story keeps the albums it is in, in the order of the albums.
	b := mustAlbum(t, s, peer, "Second", ids[0])
	if err = s.ReorderAlbums(ctx, peer, []int32{b.Id}); err != nil {
		t.Fatal(err)
	}
	of, err := s.AlbumsOf(ctx, peer, []int32{ids[0], ids[1], ids[3]})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(of[ids[0]], []int32{b.Id, a.Id}) || of[ids[1]] != nil || !slices.Equal(of[ids[3]], []int32{a.Id}) {
		t.Errorf("albums of the stories: %v", of)
	}
}

// The order of albums: the named ones first, the others after them.
func TestReorderAlbums(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	for _, title := range []string{"1", "2", "3", "4"} {
		mustAlbum(t, s, peer, title)
	}
	if err := s.ReorderAlbums(ctx, peer, []int32{3, 77, 1, 3}); err != nil {
		t.Fatal(err)
	}
	list, err := s.Albums(ctx, peer)
	if err != nil || !slices.Equal(albumIds(list), []int32{3, 1, 2, 4}) {
		t.Errorf("after the order: %v, %v", albumIds(list), err)
	}
	// A new album goes last.
	mustAlbum(t, s, peer, "5")
	if list, _ = s.Albums(ctx, peer); !slices.Equal(albumIds(list), []int32{3, 1, 2, 4, 5}) {
		t.Errorf("a new album: %v", albumIds(list))
	}
	if err = s.ReorderAlbums(ctx, UserPeer(itId()), []int32{1}); err != nil {
		t.Errorf("the order of a peer with no albums: %v", err)
	}
}

// A deleted story leaves its albums; a deleted album leaves its stories.
func TestDeleteStoriesAndAlbums(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	one := mustCreate(t, s, itStory(peer, peer.Id, itNow))
	two := mustCreate(t, s, itStory(peer, peer.Id, itNow+1))
	a := mustAlbum(t, s, peer, "A", one.Id, two.Id)
	b := mustAlbum(t, s, peer, "B", one.Id)

	if _, err := s.Delete(ctx, peer, []int32{one.Id}, itNow+2); err != nil {
		t.Fatal(err)
	}
	if got := albumStoryIds(t, s, peer, a.Id); !slices.Equal(got, []int32{two.Id}) {
		t.Errorf("album A after the deletion: %v", got)
	}
	if got := albumStoryIds(t, s, peer, b.Id); len(got) != 0 {
		t.Errorf("album B after the deletion: %v", got)
	}
	var rows int
	if err := s.db.QueryRowPartial(ctx, &rows, "SELECT COUNT(*) FROM story_album_items WHERE peer_type = ? AND peer_id = ? AND story_id = ?", peer.Type, peer.Id, one.Id); err != nil || rows != 0 {
		t.Errorf("rows of the deleted story: %d, %v", rows, err)
	}

	items, err := s.DeleteAlbum(ctx, peer, a.Id)
	if err != nil || !slices.Equal(items, []int32{two.Id}) {
		t.Errorf("delete album: %v, %v", items, err)
	}
	if st, _ := s.One(ctx, peer, two.Id); st == nil {
		t.Errorf("deleting an album deleted its story")
	}
	if _, err = s.DeleteAlbum(ctx, peer, a.Id); !errors.Is(err, ErrAlbumInvalid) {
		t.Errorf("a second deletion: %v", err)
	}
	if list, _ := s.Albums(ctx, peer); !slices.Equal(albumIds(list), []int32{b.Id}) {
		t.Errorf("albums: %v", albumIds(list))
	}
}

// A new story goes into the albums it names that exist and have room, the
// others are passed over; the story, its repeat, its edit and its viewers
// carry the albums.
func TestCreateWithAlbums(t *testing.T) {
	db := itDB()
	s, ctx := NewStore(db), context.Background()
	peer := UserPeer(itId())
	first := mustCreate(t, s, itStory(peer, peer.Id, itNow))
	a := mustAlbum(t, s, peer, "A", first.Id)
	b := mustAlbum(t, s, peer, "B")
	full := mustAlbum(t, s, peer, "Full")
	// Fill the album straight in the table: the posting limits stop far
	// before.
	rawStories(t, db, peer, 1000, MaxAlbumStories)
	if _, err := db.Exec(ctx, "INSERT INTO story_album_items (peer_type, peer_id, album_id, story_id, position)"+
		" SELECT peer_type, peer_id, ?, story_id, story_id FROM stories WHERE peer_type = ? AND peer_id = ? AND story_id >= 1000",
		full.Id, peer.Type, peer.Id); err != nil {
		t.Fatal(err)
	}
	if err := s.ReorderAlbums(ctx, peer, []int32{b.Id, a.Id}); err != nil {
		t.Fatal(err)
	}

	st := itStory(peer, peer.Id, itNow+1)
	st.Albums = []int32{a.Id, 77, full.Id, b.Id, a.Id}
	stored := mustCreate(t, s, st)
	// In the order of the albums of the profile.
	if !slices.Equal(stored.Albums, []int32{b.Id, a.Id}) {
		t.Errorf("the albums of the new story: %v", stored.Albums)
	}
	if got := albumStoryIds(t, s, peer, a.Id); !slices.Equal(got, []int32{first.Id, stored.Id}) {
		t.Errorf("album A: %v", got)
	}
	if got := albumStoryIds(t, s, peer, full.Id); len(got) != MaxAlbumStories || slices.Contains(got, stored.Id) {
		t.Errorf("the full album took the story: %d stories", len(got))
	}
	again, created, err := s.Create(ctx, st)
	if err != nil || created || !slices.Equal(again.Albums, []int32{b.Id, a.Id}) {
		t.Errorf("a repeat: %+v, %v, %v", again, created, err)
	}
	if got := albumStoryIds(t, s, peer, a.Id); len(got) != 2 {
		t.Errorf("a repeat placed the story again: %v", got)
	}

	before, after, err := s.Update(ctx, peer, stored.Id, itNow+2, func(e *Story) { e.Caption = "new" })
	if err != nil || !slices.Equal(before.Albums, []int32{b.Id, a.Id}) || !slices.Equal(after.Albums, []int32{b.Id, a.Id}) {
		t.Errorf("an edit: %v, %v, %v", before, after, err)
	}

	list, err := s.Get(ctx, peer, []int32{first.Id, stored.Id})
	if err != nil {
		t.Fatal(err)
	}
	seen, err := s.SeenOf(ctx, peer, list, peer.Id+1, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := Item(list[0], seen(list[0])).GetAlbums(); !slices.Equal(got, []int32{a.Id}) {
		t.Errorf("the item of the first story: %v", got)
	}
	if got := Item(list[1], seen(list[1])).GetAlbums(); !slices.Equal(got, []int32{b.Id, a.Id}) {
		t.Errorf("the item of the new story: %v", got)
	}
	plain := mustCreate(t, s, itStory(peer, peer.Id, itNow+3))
	if plain.Albums != nil {
		t.Errorf("a story without albums: %v", plain.Albums)
	}
	if seen, err = s.SeenOf(ctx, peer, []*Story{plain}, peer.Id, true); err != nil {
		t.Fatal(err)
	}
	if got := Item(plain, seen(plain)).GetAlbums(); got != nil {
		t.Errorf("the item of a story without albums: %v", got)
	}
}

// A story deleted while it is put into an album: either it is in the album
// and there, or neither; never a row of the album without its story.
func TestDeleteAndAddConcurrently(t *testing.T) {
	s, ctx := NewStore(itDB()), context.Background()
	peer := UserPeer(itId())
	a := mustAlbum(t, s, peer, "A")
	for i := range 20 {
		st := mustCreate(t, s, itStory(peer, peer.Id, itNow+int32(i)))
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := s.Delete(context.Background(), peer, []int32{st.Id}, itNow+int32(i)); err != nil {
				t.Errorf("delete: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, _, err := s.EditAlbum(context.Background(), peer, a.Id, nil, nil, []int32{st.Id}, nil); err != nil && !errors.Is(err, ErrStoryInvalid) {
				t.Errorf("add: %v", err)
			}
		}()
		wg.Wait()
	}
	var orphans int
	if err := s.db.QueryRowPartial(ctx, &orphans, "SELECT COUNT(*) FROM story_album_items i LEFT JOIN stories s"+
		" ON s.peer_type = i.peer_type AND s.peer_id = i.peer_id AND s.story_id = i.story_id"+
		" WHERE i.peer_type = ? AND i.peer_id = ? AND s.story_id IS NULL", peer.Type, peer.Id); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("%d rows of albums without their story", orphans)
	}
}

// The migration of task 104 on a database with the stories of task 084:
// twice in a row, the columns come once and the stories stay.
func TestStoryAlbumsMigration(t *testing.T) {
	const migration = "migrate-20261119-story-albums-search.sql"
	ctx := context.Background()
	files, err := testinfra.SchemaFilesBefore(migration)
	if err != nil {
		t.Fatal(err)
	}
	old, err := testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles(files))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Terminate(ctx) })
	db, err := sql.Open("mysql", old.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("INSERT INTO story_counters (peer_type, peer_id, last_id) VALUES (2, 7, 2)")
	for id := 1; id <= 2; id++ {
		exec("INSERT INTO stories (peer_type, peer_id, story_id, random_id, sender_user_id, `date`, expire_date,"+
			" privacy, caption, entities, media, media_areas, fwd_from, music, mentions) VALUES (2, 7, ?, ?, 7, 100, 200, '', 'c', '', '', '', '', '', '')", id, id)
	}

	path, err := testinfra.SchemaFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = old.ApplySQLFile(ctx, path); err != nil {
			t.Fatal(err)
		}
	}
	columns := func(table, column string) int {
		t.Helper()
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?", table, column).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if columns("story_counters", "last_album_id") != 1 || columns("stories", "search_indexed") != 1 {
		t.Errorf("the new columns are not there once")
	}
	var keys int
	if err = db.QueryRow("SELECT COUNT(DISTINCT index_name) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'stories' AND index_name = 'unindexed'").Scan(&keys); err != nil || keys != 1 {
		t.Errorf("the key unindexed: %d, %v", keys, err)
	}
	for _, table := range []string{"story_albums", "story_album_items", "story_tags", "story_places"} {
		if columns(table, "peer_id") != 1 {
			t.Errorf("table %s is not there", table)
		}
	}
	var stories, unindexed, lastAlbum, lastId int
	if err = db.QueryRow("SELECT COUNT(*), SUM(search_indexed = 0) FROM stories WHERE peer_type = 2 AND peer_id = 7").Scan(&stories, &unindexed); err != nil {
		t.Fatal(err)
	}
	if stories != 2 || unindexed != 2 {
		t.Errorf("the stories after the migration: %d, %d unindexed", stories, unindexed)
	}
	if err = db.QueryRow("SELECT last_id, last_album_id FROM story_counters WHERE peer_type = 2 AND peer_id = 7").Scan(&lastId, &lastAlbum); err != nil || lastId != 2 || lastAlbum != 0 {
		t.Errorf("the counter after the migration: %d, %d, %v", lastId, lastAlbum, err)
	}
}

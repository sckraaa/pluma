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
	"context"
	"errors"
	"strings"
	"unicode/utf16"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// The limits of albums (task 104, ADR 0069, p. 6). The clients read the
// first two from the app config (stories_albums_limit,
// stories_album_stories_limit: Android AppGlobalConfig.java:47-48, iOS
// PeerInfoStoryPaneNode.swift:1831-1843); Telegram's own values are not
// known.
const (
	// MaxAlbums is the most albums of a peer.
	MaxAlbums = 100
	// MaxAlbumStories is the most stories of one album.
	MaxAlbumStories = 1000
	// MaxAlbumTitle is the longest title, in UTF-16 units, as the clients
	// cut it (tdesktop info_stories_albums.cpp:25 kAlbumNameLimit, Android
	// AlertsCreator.java:915-916).
	MaxAlbumTitle = 12
)

var (
	// ErrAlbumsTooMany: the peer has MaxAlbums albums.
	ErrAlbumsTooMany = errors.New("stories: too many albums")
	// ErrAlbumInvalid: the peer has no such album.
	ErrAlbumInvalid = errors.New("stories: no such album")
	// ErrAlbumFull: the album would hold more than MaxAlbumStories stories.
	ErrAlbumFull = errors.New("stories: album is full")
	// ErrStoryInvalid: a story named for an album is not one of the peer's.
	ErrStoryInvalid = errors.New("stories: no such story")
	// ErrAlbumTitleInvalid: the title is empty or longer than MaxAlbumTitle
	// once trimmed (AlbumTitle).
	ErrAlbumTitleInvalid = errors.New("stories: bad album title")
)

// Album is one album of the stories of a peer. Position orders the albums
// of the profile.
type Album struct {
	Peer     Peer
	Id       int32
	Title    string
	Position int64
}

// AlbumTitle is the title of an album as it is stored: raw without the
// spaces around it, and whether it is 1 to MaxAlbumTitle UTF-16 units long.
func AlbumTitle(raw string) (string, bool) {
	title := strings.TrimSpace(raw)
	units := 0
	for _, r := range title {
		units += utf16.RuneLen(r)
	}
	return title, units > 0 && units <= MaxAlbumTitle
}

// uniqueIds is ids without repeats and non-positive ids, in their order.
func uniqueIds(ids []int32) []int32 {
	out := make([]int32, 0, len(ids))
	seen := make(map[int32]bool, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// namedFirst is the order of current with the ids of order that are in it
// first, in the order given, and the rest after them in their order.
func namedFirst(current, order []int32) []int32 {
	in := make(map[int32]bool, len(current))
	for _, id := range current {
		in[id] = true
	}
	out := make([]int32, 0, len(current))
	placed := make(map[int32]bool, len(current))
	for _, id := range order {
		if in[id] && !placed[id] {
			placed[id] = true
			out = append(out, id)
		}
	}
	for _, id := range current {
		if !placed[id] {
			out = append(out, id)
		}
	}
	return out
}

type albumRow struct {
	PeerType int32  `db:"peer_type"`
	PeerId   int64  `db:"peer_id"`
	AlbumId  int32  `db:"album_id"`
	Title    string `db:"title"`
	Position int64  `db:"position"`
}

func (r albumRow) album() *Album {
	return &Album{Peer: Peer{Type: r.PeerType, Id: r.PeerId}, Id: r.AlbumId, Title: r.Title, Position: r.Position}
}

const albumColumns = "peer_type, peer_id, album_id, title, position"

// albumTx reads the album id of peer in tx, nil if there is none.
func albumTx(tx *sqlx.Tx, peer Peer, id int32) (*Album, error) {
	var rows []albumRow
	if err := tx.QueryRowsPartial(&rows, "SELECT "+albumColumns+" FROM story_albums WHERE peer_type = ? AND peer_id = ? AND album_id = ?",
		peer.Type, peer.Id, id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].album(), nil
}

// existingTx is the stories of ids that peer has, in the order of ids.
func existingTx(tx *sqlx.Tx, peer Peer, ids []int32) ([]int32, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var found []int32
	if err := tx.QueryRowsPartial(&found, "SELECT story_id FROM stories WHERE peer_type = ? AND peer_id = ? AND story_id IN ("+in(len(ids))+")",
		int32Args([]any{peer.Type, peer.Id}, ids)...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	has := make(map[int32]bool, len(found))
	for _, id := range found {
		has[id] = true
	}
	out := make([]int32, 0, len(found))
	for _, id := range ids {
		if has[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// albumItemsTx is the stories of the album id of peer, in their order.
func albumItemsTx(tx *sqlx.Tx, peer Peer, id int32) ([]int32, error) {
	var ids []int32
	if err := tx.QueryRowsPartial(&ids, "SELECT story_id FROM story_album_items WHERE peer_type = ? AND peer_id = ? AND album_id = ? ORDER BY position, story_id",
		peer.Type, peer.Id, id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return ids, nil
}

// appendItemsTx puts ids, none of them in the album yet, at the end of the
// album id of peer.
func appendItemsTx(tx *sqlx.Tx, peer Peer, id int32, ids []int32) error {
	if len(ids) == 0 {
		return nil
	}
	var last int64
	if err := tx.QueryRowPartial(&last, "SELECT COALESCE(MAX(position), 0) FROM story_album_items WHERE peer_type = ? AND peer_id = ? AND album_id = ?",
		peer.Type, peer.Id, id); err != nil {
		return err
	}
	args := make([]any, 0, len(ids)*5)
	for i, story := range ids {
		args = append(args, peer.Type, peer.Id, id, story, last+int64(i)+1)
	}
	_, err := tx.Exec("INSERT INTO story_album_items (peer_type, peer_id, album_id, story_id, position) VALUES "+
		strings.TrimSuffix(strings.Repeat("(?, ?, ?, ?, ?), ", len(ids)), ", "), args...)
	return err
}

// albumsOfTx is AlbumsOf in tx.
func albumsOfTx(q func(v any, query string, args ...any) error, peer Peer, ids []int32) (map[int32][]int32, error) {
	out := make(map[int32][]int32)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		StoryId int32 `db:"story_id"`
		AlbumId int32 `db:"album_id"`
	}
	if err := q(&rows, "SELECT i.story_id, i.album_id FROM story_album_items i JOIN story_albums a"+
		" ON a.peer_type = i.peer_type AND a.peer_id = i.peer_id AND a.album_id = i.album_id"+
		" WHERE i.peer_type = ? AND i.peer_id = ? AND i.story_id IN ("+in(len(ids))+") ORDER BY a.position, a.album_id",
		int32Args([]any{peer.Type, peer.Id}, ids)...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	for _, r := range rows {
		out[r.StoryId] = append(out[r.StoryId], r.AlbumId)
	}
	return out, nil
}

// CreateAlbum makes the album title of peer at now, after its other
// albums, with the stories storyIds in their order (repeats count once).
// ErrAlbumTitleInvalid: see AlbumTitle; ErrAlbumsTooMany: the peer has
// MaxAlbums albums; ErrStoryInvalid: one of the stories is not the peer's;
// ErrAlbumFull: more than MaxAlbumStories stories.
func (s *Store) CreateAlbum(ctx context.Context, peer Peer, title string, storyIds []int32, now int32) (*Album, error) {
	title, ok := AlbumTitle(title)
	if !ok {
		return nil, ErrAlbumTitleInvalid
	}
	ids := uniqueIds(storyIds)
	if len(ids) > MaxAlbumStories {
		return nil, ErrAlbumFull
	}
	var album *Album
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		album = nil
		if _, err := lockPeerTx(tx, peer); err != nil {
			return err
		}
		var albums struct {
			Count int32 `db:"n"`
			Last  int64 `db:"last"`
		}
		if err := tx.QueryRowPartial(&albums, "SELECT COUNT(*) AS n, COALESCE(MAX(position), 0) AS last FROM story_albums WHERE peer_type = ? AND peer_id = ?",
			peer.Type, peer.Id); err != nil {
			return err
		}
		if albums.Count >= MaxAlbums {
			return ErrAlbumsTooMany
		}
		found, err := existingTx(tx, peer, ids)
		if err != nil {
			return err
		}
		if len(found) != len(ids) {
			return ErrStoryInvalid
		}
		var last int32
		if err := tx.QueryRowPartial(&last, "SELECT last_album_id FROM story_counters WHERE peer_type = ? AND peer_id = ?", peer.Type, peer.Id); err != nil {
			return err
		}
		a := &Album{Peer: peer, Id: last + 1, Title: title, Position: albums.Last + 1}
		if _, err := tx.Exec("UPDATE story_counters SET last_album_id = ? WHERE peer_type = ? AND peer_id = ?", a.Id, peer.Type, peer.Id); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO story_albums ("+albumColumns+", created_at) VALUES (?, ?, ?, ?, ?, ?)",
			peer.Type, peer.Id, a.Id, a.Title, a.Position, now); err != nil {
			return err
		}
		if err := appendItemsTx(tx, peer, a.Id, ids); err != nil {
			return err
		}
		album = a
		return nil
	})
	if err != nil {
		return nil, err
	}
	return album, nil
}

// EditAlbum changes the album albumId of peer: its title when title is not
// nil, then takes the stories del out of it, puts the stories add at its
// end (those already in it stay where they are), and then puts the stories
// of order that are in it first, in that order, the others after them in
// their order. It returns the album and the stories that came in or went
// out, whose storyItem.albums changed. ErrAlbumInvalid: no such album;
// ErrAlbumTitleInvalid: see AlbumTitle; ErrStoryInvalid: a story of add is
// not the peer's; ErrAlbumFull: the album would hold more than
// MaxAlbumStories stories.
func (s *Store) EditAlbum(ctx context.Context, peer Peer, albumId int32, title *string, del, add, order []int32) (*Album, []int32, error) {
	var newTitle string
	if title != nil {
		var ok bool
		if newTitle, ok = AlbumTitle(*title); !ok {
			return nil, nil, ErrAlbumTitleInvalid
		}
	}
	del, add, order = uniqueIds(del), uniqueIds(add), uniqueIds(order)
	var (
		album   *Album
		changed []int32
	)
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		album, changed = nil, nil
		if _, err := lockPeerTx(tx, peer); err != nil {
			return err
		}
		a, err := albumTx(tx, peer, albumId)
		if err != nil {
			return err
		}
		if a == nil {
			return ErrAlbumInvalid
		}
		if title != nil && newTitle != a.Title {
			if _, err := tx.Exec("UPDATE story_albums SET title = ? WHERE peer_type = ? AND peer_id = ? AND album_id = ?",
				newTitle, peer.Type, peer.Id, albumId); err != nil {
				return err
			}
			a.Title = newTitle
		}
		items, err := albumItemsTx(tx, peer, albumId)
		if err != nil {
			return err
		}
		held := make(map[int32]bool, len(items))
		for _, id := range items {
			held[id] = true
		}
		var gone []int32
		for _, id := range del {
			if held[id] {
				gone = append(gone, id)
				delete(held, id)
			}
		}
		if len(gone) > 0 {
			if _, err := tx.Exec("DELETE FROM story_album_items WHERE peer_type = ? AND peer_id = ? AND album_id = ? AND story_id IN ("+in(len(gone))+")",
				int32Args([]any{peer.Type, peer.Id, albumId}, gone)...); err != nil {
				return err
			}
			kept := items[:0:0]
			for _, id := range items {
				if held[id] {
					kept = append(kept, id)
				}
			}
			items = kept
		}
		var fresh []int32
		for _, id := range add {
			if !held[id] {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) > 0 {
			found, err := existingTx(tx, peer, fresh)
			if err != nil {
				return err
			}
			if len(found) != len(fresh) {
				return ErrStoryInvalid
			}
			if len(items)+len(fresh) > MaxAlbumStories {
				return ErrAlbumFull
			}
			if err := appendItemsTx(tx, peer, albumId, fresh); err != nil {
				return err
			}
			items = append(items, fresh...)
		}
		if len(order) > 0 {
			if err := rewriteItemsTx(tx, peer, albumId, namedFirst(items, order)); err != nil {
				return err
			}
		}
		// A story both taken out and put back counts once.
		album, changed = a, uniqueIds(append(gone, fresh...))
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return album, changed, nil
}

// rewriteItemsTx gives the stories of the album id of peer the positions of
// their order in ids, from 1.
func rewriteItemsTx(tx *sqlx.Tx, peer Peer, id int32, ids []int32) error {
	for i, story := range ids {
		if _, err := tx.Exec("UPDATE story_album_items SET position = ? WHERE peer_type = ? AND peer_id = ? AND album_id = ? AND story_id = ?",
			i+1, peer.Type, peer.Id, id, story); err != nil {
			return err
		}
	}
	return nil
}

// DeleteAlbum removes the album id of peer, and returns the stories that
// were in it, whose storyItem.albums changed; the stories stay.
// ErrAlbumInvalid: no such album.
func (s *Store) DeleteAlbum(ctx context.Context, peer Peer, id int32) ([]int32, error) {
	var items []int32
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		items = nil
		if _, err := lockPeerTx(tx, peer); err != nil {
			return err
		}
		a, err := albumTx(tx, peer, id)
		if err != nil {
			return err
		}
		if a == nil {
			return ErrAlbumInvalid
		}
		if items, err = albumItemsTx(tx, peer, id); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM story_album_items WHERE peer_type = ? AND peer_id = ? AND album_id = ?", peer.Type, peer.Id, id); err != nil {
			return err
		}
		_, err = tx.Exec("DELETE FROM story_albums WHERE peer_type = ? AND peer_id = ? AND album_id = ?", peer.Type, peer.Id, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// ReorderAlbums puts the albums of order that peer has first, in that
// order, and its other albums after them in their order.
func (s *Store) ReorderAlbums(ctx context.Context, peer Peer, order []int32) error {
	order = uniqueIds(order)
	return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		if _, err := lockPeerTx(tx, peer); err != nil {
			return err
		}
		var ids []int32
		if err := tx.QueryRowsPartial(&ids, "SELECT album_id FROM story_albums WHERE peer_type = ? AND peer_id = ? ORDER BY position, album_id",
			peer.Type, peer.Id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
			return err
		}
		for i, id := range namedFirst(ids, order) {
			if _, err := tx.Exec("UPDATE story_albums SET position = ? WHERE peer_type = ? AND peer_id = ? AND album_id = ?",
				i+1, peer.Type, peer.Id, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// Albums returns the albums of peer in their order.
func (s *Store) Albums(ctx context.Context, peer Peer) ([]*Album, error) {
	var rows []albumRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+albumColumns+" FROM story_albums WHERE peer_type = ? AND peer_id = ? ORDER BY position, album_id",
		peer.Type, peer.Id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make([]*Album, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.album())
	}
	return out, nil
}

// AlbumStories returns every story of the album id of peer in the album's
// order, MaxAlbumStories at most; who may see which of them is the caller's
// to decide. ErrAlbumInvalid: no such album.
func (s *Store) AlbumStories(ctx context.Context, peer Peer, id int32) ([]*Story, error) {
	var album []int32
	if err := s.db.QueryRowsPartial(ctx, &album, "SELECT album_id FROM story_albums WHERE peer_type = ? AND peer_id = ? AND album_id = ?",
		peer.Type, peer.Id, id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	if len(album) == 0 {
		return nil, ErrAlbumInvalid
	}
	var ids []int32
	if err := s.db.QueryRowsPartial(ctx, &ids, "SELECT story_id FROM story_album_items WHERE peer_type = ? AND peer_id = ? AND album_id = ? ORDER BY position, story_id LIMIT ?",
		peer.Type, peer.Id, id, MaxAlbumStories); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	list, err := s.Get(ctx, peer, ids)
	if err != nil {
		return nil, err
	}
	byId := make(map[int32]*Story, len(list))
	for _, st := range list {
		byId[st.Id] = st
	}
	out := make([]*Story, 0, len(list))
	for _, id := range ids {
		if st := byId[id]; st != nil {
			out = append(out, st)
		}
	}
	return out, nil
}

// AlbumsOf returns the albums of each of the stories ids of peer, in the
// order of the albums; a story in none has no entry.
func (s *Store) AlbumsOf(ctx context.Context, peer Peer, ids []int32) (map[int32][]int32, error) {
	return albumsOfTx(func(v any, query string, args ...any) error {
		return s.db.QueryRowsPartial(ctx, v, query, args...)
	}, peer, ids)
}

// placeTx puts the new story id of peer at the end of the albums of
// albums that exist and have room, in the transaction that stores it, and
// returns them; the others are passed over (ADR 0069, p. 6).
func placeTx(tx *sqlx.Tx, peer Peer, id int32, albums []int32) ([]int32, error) {
	albums = uniqueIds(albums)
	if len(albums) == 0 {
		return nil, nil
	}
	var rows []struct {
		AlbumId int32 `db:"album_id"`
		Count   int32 `db:"n"`
	}
	if err := tx.QueryRowsPartial(&rows, "SELECT a.album_id, COUNT(i.story_id) AS n FROM story_albums a LEFT JOIN story_album_items i"+
		" ON i.peer_type = a.peer_type AND i.peer_id = a.peer_id AND i.album_id = a.album_id"+
		" WHERE a.peer_type = ? AND a.peer_id = ? AND a.album_id IN ("+in(len(albums))+") GROUP BY a.album_id",
		int32Args([]any{peer.Type, peer.Id}, albums)...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	room := make(map[int32]bool, len(rows))
	for _, r := range rows {
		room[r.AlbumId] = r.Count < MaxAlbumStories
	}
	var placed []int32
	for _, album := range albums {
		if !room[album] {
			continue
		}
		if err := appendItemsTx(tx, peer, album, []int32{id}); err != nil {
			return nil, err
		}
		placed = append(placed, album)
	}
	return placed, nil
}

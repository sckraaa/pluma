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
	"hash/crc32"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/hashx"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// The albums of stories (task 104, ADR 0069). Who sees the profile of a
// peer sees its albums; each viewer is shown in an album only the stories
// it is shown anywhere outside the feeds (shown), and the cover of the
// album and the hash of the list are counted from those: a cover taken
// from a story the viewer may not open would give that story away.

// maxAlbumIds bounds the story ids of one request about an album. The
// clients send them unchunked: Android puts every story selected in the
// profile into one add_stories (StoriesController.java:5156-5163), and
// tdesktop sends the order of every story of the album it loaded
// (data_stories.cpp:2007-2034). No album holds more than this.
const maxAlbumIds = stories.MaxAlbumStories

// maxAlbumStoriesPage bounds a page of stories.getAlbumStories.
const maxAlbumStoriesPage = 100

// albumCover is what stands for an album on its tab: the media of the first
// of list, the stories of the album the viewer is shown, in its order. A
// photo is icon_photo, a video icon_video; tdesktop reads both
// (data_stories.cpp:102-116), Android draws icon_photo only
// (ItemOptions.java:2374-2386). An album the viewer is shown no story of,
// or whose first story has other media, has none.
func albumCover(list []*stories.Story) (*mtproto.Photo, *mtproto.Document) {
	if len(list) == 0 || list[0].Media == nil {
		return nil, nil
	}
	media := list[0].Media
	switch media.GetPredicateName() {
	case mtproto.Predicate_messageMediaPhoto:
		return media.GetPhoto_FLAGPHOTO(), nil
	case mtproto.Predicate_messageMediaDocument:
		return nil, media.GetDocument()
	}
	return nil, nil
}

// storyAlbum is storyAlbum of a for a viewer shown the stories list of it.
func storyAlbum(a *stories.Album, list []*stories.Story) *mtproto.StoryAlbum {
	photo, video := albumCover(list)
	return mtproto.MakeTLStoryAlbum(&mtproto.StoryAlbum{
		AlbumId:   a.Id,
		Title:     a.Title,
		IconPhoto: photo,
		IconVideo: video,
	}).To_StoryAlbum()
}

// albumsHash is stories.albums.hash of the list as one viewer gets it: it
// follows the albums, their order, titles and covers, so that tdesktop,
// which sends it back (data_stories.cpp:1888-1902), is told
// albumsNotModified while none of them changed for it. How Telegram counts
// it is not documented; the clients treat it as opaque.
func albumsHash(list []*mtproto.StoryAlbum) int64 {
	var hash int64
	for i, a := range list {
		hash = hashx.CombineInt64Hash(hash, int64(a.GetAlbumId()))
		hash = hashx.CombineInt64Hash(hash, int64(i))
		hash = hashx.CombineInt64Hash(hash, int64(crc32.ChecksumIEEE([]byte(a.GetTitle()))))
		var cover int64
		if p := a.GetIconPhoto(); p != nil {
			cover = p.GetId()
		} else if d := a.GetIconVideo(); d != nil {
			cover = d.GetId()
		}
		hash = hashx.CombineInt64Hash(hash, cover)
	}
	return hash
}

// albumsUnchanged: the client holds the list of the hash it sent. A hash of
// 0 asks for the list (iOS and Android always send it,
// StoryListContext.swift:1578-1590, StoriesController.java:5002).
func albumsUnchanged(sent, hash int64) bool {
	return sent != 0 && sent == hash
}

// albumShown returns the stories of the album id of v's peer the caller is
// shown, in the album's order. ErrAlbumInvalid: no such album.
func (c *StoriesCore) albumShown(v *peerView, id int32) ([]*stories.Story, error) {
	list, err := c.svcCtx.Dao.Store.AlbumStories(c.ctx, v.peer, id)
	if err != nil {
		return nil, err
	}
	return c.shown(v, list)
}

// albumOf is storyAlbum of a as the caller sees it.
func (c *StoriesCore) albumOf(v *peerView, a *stories.Album) (*mtproto.StoryAlbum, error) {
	list, err := c.albumShown(v, a.Id)
	if err != nil {
		return nil, err
	}
	return storyAlbum(a, list), nil
}

// albumsFor returns the albums of v's peer the caller is shown, in their
// order: all of them to those who manage the peer's stories, to the others
// those with a story they are shown (ADR 0069, p. 2).
func (c *StoriesCore) albumsFor(v *peerView) ([]*mtproto.StoryAlbum, error) {
	albums, err := c.svcCtx.Dao.Store.Albums(c.ctx, v.peer)
	if err != nil {
		return nil, err
	}
	out := make([]*mtproto.StoryAlbum, 0, len(albums))
	for _, a := range albums {
		list, err := c.albumShown(v, a.Id)
		if errors.Is(err, stories.ErrAlbumInvalid) {
			// Deleted since the list was read.
			continue
		}
		if err != nil {
			return nil, err
		}
		if len(list) == 0 && !v.manages() {
			continue
		}
		out = append(out, storyAlbum(a, list))
	}
	return out, nil
}

// pushAlbumChanges sends the caller's other devices the stories ids of v's
// peer, whose storyItem.albums changed, in containers of stories.MaxIds
// stories at most: an album change may touch a whole album.
func (c *StoriesCore) pushAlbumChanges(v *peerView, ids []int32) {
	for len(ids) > 0 {
		n := min(len(ids), stories.MaxIds)
		c.pushChanged(v, ids[:n])
		ids = ids[n:]
	}
}

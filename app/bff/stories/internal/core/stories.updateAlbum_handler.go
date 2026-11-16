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
	"github.com/teamgram/proto/mtproto"
)

// StoriesUpdateAlbum
// stories.updateAlbum#5e5259b6 flags:# peer:InputPeer album_id:int title:flags.0?string delete_stories:flags.1?Vector<int> add_stories:flags.2?Vector<int> order:flags.3?Vector<int> = StoryAlbum;
//
// Renames an album (tdesktop data_stories.cpp:1946-1976), takes stories
// out of it and puts others at its end (Android
// StoriesController.java:5156-5220), and reorders its stories: those of
// order first, in that order, the rest after them, as tdesktop sends the
// order of the stories it loaded (data_stories.cpp:2007-2050). A request
// that changes nothing answers the album. The stories that came in or went
// out reach the caller's other devices with their new albums; Android
// does not wait for the answer and changes storyItem.albums itself
// (StoriesController.java:5165-5172).
func (c *StoriesCore) StoriesUpdateAlbum(in *mtproto.TLStoriesUpdateAlbum) (*mtproto.StoryAlbum, error) {
	v, err := c.albumEditor(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.updateAlbum - error: %v", err)
		return nil, err
	}
	if len(in.DeleteStories) > maxAlbumIds || len(in.AddStories) > maxAlbumIds || len(in.Order) > maxAlbumIds {
		return nil, mtproto.ErrLimitInvalid
	}
	var title *string
	if in.Title != nil {
		value := in.Title.GetValue()
		title = &value
	}
	album, changed, err := c.svcCtx.Dao.Store.EditAlbum(c.ctx, v.peer, in.AlbumId, title, in.DeleteStories, in.AddStories, in.Order)
	if err != nil {
		c.Logger.Errorf("stories.updateAlbum - error: %v", err)
		return nil, albumError(err)
	}
	c.pushAlbumChanges(v, changed)
	answer, err := c.albumOf(v, album)
	if err != nil {
		c.Logger.Errorf("stories.updateAlbum - error: %v", err)
		return nil, albumError(err)
	}
	return answer, nil
}

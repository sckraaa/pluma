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

// StoriesGetAlbumStories
// stories.getAlbumStories#ac806d61 peer:InputPeer album_id:int offset:int limit:int = stories.Stories;
//
// A page of the stories of an album in the album's order, of those the
// caller is shown; count is all of them. offset is the number of stories
// the client already has (Android StoriesController.java:3850-3856,
// tdesktop data_stories.cpp:1854-1858), which keep their order: the
// clients do not sort the stories of an album (tdesktop
// data_stories.cpp:1806-1810). Past the end the page is empty, which
// tdesktop takes for the end (data_stories.cpp:1825).
func (c *StoriesCore) StoriesGetAlbumStories(in *mtproto.TLStoriesGetAlbumStories) (*mtproto.Stories_Stories, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.getAlbumStories - error: %v", err)
		return nil, err
	}
	if in.Offset < 0 {
		return nil, mtproto.ErrOffsetInvalid
	}
	list, err := c.albumShown(v, in.AlbumId)
	if err != nil {
		c.Logger.Errorf("stories.getAlbumStories - error: %v", err)
		return nil, albumError(err)
	}
	page := albumPage(len(list), int(in.Offset), int(in.Limit))
	return c.storiesAnswer(v, list[page[0]:page[1]], int32(len(list)), nil)
}

// albumPage is the bounds of the page of n stories from offset by limit,
// limit taken as 1 to maxAlbumStoriesPage.
func albumPage(n, offset, limit int) [2]int {
	limit = min(max(limit, 1), maxAlbumStoriesPage)
	from := min(offset, n)
	return [2]int{from, min(from+limit, n)}
}

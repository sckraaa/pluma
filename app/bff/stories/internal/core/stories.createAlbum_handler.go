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

// StoriesCreateAlbum
// stories.createAlbum#a36396e5 peer:InputPeer title:string stories:Vector<int> = StoryAlbum;
//
// Makes an album of the peer after its other albums, with the stories
// given in their order: none (Android StoriesController.java:5062), one
// (tdesktop data_stories.cpp:1915-1924) or a list (iOS
// StoryListContext.swift:682-700). The clients put the answer at the end
// of their list (tdesktop data_stories.cpp:1932-1935). The stories joined
// reach the caller's other devices with their new albums.
func (c *StoriesCore) StoriesCreateAlbum(in *mtproto.TLStoriesCreateAlbum) (*mtproto.StoryAlbum, error) {
	v, err := c.albumEditor(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.createAlbum - error: %v", err)
		return nil, err
	}
	if len(in.Stories) > maxAlbumIds {
		return nil, mtproto.ErrLimitInvalid
	}
	album, err := c.svcCtx.Dao.Store.CreateAlbum(c.ctx, v.peer, in.Title, in.Stories, now())
	if err != nil {
		c.Logger.Errorf("stories.createAlbum - error: %v", err)
		return nil, albumError(err)
	}
	c.pushAlbumChanges(v, uniqueStoryIds(in.Stories))
	answer, err := c.albumOf(v, album)
	if err != nil {
		c.Logger.Errorf("stories.createAlbum - error: %v", err)
		return nil, albumError(err)
	}
	return answer, nil
}

// uniqueStoryIds is ids without repeats, in their order.
func uniqueStoryIds(ids []int32) []int32 {
	out := make([]int32, 0, len(ids))
	seen := make(map[int32]bool, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

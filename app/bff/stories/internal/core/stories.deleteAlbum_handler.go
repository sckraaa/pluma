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

// StoriesDeleteAlbum
// stories.deleteAlbum#8d3456d0 peer:InputPeer album_id:int = Bool;
//
// Deletes an album; its stories stay. They reach the caller's other
// devices without it in storyItem.albums. Android and tdesktop do not wait
// for the answer (StoriesController.java:5127-5138, data_stories.cpp:1978-1982).
func (c *StoriesCore) StoriesDeleteAlbum(in *mtproto.TLStoriesDeleteAlbum) (*mtproto.Bool, error) {
	v, err := c.albumEditor(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.deleteAlbum - error: %v", err)
		return nil, err
	}
	items, err := c.svcCtx.Dao.Store.DeleteAlbum(c.ctx, v.peer, in.AlbumId)
	if err != nil {
		c.Logger.Errorf("stories.deleteAlbum - error: %v", err)
		return nil, albumError(err)
	}
	c.pushAlbumChanges(v, items)
	return mtproto.BoolTrue, nil
}

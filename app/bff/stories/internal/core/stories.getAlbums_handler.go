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

// StoriesGetAlbums
// stories.getAlbums#25b3eac7 peer:InputPeer hash:long = stories.Albums;
//
// The albums of the profile of a peer as the caller sees them (albumsFor),
// with their covers. iOS asks for them each time it loads the stories of a
// profile (StoryListContext.swift:1578-1590); tdesktop sends back the hash
// of its list and is answered albumsNotModified while it holds
// (data_stories.cpp:1888-1913).
func (c *StoriesCore) StoriesGetAlbums(in *mtproto.TLStoriesGetAlbums) (*mtproto.Stories_Albums, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.getAlbums - error: %v", err)
		return nil, err
	}
	list, err := c.albumsFor(v)
	if err != nil {
		c.Logger.Errorf("stories.getAlbums - error: %v", err)
		return nil, err
	}
	hash := albumsHash(list)
	if albumsUnchanged(in.Hash, hash) {
		return mtproto.MakeTLStoriesAlbumsNotModified(&mtproto.Stories_Albums{}).To_Stories_Albums(), nil
	}
	return mtproto.MakeTLStoriesAlbums(&mtproto.Stories_Albums{Hash: hash, Albums: list}).To_Stories_Albums(), nil
}

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
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// StoriesReorderAlbums
// stories.reorderAlbums#8535fbd9 peer:InputPeer order:Vector<int> = Bool;
//
// Puts the albums of order first, in that order, and the others after them
// in theirs; ids of no album are passed over. There is no update of the
// list of albums in the schema: the other devices read it again when they
// open the profile.
func (c *StoriesCore) StoriesReorderAlbums(in *mtproto.TLStoriesReorderAlbums) (*mtproto.Bool, error) {
	v, err := c.albumEditor(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.reorderAlbums - error: %v", err)
		return nil, err
	}
	if len(in.Order) > stories.MaxAlbums {
		return nil, mtproto.ErrLimitInvalid
	}
	if err := c.svcCtx.Dao.Store.ReorderAlbums(c.ctx, v.peer, in.Order); err != nil {
		c.Logger.Errorf("stories.reorderAlbums - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

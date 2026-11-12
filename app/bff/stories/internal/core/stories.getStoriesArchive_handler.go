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

// StoriesGetStoriesArchive
// stories.getStoriesArchive#b4352016 peer:InputPeer offset_id:int limit:int = stories.Stories;
//
// A page of every story of the peer, newest first, below offset_id: the
// archive, which keeps the stories that left the feeds. Only the account
// itself and the admins of a channel with a right over stories read it.
func (c *StoriesCore) StoriesGetStoriesArchive(in *mtproto.TLStoriesGetStoriesArchive) (*mtproto.Stories_Stories, error) {
	v, err := c.manager(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.getStoriesArchive - error: %v", err)
		return nil, err
	}
	list, count, err := c.svcCtx.Dao.Store.Archive(c.ctx, v.peer, in.OffsetId, clampLimit(in.Limit, 20, 100))
	if err != nil {
		c.Logger.Errorf("stories.getStoriesArchive - error: %v", err)
		return nil, err
	}
	return c.storiesAnswer(v, list, count, nil)
}

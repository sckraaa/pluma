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
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// maxProfileStories bounds the stories of a profile read at once for a
// viewer whose view of them the rules of each story decide.
const maxProfileStories = 1000

// StoriesGetPinnedStories
// stories.getPinnedStories#5821a5dc peer:InputPeer offset_id:int limit:int = stories.Stories;
//
// A page of the stories of the profile of a peer, newest first, below
// offset_id: those the caller may open. pinned_to_top are the ones at the
// top of the profile, in their order.
func (c *StoriesCore) StoriesGetPinnedStories(in *mtproto.TLStoriesGetPinnedStories) (*mtproto.Stories_Stories, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.getPinnedStories - error: %v", err)
		return nil, err
	}
	limit := clampLimit(in.Limit, 20, 100)
	var (
		list  []*stories.Story
		count int32
	)
	if v.own || !v.peer.IsUser() {
		list, count, err = c.svcCtx.Dao.Store.Pinned(c.ctx, v.peer, in.OffsetId, limit)
	} else {
		// The rules of each story decide what another user sees, so the
		// count is of what is left after them, not of the profile.
		list, err = c.profileOf(v)
		count = int32(len(list))
		if in.OffsetId > 0 {
			list = slices.DeleteFunc(slices.Clone(list), func(st *stories.Story) bool { return st.Id >= in.OffsetId })
		}
		if int32(len(list)) > limit {
			list = list[:limit]
		}
	}
	if err != nil {
		c.Logger.Errorf("stories.getPinnedStories - error: %v", err)
		return nil, err
	}
	top, err := c.svcCtx.Dao.Store.PinnedToTop(c.ctx, v.peer)
	if err != nil {
		c.Logger.Errorf("stories.getPinnedStories - error: %v", err)
		return nil, err
	}
	if !v.own && v.peer.IsUser() {
		// Only the ones the caller sees.
		seen, err := c.profileOf(v)
		if err != nil {
			c.Logger.Errorf("stories.getPinnedStories - error: %v", err)
			return nil, err
		}
		ids := idsOf(seen)
		top = slices.DeleteFunc(top, func(id int32) bool { return !slices.Contains(ids, id) })
	}
	return c.storiesAnswer(v, list, count, top)
}

// profileOf returns the stories of the profile of another user that the
// caller may open, newest first.
func (c *StoriesCore) profileOf(v *peerView) ([]*stories.Story, error) {
	list, _, err := c.svcCtx.Dao.Store.Pinned(c.ctx, v.peer, 0, maxProfileStories)
	if err != nil {
		return nil, err
	}
	return c.visible(v, list)
}

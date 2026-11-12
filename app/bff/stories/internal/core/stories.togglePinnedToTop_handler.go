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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// StoriesTogglePinnedToTop
// stories.togglePinnedToTop#b297e9b peer:InputPeer id:Vector<int> = Bool;
//
// Makes the stories id, in their order, the ones at the top of the
// profile; none clears the top. They must be in the profile, and there are
// stories.MaxPinnedToTop at most (Android sends the whole new list,
// StoriesController.java:4184-4190).
func (c *StoriesCore) StoriesTogglePinnedToTop(in *mtproto.TLStoriesTogglePinnedToTop) (*mtproto.Bool, error) {
	v, err := c.manager(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.togglePinnedToTop - error: %v", err)
		return nil, err
	}
	if len(in.Id) > stories.MaxPinnedToTop {
		return nil, mtproto.ErrLimitInvalid
	}
	seen := map[int32]bool{}
	for _, id := range in.Id {
		if seen[id] {
			return nil, errStoryIdInvalid
		}
		seen[id] = true
	}
	if err = c.svcCtx.Dao.Store.SetPinnedToTop(c.ctx, v.peer, in.Id); err != nil {
		if errors.Is(err, stories.ErrNotPinned) {
			return nil, errStoryIdInvalid
		}
		c.Logger.Errorf("stories.togglePinnedToTop - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

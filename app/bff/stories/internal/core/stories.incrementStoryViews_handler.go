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

// StoriesIncrementStoryViews
// stories.incrementStoryViews#b2028afb peer:InputPeer id:Vector<int> = Bool;
//
// The caller opened these stories of the peer outside the feeds: from its
// profile or its archive (Android StoriesController.java:3769-3779). Each
// the caller may open gets its view once.
func (c *StoriesCore) StoriesIncrementStoryViews(in *mtproto.TLStoriesIncrementStoryViews) (*mtproto.Bool, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.incrementStoryViews - error: %v", err)
		return nil, err
	}
	if len(in.Id) == 0 {
		return nil, mtproto.ErrStoryIdEmpty
	}
	if len(in.Id) > stories.MaxIds {
		return nil, mtproto.ErrLimitInvalid
	}
	list, err := c.svcCtx.Dao.Store.Get(c.ctx, v.peer, in.Id)
	if err != nil {
		c.Logger.Errorf("stories.incrementStoryViews - error: %v", err)
		return nil, err
	}
	if list, err = c.shown(v, list); err != nil {
		c.Logger.Errorf("stories.incrementStoryViews - error: %v", err)
		return nil, err
	}
	if err = c.viewed(v, idsOf(list), now()); err != nil {
		c.Logger.Errorf("stories.incrementStoryViews - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

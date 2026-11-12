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

// StoriesGetStoriesViews
// stories.getStoriesViews#28e16cc8 peer:InputPeer id:Vector<int> = stories.StoryViews;
//
// The counters of stories, in the order of id: of the account's own
// stories, with the last viewers, and of a channel's for whoever reads the
// channel. A story that is gone answers zeros, to keep the order.
func (c *StoriesCore) StoriesGetStoriesViews(in *mtproto.TLStoriesGetStoriesViews) (*mtproto.Stories_StoryViews, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.getStoriesViews - error: %v", err)
		return nil, err
	}
	if v.peer.IsUser() && !v.own {
		return nil, mtproto.ErrPeerIdInvalid
	}
	if len(in.Id) == 0 {
		return nil, mtproto.ErrStoryIdEmpty
	}
	if len(in.Id) > stories.MaxIds {
		return nil, mtproto.ErrLimitInvalid
	}
	list, err := c.svcCtx.Dao.Store.Get(c.ctx, v.peer, in.Id)
	if err != nil {
		c.Logger.Errorf("stories.getStoriesViews - error: %v", err)
		return nil, err
	}
	stats, err := c.svcCtx.Dao.Store.StatsOf(c.ctx, v.peer, idsOf(list), 0)
	if err != nil {
		c.Logger.Errorf("stories.getStoriesViews - error: %v", err)
		return nil, err
	}
	forwards := map[int32]int32{}
	for _, st := range list {
		forwards[st.Id] = st.ForwardsCount
	}
	var (
		views = make([]*mtproto.StoryViews, 0, len(in.Id))
		r     = &refs{}
	)
	for _, id := range in.Id {
		views = append(views, stories.Views(stats[id], forwards[id], v.own))
		if v.own && stats[id] != nil {
			r.user(stats[id].Recent...)
		}
	}
	users, _, err := c.resolve(r)
	if err != nil {
		c.Logger.Errorf("stories.getStoriesViews - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLStoriesStoryViews(&mtproto.Stories_StoryViews{
		Views: views,
		Users: users,
	}).To_Stories_StoryViews(), nil
}

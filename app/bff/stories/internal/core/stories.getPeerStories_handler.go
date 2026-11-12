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

// StoriesGetPeerStories
// stories.getPeerStories#2c4ada50 peer:InputPeer = stories.PeerStories;
//
// The stories of a peer that are in the feeds, those the caller may open.
// A peer without any answers an empty list: Android stores it as the
// stories of the peer (StoriesController.java:1095-1112).
func (c *StoriesCore) StoriesGetPeerStories(in *mtproto.TLStoriesGetPeerStories) (*mtproto.Stories_PeerStories, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.getPeerStories - error: %v", err)
		return nil, err
	}
	ps, r, err := c.activeOf(v)
	if err != nil {
		c.Logger.Errorf("stories.getPeerStories - error: %v", err)
		return nil, err
	}
	users, chats, err := c.resolve(r)
	if err != nil {
		c.Logger.Errorf("stories.getPeerStories - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLStoriesPeerStories(&mtproto.Stories_PeerStories{
		Stories: ps,
		Chats:   chats,
		Users:   users,
	}).To_Stories_PeerStories(), nil
}

// activeOf is peerStories of the stories of v's peer in the feeds that the
// caller may open, with what they refer to.
func (c *StoriesCore) activeOf(v *peerView) (*mtproto.PeerStories, *refs, error) {
	list, err := c.svcCtx.Dao.Store.Active(c.ctx, v.peer, now())
	if err != nil {
		return nil, nil, err
	}
	if list, err = c.visible(v, list); err != nil {
		return nil, nil, err
	}
	ps, err := c.peerStories(v, list)
	if err != nil {
		return nil, nil, err
	}
	r := &refs{}
	r.peer(v.peer)
	for _, st := range list {
		r.story(st, v.own)
	}
	return ps, r, nil
}

// clampLimit bounds the limit of a page: 0 or less is the default.
func clampLimit(limit, def, max int32) int32 {
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}

// idsOf are the ids of the stories of list.
func idsOf(list []*stories.Story) []int32 {
	ids := make([]int32, 0, len(list))
	for _, st := range list {
		ids = append(ids, st.Id)
	}
	return ids
}

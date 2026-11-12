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
	"fmt"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// items builds storyItem of each story of list, stories of v's peer the
// caller may open, as the caller sees them.
func (c *StoriesCore) items(v *peerView, list []*stories.Story) ([]*mtproto.StoryItem, error) {
	seen, err := c.svcCtx.Dao.Store.SeenOf(c.ctx, v.peer, list, c.MD.UserId, v.own)
	if err != nil {
		return nil, err
	}
	out := make([]*mtproto.StoryItem, 0, len(list))
	for _, st := range list {
		out = append(out, stories.Item(st, seen(st)))
	}
	return out, nil
}

// peerStories builds peerStories of list for the caller, with how far the
// caller has read the peer.
func (c *StoriesCore) peerStories(v *peerView, list []*stories.Story) (*mtproto.PeerStories, error) {
	seen, err := c.svcCtx.Dao.Store.SeenOf(c.ctx, v.peer, list, c.MD.UserId, v.own)
	if err != nil {
		return nil, err
	}
	maxRead, err := c.svcCtx.Dao.Store.MaxRead(c.ctx, c.MD.UserId, v.peer)
	if err != nil {
		return nil, err
	}
	return stories.PeerStories(v.peer, list, maxRead, seen), nil
}

// storiesAnswer is stories.stories of list with what it refers to.
func (c *StoriesCore) storiesAnswer(v *peerView, list []*stories.Story, count int32, pinnedToTop []int32) (*mtproto.Stories_Stories, error) {
	items, err := c.items(v, list)
	if err != nil {
		return nil, err
	}
	r := &refs{}
	r.peer(v.peer)
	for _, st := range list {
		r.story(st, v.own)
	}
	users, chats, err := c.resolve(r)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLStoriesStories(&mtproto.Stories_Stories{
		Count:       count,
		Stories:     items,
		PinnedToTop: pinnedToTop,
		Chats:       chats,
		Users:       users,
	}).To_Stories_Stories(), nil
}

// stealthMode is storiesStealthMode of st at the time at: a date that has
// passed is left out.
func stealthMode(st stories.Stealth, at int32) *mtproto.StoriesStealthMode {
	out := &mtproto.StoriesStealthMode{}
	if st.ActiveUntil > at {
		out.ActiveUntilDate = mtproto.MakeFlagsInt32(st.ActiveUntil)
	}
	if st.CooldownUntil > at {
		out.CooldownUntilDate = mtproto.MakeFlagsInt32(st.CooldownUntil)
	}
	return mtproto.MakeTLStoriesStealthMode(out).To_StoriesStealthMode()
}

// refreshMaxId tells the service of users that the stories of the peer in
// the feeds changed at the time at, so that user.stories_max_id of every
// viewer follows (the service reads the stories with the user, ADR 0054),
// and clears the mark of the change. A channel's are read with the channel
// each time and need no telling. On an error the mark stays and the worker
// repeats the request.
func refreshMaxId(c *StoriesCore, peer stories.Peer, at int32) error {
	if peer.IsUser() {
		id, err := c.svcCtx.Dao.Store.MaxActiveId(c.ctx, peer, now())
		if err != nil {
			return err
		}
		ok, err := c.svcCtx.Dao.UserSetStoriesMaxId(c.ctx, &userpb.TLUserSetStoriesMaxId{UserId: peer.Id, Id: id})
		if err != nil {
			return err
		}
		if !mtproto.FromBool(ok) {
			return fmt.Errorf("user.setStoriesMaxId of user %d: not stored", peer.Id)
		}
	}
	return c.svcCtx.Dao.Store.ClearDirty(c.ctx, peer, at)
}

// changed is refreshMaxId after a change the request made: the change is
// stored, so a failure here is logged and left to the worker.
func (c *StoriesCore) changed(peer stories.Peer, at int32) {
	if err := refreshMaxId(c, peer, at); err != nil {
		c.Logger.Errorf("stories: refresh of peer %d/%d: %v", peer.Type, peer.Id, err)
	}
}

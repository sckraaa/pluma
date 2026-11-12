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

// ActiveStories is userFull.stories of the user userId for the caller: its
// stories in the feeds that the caller may open, nil when there are none.
func (c *StoriesCore) ActiveStories(userId int64) (*mtproto.PeerStories, error) {
	v, err := c.viewOf(stories.UserPeer(userId))
	if err != nil {
		return nil, err
	}
	ps, _, err := c.activeOf(v)
	if err != nil || len(ps.GetStories()) == 0 {
		return nil, err
	}
	return ps, nil
}

// PinnedAvailable is userFull.stories_pinned_available of the user userId
// for the caller: its profile has stories the caller may open.
func (c *StoriesCore) PinnedAvailable(userId int64) (bool, error) {
	v, err := c.viewOf(stories.UserPeer(userId))
	if err != nil {
		return false, err
	}
	if v.own {
		return c.svcCtx.Dao.Store.HasPinned(c.ctx, v.peer)
	}
	list, err := c.profileOf(v)
	return len(list) > 0, err
}

// CheckStory answers whether the caller may put the story id of peer into
// a message: STORY_ID_INVALID for one it may not open (or that is gone),
// and with share CHAT_FORWARDS_RESTRICTED for one whose owner forbade
// sharing, the owner aside. A story that left the feeds and is in no
// profile is open to those who manage its peer only. The share is counted
// once the message is sent (StoryShared).
func (c *StoriesCore) CheckStory(peer *mtproto.PeerUtil, id int32, share bool) error {
	var p stories.Peer
	switch peer.PeerType {
	case mtproto.PEER_USER:
		p = stories.UserPeer(peer.PeerId)
	case mtproto.PEER_CHANNEL:
		p = stories.ChannelPeer(peer.PeerId)
	default:
		return mtproto.ErrPeerIdInvalid
	}
	v, err := c.viewOf(p)
	if err != nil {
		return err
	}
	st, err := c.svcCtx.Dao.Store.One(c.ctx, v.peer, id)
	if err != nil {
		return err
	}
	if st == nil {
		return errStoryIdInvalid
	}
	list, err := c.shown(v, []*stories.Story{st})
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return errStoryIdInvalid
	}
	if !share {
		return nil
	}
	if st.Noforwards && !v.own {
		return mtproto.ErrChatForwardsRestricted
	}
	return nil
}

// StoryShared counts the message the caller sent with randomId that shares
// the story id of peer (storyViews.forwards_count): once per message, so a
// repeated request counts nothing more.
func (c *StoriesCore) StoryShared(peer *mtproto.PeerUtil, id int32, randomId int64) error {
	var p stories.Peer
	switch peer.PeerType {
	case mtproto.PEER_USER:
		p = stories.UserPeer(peer.PeerId)
	case mtproto.PEER_CHANNEL:
		p = stories.ChannelPeer(peer.PeerId)
	default:
		return mtproto.ErrPeerIdInvalid
	}
	return c.svcCtx.Dao.Store.AddShare(c.ctx, p, id, c.MD.UserId, randomId, now())
}

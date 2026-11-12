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

// StoriesCanSendStory30EB63F0
// stories.canSendStory#30eb63f0 peer:InputPeer = stories.CanSendStoryCount;
//
// How many more stories the peer may post now. Android asks it for the
// account and before posting to a channel, and reads the limits from the
// errors (StoriesController.java:4535-4660). Boosts are not needed for a
// channel's stories (ADR 0041).
func (c *StoriesCore) StoriesCanSendStory30EB63F0(in *mtproto.TLStoriesCanSendStory30EB63F0) (*mtproto.Stories_CanSendStoryCount, error) {
	remains, err := c.canSend(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.canSendStory - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLStoriesCanSendStoryCount(&mtproto.Stories_CanSendStoryCount{
		CountRemains: remains,
	}).To_Stories_CanSendStoryCount(), nil
}

// canSend answers how many more stories the peer may have in the feeds, or
// why the caller posts none to it.
func (c *StoriesCore) canSend(peer *mtproto.InputPeer) (int32, error) {
	v, err := c.poster(peer)
	if err != nil {
		return 0, err
	}
	remains, err := c.svcCtx.Dao.Store.Remains(c.ctx, v.peer, now())
	if err != nil {
		return 0, limitError(err)
	}
	return remains, nil
}

// poster resolves the peer a story is posted to and checks that the caller
// posts there: its own account, or a channel it is an admin of with
// post_stories. Bots post no stories.
func (c *StoriesCore) poster(peer *mtproto.InputPeer) (*peerView, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	v, err := c.view(peer)
	if err != nil {
		return nil, err
	}
	if !v.canPost() {
		if v.peer.IsUser() {
			return nil, mtproto.ErrPeerIdInvalid
		}
		return nil, mtproto.ErrChatAdminRequired
	}
	return v, nil
}

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

// StoriesTogglePinned
// stories.togglePinned#9a75a1ef peer:InputPeer id:Vector<int> pinned:Bool = Vector<int>;
//
// Puts stories into the profile of the peer (they stay there after they
// leave the feeds) or takes them out, and answers the ids it found.
func (c *StoriesCore) StoriesTogglePinned(in *mtproto.TLStoriesTogglePinned) (*mtproto.Vector_Int, error) {
	v, err := c.manager(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.togglePinned - error: %v", err)
		return nil, err
	}
	if len(in.Id) == 0 {
		return nil, mtproto.ErrStoryIdEmpty
	}
	if len(in.Id) > stories.MaxIds {
		return nil, mtproto.ErrLimitInvalid
	}
	found, err := c.svcCtx.Dao.Store.SetPinned(c.ctx, v.peer, in.Id, mtproto.FromBool(in.Pinned))
	if err != nil {
		c.Logger.Errorf("stories.togglePinned - error: %v", err)
		return nil, err
	}
	c.pushChanged(v, found)
	if found == nil {
		found = []int32{}
	}
	return &mtproto.Vector_Int{Datas: found}, nil
}

// manager resolves the peer of a request that arranges its stories and
// checks that the caller does: the account itself, an admin of the channel
// with a right over stories.
func (c *StoriesCore) manager(peer *mtproto.InputPeer) (*peerView, error) {
	if c.MD.IsBot {
		return nil, mtproto.ErrBotMethodInvalid
	}
	v, err := c.view(peer)
	if err != nil {
		return nil, err
	}
	if !v.manages() {
		if v.peer.IsUser() {
			return nil, mtproto.ErrPeerIdInvalid
		}
		return nil, mtproto.ErrChatAdminRequired
	}
	return v, nil
}

// pushChanged sends the caller's other devices the stories ids of v's peer
// as they are now.
func (c *StoriesCore) pushChanged(v *peerView, ids []int32) {
	if len(ids) == 0 {
		return
	}
	list, err := c.svcCtx.Dao.Store.Get(c.ctx, v.peer, ids)
	if err != nil {
		c.Logger.Errorf("stories: push of changed stories: %v", err)
		return
	}
	items, err := c.items(v, list)
	if err != nil {
		c.Logger.Errorf("stories: push of changed stories: %v", err)
		return
	}
	updates := make([]*mtproto.Update, 0, len(items))
	for _, item := range items {
		updates = append(updates, updateStory(v.peer, item))
	}
	c.pushOthers(updatesOf(updates, nil, nil))
}

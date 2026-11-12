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

// StoriesDeleteStories
// stories.deleteStories#ae59db5f peer:InputPeer id:Vector<int> = Vector<int>;
//
// Deletes stories for good, with their views, and answers the ids that
// were deleted. In a channel an admin deletes its own stories, and those
// of the others with delete_stories; the ones it may not delete are left
// and not answered.
func (c *StoriesCore) StoriesDeleteStories(in *mtproto.TLStoriesDeleteStories) (*mtproto.Vector_Int, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.deleteStories - error: %v", err)
		return nil, err
	}
	if len(in.Id) == 0 {
		return nil, mtproto.ErrStoryIdEmpty
	}
	if len(in.Id) > stories.MaxIds {
		return nil, mtproto.ErrLimitInvalid
	}
	if v.peer.IsUser() && !v.own {
		return nil, mtproto.ErrPeerIdInvalid
	}
	list, err := c.svcCtx.Dao.Store.Get(c.ctx, v.peer, in.Id)
	if err != nil {
		c.Logger.Errorf("stories.deleteStories - error: %v", err)
		return nil, err
	}
	var ids []int32
	for _, st := range list {
		if v.canDelete(st, c.MD.UserId) {
			ids = append(ids, st.Id)
		}
	}
	if len(list) > 0 && len(ids) == 0 && !v.peer.IsUser() {
		return nil, mtproto.ErrChatAdminRequired
	}
	at := now()
	deleted, err := c.svcCtx.Dao.Store.Delete(c.ctx, v.peer, ids, at)
	if err != nil {
		c.Logger.Errorf("stories.deleteStories - error: %v", err)
		return nil, err
	}
	if len(deleted) > 0 {
		c.changed(v.peer, at)
		updates := make([]*mtproto.Update, 0, len(deleted))
		for _, st := range deleted {
			c.announce(st, nil, true)
			updates = append(updates, updateStory(st.Peer, stories.Deleted(st.Id)))
		}
		c.pushOthers(updatesOf(updates, nil, nil))
	}
	return &mtproto.Vector_Int{Datas: idsOf(deleted)}, nil
}

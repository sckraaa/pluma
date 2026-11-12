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

// StoriesReadStories
// stories.readStories#a556dac8 peer:InputPeer max_id:int = Vector<int>;
//
// The caller has watched the stories of the peer in the feeds up to
// max_id. It is how the stories of the feeds are counted as viewed: the
// clients call nothing else while one watches them (Android
// StoriesController.java:1329-1351; stories.incrementStoryViews is for the
// stories opened from a profile). The stories between the one read before
// and max_id that the caller may open get its view, unless they are its
// own or its stealth mode is on, and their ids are the answer. The other
// devices of the caller get updateReadStories.
func (c *StoriesCore) StoriesReadStories(in *mtproto.TLStoriesReadStories) (*mtproto.Vector_Int, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.readStories - error: %v", err)
		return nil, err
	}
	at := now()
	list, err := c.svcCtx.Dao.Store.Active(c.ctx, v.peer, at)
	if err != nil {
		c.Logger.Errorf("stories.readStories - error: %v", err)
		return nil, err
	}
	if list, err = c.visible(v, list); err != nil {
		c.Logger.Errorf("stories.readStories - error: %v", err)
		return nil, err
	}
	before, err := c.svcCtx.Dao.Store.MaxRead(c.ctx, c.MD.UserId, v.peer)
	if err != nil {
		c.Logger.Errorf("stories.readStories - error: %v", err)
		return nil, err
	}
	// What is read is a story that exists: an id beyond the newest one
	// would mark stories not posted yet as read.
	var (
		read  = []int32{}
		maxId int32
	)
	for _, st := range list {
		if st.Id > in.MaxId {
			break
		}
		maxId = st.Id
		if st.Id > before {
			read = append(read, st.Id)
		}
	}
	if maxId <= before {
		return &mtproto.Vector_Int{Datas: []int32{}}, nil
	}
	if err = c.viewed(v, read, at); err != nil {
		c.Logger.Errorf("stories.readStories - error: %v", err)
		return nil, err
	}
	changed, err := c.svcCtx.Dao.Store.MarkRead(c.ctx, c.MD.UserId, v.peer, maxId)
	if err != nil {
		c.Logger.Errorf("stories.readStories - error: %v", err)
		return nil, err
	}
	if changed {
		c.pushOthers(updatesOf([]*mtproto.Update{updateReadStories(v.peer, maxId)}, nil, nil))
	}
	return &mtproto.Vector_Int{Datas: read}, nil
}

// viewed records the caller's view of the stories ids of v's peer. Its own
// stories take none, and neither do the stories of users while the
// caller's stealth mode is on: the mode hides whom the viewer watched.
func (c *StoriesCore) viewed(v *peerView, ids []int32, at int32) error {
	if v.own || len(ids) == 0 {
		return nil
	}
	if v.peer.IsUser() {
		stealth, err := c.svcCtx.Dao.Store.Stealth(c.ctx, c.MD.UserId)
		if err != nil {
			return err
		}
		if stealth.Active(at) {
			return nil
		}
	}
	_, err := c.svcCtx.Dao.Store.AddViews(c.ctx, v.peer, ids, c.MD.UserId, at)
	return err
}

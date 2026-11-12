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

// StoriesGetStoriesByID
// stories.getStoriesByID#5774ca74 peer:InputPeer id:Vector<int> = stories.Stories;
//
// The stories id of the peer that the caller may open: those in the feeds
// and of the profile, and for the one who manages the peer all of them. A
// story that is gone or not for the caller is left out, which the clients
// read as a story that is no more (Android StoriesController.java:1703-1720
// resolves the story of a message or of a link this way).
func (c *StoriesCore) StoriesGetStoriesByID(in *mtproto.TLStoriesGetStoriesByID) (*mtproto.Stories_Stories, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.getStoriesByID - error: %v", err)
		return nil, err
	}
	if len(in.Id) > stories.MaxIds {
		return nil, mtproto.ErrLimitInvalid
	}
	list, err := c.svcCtx.Dao.Store.Get(c.ctx, v.peer, in.Id)
	if err != nil {
		c.Logger.Errorf("stories.getStoriesByID - error: %v", err)
		return nil, err
	}
	if list, err = c.shown(v, list); err != nil {
		c.Logger.Errorf("stories.getStoriesByID - error: %v", err)
		return nil, err
	}
	return c.storiesAnswer(v, list, int32(len(list)), nil)
}

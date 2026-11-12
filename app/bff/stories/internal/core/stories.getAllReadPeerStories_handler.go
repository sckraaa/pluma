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

// StoriesGetAllReadPeerStories
// stories.getAllReadPeerStories#9b5ae7f9 = Updates;
//
// How far the caller has read the stories of every peer, as one
// updateReadStories each: Android asks once after login and applies them
// as updates (StoriesController.java:195-211).
func (c *StoriesCore) StoriesGetAllReadPeerStories(in *mtproto.TLStoriesGetAllReadPeerStories) (*mtproto.Updates, error) {
	reads, err := c.svcCtx.Dao.Store.Reads(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("stories.getAllReadPeerStories - error: %v", err)
		return nil, err
	}
	updates := make([]*mtproto.Update, 0, len(reads))
	for peer, maxId := range reads {
		updates = append(updates, updateReadStories(peer, maxId))
	}
	return updatesOf(updates, nil, nil), nil
}

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

// StoriesGetAllStories
// stories.getAllStories#eeb0d625 flags:# next:flags.1?true hidden:flags.2?true state:flags.0?string = stories.AllStories;
//
// The row of stories above the chats (hidden: the one in the archive). The
// whole list is one page: with the hundred accounts of a server it stays
// short, so has_more is never set and a request for the next page gets an
// empty one. Android keeps has_more of the hidden list while it has no
// answer and then shows "1 Story" in the archive row
// (StoriesController.java:1739-1743: max(1, count) while has_more).
// The state the client sends back answers stories.allStoriesNotModified
// while nothing of the list changed (StoriesController.java:385-437).
func (c *StoriesCore) StoriesGetAllStories(in *mtproto.TLStoriesGetAllStories) (*mtproto.Stories_AllStories, error) {
	at := now()
	stealth, err := c.svcCtx.Dao.Store.Stealth(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("stories.getAllStories - error: %v", err)
		return nil, err
	}
	all, err := c.feed(at)
	if err != nil {
		c.Logger.Errorf("stories.getAllStories - error: %v", err)
		return nil, err
	}
	entries := make([]*feedEntry, 0, len(all))
	for _, e := range all {
		if e.hidden == in.GetHidden() {
			entries = append(entries, e)
		}
	}
	list, r, state, err := c.feedAnswer(entries)
	if err != nil {
		c.Logger.Errorf("stories.getAllStories - error: %v", err)
		return nil, err
	}
	if in.GetState() != nil && in.GetState().GetValue() == state && !in.GetNext() {
		return mtproto.MakeTLStoriesAllStoriesNotModified(&mtproto.Stories_AllStories{
			State:       state,
			StealthMode: stealthMode(stealth, at),
		}).To_Stories_AllStories(), nil
	}
	answer := &mtproto.Stories_AllStories{
		HasMore:     false,
		Count:       int32(len(list)),
		State:       state,
		PeerStories: []*mtproto.PeerStories{},
		Chats:       []*mtproto.Chat{},
		Users:       []*mtproto.User{},
		StealthMode: stealthMode(stealth, at),
	}
	if !in.GetNext() {
		answer.PeerStories = list
		if answer.Users, answer.Chats, err = c.resolve(r); err != nil {
			c.Logger.Errorf("stories.getAllStories - error: %v", err)
			return nil, err
		}
	}
	return mtproto.MakeTLStoriesAllStories(answer).To_Stories_AllStories(), nil
}

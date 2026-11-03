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
// Pluma has no stories yet: nobody can post one (stories.sendStory is not
// served), so the list of every contact's stories, hidden or not, is empty
// and complete. Without this answer Android keeps has_more of the hidden
// list and shows "1 Story" in the archive row
// (StoriesController.getTotalStoriesCount: max(1, count) while has_more).
func (c *StoriesCore) StoriesGetAllStories(in *mtproto.TLStoriesGetAllStories) (*mtproto.Stories_AllStories, error) {
	return mtproto.MakeTLStoriesAllStories(&mtproto.Stories_AllStories{
		HasMore:     false,
		Count:       0,
		State:       "",
		PeerStories: []*mtproto.PeerStories{},
		Chats:       []*mtproto.Chat{},
		Users:       []*mtproto.User{},
		StealthMode: mtproto.MakeTLStoriesStealthMode(&mtproto.StoriesStealthMode{}).To_StoriesStealthMode(),
	}).To_Stories_AllStories(), nil
}

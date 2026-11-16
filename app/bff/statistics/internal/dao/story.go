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

package dao

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// The stories of channels and supergroups (ADR 0054): a story is a row of
// stories while it is there (stories.deleteStories takes the row, its views
// and its shares away), and story_counters has a row of a peer from its
// first story on. Their views, shares and reactions are counted by
// StoryViewCounts, StoryShareCounts and StoryReactionCounts.

// StoryDate is the date of the story storyId of the channel; ok is false
// when there is no such story (never posted, or deleted).
func (d *Dao) StoryDate(ctx context.Context, channelId int64, storyId int32) (date int64, ok bool, err error) {
	var dates []int64
	if err = d.db.QueryRowsPartial(ctx, &dates,
		"SELECT s.`date` FROM stories s WHERE s.peer_type = ? AND s.peer_id = ? AND s.story_id = ?",
		mtproto.PEER_CHANNEL, channelId, storyId); err != nil {
		return 0, false, err
	}
	if len(dates) == 0 {
		return 0, false, nil
	}
	return dates[0], true, nil
}

// StoriesPosted tells whether the channel ever posted a story: story_counters
// keeps the last id of a peer from its first story on, also when every story
// is deleted.
func (d *Dao) StoriesPosted(ctx context.Context, channelId int64) (bool, error) {
	var ids []int32
	if err := d.db.QueryRowsPartial(ctx, &ids,
		"SELECT last_id FROM story_counters WHERE peer_type = ? AND peer_id = ?", mtproto.PEER_CHANNEL, channelId); err != nil {
		return false, err
	}
	return len(ids) > 0 && ids[0] > 0, nil
}

// Stories is the store of pkg/stories on the database of the statistics:
// the stories that repost a channel's story read as their viewers see
// them. Only its reads are used.
func (d *Dao) Stories() *stories.Store {
	return stories.NewStore(d.db)
}

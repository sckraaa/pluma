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
	"sort"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/reactions"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// StoriesGetStoryReactionsList
// stories.getStoryReactionsList#b9b2881f flags:# forwards_first:flags.2?true peer:InputPeer id:int reaction:flags.0?Reaction offset:flags.1?string limit:int = stories.StoryReactionsList;
//
// Who reacted to a story, newest first, with reaction only those who left
// that one: for the one who manages the peer, which for a channel is its
// admins with a right over stories (Android asks it for a channel's story
// where it asks stories.getStoryViewsList for a user's,
// SelfStoryViewsPage.java:1262-1314). The reposts and forwards are not
// listed, so forwards_first changes nothing.
func (c *StoriesCore) StoriesGetStoryReactionsList(in *mtproto.TLStoriesGetStoryReactionsList) (*mtproto.Stories_StoryReactionsList, error) {
	v, err := c.manager(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.getStoryReactionsList - error: %v", err)
		return nil, err
	}
	st, err := c.svcCtx.Dao.Store.One(c.ctx, v.peer, in.Id)
	if err != nil {
		c.Logger.Errorf("stories.getStoryReactionsList - error: %v", err)
		return nil, err
	}
	if st == nil {
		return nil, errStoryIdInvalid
	}
	offset, err := pageOffset(in.GetOffset().GetValue())
	if err != nil {
		return nil, err
	}
	var only string
	if in.Reaction != nil && in.Reaction.GetPredicateName() != mtproto.Predicate_reactionEmpty {
		if only, err = reactions.Key(in.Reaction); err != nil {
			return nil, err
		}
	}
	views, err := c.svcCtx.Dao.Store.Views(c.ctx, v.peer, in.Id)
	if err != nil {
		c.Logger.Errorf("stories.getStoryReactionsList - error: %v", err)
		return nil, err
	}
	kept := make([]stories.View, 0, len(views))
	for _, view := range views {
		if view.Reaction != "" && (only == "" || view.Reaction == only) {
			kept = append(kept, view)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].ReactionDate > kept[j].ReactionDate })

	limit := int(clampLimit(in.Limit, 20, 100))
	page, next := pageOf(len(kept), offset, limit)
	answer := &mtproto.Stories_StoryReactionsList{
		Count:     int32(len(kept)),
		Reactions: make([]*mtproto.StoryReaction, 0, limit),
	}
	r := &refs{}
	for _, view := range kept[page[0]:page[1]] {
		answer.Reactions = append(answer.Reactions, mtproto.MakeTLStoryReaction(&mtproto.StoryReaction{
			PeerId:   mtproto.MakePeerUser(view.ViewerId),
			Date:     view.ReactionDate,
			Reaction: reactions.FromKey(view.Reaction),
		}).To_StoryReaction())
		r.user(view.ViewerId)
	}
	if next != "" {
		answer.NextOffset = mtproto.MakeFlagsString(next)
	}
	if answer.Users, answer.Chats, err = c.resolve(r); err != nil {
		c.Logger.Errorf("stories.getStoryReactionsList - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLStoriesStoryReactionsList(answer).To_Stories_StoryReactionsList(), nil
}

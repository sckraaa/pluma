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

package stories_helper

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

// UserStories is what the other modules need of the stories: what userFull
// says of the stories of a user (the story plugin of the users module),
// whether a story may go into a message (that of the messages module), and
// the blocklist of stories (that of the contacts module).
type UserStories struct {
	svcCtx *svc.ServiceContext
}

// GetStoriesPinnedAvailable is userFull.stories_pinned_available: the
// profile of peerUserId has stories that toSelfUserId may open.
func (p *UserStories) GetStoriesPinnedAvailable(ctx context.Context, peerUserId, toSelfUserId int64) bool {
	ok, err := core.NewFor(ctx, p.svcCtx, toSelfUserId).PinnedAvailable(peerUserId)
	if err != nil {
		logx.WithContext(ctx).Errorf("stories: pinned stories of user %d: %v", peerUserId, err)
		return false
	}
	return ok
}

// GetBlockedMyStoriesFrom is userFull.blocked_my_stories_from: toSelfUserId
// hides its stories from peerUserId (the blocklist of stories).
func (p *UserStories) GetBlockedMyStoriesFrom(ctx context.Context, peerUserId, toSelfUserId int64) bool {
	hidden, err := core.NewFor(ctx, p.svcCtx, toSelfUserId).HidesStoriesFrom(toSelfUserId, peerUserId)
	if err != nil {
		logx.WithContext(ctx).Errorf("stories: blocklist of stories of user %d: %v", toSelfUserId, err)
		return false
	}
	return hidden
}

// SetStoriesBlocked is contacts.block (blocked) and contacts.unblock with
// my_stories_from for the user userId (the stories blocklist of the
// contacts module).
func (p *UserStories) SetStoriesBlocked(ctx context.Context, userId int64, peer *mtproto.InputPeer, blocked bool) error {
	return core.NewFor(ctx, p.svcCtx, userId).SetStoriesBlocked(peer, blocked)
}

// GetStoriesBlocked is contacts.getBlocked with my_stories_from.
func (p *UserStories) GetStoriesBlocked(ctx context.Context, userId int64, offset, limit int32) (*mtproto.Contacts_Blocked, error) {
	return core.NewFor(ctx, p.svcCtx, userId).StoriesBlocklist(offset, limit)
}

// ReplaceStoriesBlocked is contacts.setBlocked with my_stories_from.
func (p *UserStories) ReplaceStoriesBlocked(ctx context.Context, userId int64, peers []*mtproto.InputPeer, limit int32) error {
	return core.NewFor(ctx, p.svcCtx, userId).SetStoriesBlocklist(peers, limit)
}

// GetActiveStories is userFull.stories: the stories of peerUserId in the
// feeds that toSelfUserId may open, nil when there are none.
func (p *UserStories) GetActiveStories(ctx context.Context, peerUserId, toSelfUserId int64) *mtproto.PeerStories {
	ps, err := core.NewFor(ctx, p.svcCtx, toSelfUserId).ActiveStories(peerUserId)
	if err != nil {
		logx.WithContext(ctx).Errorf("stories: stories of user %d: %v", peerUserId, err)
		return nil
	}
	return ps
}

// CheckStory answers whether the user userId may put the story id of peer
// into a message (the story plugin of the messages module): it must be
// able to open the story, and to share it (share: the story is the media
// of the message) the story must allow that.
func (p *UserStories) CheckStory(ctx context.Context, userId int64, peer *mtproto.PeerUtil, id int32, share bool) error {
	return core.NewFor(ctx, p.svcCtx, userId).CheckStory(peer, id, share)
}

// StoryShared counts the message of userId with randomId that shared the
// story id of peer, once it is sent.
func (p *UserStories) StoryShared(ctx context.Context, userId int64, peer *mtproto.PeerUtil, id int32, randomId int64) error {
	return core.NewFor(ctx, p.svcCtx, userId).StoryShared(peer, id, randomId)
}

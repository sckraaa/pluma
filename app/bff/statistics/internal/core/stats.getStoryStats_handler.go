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
	"context"
	"fmt"
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"

	"google.golang.org/grpc/status"
)

// errStoryIdInvalid is the error of a story that is not there (never
// posted, or deleted), as bff stories gives it.
var errStoryIdInvalid = status.Error(mtproto.ErrBadRequest, "STORY_ID_INVALID")

// The graphs of a story that zoom: its views and shares, and its reactions,
// from its posting on.
func init() {
	for kind, spec := range map[string]graphSpec{
		"one_story_views":     {object: true, story: true, kind: anyChannel, build: storyInteractionsGraph},
		"one_story_reactions": {object: true, story: true, kind: anyChannel, build: storyReactionsGraph},
	} {
		if _, ok := graphs[kind]; ok {
			panic(fmt.Sprintf("stats: graph %q twice", kind))
		}
		graphs[kind] = spec
	}
}

// StatsGetStoryStats
// stats.getStoryStats#374fef40 flags:# dark:flags.0?true peer:InputPeer id:int = stats.StoryStats;
//
// The statistics of a story of a channel or a supergroup, for whoever sees
// the statistics of its peer: its views and shares (the messages that shared
// it and its reposts), and its reactions by emotion, from its posting on; in
// hours while it is 48 hours old or younger, as the graph of a post, in days
// after, zooming into the hours of a day. Android and tdesktop open it from
// the story of a channel when can_view_stats is set (PeerStoriesView.java:
// 1372, media_view_overlay_widget.cpp:2515-2525). A story of a user has no
// statistics: PEER_ID_INVALID (ADR 0066, p. 1); a peer that never posted a
// story STORIES_NEVER_CREATED and a story that is not there
// STORY_ID_INVALID.
func (c *StatisticsCore) StatsGetStoryStats(in *mtproto.TLStatsGetStoryStats) (_ *mtproto.Stats_StoryStats, err error) {
	defer c.observe("stats.getStoryStats", time.Now(), &err)
	if err = c.enabled(); err != nil {
		return nil, err
	}
	mc, err := c.storyChannel(in.GetPeer())
	if err != nil {
		return nil, err
	}
	ch := mc.GetChannel()
	t, err := c.storyTarget(c.ctx, ch.GetId(), ch.GetBroadcast(), int64(ch.GetParticipantsCount()), in.GetId())
	if err != nil {
		return nil, err
	}
	v := view{lang: c.MD.GetLangCode(), dark: in.GetDark()}
	return build(c, c.flightKey("story", t.channelId, int64(t.object), v), func(ctx context.Context) (*mtproto.Stats_StoryStats, error) {
		return c.storyStats(ctx, t, v)
	})
}

// storyChannel is the channel of a story's statistics named by peer, with
// the viewer's access checked as statsChannel does: a peer that is not a
// channel is PEER_ID_INVALID.
func (c *StatisticsCore) storyChannel(peer *mtproto.InputPeer) (*channelpb.MutableChannel, error) {
	if peer.GetPredicateName() != mtproto.Predicate_inputPeerChannel {
		return nil, mtproto.ErrPeerIdInvalid
	}
	return c.statsChannel(mtproto.MakeTLInputChannel(&mtproto.InputChannel{
		ChannelId:  peer.GetChannelId(),
		AccessHash: peer.GetAccessHash(),
	}).To_InputChannel(), anyChannel)
}

// storyTarget is the target of the story storyId of the channel, or
// STORIES_NEVER_CREATED for a channel that never posted one and
// STORY_ID_INVALID for one that is not there.
func (c *StatisticsCore) storyTarget(ctx context.Context, channelId int64, broadcast bool, members int64, storyId int32) (target, error) {
	if storyId <= 0 {
		return target{}, errStoryIdInvalid
	}
	date, ok, err := c.svcCtx.Dao.StoryDate(ctx, channelId, storyId)
	if err != nil {
		return target{}, err
	}
	if !ok {
		posted, err := c.svcCtx.Dao.StoriesPosted(ctx, channelId)
		if err != nil {
			return target{}, err
		}
		if !posted {
			return target{}, mtproto.ErrStoriesNeverCreated
		}
		return target{}, errStoryIdInvalid
	}
	return target{channelId: channelId, object: storyId, broadcast: broadcast, start: date, members: members, now: c.now}, nil
}

func (c *StatisticsCore) storyStats(ctx context.Context, t target, v view) (*mtproto.Stats_StoryStats, error) {
	out := &mtproto.Stats_StoryStats{}
	var err error
	if t.now-t.start <= hourlyAge {
		hours := statsgraph.Span(statsgraph.Hour, t.start, t.now)
		if out.ViewsGraph, err = c.hourly(ctx, "one_story_views", t, hours, v); err != nil {
			return nil, err
		}
		if out.ReactionsByEmotionGraph, err = c.hourly(ctx, "one_story_reactions", t, hours, v); err != nil {
			return nil, err
		}
	} else {
		if out.ViewsGraph, err = c.daily(ctx, "one_story_views", t, v); err != nil {
			return nil, err
		}
		if out.ReactionsByEmotionGraph, err = c.daily(ctx, "one_story_reactions", t, v); err != nil {
			return nil, err
		}
	}
	return mtproto.MakeTLStatsStoryStats(out).To_Stats_StoryStats(), nil
}
